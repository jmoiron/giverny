package main

import (
	"testing"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestMigrationsUpDown(t *testing.T) {
	db, err := sqlx.Connect("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec("PRAGMA foreign_keys = ON")

	// monet's auth app creates the base user table
	authApp := auth.NewApp(conf.Default(), db)
	if err := authApp.Migrate(); err != nil {
		t.Fatalf("monet auth migrate: %v", err)
	}

	// giverny's auth app extends with user_profile + invitations
	gauthApp := gauth.NewApp(db, "http://localhost:7100")
	if err := gauthApp.Migrate(); err != nil {
		t.Fatalf("giverny auth migrate: %v", err)
	}

	m, err := monarch.NewManager(db)
	if err != nil {
		t.Fatal(err)
	}

	sets := givernyMigrations()

	// Upgrade all
	for _, s := range sets {
		if err := m.Upgrade(s); err != nil {
			t.Fatalf("upgrade %s: %v", s.Name, err)
		}
	}

	// Downgrade all in reverse order
	for i := len(sets) - 1; i >= 0; i-- {
		s := sets[i]
		for range s.Migrations {
			if err := m.Downgrade(s.Name); err != nil {
				t.Fatalf("downgrade %s: %v", s.Name, err)
			}
		}
	}
}
