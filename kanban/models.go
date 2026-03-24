package kanban

import (
	"time"

	"github.com/jmoiron/monet/db/monarch"
)

// Board visibility levels.
const (
	VisibilityPrivate = "private" // admin/superadmin only
	VisibilityOpen    = "open"    // all logged-in users can view
	VisibilityPublic  = "public"  // anyone, including anonymous
)

type Board struct {
	ID          int64     `db:"id"`
	Name        string    `db:"name"`
	Slug        string    `db:"slug"`
	Description string    `db:"description"`
	Visibility  string    `db:"visibility"`
	CreatedBy   int64     `db:"created_by"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

type Column struct {
	ID        int64     `db:"id"`
	BoardID   int64     `db:"board_id"`
	Name      string    `db:"name"`
	Position  int       `db:"position"`
	WIPLimit  int       `db:"wip_limit"`
	CreatedAt time.Time `db:"created_at"`
}

type Card struct {
	ID              int64      `db:"id"`
	ColumnID        int64      `db:"column_id"`
	BoardID         int64      `db:"board_id"`
	Title           string     `db:"title"`
	Content         string     `db:"content"`
	ContentRendered string     `db:"content_rendered"`
	Position        int        `db:"position"`
	AssigneeID      *int64     `db:"assignee_id"`
	DueDate         *time.Time `db:"due_date"`
	ArchivedAt      *time.Time `db:"archived_at"`
	CreatedBy       int64      `db:"created_by"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

type Label struct {
	ID      int64  `db:"id"`
	BoardID int64  `db:"board_id"`
	Name    string `db:"name"`
	Color   string `db:"color"`
}

type CardLabel struct {
	CardID  int64 `db:"card_id"`
	LabelID int64 `db:"label_id"`
}

type Checklist struct {
	ID       int64  `db:"id"`
	CardID   int64  `db:"card_id"`
	Title    string `db:"title"`
	Position int    `db:"position"`
}

type ChecklistItem struct {
	ID          int64  `db:"id"`
	ChecklistID int64  `db:"checklist_id"`
	Text        string `db:"text"`
	Done        bool   `db:"done"`
	Position    int    `db:"position"`
}

type Comment struct {
	ID           int64     `db:"id"`
	CardID       int64     `db:"card_id"`
	AuthorID     int64     `db:"author_id"`
	Body         string    `db:"body"`
	BodyRendered string    `db:"body_rendered"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

type Attachment struct {
	ID         int64     `db:"id"`
	CardID     int64     `db:"card_id"`
	UploadedBy int64     `db:"uploaded_by"`
	Filename   string    `db:"filename"`
	Filepath   string    `db:"filepath"`
	MimeType   string    `db:"mime_type"`
	Size       int64     `db:"size"`
	CreatedAt  time.Time `db:"created_at"`
}

type Activity struct {
	ID        int64     `db:"id"`
	BoardID   int64     `db:"board_id"`
	CardID    *int64    `db:"card_id"`
	UserID    int64     `db:"user_id"`
	Action    string    `db:"action"`
	Detail    string    `db:"detail"`
	CreatedAt time.Time `db:"created_at"`
}

// Migrations

var BoardMigrations = monarch.Set{
	Name: "board",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS board (
				id INTEGER NOT NULL PRIMARY KEY,
				name TEXT NOT NULL,
				slug TEXT NOT NULL UNIQUE,
				description TEXT DEFAULT '',
				visibility TEXT NOT NULL DEFAULT 'private',
				created_by INTEGER NOT NULL REFERENCES user(id),
				created_at DATETIME DEFAULT (datetime('now')),
				updated_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE board;`,
		},
	},
}

var ColumnMigrations = monarch.Set{
	Name: "board_column",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS board_column (
				id INTEGER NOT NULL PRIMARY KEY,
				board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
				name TEXT NOT NULL,
				position INTEGER NOT NULL DEFAULT 0,
				wip_limit INTEGER NOT NULL DEFAULT 0,
				created_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE board_column;`,
		},
	},
}

var CardMigrations = monarch.Set{
	Name: "card",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS card (
				id INTEGER NOT NULL PRIMARY KEY,
				column_id INTEGER NOT NULL REFERENCES board_column(id) ON DELETE CASCADE,
				board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
				title TEXT NOT NULL,
				content TEXT DEFAULT '',
				content_rendered TEXT DEFAULT '',
				position INTEGER NOT NULL DEFAULT 0,
				assignee_id INTEGER REFERENCES user(id) ON DELETE SET NULL,
				due_date DATETIME,
				archived_at DATETIME,
				created_by INTEGER NOT NULL REFERENCES user(id),
				created_at DATETIME DEFAULT (datetime('now')),
				updated_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE card;`,
		},
	},
}

var LabelMigrations = monarch.Set{
	Name: "label",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS label (
				id INTEGER NOT NULL PRIMARY KEY,
				board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
				name TEXT NOT NULL,
				color TEXT NOT NULL DEFAULT '#888888'
			);`,
			Down: `DROP TABLE label;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS card_label (
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				label_id INTEGER NOT NULL REFERENCES label(id) ON DELETE CASCADE,
				PRIMARY KEY (card_id, label_id)
			);`,
			Down: `DROP TABLE card_label;`,
		},
	},
}

var ChecklistMigrations = monarch.Set{
	Name: "checklist",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS checklist (
				id INTEGER NOT NULL PRIMARY KEY,
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				title TEXT NOT NULL,
				position INTEGER NOT NULL DEFAULT 0
			);`,
			Down: `DROP TABLE checklist;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS checklist_item (
				id INTEGER NOT NULL PRIMARY KEY,
				checklist_id INTEGER NOT NULL REFERENCES checklist(id) ON DELETE CASCADE,
				text TEXT NOT NULL,
				done INTEGER NOT NULL DEFAULT 0,
				position INTEGER NOT NULL DEFAULT 0
			);`,
			Down: `DROP TABLE checklist_item;`,
		},
	},
}

var CommentMigrations = monarch.Set{
	Name: "comment",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS comment (
				id INTEGER NOT NULL PRIMARY KEY,
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				author_id INTEGER NOT NULL REFERENCES user(id),
				body TEXT NOT NULL DEFAULT '',
				body_rendered TEXT NOT NULL DEFAULT '',
				created_at DATETIME DEFAULT (datetime('now')),
				updated_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE comment;`,
		},
	},
}

var AttachmentMigrations = monarch.Set{
	Name: "attachment",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS attachment (
				id INTEGER NOT NULL PRIMARY KEY,
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				uploaded_by INTEGER NOT NULL REFERENCES user(id),
				filename TEXT NOT NULL,
				filepath TEXT NOT NULL,
				mime_type TEXT NOT NULL DEFAULT '',
				size INTEGER NOT NULL DEFAULT 0,
				created_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE attachment;`,
		},
	},
}

var ActivityMigrations = monarch.Set{
	Name: "activity",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS activity (
				id INTEGER NOT NULL PRIMARY KEY,
				board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
				card_id INTEGER REFERENCES card(id) ON DELETE SET NULL,
				user_id INTEGER NOT NULL REFERENCES user(id),
				action TEXT NOT NULL,
				detail TEXT DEFAULT '{}',
				created_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE activity;`,
		},
	},
}

// FTS5 for card search (title + content).
var CardFTSMigrations = monarch.Set{
	Name: "card_fts",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE VIRTUAL TABLE card_fts USING fts5(
				id, title, content, board_id,
				content='card',
				content_rowid='id',
				tokenize="trigram"
			);`,
			Down: `DROP TABLE card_fts;`,
		},
		{
			Up:   `INSERT INTO card_fts SELECT id, title, content, board_id FROM card;`,
			Down: `DELETE FROM card_fts;`,
		},
		{
			Up: `CREATE TRIGGER card_fts_i AFTER INSERT ON card BEGIN
				INSERT INTO card_fts (id, title, content, board_id)
					VALUES (new.id, new.title, new.content, new.board_id);
			END;`,
			Down: `DROP TRIGGER card_fts_i;`,
		},
		{
			Up: `CREATE TRIGGER card_fts_d AFTER DELETE ON card BEGIN
				INSERT INTO card_fts (card_fts, id, title, content, board_id)
					VALUES ('delete', old.id, old.title, old.content, old.board_id);
			END;`,
			Down: `DROP TRIGGER card_fts_d;`,
		},
		{
			Up: `CREATE TRIGGER card_fts_u AFTER UPDATE ON card BEGIN
				INSERT INTO card_fts (card_fts, id, title, content, board_id)
					VALUES ('delete', old.id, old.title, old.content, old.board_id);
				INSERT INTO card_fts (id, title, content, board_id)
					VALUES (new.id, new.title, new.content, new.board_id);
			END;`,
			Down: `DROP TRIGGER card_fts_u;`,
		},
	},
}

// FTS5 for comment search (body).
var CommentFTSMigrations = monarch.Set{
	Name: "comment_fts",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE VIRTUAL TABLE comment_fts USING fts5(
				id, body, card_id,
				content='comment',
				content_rowid='id',
				tokenize="trigram"
			);`,
			Down: `DROP TABLE comment_fts;`,
		},
		{
			Up:   `INSERT INTO comment_fts SELECT id, body, card_id FROM comment;`,
			Down: `DELETE FROM comment_fts;`,
		},
		{
			Up: `CREATE TRIGGER comment_fts_i AFTER INSERT ON comment BEGIN
				INSERT INTO comment_fts (id, body, card_id)
					VALUES (new.id, new.body, new.card_id);
			END;`,
			Down: `DROP TRIGGER comment_fts_i;`,
		},
		{
			Up: `CREATE TRIGGER comment_fts_d AFTER DELETE ON comment BEGIN
				INSERT INTO comment_fts (comment_fts, id, body, card_id)
					VALUES ('delete', old.id, old.body, old.card_id);
			END;`,
			Down: `DROP TRIGGER comment_fts_d;`,
		},
		{
			Up: `CREATE TRIGGER comment_fts_u AFTER UPDATE ON comment BEGIN
				INSERT INTO comment_fts (comment_fts, id, body, card_id)
					VALUES ('delete', old.id, old.body, old.card_id);
				INSERT INTO comment_fts (id, body, card_id)
					VALUES (new.id, new.body, new.card_id);
			END;`,
			Down: `DROP TRIGGER comment_fts_u;`,
		},
	},
}
