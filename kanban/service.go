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

	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/sqlx"
)

type ColumnWithCards struct {
	*Column
	Cards []*Card
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
	return cards, nil
}

func (s *CardService) Update(id int64, title, content, color string, labelIDs []int64) error {
	rendered := mtr.RenderMarkdown(content)
	return db.With(s.db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(
			`UPDATE card SET title=?, content=?, content_rendered=?, color=?, updated_at=datetime('now') WHERE id=?`,
			title, content, rendered, strings.TrimSpace(color), id,
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
	return db.With(s.db, func(tx *sqlx.Tx) error {
		var card Card
		if err := tx.Get(&card, `SELECT * FROM card WHERE id=?`, cardID); err != nil {
			return err
		}
		var doneColumnID int64
		if err := tx.Get(&doneColumnID, `SELECT id FROM board_column WHERE board_id=? AND done=1 ORDER BY id LIMIT 1`, card.BoardID); err != nil {
			return err
		}
		if card.ColumnID == doneColumnID {
			return nil
		}
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM card WHERE column_id=? AND archived_at IS NULL`, doneColumnID)
		_, err := tx.Exec(`UPDATE card SET column_id=?, position=?, updated_at=datetime('now') WHERE id=?`, doneColumnID, maxPos+1, cardID)
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
