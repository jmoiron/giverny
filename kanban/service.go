package kanban

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

type ColumnWithCards struct {
	*Column
	Cards []*Card
}

type DashboardCard struct {
	Card
	BoardName string `db:"board_name"`
	BoardSlug string `db:"board_slug"`
}

type BoardService struct{ db db.DB }
type ColumnService struct{ db db.DB }
type CardService struct{ db db.DB }
type LabelService struct{ db db.DB }

func NewBoardService(dbh db.DB) *BoardService   { return &BoardService{db: dbh} }
func NewColumnService(dbh db.DB) *ColumnService { return &ColumnService{db: dbh} }
func NewCardService(dbh db.DB) *CardService     { return &CardService{db: dbh} }
func NewLabelService(dbh db.DB) *LabelService   { return &LabelService{db: dbh} }

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

func normalizeLabelTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func canonicalLabelTitle(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func sanitizeLabelColor(s string) string {
	s = strings.TrimSpace(s)
	if len(s) == 7 && strings.HasPrefix(s, "#") {
		return s
	}
	return "#888888"
}

func sanitizeOptionalColor(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 7 && strings.HasPrefix(s, "#") {
		return s
	}
	return ""
}

func labelTextClass(color string) string {
	color = strings.TrimSpace(strings.TrimPrefix(color, "#"))
	if len(color) != 6 {
		return "fg-dark"
	}
	r, err := strconv.ParseInt(color[0:2], 16, 64)
	if err != nil {
		return "fg-dark"
	}
	g, err := strconv.ParseInt(color[2:4], 16, 64)
	if err != nil {
		return "fg-dark"
	}
	b, err := strconv.ParseInt(color[4:6], 16, 64)
	if err != nil {
		return "fg-dark"
	}
	luminance := 0.2126*channelLuminance(float64(r)/255.0) +
		0.7152*channelLuminance(float64(g)/255.0) +
		0.0722*channelLuminance(float64(b)/255.0)
	if luminance > 0.58 {
		return "fg-dark"
	}
	return "fg-light"
}

func channelLuminance(v float64) float64 {
	if v <= 0.03928 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func finalizeLabel(label *Label) {
	if label == nil {
		return
	}
	label.TextClass = labelTextClass(label.Color)
}

func finalizeLabels(labels []*Label) {
	for _, label := range labels {
		finalizeLabel(label)
	}
}

func attachmentIconClass(mimeType string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return "fa-solid fa-image"
	}
	return "fa-solid fa-file"
}

// BoardService methods

func (s *BoardService) Create(name, slug, description, visibility string, createdBy int64) (*Board, error) {
	if slug == "" {
		slug = slugify(name)
	}
	var b Board
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO board (name, slug, description, visibility, created_by) VALUES (?, ?, ?, ?, ?)`,
			name, slug, description, visibility, createdBy,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		return tx.Get(&b, `SELECT * FROM board WHERE id=?`, id)
	})
	return &b, err
}

func (s *BoardService) GetBySlug(slug string) (*Board, error) {
	var b Board
	err := s.db.Get(&b, `SELECT * FROM board WHERE slug=?`, slug)
	return &b, err
}

func (s *BoardService) List(isAdmin bool) ([]*Board, error) {
	var boards []*Board
	var err error
	if isAdmin {
		err = s.db.Select(&boards, `SELECT * FROM board ORDER BY name`)
	} else {
		err = s.db.Select(&boards, `SELECT * FROM board WHERE visibility IN ('open','public') ORDER BY name`)
	}
	return boards, err
}

func (s *BoardService) ListPublic() ([]*Board, error) {
	var boards []*Board
	err := s.db.Select(&boards, `SELECT * FROM board WHERE visibility='public' ORDER BY name`)
	return boards, err
}

func (s *BoardService) RecentByCardActivity(limit int, isAdmin bool) ([]*Board, error) {
	var boards []*Board
	query := `
		SELECT b.*
		FROM board b
		JOIN (
			SELECT board_id, MAX(created_at) AS last_card_at
			FROM card
			WHERE archived_at IS NULL
			GROUP BY board_id
		) rc ON rc.board_id = b.id
	`
	args := []any{}
	if !isAdmin {
		query += ` WHERE b.visibility IN ('open','public')`
	}
	query += ` ORDER BY rc.last_card_at DESC, b.name LIMIT ?`
	args = append(args, limit)
	err := s.db.Select(&boards, query, args...)
	return boards, err
}

func (s *BoardService) Update(id int64, name, slug, description, visibility string) error {
	_, err := s.db.Exec(
		`UPDATE board SET name=?, slug=?, description=?, visibility=?, updated_at=datetime('now') WHERE id=?`,
		name, slug, description, visibility, id,
	)
	return err
}

func (s *BoardService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM board WHERE id=?`, id)
	return err
}

var ErrDoneColumnRequired = errors.New("board requires a done column")

// ColumnService methods

func (s *ColumnService) Create(boardID int64, name, color string, done, late bool) (*Column, error) {
	var col Column
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		if done {
			if _, err := tx.Exec(`UPDATE board_column SET done=0 WHERE board_id=?`, boardID); err != nil {
				return err
			}
		}
		if late {
			if _, err := tx.Exec(`UPDATE board_column SET late=0 WHERE board_id=?`, boardID); err != nil {
				return err
			}
		}
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM board_column WHERE board_id=?`, boardID)
		res, err := tx.Exec(
			`INSERT INTO board_column (board_id, name, position, color, done, late) VALUES (?, ?, ?, ?, ?, ?)`,
			boardID, name, maxPos+1, strings.TrimSpace(color), done, late,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		return tx.Get(&col, `SELECT * FROM board_column WHERE id=?`, id)
	})
	return &col, err
}

func (s *ColumnService) Get(id int64) (*Column, error) {
	var col Column
	err := s.db.Get(&col, `SELECT * FROM board_column WHERE id=?`, id)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

func (s *ColumnService) ListByBoard(boardID int64) ([]*Column, error) {
	var cols []*Column
	err := s.db.Select(&cols, `SELECT * FROM board_column WHERE board_id=? ORDER BY position, id`, boardID)
	return cols, err
}

func (s *ColumnService) Update(id int64, name string, wipLimit int, color string, done, late bool) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		var current Column
		if err := tx.Get(&current, `SELECT * FROM board_column WHERE id=?`, id); err != nil {
			return err
		}
		if !done {
			var otherDoneCount int
			if err := tx.Get(&otherDoneCount, `SELECT COUNT(*) FROM board_column WHERE board_id=? AND done=1 AND id<>?`, current.BoardID, id); err != nil {
				return err
			}
			if current.Done && otherDoneCount == 0 {
				return ErrDoneColumnRequired
			}
		}
		if done {
			if _, err := tx.Exec(`UPDATE board_column SET done=0 WHERE board_id=? AND id<>?`, current.BoardID, id); err != nil {
				return err
			}
		}
		if late {
			if _, err := tx.Exec(`UPDATE board_column SET late=0 WHERE board_id=? AND id<>?`, current.BoardID, id); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`UPDATE board_column SET name=?, wip_limit=?, color=?, done=?, late=? WHERE id=?`,
			name, wipLimit, strings.TrimSpace(color), done, late, id)
		return err
	})
}

func (s *ColumnService) Delete(id int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		var current Column
		if err := tx.Get(&current, `SELECT * FROM board_column WHERE id=?`, id); err != nil {
			return err
		}
		if current.Done {
			return ErrDoneColumnRequired
		}
		_, err := tx.Exec(`DELETE FROM board_column WHERE id=?`, id)
		return err
	})
}

func (s *ColumnService) Reorder(boardID int64, ids []int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(`UPDATE board_column SET position=? WHERE id=? AND board_id=?`, i, id, boardID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *ColumnService) IDsByBoard(boardID int64) ([]int64, error) {
	var ids []int64
	err := s.db.Select(&ids, `SELECT id FROM board_column WHERE board_id=? ORDER BY position, id`, boardID)
	return ids, err
}

func (s *ColumnService) DoneByBoard(boardID int64) (*Column, error) {
	var col Column
	err := s.db.Get(&col, `SELECT * FROM board_column WHERE board_id=? AND done=1 ORDER BY id LIMIT 1`, boardID)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

func (s *ColumnService) LateByBoard(boardID int64) (*Column, error) {
	var col Column
	err := s.db.Get(&col, `SELECT * FROM board_column WHERE board_id=? AND late=1 ORDER BY id LIMIT 1`, boardID)
	if err != nil {
		return nil, err
	}
	return &col, nil
}

// CardService methods

func (s *CardService) Create(columnID, boardID, createdBy int64, title, content, color string) (*Card, error) {
	rendered := mtr.RenderMarkdown(content)
	var card Card
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM card WHERE column_id=?`, columnID)
		res, err := tx.Exec(
			`INSERT INTO card (column_id, board_id, title, content, content_rendered, position, color, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			columnID, boardID, title, content, rendered, maxPos+1, strings.TrimSpace(color), createdBy,
		)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		return tx.Get(&card, `SELECT * FROM card WHERE id=?`, id)
	})
	return &card, err
}

func (s *CardService) Get(id int64) (*Card, error) {
	var card Card
	err := s.db.Get(&card, `SELECT * FROM card WHERE id=?`, id)
	if err != nil {
		return &card, err
	}
	card.Labels, err = s.labelsForCard(id)
	if err != nil {
		return &card, err
	}
	card.Assignees, err = s.assigneesForCard(id)
	if err != nil {
		return &card, err
	}
	card.Checklist, err = s.checklistForCard(id)
	if err != nil {
		return &card, err
	}
	card.Attachments, err = s.attachmentsForCard(id)
	if err != nil {
		return &card, err
	}
	if card.Checklist != nil {
		card.ChecklistTotal = len(card.Checklist.Items)
		for _, item := range card.Checklist.Items {
			if item != nil && item.Done {
				card.ChecklistDone++
			}
		}
		if card.ChecklistTotal > 0 {
			card.ChecklistPct = int(float64(card.ChecklistDone) / float64(card.ChecklistTotal) * 100)
		}
	}
	return &card, err
}

func (s *CardService) ListByBoard(boardID int64) ([]*Card, error) {
	var cards []*Card
	err := s.db.Select(&cards,
		`SELECT * FROM card WHERE board_id=? AND archived_at IS NULL ORDER BY column_id, position, id`,
		boardID,
	)
	if err != nil {
		return nil, err
	}
	if err := s.attachLabelsByBoard(cards, boardID); err != nil {
		return nil, err
	}
	if err := s.attachAssignees(cards); err != nil {
		return nil, err
	}
	if err := s.attachChecklistSummary(cards); err != nil {
		return nil, err
	}
	return cards, err
}

func (s *CardService) ListArchivedByBoard(boardID int64) ([]*Card, error) {
	var cards []*Card
	err := s.db.Select(&cards,
		`SELECT * FROM card WHERE board_id=? AND archived_at IS NOT NULL ORDER BY archived_at DESC, id DESC`,
		boardID,
	)
	if err != nil {
		return nil, err
	}
	if err := s.attachLabelsByBoard(cards, boardID); err != nil {
		return nil, err
	}
	if err := s.attachAssignees(cards); err != nil {
		return nil, err
	}
	if err := s.attachChecklistSummary(cards); err != nil {
		return nil, err
	}
	return cards, nil
}

func (s *CardService) Recent(limit int, isAdmin bool) ([]*DashboardCard, error) {
	var cards []*DashboardCard
	query := `
		SELECT c.*, b.name AS board_name, b.slug AS board_slug
		FROM card c
		JOIN board b ON b.id = c.board_id
		WHERE c.archived_at IS NULL
	`
	args := []any{}
	if !isAdmin {
		query += ` AND b.visibility IN ('open','public')`
	}
	query += ` ORDER BY c.created_at DESC, c.id DESC LIMIT ?`
	args = append(args, limit)
	if err := s.db.Select(&cards, query, args...); err != nil {
		return nil, err
	}
	cardPtrs := make([]*Card, 0, len(cards))
	for _, card := range cards {
		cardPtrs = append(cardPtrs, &card.Card)
	}
	if err := s.attachLabels(cardPtrs); err != nil {
		return nil, err
	}
	if err := s.attachAssignees(cardPtrs); err != nil {
		return nil, err
	}
	if err := s.attachChecklistSummary(cardPtrs); err != nil {
		return nil, err
	}
	return cards, nil
}

func (s *CardService) Update(id int64, title, content, color string, labelIDs []int64) error {
	rendered := mtr.RenderMarkdown(content)
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(
			`UPDATE card SET title=?, content=?, content_rendered=?, color=?, updated_at=datetime('now') WHERE id=?`,
			title, content, rendered, sanitizeOptionalColor(color), id,
		); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM card_label WHERE card_id=?`, id); err != nil {
			return err
		}
		seen := make(map[int64]struct{}, len(labelIDs))
		for _, labelID := range labelIDs {
			if labelID == 0 {
				continue
			}
			if _, ok := seen[labelID]; ok {
				continue
			}
			seen[labelID] = struct{}{}
			if _, err := tx.Exec(`INSERT INTO card_label (card_id, label_id) VALUES (?, ?)`, id, labelID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *CardService) AddLabel(cardID, labelID int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO card_label (card_id, label_id) VALUES (?, ?)`, cardID, labelID); err != nil {
			return err
		}
		var labelCount int
		if err := tx.Get(&labelCount, `SELECT COUNT(*) FROM card_label WHERE card_id=?`, cardID); err != nil {
			return err
		}
		if labelCount == 1 {
			var cardColor string
			if err := tx.Get(&cardColor, `SELECT color FROM card WHERE id=?`, cardID); err != nil {
				return err
			}
			if strings.TrimSpace(cardColor) == "" {
				var labelColor string
				if err := tx.Get(&labelColor, `SELECT color FROM label WHERE id=?`, labelID); err != nil {
					return err
				}
				if _, err := tx.Exec(`UPDATE card SET color=?, updated_at=datetime('now') WHERE id=?`, sanitizeLabelColor(labelColor), cardID); err != nil {
					return err
				}
				return nil
			}
		}
		_, err := tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
}

func (s *CardService) RemoveLabel(cardID, labelID int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`DELETE FROM card_label WHERE card_id=? AND label_id=?`, cardID, labelID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
}

func (s *CardService) Move(id, columnID int64, position int) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		var current Card
		if err := tx.Get(&current, `SELECT * FROM card WHERE id=?`, id); err != nil {
			return err
		}

		if current.ColumnID == columnID {
			ids, err := orderedCardIDsTx(tx, columnID, id)
			if err != nil {
				return err
			}
			if position < 0 {
				position = 0
			}
			if position > len(ids) {
				position = len(ids)
			}
			ids = insertIDAt(ids, position, id)
			if err := updateCardPositionsTx(tx, columnID, ids); err != nil {
				return err
			}
			_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, id)
			return err
		}

		sourceIDs, err := orderedCardIDsTx(tx, current.ColumnID, id)
		if err != nil {
			return err
		}
		destIDs, err := orderedCardIDsTx(tx, columnID, id)
		if err != nil {
			return err
		}
		if position < 0 {
			position = 0
		}
		if position > len(destIDs) {
			position = len(destIDs)
		}
		destIDs = insertIDAt(destIDs, position, id)

		if err := updateCardPositionsTx(tx, current.ColumnID, sourceIDs); err != nil {
			return err
		}
		for i, cardID := range destIDs {
			if _, err := tx.Exec(`UPDATE card SET column_id=?, position=? WHERE id=?`, columnID, i, cardID); err != nil {
				return fmt.Errorf("move card %d into column %d: %w", cardID, columnID, err)
			}
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, id)
		return err
	})
}

func (s *CardService) Archive(id int64) error {
	_, err := s.db.Exec(`UPDATE card SET archived_at=datetime('now'), updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *CardService) SetColor(id int64, color string) error {
	_, err := s.db.Exec(`UPDATE card SET color=?, updated_at=datetime('now') WHERE id=?`, sanitizeOptionalColor(color), id)
	return err
}

func (s *CardService) AddAssignee(id, assigneeID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO card_assignee (card_id, user_id) VALUES (?, ?)`, id, assigneeID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *CardService) RemoveAssignee(id, assigneeID int64) error {
	_, err := s.db.Exec(`DELETE FROM card_assignee WHERE card_id=? AND user_id=?`, id, assigneeID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *CardService) SetStartDate(id int64, startDate *time.Time) error {
	_, err := s.db.Exec(`UPDATE card SET start_date=?, updated_at=datetime('now') WHERE id=?`, startDate, id)
	return err
}

func (s *CardService) SetDueDate(id int64, dueDate *time.Time) error {
	_, err := s.db.Exec(`UPDATE card SET due_date=?, updated_at=datetime('now') WHERE id=?`, dueDate, id)
	return err
}

func (s *CardService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM card WHERE id=?`, id)
	return err
}

func (s *CardService) DeleteArchivedByBoard(boardID int64) error {
	_, err := s.db.Exec(`DELETE FROM card WHERE board_id=? AND archived_at IS NOT NULL`, boardID)
	return err
}

func (s *CardService) Unarchive(id int64) (*Card, error) {
	var card Card
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		if err := tx.Get(&card, `SELECT * FROM card WHERE id=?`, id); err != nil {
			return err
		}
		var firstColumnID int64
		if err := tx.Get(&firstColumnID, `SELECT id FROM board_column WHERE board_id=? ORDER BY position, id LIMIT 1`, card.BoardID); err != nil {
			return err
		}
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM card WHERE column_id=? AND archived_at IS NULL`, firstColumnID)
		if _, err := tx.Exec(
			`UPDATE card SET archived_at=NULL, column_id=?, position=?, updated_at=datetime('now') WHERE id=?`,
			firstColumnID, maxPos+1, id,
		); err != nil {
			return err
		}
		return tx.Get(&card, `SELECT * FROM card WHERE id=?`, id)
	})
	if err != nil {
		return nil, err
	}
	card.Labels, err = s.labelsForCard(id)
	if err != nil {
		return nil, err
	}
	return &card, nil
}

func (s *CardService) Reorder(columnID int64, ids []int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		return updateCardPositionsTx(tx, columnID, ids)
	})
}

func (s *CardService) IDsByColumn(columnID int64) ([]int64, error) {
	var ids []int64
	err := s.db.Select(&ids, `SELECT id FROM card WHERE column_id=? AND archived_at IS NULL ORDER BY position, id`, columnID)
	return ids, err
}

func (s *CardService) MarkDone(cardID int64) error {
	card, err := s.Get(cardID)
	if err != nil {
		return err
	}
	doneCol, err := (&ColumnService{db: s.db}).DoneByBoard(card.BoardID)
	if err != nil {
		return err
	}
	if card.ColumnID == doneCol.ID {
		return nil
	}
	return s.Move(cardID, doneCol.ID, math.MaxInt32)
}

func (s *CardService) IsSubscribed(cardID, userID int64) (bool, error) {
	var count int
	if err := s.db.Get(&count, `SELECT COUNT(*) FROM card_subscription WHERE card_id=? AND user_id=?`, cardID, userID); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *CardService) Subscribe(cardID, userID int64) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO card_subscription (card_id, user_id) VALUES (?, ?)`, cardID, userID)
	return err
}

func (s *CardService) Unsubscribe(cardID, userID int64) error {
	_, err := s.db.Exec(`DELETE FROM card_subscription WHERE card_id=? AND user_id=?`, cardID, userID)
	return err
}

func (s *CardService) RecordSubscriptionMessage(cardID int64, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	_, err := s.db.Exec(`
		INSERT INTO subscription_message (card_id, user_id, message)
		SELECT ?, user_id, ?
		FROM card_subscription
		WHERE card_id=?
	`, cardID, message, cardID)
	return err
}

func (s *CardService) AddChecklistItem(cardID int64, text string) (*Checklist, error) {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if text == "" {
		return nil, errors.New("checklist item text is required")
	}
	var checklistID int64
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		id, err := ensureChecklistTx(tx, cardID)
		if err != nil {
			return err
		}
		checklistID = id
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), -1) FROM checklist_item WHERE checklist_id=?`, checklistID)
		if _, err := tx.Exec(
			`INSERT INTO checklist_item (checklist_id, text, done, position) VALUES (?, ?, 0, ?)`,
			checklistID, text, maxPos+1,
		); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistForCard(cardID)
}

func (s *CardService) SetChecklistItemDone(cardID, itemID int64, done bool) (*Checklist, error) {
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`
			UPDATE checklist_item
			SET done=?
			WHERE id=? AND checklist_id IN (SELECT id FROM checklist WHERE card_id=?)
		`, done, itemID, cardID)
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			return sql.ErrNoRows
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistForCard(cardID)
}

func (s *CardService) ReorderChecklistItems(cardID int64, ids []int64) (*Checklist, error) {
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		var checklistID int64
		if err := tx.Get(&checklistID, `SELECT id FROM checklist WHERE card_id=?`, cardID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.Exec(`UPDATE checklist_item SET position=? WHERE id=? AND checklist_id=?`, i, id, checklistID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistForCard(cardID)
}

func (s *CardService) DeleteChecklistItem(cardID, itemID int64) (*Checklist, error) {
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`
			DELETE FROM checklist_item
			WHERE id=? AND checklist_id IN (SELECT id FROM checklist WHERE card_id=?)
		`, itemID, cardID)
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			return sql.ErrNoRows
		}
		var checklistID int64
		if err := tx.Get(&checklistID, `SELECT id FROM checklist WHERE card_id=?`, cardID); err != nil {
			return err
		}
		var ids []int64
		if err := tx.Select(&ids, `SELECT id FROM checklist_item WHERE checklist_id=? ORDER BY position, id`, checklistID); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.Exec(`UPDATE checklist_item SET position=? WHERE id=? AND checklist_id=?`, i, id, checklistID); err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			if _, err := tx.Exec(`DELETE FROM checklist WHERE id=?`, checklistID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.checklistForCard(cardID)
}

func (s *CardService) DeleteChecklist(cardID int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`DELETE FROM checklist WHERE card_id=?`, cardID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
}

func (s *CardService) AddAttachment(cardID, uploadedBy int64, filename, filepath, mimeType string, size int64) (*Attachment, error) {
	var attachment Attachment
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`
			INSERT INTO attachment (card_id, uploaded_by, filename, filepath, mime_type, size)
			VALUES (?, ?, ?, ?, ?, ?)
		`, cardID, uploadedBy, filename, filepath, strings.TrimSpace(mimeType), size)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID); err != nil {
			return err
		}
		return tx.Get(&attachment, `SELECT * FROM attachment WHERE id=?`, id)
	})
	if err != nil {
		return nil, err
	}
	attachment.IconClass = attachmentIconClass(attachment.MimeType)
	return &attachment, nil
}

func (s *CardService) Attachment(cardID, attachmentID int64) (*Attachment, error) {
	var attachment Attachment
	if err := s.db.Get(&attachment, `SELECT * FROM attachment WHERE id=? AND card_id=?`, attachmentID, cardID); err != nil {
		return nil, err
	}
	attachment.IconClass = attachmentIconClass(attachment.MimeType)
	return &attachment, nil
}

func (s *CardService) DeleteAttachment(cardID, attachmentID int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`DELETE FROM attachment WHERE id=? AND card_id=?`, attachmentID, cardID)
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			return sql.ErrNoRows
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
}

func (s *CardService) RenameAttachment(cardID, attachmentID int64, filename string) error {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return errors.New("filename is required")
	}
	return db.With(s.db, func(tx *sqlx.Tx) error {
		res, err := tx.Exec(`UPDATE attachment SET filename=? WHERE id=? AND card_id=?`, filename, attachmentID, cardID)
		if err != nil {
			return err
		}
		rows, _ := res.RowsAffected()
		if rows == 0 {
			return sql.ErrNoRows
		}
		_, err = tx.Exec(`UPDATE card SET updated_at=datetime('now') WHERE id=?`, cardID)
		return err
	})
}

func (s *CardService) MoveOverdueToLate(boardID int64, now time.Time) (int64, error) {
	var moved int64
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		var lateColumnID int64
		if err := tx.Get(&lateColumnID, `SELECT id FROM board_column WHERE board_id=? AND late=1 ORDER BY id LIMIT 1`, boardID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		res, err := tx.Exec(`
			UPDATE card
			SET column_id=?, updated_at=datetime('now')
			WHERE board_id=? AND archived_at IS NULL AND due_date IS NOT NULL AND due_date<? AND column_id<>?
		`, lateColumnID, boardID, now.UTC(), lateColumnID)
		if err != nil {
			return err
		}
		moved, _ = res.RowsAffected()
		return nil
	})
	return moved, err
}

func ensureChecklistTx(tx *sqlx.Tx, cardID int64) (int64, error) {
	var checklistID int64
	err := tx.Get(&checklistID, `SELECT id FROM checklist WHERE card_id=?`, cardID)
	if err == nil {
		return checklistID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := tx.Exec(`INSERT INTO checklist (card_id, title, position) VALUES (?, '', 0)`, cardID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *CardService) checklistForCard(cardID int64) (*Checklist, error) {
	var checklist Checklist
	if err := s.db.Get(&checklist, `SELECT * FROM checklist WHERE card_id=?`, cardID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := s.db.Select(&checklist.Items, `
		SELECT id, checklist_id, text, done, position
		FROM checklist_item
		WHERE checklist_id=?
		ORDER BY position, id
	`, checklist.ID); err != nil {
		return nil, err
	}
	return &checklist, nil
}

func (s *CardService) attachmentsForCard(cardID int64) ([]*Attachment, error) {
	var attachments []*Attachment
	if err := s.db.Select(&attachments, `
		SELECT id, card_id, uploaded_by, filename, filepath, mime_type, size, created_at
		FROM attachment
		WHERE card_id=?
		ORDER BY created_at, id
	`, cardID); err != nil {
		return nil, err
	}
	for _, attachment := range attachments {
		attachment.IconClass = attachmentIconClass(attachment.MimeType)
	}
	return attachments, nil
}

func orderedCardIDsTx(tx *sqlx.Tx, columnID, excludeID int64) ([]int64, error) {
	var ids []int64
	query := `SELECT id FROM card WHERE column_id=? AND archived_at IS NULL`
	args := []any{columnID}
	if excludeID != 0 {
		query += ` AND id<>?`
		args = append(args, excludeID)
	}
	query += ` ORDER BY position, id`
	if err := tx.Select(&ids, query, args...); err != nil {
		return nil, err
	}
	return ids, nil
}

func updateCardPositionsTx(tx *sqlx.Tx, columnID int64, ids []int64) error {
	for i, id := range ids {
		if _, err := tx.Exec(`UPDATE card SET position=? WHERE id=? AND column_id=?`, i, id, columnID); err != nil {
			return fmt.Errorf("reorder card %d: %w", id, err)
		}
	}
	return nil
}

func insertIDAt(ids []int64, pos int, id int64) []int64 {
	if pos < 0 {
		pos = 0
	}
	if pos > len(ids) {
		pos = len(ids)
	}
	ids = append(ids, 0)
	copy(ids[pos+1:], ids[pos:])
	ids[pos] = id
	return ids
}

func (s *CardService) labelsForCard(cardID int64) ([]*Label, error) {
	var labels []*Label
	err := s.db.Select(&labels, `
		SELECT l.id, l.title, l.normalized_title, l.description, l.color, l.created_at
		FROM label l
		JOIN card_label cl ON cl.label_id = l.id
		WHERE cl.card_id=?
		ORDER BY l.title COLLATE NOCASE, l.id
	`, cardID)
	finalizeLabels(labels)
	return labels, err
}

func (s *CardService) assigneesForCard(cardID int64) ([]*gauth.User, error) {
	var users []*gauth.User
	err := s.db.Select(&users, `
		SELECT u.id, u.username, p.email, p.role, p.profile_image_uri, p.created_at, p.last_login_at
		FROM card_assignee ca
		JOIN user u ON u.id = ca.user_id
		JOIN user_profile p ON p.user_id = u.id
		WHERE ca.card_id=?
		ORDER BY u.username
	`, cardID)
	return users, err
}

func (s *CardService) attachLabelsByBoard(cards []*Card, boardID int64) error {
	if len(cards) == 0 {
		return nil
	}
	cardsByID := make(map[int64]*Card, len(cards))
	for _, card := range cards {
		card.Labels = nil
		cardsByID[card.ID] = card
	}
	type cardLabelRow struct {
		CardID          int64     `db:"card_id"`
		ID              int64     `db:"id"`
		Title           string    `db:"title"`
		NormalizedTitle string    `db:"normalized_title"`
		Description     string    `db:"description"`
		Color           string    `db:"color"`
		CreatedAt       time.Time `db:"created_at"`
	}
	var rows []cardLabelRow
	if err := s.db.Select(&rows, `
		SELECT cl.card_id, l.id, l.title, l.normalized_title, l.description, l.color, l.created_at
		FROM card_label cl
		JOIN label l ON l.id = cl.label_id
		JOIN card c ON c.id = cl.card_id
		WHERE c.board_id=? AND c.archived_at IS NULL
		ORDER BY l.title COLLATE NOCASE, l.id
	`, boardID); err != nil {
		return err
	}
	for _, row := range rows {
		card := cardsByID[row.CardID]
		if card == nil {
			continue
		}
		card.Labels = append(card.Labels, &Label{
			ID:              row.ID,
			Title:           row.Title,
			NormalizedTitle: row.NormalizedTitle,
			Description:     row.Description,
			Color:           row.Color,
			CreatedAt:       row.CreatedAt,
			TextClass:       labelTextClass(row.Color),
		})
	}
	return nil
}

func (s *CardService) attachLabels(cards []*Card) error {
	if len(cards) == 0 {
		return nil
	}
	cardsByID := make(map[int64]*Card, len(cards))
	for _, card := range cards {
		card.Labels = nil
		cardsByID[card.ID] = card
	}
	type cardLabelRow struct {
		CardID          int64     `db:"card_id"`
		ID              int64     `db:"id"`
		Title           string    `db:"title"`
		NormalizedTitle string    `db:"normalized_title"`
		Description     string    `db:"description"`
		Color           string    `db:"color"`
		CreatedAt       time.Time `db:"created_at"`
	}
	var rows []cardLabelRow
	if err := s.db.Select(&rows, `
		SELECT cl.card_id, l.id, l.title, l.normalized_title, l.description, l.color, l.created_at
		FROM card_label cl
		JOIN label l ON l.id = cl.label_id
		WHERE cl.card_id IN (SELECT id FROM card WHERE archived_at IS NULL)
		ORDER BY l.title COLLATE NOCASE, l.id
	`); err != nil {
		return err
	}
	for _, row := range rows {
		card := cardsByID[row.CardID]
		if card == nil {
			continue
		}
		card.Labels = append(card.Labels, &Label{
			ID:              row.ID,
			Title:           row.Title,
			NormalizedTitle: row.NormalizedTitle,
			Description:     row.Description,
			Color:           row.Color,
			CreatedAt:       row.CreatedAt,
			TextClass:       labelTextClass(row.Color),
		})
	}
	return nil
}

func (s *CardService) attachAssignees(cards []*Card) error {
	if len(cards) == 0 {
		return nil
	}
	query := `SELECT ca.card_id, u.id, u.username, p.email, p.role, p.profile_image_uri, p.created_at, p.last_login_at
		FROM card_assignee ca
		JOIN user u ON u.id = ca.user_id
		JOIN user_profile p ON p.user_id = u.id
		WHERE ca.card_id IN (?) ORDER BY u.username`
	ids := make([]int64, 0, len(cards))
	byCard := make(map[int64]*Card, len(cards))
	for _, card := range cards {
		ids = append(ids, card.ID)
		byCard[card.ID] = card
	}
	query, args, err := sqlx.In(query, ids)
	if err != nil {
		return err
	}
	type row struct {
		CardID          int64      `db:"card_id"`
		ID              int64      `db:"id"`
		Username        string     `db:"username"`
		Email           string     `db:"email"`
		Role            string     `db:"role"`
		ProfileImageURI string     `db:"profile_image_uri"`
		CreatedAt       time.Time  `db:"created_at"`
		LastLoginAt     *time.Time `db:"last_login_at"`
	}
	var rows []row
	if err := s.db.Select(&rows, query, args...); err != nil {
		return err
	}
	for _, r := range rows {
		card := byCard[r.CardID]
		if card == nil {
			continue
		}
		card.Assignees = append(card.Assignees, &gauth.User{
			ID:              r.ID,
			Username:        r.Username,
			Email:           r.Email,
			Role:            r.Role,
			ProfileImageURI: r.ProfileImageURI,
			CreatedAt:       r.CreatedAt,
			LastLoginAt:     r.LastLoginAt,
		})
	}
	return nil
}

func (s *CardService) attachChecklistSummary(cards []*Card) error {
	if len(cards) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(cards))
	byCard := make(map[int64]*Card, len(cards))
	for _, card := range cards {
		card.ChecklistDone = 0
		card.ChecklistTotal = 0
		card.ChecklistPct = 0
		ids = append(ids, card.ID)
		byCard[card.ID] = card
	}
	query, args, err := sqlx.In(`
		SELECT c.card_id,
		       COUNT(ci.id) AS total_count,
		       COALESCE(SUM(CASE WHEN ci.done=1 THEN 1 ELSE 0 END), 0) AS done_count
		FROM checklist c
		LEFT JOIN checklist_item ci ON ci.checklist_id = c.id
		WHERE c.card_id IN (?)
		GROUP BY c.card_id
	`, ids)
	if err != nil {
		return err
	}
	type row struct {
		CardID     int64 `db:"card_id"`
		TotalCount int   `db:"total_count"`
		DoneCount  int   `db:"done_count"`
	}
	var rows []row
	if err := s.db.Select(&rows, query, args...); err != nil {
		return err
	}
	for _, r := range rows {
		card := byCard[r.CardID]
		if card == nil {
			continue
		}
		card.ChecklistTotal = r.TotalCount
		card.ChecklistDone = r.DoneCount
		if r.TotalCount > 0 {
			card.ChecklistPct = int(float64(r.DoneCount) / float64(r.TotalCount) * 100)
		}
	}
	return nil
}

// LabelService methods

func (s *LabelService) List() ([]*Label, error) {
	var labels []*Label
	err := s.db.Select(&labels, `
		SELECT id, title, normalized_title, description, color, created_at
		FROM label
		ORDER BY title COLLATE NOCASE, id
	`)
	finalizeLabels(labels)
	return labels, err
}

func (s *LabelService) Get(id int64) (*Label, error) {
	var label Label
	err := s.db.Get(&label, `
		SELECT id, title, normalized_title, description, color, created_at
		FROM label
		WHERE id=?
	`, id)
	if err != nil {
		return nil, err
	}
	finalizeLabel(&label)
	return &label, nil
}

func (s *LabelService) CreateOrGet(title, description, color string) (*Label, bool, error) {
	title = canonicalLabelTitle(title)
	normalized := normalizeLabelTitle(title)
	if normalized == "" {
		return nil, false, fmt.Errorf("label title is required")
	}
	var label Label
	err := s.db.Get(&label, `
		SELECT id, title, normalized_title, description, color, created_at
		FROM label WHERE normalized_title=?
	`, normalized)
	if err == nil {
		finalizeLabel(&label)
		return &label, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	description = strings.TrimSpace(description)
	err = db.With(s.db, func(tx *sqlx.Tx) error {
		if strings.TrimSpace(color) == "" {
			var usedColors []string
			if err := tx.Select(&usedColors, `SELECT color FROM label ORDER BY id`); err != nil {
				return err
			}
			color = nextLabelColor(usedColors)
		} else {
			color = sanitizeLabelColor(color)
		}
		res, err := tx.Exec(
			`INSERT INTO label (title, normalized_title, description, color) VALUES (?, ?, ?, ?)`,
			title, normalized, description, color,
		)
		if err != nil {
			var existing Label
			if getErr := tx.Get(&existing, `
				SELECT id, title, normalized_title, description, color, created_at
				FROM label WHERE normalized_title=?
			`, normalized); getErr == nil {
				label = existing
				return nil
			}
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		return tx.Get(&label, `
			SELECT id, title, normalized_title, description, color, created_at
			FROM label WHERE id=?
		`, id)
	})
	if err != nil {
		return nil, false, err
	}
	finalizeLabel(&label)
	return &label, true, nil
}

func (s *LabelService) Update(id int64, title, description, color string) error {
	title = canonicalLabelTitle(title)
	normalized := normalizeLabelTitle(title)
	if normalized == "" {
		return fmt.Errorf("label title is required")
	}
	_, err := s.db.Exec(
		`UPDATE label SET title=?, normalized_title=?, description=?, color=? WHERE id=?`,
		title, normalized, strings.TrimSpace(description), sanitizeLabelColor(color), id,
	)
	return err
}

func (s *LabelService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM label WHERE id=?`, id)
	return err
}

// BuildColumns groups cards by column_id into ColumnWithCards in column order.
func BuildColumns(cols []*Column, cards []*Card) []*ColumnWithCards {
	cardsByCol := make(map[int64][]*Card)
	for _, c := range cards {
		cardsByCol[c.ColumnID] = append(cardsByCol[c.ColumnID], c)
	}
	result := make([]*ColumnWithCards, len(cols))
	for i, col := range cols {
		result[i] = &ColumnWithCards{
			Column: col,
			Cards:  cardsByCol[col.ID],
		}
	}
	return result
}
