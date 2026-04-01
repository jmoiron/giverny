package kanban

import "encoding/json"

// BoardGlobal is used as the Board field on events that should be delivered
// to all connected clients regardless of which board they are watching.
const BoardGlobal = ""

// Event type constants.
const (
	EventCardCreated             = "card.created"
	EventCardDeleted             = "card.deleted"
	EventCardUpdated             = "card.updated"
	EventCardMoved               = "card.move"
	EventCardReordered           = "card.reorder"
	EventCardArchived            = "card.archived"
	EventCardColorChanged        = "card.color.changed"
	EventCardDateUpdated         = "card.date.updated"
	EventCardTitleModified       = "card.title.modified"
	EventCardDescriptionModified = "card.description.modified"
	EventCardChecklistUpdated    = "card.checklist.updated"
	EventCardAttachmentsUpdated  = "card.attachments.updated"
	EventCardLabelAdded          = "card.label.added"
	EventCardLabelRemoved        = "card.label.removed"
	EventLabelColorChanged       = "label.color.changed"
	EventColumnCreated           = "column.created"
	EventColumnChanged           = "column.changed"
	EventColumnDeleted           = "column.deleted"
	EventColumnReordered         = "column.reorder"
	EventBoardUpdated            = "board.updated"
	EventCardCommentAdded        = "card.comment.added"
	EventCardCommentEdited       = "card.comment.edited"
	EventCardCommentDeleted      = "card.comment.deleted"
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

type LabelColorChangedPayload struct {
	LabelID   int64  `json:"label_id"`
	Color     string `json:"color"`
	TextClass string `json:"text_class"`
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

type CardColorChangedPayload struct {
	CardID int64  `json:"card_id"`
	Color  string `json:"color"`
}

type CardDateUpdatedPayload struct {
	CardID           int64  `json:"card_id"`
	StartDateValue   string `json:"start_date_value"`
	DueDateValue     string `json:"due_date_value"`
	UpdatedAtValue   string `json:"updated_at_value"`
	UpdatedAtDisplay string `json:"updated_at_display"`
}

type EventAttachmentPayload struct {
	ID        int64  `json:"id"`
	Filename  string `json:"filename"`
	Filepath  string `json:"filepath"`
	MimeType  string `json:"mime_type"`
	IconClass string `json:"icon_class"`
}

type CardAttachmentsUpdatedPayload struct {
	CardID           int64                    `json:"card_id"`
	Attachments      []EventAttachmentPayload `json:"attachments"`
	UpdatedAtValue   string                   `json:"updated_at_value"`
	UpdatedAtDisplay string                   `json:"updated_at_display"`
}

type ChecklistItemPayload struct {
	ID       int64  `json:"id"`
	Text     string `json:"text"`
	Done     bool   `json:"done"`
	Position int    `json:"position"`
}

type CardChecklistUpdatedPayload struct {
	CardID          int64                  `json:"card_id"`
	ChecklistID     int64                  `json:"checklist_id"`
	Exists          bool                   `json:"exists"`
	Items           []ChecklistItemPayload `json:"items"`
	CompletedCount  int                    `json:"completed_count"`
	TotalCount      int                    `json:"total_count"`
	PercentComplete int                    `json:"percent_complete"`
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

type CardDeletedPayload struct {
	CardID int64 `json:"card_id"`
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

type CardCommentPayload struct {
	CommentID        int64  `json:"comment_id"`
	CardID           int64  `json:"card_id"`
	AuthorID         int64  `json:"author_id"`
	AuthorUsername   string `json:"author_username"`
	AuthorImage      string `json:"author_image"`
	Body             string `json:"body"`
	BodyRendered     string `json:"body_rendered"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	CreatedAtDisplay string `json:"created_at_display"`
}

type CardCommentDeletedPayload struct {
	CommentID int64 `json:"comment_id"`
	CardID    int64 `json:"card_id"`
}
