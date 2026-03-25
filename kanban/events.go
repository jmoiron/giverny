package kanban

import "encoding/json"

// BoardGlobal is used as the Board field on events that should be delivered
// to all connected clients regardless of which board they are watching.
const BoardGlobal = ""

// Event type constants.
const (
	EventCardCreated             = "card.created"
	EventCardUpdated             = "card.updated"
	EventCardMoved               = "card.move"
	EventCardReordered           = "card.reorder"
	EventCardArchived            = "card.archived"
	EventCardTitleModified       = "card.title.modified"
	EventCardDescriptionModified = "card.description.modified"
	EventCardLabelAdded          = "card.label.added"
	EventCardLabelRemoved        = "card.label.removed"
	EventColumnCreated           = "column.created"
	EventColumnChanged           = "column.changed"
	EventColumnDeleted           = "column.deleted"
	EventColumnReordered         = "column.reorder"
	EventBoardUpdated            = "board.updated"
)

// Event is the envelope sent over every WebSocket connection.
// Board is the routing key: empty means global (all clients), non-empty
// means only clients watching that board slug receive the event.
type Event struct {
	Type    string          `json:"type"`
	Board   string          `json:"board,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type EventLabelPayload struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Color       string `json:"color"`
	TextClass   string `json:"text_class"`
	Description string `json:"description"`
}

type CardLabelAddedPayload struct {
	CardID int64             `json:"card_id"`
	Label  EventLabelPayload `json:"label"`
}

type CardLabelRemovedPayload struct {
	CardID  int64 `json:"card_id"`
	LabelID int64 `json:"label_id"`
}

type CardTitleModifiedPayload struct {
	CardID int64  `json:"card_id"`
	Title  string `json:"title"`
}

type CardDescriptionModifiedPayload struct {
	CardID          int64  `json:"card_id"`
	Content         string `json:"content"`
	ContentRendered string `json:"content_rendered"`
}

type CardReorderedPayload struct {
	ColumnID int64   `json:"column_id"`
	CardIDs  []int64 `json:"card_ids"`
}

type CardMovedPayload struct {
	CardID       int64   `json:"card_id"`
	FromColumnID int64   `json:"from_column_id"`
	ToColumnID   int64   `json:"to_column_id"`
	FromCardIDs  []int64 `json:"from_card_ids"`
	ToCardIDs    []int64 `json:"to_card_ids"`
}

type CardCreatedPayload struct {
	CardID   int64  `json:"card_id"`
	ColumnID int64  `json:"column_id"`
	HTML     string `json:"html"`
}

type ColumnReorderedPayload struct {
	ColumnIDs []int64 `json:"column_ids"`
}

type EventColumnPayload struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	WIPLimit int    `json:"wip_limit"`
	Color    string `json:"color"`
	Done     bool   `json:"done"`
	Late     bool   `json:"late"`
}

type ColumnChangedPayload struct {
	Columns []EventColumnPayload `json:"columns"`
}
