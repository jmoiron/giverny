package kanban

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/coder/websocket"
)

// Client is a single connected WebSocket client.
type Client struct {
	board string
	send  chan []byte
	conn  *websocket.Conn
}

// writePump drains the send channel and writes outbound messages to the
// WebSocket connection. It returns when the channel is closed or the context
// is done.
func (c *Client) writePump(ctx context.Context) {
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// Hub manages all active WebSocket clients and routes published events to them.
// A single goroutine owns the client map, so no mutex is needed.
type Hub struct {
	register   chan *Client
	unregister chan *Client
	publish    chan Event
	clients    map[string]map[*Client]struct{} // board slug → clients
}

// NewHub allocates a Hub. Call Run() in a goroutine to start it.
func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client, 16),
		unregister: make(chan *Client, 16),
		publish:    make(chan Event, 256),
		clients:    make(map[string]map[*Client]struct{}),
	}
}

// Run is the hub's main loop. It must be called in a goroutine.
func (h *Hub) Run() {
	for {
		select {
		case c := <-h.register:
			if h.clients[c.board] == nil {
				h.clients[c.board] = make(map[*Client]struct{})
			}
			h.clients[c.board][c] = struct{}{}

		case c := <-h.unregister:
			if bucket := h.clients[c.board]; bucket != nil {
				delete(bucket, c)
				if len(bucket) == 0 {
					delete(h.clients, c.board)
				}
			}
			close(c.send)

		case evt := <-h.publish:
			data, err := json.Marshal(evt)
			if err != nil {
				slog.Error("hub marshal event", "err", err)
				continue
			}
			if evt.Board == BoardGlobal {
				// Global: deliver to every connected client.
				for _, bucket := range h.clients {
					h.fanOut(bucket, data)
				}
			} else {
				// Board-scoped: deliver only to clients watching this board.
				h.fanOut(h.clients[evt.Board], data)
			}
		}
	}
}

func (h *Hub) fanOut(bucket map[*Client]struct{}, data []byte) {
	for c := range bucket {
		select {
		case c.send <- data:
		default:
			// Slow client — drop rather than block the hub.
			slog.Warn("hub dropping event for slow client", "board", c.board)
		}
	}
}

// Register adds a client to the hub.
func (h *Hub) Register(c *Client) { h.register <- c }

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) { h.unregister <- c }

// Publish sends an event to all relevant clients. It is non-blocking; events
// are dropped with a warning if the publish channel is full.
func (h *Hub) Publish(e Event) {
	select {
	case h.publish <- e:
	default:
		slog.Warn("hub publish channel full, dropping event", "type", e.Type)
	}
}
