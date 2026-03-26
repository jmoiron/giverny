package main

import (
	"testing"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/giverny/kanban"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/conf"
	"github.com/jmoiron/monet/pkg/vfs"
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

	authApp := auth.NewApp(conf.Default(), db)
	if err := authApp.Migrate(); err != nil {
		t.Fatalf("monet auth migrate: %v", err)
	}

	fss := vfs.NewRegistry(vfs.NewURLMapper(map[string]string{
		"avatars": "/media/avatars",
	}))
	gauthApp := gauth.NewApp(db, "http://localhost:7100", fss)
	if err := gauthApp.Migrate(); err != nil {
		t.Fatalf("giverny auth migrate: %v", err)
	}

	kanbanApp := kanban.NewApp(db)
	if err := kanbanApp.Migrate(); err != nil {
		t.Fatalf("kanban migrate: %v", err)
	}
}
