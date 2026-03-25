# Plan: WebSocket Live Update Integration

## Context

Per `plans/tech.md`, each page connects to a change feed WebSocket. Every backend mutation publishes an update payload to all connected clients watching the same board. Frontend UI updates are driven by these payloads rather than optimistic local state. This plan covers the server-side hub, event publishing hooks in the kanban handlers, and the client-side event dispatch loop.

Library: `github.com/coder/websocket` (nhooyr-style, context-aware, no gorilla dep).

---

## Architecture

### Event payload

All events share a common envelope:

```go
type Event struct {
    Type    string          `json:"type"`    // e.g. "card.created"
    Board   string          `json:"board"`   // board slug, or "" for global events
    Payload json.RawMessage `json:"payload"` // event-specific JSON
}
```

`Board == ""` means the event is **global** — delivered to every connected client regardless of which board they are watching. `Board != ""` means the event is **board-scoped** — delivered only to clients watching that board.

Event types (constants):
- `card.created`, `card.updated`, `card.moved`, `card.archived`
- `column.created`, `column.deleted`, `column.reordered`
- `board.updated`
- `label.created`, `label.updated`, `label.deleted` *(global — labels are shared across boards)*

### Hub

Channel-driven fan-out. A single Hub goroutine owns the client map to avoid locking:

```go
type Hub struct {
    register   chan *Client
    unregister chan *Client
    publish    chan Event
    clients    map[string]map[*Client]struct{} // board slug → clients
}

type Client struct {
    board string
    send  chan []byte       // buffered outbound channel
    conn  *websocket.Conn
}
```

`hub.Run()` loops on all three channels. `hub.Publish(event)` sends to the `publish` channel (non-blocking from handlers). Each client has a `writePump` goroutine that drains `send` and writes to the WebSocket.

**Fan-out routing in `Run()`:**
- If `event.Board == ""` → send to every client in every board bucket (global broadcast)
- If `event.Board != ""` → send only to clients in that board's bucket

### Integration with App

`kanban.App` gains a `hub *Hub` field. `NewApp` creates and starts the hub. After each successful mutation in a handler, the handler calls a small helper:

```go
a.hub.Publish(Event{Type: EventCardCreated, Board: slug, Payload: marshalCard(card)})
```

This keeps publishing as a one-liner at the call site — no changes to the service layer.

### WebSocket route

```
GET /boards/{slug}/ws   → a.handleWS   (board-scoped client)
```

`handleWS` upgrades with `websocket.Accept`, registers the client with its board slug, spawns the `writePump` goroutine, and blocks in a `readPump` that only handles pings/keepalive (clients don't send data up the pipe in phase 1).

Because every client automatically receives global events from the hub's fan-out logic, no separate global WebSocket endpoint is needed for the board page. If a future non-board page needs live updates (e.g. a labels admin page), a generic `GET /ws` route can be added that registers a client with `board: ""`, causing it to receive only global events.

---

## Files to Create / Modify

### New files

**`kanban/hub.go`**
- `Hub` struct and `Client` struct
- `NewHub() *Hub` — allocates channels and map, does NOT start goroutine
- `(h *Hub) Run()` — main select loop; called in a goroutine from `NewApp`
- `(h *Hub) Register(c *Client)` — sends to `h.register`
- `(h *Hub) Unregister(c *Client)` — sends to `h.unregister`
- `(h *Hub) Publish(e Event)` — non-blocking send to `h.publish` (drop on full channel rather than block handler)
- `(c *Client) writePump(ctx context.Context)` — drains `c.send`, writes text frames, closes on ctx done

**`kanban/events.go`**
- Event type constants
- `BoardGlobal = ""` sentinel constant (documents intent at call sites)
- `Event` struct
- Per-event payload structs (e.g. `CardCreatedPayload`, `CardMovedPayload`)
- `marshalEvent(typ, board string, payload any) Event` helper — pass `BoardGlobal` for global events

### Modified files

**`kanban/app.go`**
- Add `hub *Hub` field to `App`
- `NewApp`: call `NewHub()`, start `hub.Run()` in a goroutine
- Add route: `r.Get("/boards/{slug}/ws", a.handleWS)`
- `handleWS(w, r)`: accept upgrade, create Client, register, spawn writePump, readPump (drain only)
- After each successful mutation, add one `a.hub.Publish(...)` call:
  - `handleCreateCard` → `card.created` (board-scoped)
  - `handleUpdateCard` → `card.updated` (board-scoped)
  - `handleMoveCard` → `card.moved` (board-scoped)
  - `handleArchiveCard` → `card.archived` (board-scoped)
  - `handleCreateColumn` → `column.created` (board-scoped)
  - `handleDeleteColumn` → `column.deleted` (board-scoped)
  - `handleReorderColumns` → `column.reordered` (board-scoped)
  - `handleBoardEditSubmit` → `board.updated` (board-scoped)
  - Future label/tag handlers → `label.*` with `BoardGlobal` (global)

**`go.mod` / `go.sum`**
- `go get github.com/coder/websocket`

**`static/js/kanban.js`**
- After board slug is confirmed, open WebSocket:
  ```js
  var proto = location.protocol === 'https:' ? 'wss' : 'ws';
  var ws = new WebSocket(proto + '://' + location.host + '/boards/' + board + '/ws');
  ws.onmessage = function(e) { handleEvent(JSON.parse(e.data)); };
  ws.onclose = function() { /* optional: reconnect with backoff */ };
  ```
- `handleEvent(evt)` dispatches by `evt.type` to individual handlers:
  - `card.created` → append card snippet HTML to the target column (server sends rendered HTML in payload)
  - `card.updated` → update `.card-title` text in DOM for matching card id
  - `card.moved` → `location.reload()` (simplest for phase 1; can be made surgical later)
  - `card.archived` → remove card element from DOM
  - `column.created` / `column.deleted` / `column.reordered` → `location.reload()`
  - `board.updated` → update header text
  - `label.*` → reload any label UI present on the page (global events arrive on the same connection)

---

## Card snippet in event payload

For `card.created`, the payload includes the rendered HTML snippet (same as `card_snippet.html` partial) so the client can append it without a second HTTP round-trip. The handler already renders and returns this snippet for the AJAX path; we reuse the same rendered string in the event payload:

```go
type CardCreatedPayload struct {
    ColumnID int64  `json:"column_id"`
    HTML     string `json:"html"`   // rendered card_snippet.html
}
```

For other types, the payload carries just the minimal fields the JS needs (id, title, etc.).

---

## Reconnection

Add simple exponential-backoff reconnect in JS (capped at ~30s). On reconnect, do a `location.reload()` to resync any missed events. This is sufficient for phase 1.

---

## Verification

1. `go get github.com/coder/websocket && make` — clean build
2. Open board in two browser windows
3. Add a card in window A → card appears in window B without reload
4. Archive a card in window A → card disappears in window B
5. Open browser devtools Network tab → confirm `ws` connection stays open, messages appear on mutation
6. Close one tab → no panic/error in server log (unregister path works)
7. `go test ./...` — existing tests still pass
