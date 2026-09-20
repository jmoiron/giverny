package mobile

import (
	"embed"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/giverny/conf"
	"github.com/jmoiron/giverny/kanban"
	"github.com/jmoiron/monet/app"
	mauth "github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

//go:embed mobile/*.html
var templates embed.FS

// App is the mobile presentation layer. Board/card mutations intentionally
// remain owned by kanban and are called by the mobile client directly.
type App struct {
	db        db.DB
	cfg       *conf.Config
	kanban    *kanban.App
	gauth     *gauth.App
	monetAuth *mauth.App
	fss       vfs.Registry
	push      *PushService
}

func NewApp(dbh db.DB, cfg *conf.Config, kanbanApp *kanban.App, gauthApp *gauth.App, monetAuth *mauth.App, fss vfs.Registry) *App {
	return &App{db: dbh, cfg: cfg, kanban: kanbanApp, gauth: gauthApp, monetAuth: monetAuth, fss: fss, push: NewPushService(dbh, cfg, kanbanApp)}
}

func (a *App) Name() string { return "mobile" }
func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	return m.Upgrade(PushMigrations)
}

func (a *App) PushNotifier() kanban.PushNotifier { return a.push }

func (a *App) Register(reg *mtr.Registry) {
	reg.AddPathFS("mobile/login.html", templates)
	reg.AddPathFS("mobile/home.html", templates)
	reg.AddPathFS("mobile/cards.html", templates)
	reg.AddPathFS("mobile/board.html", templates)
	reg.AddPathFS("mobile/card.html", templates)
}

func (a *App) GetAdmin() (app.Admin, error) { return nil, nil }

func (a *App) Bind(r chi.Router) {
	r.Get("/mobile/login/", a.handleLogin)
	r.Post("/mobile/login/", a.handleLogin)
	r.Get("/mobile/logout/", a.handleLogout)
	r.Route("/mobile", func(r chi.Router) {
		r.Use(a.RequireAuth)
		r.Get("/", a.handleHome)
		r.Get("/cards/my-tasks/", a.handleMyTasks)
		r.Get("/cards/subscribed/", a.handleSubscribedCards)
		r.Get("/cards/in-progress/", a.handleInProgressCards)
		r.Get("/boards/{slug}/", a.handleBoardDetail)
		r.Get("/boards/{slug}/columns/{colID}/cards", a.handleColumnCardsPartial)
		r.Get("/boards/{slug}/cards/{cardID}/", a.handleCardDetail)
		r.Get("/push/vapid-key", a.handleVAPIDPublicKey)
		r.Post("/push/subscribe", a.handlePushSubscribe)
		r.Post("/push/unsubscribe", a.handlePushUnsubscribe)
	})
}

func (a *App) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gauth.UserFromContext(r.Context()) == nil {
			redirect := r.URL.RequestURI()
			http.Redirect(w, r, "/mobile/login/?redirect="+url.QueryEscape(redirect), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/mobile/"
	}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		if ok, _ := a.gauth.Users().Validate(username, r.FormValue("password")); ok {
			session := mauth.SessionFromContext(r.Context()).Session(r)
			session.Values["authenticated"] = true
			session.Values["user"] = username
			if err := session.Save(r, w); err != nil {
				http.Error(w, "could not save session", http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, redirect, http.StatusSeeOther)
			return
		}
		a.render(w, r, "mobile/login.html", mtr.Ctx{"title": "login", "error": "invalid username or password", "redirect": redirect})
		return
	}
	a.render(w, r, "mobile/login.html", mtr.Ctx{"title": "login", "redirect": redirect})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	session := mauth.SessionFromContext(r.Context()).Session(r)
	session.Options.MaxAge = -1
	_ = session.Save(r, w)
	http.Redirect(w, r, "/mobile/login/", http.StatusSeeOther)
}

func (a *App) handleHome(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	boards, err := a.kanban.RecentBoards(0, user)
	if err != nil {
		app.Http500("loading mobile boards", w, err)
		return
	}
	a.render(w, r, "mobile/home.html", mtr.Ctx{"title": "boards", "user": user, "boards": boards})
}

func (a *App) handleBoardDetail(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	board, err := a.kanban.Boards().GetBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !a.kanban.CanViewBoard(board, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	canEdit := a.kanban.CanModifyBoard(board, user)
	columns, err := a.kanban.RenderedColumns(r, board, canEdit)
	if err != nil {
		app.Http500("rendering mobile board", w, err)
		return
	}
	a.render(w, r, "mobile/board.html", mtr.Ctx{"title": board.Name, "user": user, "board": board, "columns": columns, "canEdit": canEdit})
}

func (a *App) handleColumnCardsPartial(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	board, err := a.kanban.Boards().GetBySlug(chi.URLParam(r, "slug"))
	if err != nil || !a.kanban.CanViewBoard(board, user) {
		http.NotFound(w, r)
		return
	}
	colID, err := strconv.ParseInt(chi.URLParam(r, "colID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid column id", http.StatusBadRequest)
		return
	}
	col, err := a.kanban.Columns().Get(colID)
	if err != nil || col.BoardID != board.ID {
		http.NotFound(w, r)
		return
	}
	html, err := a.kanban.RenderColumnCards(r, colID, a.kanban.CanModifyBoard(board, user))
	if err != nil {
		app.Http500("rendering mobile column", w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (a *App) handleCardDetail(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	board, err := a.kanban.Boards().GetBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cardID, err := strconv.ParseInt(chi.URLParam(r, "cardID"), 10, 64)
	if err != nil {
		http.Error(w, "invalid card id", http.StatusBadRequest)
		return
	}
	card, err := a.kanban.Cards().Get(cardID)
	if err != nil || card.BoardID != board.ID {
		http.NotFound(w, r)
		return
	}
	if !a.kanban.CanViewBoard(board, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body, err := a.kanban.RenderCardDetailHTML(r, board, card, user)
	if err != nil {
		app.Http500("rendering mobile card", w, err)
		return
	}
	a.render(w, r, "mobile/card.html", mtr.Ctx{"title": card.Title, "user": user, "board": board, "cardBody": body, "mobileCard": true})
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name string, ctx mtr.Ctx) {
	if err := mtr.RegistryFromContext(r.Context()).RenderWithBase(w, "mobile-base", name, ctx); err != nil {
		app.Http500("rendering mobile page", w, err)
	}
}

var _ app.App = (*App)(nil)
