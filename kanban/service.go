package kanban

import (
	"fmt"
	"regexp"
	"strings"

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

func NewBoardService(dbh db.DB) *BoardService   { return &BoardService{db: dbh} }
func NewColumnService(dbh db.DB) *ColumnService { return &ColumnService{db: dbh} }
func NewCardService(dbh db.DB) *CardService     { return &CardService{db: dbh} }

var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
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

// ColumnService methods

func (s *ColumnService) Create(boardID int64, name string) (*Column, error) {
	var col Column
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM board_column WHERE board_id=?`, boardID)
		res, err := tx.Exec(
			`INSERT INTO board_column (board_id, name, position) VALUES (?, ?, ?)`,
			boardID, name, maxPos+1,
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

func (s *ColumnService) ListByBoard(boardID int64) ([]*Column, error) {
	var cols []*Column
	err := s.db.Select(&cols, `SELECT * FROM board_column WHERE board_id=? ORDER BY position, id`, boardID)
	return cols, err
}

func (s *ColumnService) Update(id int64, name string, wipLimit int) error {
	_, err := s.db.Exec(`UPDATE board_column SET name=?, wip_limit=? WHERE id=?`, name, wipLimit, id)
	return err
}

func (s *ColumnService) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM board_column WHERE id=?`, id)
	return err
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

// CardService methods

func (s *CardService) Create(columnID, boardID, createdBy int64, title, content string) (*Card, error) {
	rendered := mtr.RenderMarkdown(content)
	var card Card
	err := db.With(s.db, func(tx *sqlx.Tx) error {
		var maxPos int
		_ = tx.Get(&maxPos, `SELECT COALESCE(MAX(position), 0) FROM card WHERE column_id=?`, columnID)
		res, err := tx.Exec(
			`INSERT INTO card (column_id, board_id, title, content, content_rendered, position, created_by) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			columnID, boardID, title, content, rendered, maxPos+1, createdBy,
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
	return &card, err
}

func (s *CardService) ListByBoard(boardID int64) ([]*Card, error) {
	var cards []*Card
	err := s.db.Select(&cards,
		`SELECT * FROM card WHERE board_id=? AND archived_at IS NULL ORDER BY column_id, position, id`,
		boardID,
	)
	return cards, err
}

func (s *CardService) Update(id int64, title, content string) error {
	rendered := mtr.RenderMarkdown(content)
	_, err := s.db.Exec(
		`UPDATE card SET title=?, content=?, content_rendered=?, updated_at=datetime('now') WHERE id=?`,
		title, content, rendered, id,
	)
	return err
}

func (s *CardService) Move(id, columnID int64, position int) error {
	_, err := s.db.Exec(
		`UPDATE card SET column_id=?, position=?, updated_at=datetime('now') WHERE id=?`,
		columnID, position, id,
	)
	return err
}

func (s *CardService) Archive(id int64) error {
	_, err := s.db.Exec(`UPDATE card SET archived_at=datetime('now'), updated_at=datetime('now') WHERE id=?`, id)
	return err
}

func (s *CardService) Reorder(columnID int64, ids []int64) error {
	return db.With(s.db, func(tx *sqlx.Tx) error {
		for i, id := range ids {
			if _, err := tx.Exec(`UPDATE card SET position=? WHERE id=? AND column_id=?`, i, id, columnID); err != nil {
				return fmt.Errorf("reorder card %d: %w", id, err)
			}
		}
		return nil
	})
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
