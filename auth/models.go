package auth

import (
	"time"

	"github.com/jmoiron/monet/db/monarch"
)

// Roles
const (
	RoleReadonly   = "readonly"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "superadmin"
)

// UserProfile extends monet's base user table (id, username, password_hash)
// with giverny-specific fields. It is 1:1 with the user table via user_id.
type UserProfile struct {
	ID              int64      `db:"id"`
	UserID          int64      `db:"user_id"`
	Email           string     `db:"email"`
	Role            string     `db:"role"`
	ProfileImageURI string     `db:"profile_image_uri"`
	CreatedAt       time.Time  `db:"created_at"`
	LastLoginAt     *time.Time `db:"last_login_at"`
}

type Invitation struct {
	ID        int64      `db:"id"`
	Email     string     `db:"email"`
	Token     string     `db:"token"`
	CreatedBy int64      `db:"created_by"`
	ExpiresAt time.Time  `db:"expires_at"`
	UsedAt    *time.Time `db:"used_at"`
	CreatedAt time.Time  `db:"created_at"`
}

var UserProfileMigrations = monarch.Set{
	Name: "user_profile",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS user_profile (
				id INTEGER NOT NULL PRIMARY KEY,
				user_id INTEGER NOT NULL UNIQUE REFERENCES user(id) ON DELETE CASCADE,
				email TEXT NOT NULL UNIQUE,
				role TEXT NOT NULL DEFAULT 'readonly',
				created_at DATETIME DEFAULT (datetime('now')),
				last_login_at DATETIME
			);`,
			Down: `DROP TABLE user_profile;`,
		},
		{
			Up:   `ALTER TABLE user_profile ADD COLUMN profile_image_uri TEXT NOT NULL DEFAULT '';`,
			Down: `SELECT 1;`, // SQLite does not support DROP COLUMN in older versions
		},
	},
}

var InvitationMigrations = monarch.Set{
	Name: "invitation",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS invitation (
				id INTEGER NOT NULL PRIMARY KEY,
				email TEXT NOT NULL,
				token TEXT NOT NULL UNIQUE,
				created_by INTEGER NOT NULL REFERENCES user(id),
				expires_at DATETIME NOT NULL,
				used_at DATETIME,
				created_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE invitation;`,
		},
	},
}
