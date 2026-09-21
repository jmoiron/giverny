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
	ID                   int64      `db:"id"`
	UserID               int64      `db:"user_id"`
	Email                string     `db:"email"`
	Role                 string     `db:"role"`
	ProfileImageURI      string     `db:"profile_image_uri"`
	Timezone             string     `db:"timezone"`
	AutoAssignCards      bool       `db:"auto_assign_cards"`
	DisablePasskeyPrompt bool       `db:"disable_passkey_prompt"`
	CreatedAt            time.Time  `db:"created_at"`
	LastLoginAt          *time.Time `db:"last_login_at"`
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
		{
			Up:   `ALTER TABLE user_profile ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';`,
			Down: `SELECT 1;`,
		},
		{
			Up:   `ALTER TABLE user_profile ADD COLUMN auto_assign_cards BOOLEAN NOT NULL DEFAULT 0;`,
			Down: `SELECT 1;`,
		},
		{
			Up:   `ALTER TABLE user_profile ADD COLUMN disable_passkey_prompt BOOLEAN NOT NULL DEFAULT 0;`,
			Down: `SELECT 1;`,
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

// WebAuthnMigrations stores passkey credential records. The complete
// credential is retained as JSON so upgrades to the WebAuthn library do not
// require a schema migration for every credential field.
var WebAuthnMigrations = monarch.Set{
	Name: "webauthn_credential",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS webauthn_credential (
				id INTEGER NOT NULL PRIMARY KEY,
				user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
				credential_id TEXT NOT NULL UNIQUE,
				public_key BLOB NOT NULL,
				attestation_type TEXT NOT NULL DEFAULT '',
				transports TEXT NOT NULL DEFAULT '',
				sign_count INTEGER NOT NULL DEFAULT 0,
				clone_warning BOOLEAN NOT NULL DEFAULT 0,
				aaguid TEXT NOT NULL DEFAULT '',
				credential_json TEXT NOT NULL,
				name TEXT NOT NULL DEFAULT '',
				created_at DATETIME DEFAULT (datetime('now')),
				last_used_at DATETIME
			);`,
			Down: `DROP TABLE webauthn_credential;`,
		},
	},
}
