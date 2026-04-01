package smtp

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
)

//go:embed smtp/*.html
var templates embed.FS

type App struct {
	db  db.DB
	svc *Service
}

func NewApp(dbh db.DB, secret string) (*App, error) {
	svc, err := NewService(dbh, secret)
	if err != nil {
		return nil, err
	}
	return &App{db: dbh, svc: svc}, nil
}

func (a *App) Name() string { return "smtp" }

func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	if err := m.Upgrade(ConfigMigrations); err != nil {
		return fmt.Errorf("%s: %w", ConfigMigrations.Name, err)
	}
	return nil
}

func (a *App) Register(reg *mtr.Registry) {
	reg.AddPathFS("smtp/config.html", templates)
}

func (a *App) Bind(r chi.Router) {
	r.Get("/admin/smtp/", a.handleConfig)
	r.Post("/admin/smtp/", a.handleSaveConfig)
}

func (a *App) GetAdmin() (app.Admin, error) { return nil, nil }

// Service returns the smtp service for use by other packages (e.g. invite emails).
func (a *App) Service() *Service { return a.svc }

func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.svc.Get()
	if err != nil {
		app.Http500("loading smtp config", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "smtp/config.html", mtr.Ctx{
		"title": "smtp",
		"cfg":   cfg,
		"saved": r.URL.Query().Get("saved") == "1",
		"user":  gauth.UserFromContext(r.Context()),
	}); err != nil {
		app.Http500("rendering smtp config", w, err)
	}
}

func (a *App) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	port, _ := strconv.Atoi(r.FormValue("port"))
	if port == 0 {
		port = 587
	}

	if err := a.svc.Save(
		r.FormValue("host"),
		port,
		r.FormValue("username"),
		r.FormValue("password"),
		r.FormValue("from_address"),
	); err != nil {
		app.Http500("saving smtp config", w, err)
		return
	}

	if r.FormValue("action") == "test" {
		cfg, _ := a.svc.Get()
		if err := a.svc.Send(cfg.FromAddress, "Giverny SMTP test", "SMTP is configured correctly."); err != nil {
			slog.Warn("smtp test send failed", "err", err)
			reg := mtr.RegistryFromContext(r.Context())
			c, _ := a.svc.Get()
			reg.RenderWithBase(w, "base", "smtp/config.html", mtr.Ctx{
				"title": "smtp",
				"cfg":   c,
				"error": fmt.Sprintf("test send failed: %v", err),
			})
			return
		}
	}

	http.Redirect(w, r, "/admin/smtp/?saved=1", http.StatusSeeOther)
}
