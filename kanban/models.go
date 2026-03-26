package kanban

import (
	"time"

	gauth "github.com/jmoiron/giverny/auth"
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
	Color     string    `db:"color"`
	Done      bool      `db:"done"`
	Late      bool      `db:"late"`
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
	Color           string     `db:"color"`
	StartDate       *time.Time `db:"start_date"`
	DueDate         *time.Time `db:"due_date"`
	ArchivedAt      *time.Time `db:"archived_at"`
	CreatedBy       int64      `db:"created_by"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
	Labels          []*Label      `db:"-"`
	Assignees       []*gauth.User `db:"-"`
	Checklist       *Checklist    `db:"-"`
}

type CardSubscription struct {
	CardID    int64     `db:"card_id"`
	UserID    int64     `db:"user_id"`
	CreatedAt time.Time `db:"created_at"`
}

type SubscriptionMessage struct {
	ID        int64      `db:"id"`
	CardID    int64      `db:"card_id"`
	UserID    int64      `db:"user_id"`
	Message   string     `db:"message"`
	ReadAt    *time.Time `db:"read_at"`
	CreatedAt time.Time  `db:"created_at"`
}

type Label struct {
	ID              int64     `db:"id"`
	Title           string    `db:"title"`
	NormalizedTitle string    `db:"normalized_title"`
	Description     string    `db:"description"`
	Color           string    `db:"color"`
	CreatedAt       time.Time `db:"created_at"`
	TextClass       string    `db:"-"`
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
	Items    []*ChecklistItem `db:"-"`
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
		{
			Up:   `ALTER TABLE board_column ADD COLUMN color TEXT NOT NULL DEFAULT '';`,
			Down: ``,
		},
		{
			Up:   `ALTER TABLE board_column ADD COLUMN done INTEGER NOT NULL DEFAULT 0;`,
			Down: ``,
		},
		{
			Up:   `UPDATE board_column SET done=1 WHERE lower(trim(name))='done';`,
			Down: ``,
		},
		{
			Up:   `ALTER TABLE board_column ADD COLUMN late INTEGER NOT NULL DEFAULT 0;`,
			Down: ``,
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
		{
			Up:   `ALTER TABLE card ADD COLUMN color TEXT NOT NULL DEFAULT '';`,
			Down: ``,
		},
		{
			Up:   `ALTER TABLE card ADD COLUMN start_date DATETIME;`,
			Down: ``,
		},
	},
}

var CardAssigneeMigrations = monarch.Set{
	Name: "card_assignee",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS card_assignee (
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
				created_at DATETIME DEFAULT (datetime('now')),
				PRIMARY KEY (card_id, user_id)
			);
			INSERT OR IGNORE INTO card_assignee (card_id, user_id)
			SELECT id, assignee_id FROM card WHERE assignee_id IS NOT NULL;`,
			Down: `DROP TABLE card_assignee;`,
		},
		{
			Up:   `ALTER TABLE card DROP COLUMN assignee_id;`,
			Down: `SELECT 1;`,
		},
	},
}

var SubscriptionMigrations = monarch.Set{
	Name: "card_subscription",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS card_subscription (
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
				created_at DATETIME DEFAULT (datetime('now')),
				PRIMARY KEY (card_id, user_id)
			);`,
			Down: `DROP TABLE card_subscription;`,
		},
		{
			Up: `CREATE TABLE IF NOT EXISTS subscription_message (
				id INTEGER NOT NULL PRIMARY KEY,
				card_id INTEGER NOT NULL REFERENCES card(id) ON DELETE CASCADE,
				user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
				message TEXT NOT NULL,
				read_at DATETIME,
				created_at DATETIME DEFAULT (datetime('now'))
			);
			CREATE INDEX IF NOT EXISTS idx_subscription_message_user_id ON subscription_message(user_id, created_at DESC);`,
			Down: `DROP INDEX IF EXISTS idx_subscription_message_user_id;
DROP TABLE subscription_message;`,
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
		{
			Up: `PRAGMA foreign_keys = OFF;
CREATE TABLE IF NOT EXISTS label_new (
	id INTEGER NOT NULL PRIMARY KEY,
	title TEXT NOT NULL,
	normalized_title TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	color TEXT NOT NULL DEFAULT '#888888',
	created_at DATETIME DEFAULT (datetime('now'))
);
INSERT OR IGNORE INTO label_new (id, title, normalized_title, description, color, created_at)
SELECT id, trim(name), lower(trim(name)), '', color, datetime('now')
FROM label;
DROP TABLE label;
ALTER TABLE label_new RENAME TO label;
CREATE INDEX IF NOT EXISTS idx_card_label_label_id ON card_label(label_id);
PRAGMA foreign_keys = ON;`,
			Down: `DROP INDEX IF EXISTS idx_card_label_label_id;
ALTER TABLE label RENAME TO label_new;
CREATE TABLE IF NOT EXISTS label (
	id INTEGER NOT NULL PRIMARY KEY,
	board_id INTEGER NOT NULL REFERENCES board(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '#888888'
);
INSERT INTO label (id, board_id, name, color)
SELECT id, 1, title, color FROM label_new;
DROP TABLE label_new;`,
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
		{
			Up:   `CREATE UNIQUE INDEX IF NOT EXISTS checklist_card_id_unique ON checklist(card_id);`,
			Down: `DROP INDEX checklist_card_id_unique;`,
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
