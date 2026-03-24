package smtp

import (
	"time"

	"github.com/jmoiron/monet/db/monarch"
)

type Config struct {
	ID                int64     `db:"id"`
	Host              string    `db:"host"`
	Port              int       `db:"port"`
	Username          string    `db:"username"`
	EncryptedPassword string    `db:"encrypted_password"`
	FromAddress       string    `db:"from_address"`
	UpdatedAt         time.Time `db:"updated_at"`
}

var ConfigMigrations = monarch.Set{
	Name: "smtp_config",
	Migrations: []monarch.Migration{
		{
			Up: `CREATE TABLE IF NOT EXISTS smtp_config (
				id INTEGER NOT NULL PRIMARY KEY,
				host TEXT NOT NULL DEFAULT '',
				port INTEGER NOT NULL DEFAULT 587,
				username TEXT NOT NULL DEFAULT '',
				encrypted_password TEXT NOT NULL DEFAULT '',
				from_address TEXT NOT NULL DEFAULT '',
				updated_at DATETIME DEFAULT (datetime('now'))
			);`,
			Down: `DROP TABLE smtp_config;`,
		},
	},
}
