package auth

import (
	"embed"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jmoiron/monet/app"
	mauth "github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
)

//go:embed auth/*.html
var templates embed.FS

// App is giverny's auth application. It manages user profiles and invitations,
// extending monet's base auth app (which owns the user table + login/logout).
type App struct {
	db      db.DB
	baseURL string
	users   *UserProfileService
	invites *InviteService
}

func NewApp(dbh db.DB, baseURL string) *App {
	return &App{
		db:      dbh,
		baseURL: baseURL,
		users:   NewUserProfileService(dbh),
		invites: NewInviteService(dbh),
	}
}

func (a *App) Name() string { return "giverny-auth" }

func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	for _, s := range []monarch.Set{UserProfileMigrations, InvitationMigrations} {
		if err := m.Upgrade(s); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	return nil
}

func (a *App) Register(reg *mtr.Registry) {
	reg.AddPathFS("auth/login.html", templates)
	reg.AddPathFS("auth/invite.html", templates)
	reg.AddPathFS("auth/users.html", templates)
}

func (a *App) Bind(r chi.Router) {
	r.Get("/invite/{token}", a.handleInviteForm)
	r.Post("/invite/{token}", a.handleInviteSubmit)

	r.Route("/admin/users", func(r chi.Router) {
		r.Use(RequireSuperAdmin)
		r.Get("/", a.handleUserList)
		r.Post("/{id}/role", a.handleSetRole)
		r.Post("/{id}/delete", a.handleDeleteUser)
		r.Post("/invite", a.handleInviteUser)
	})
}

func (a *App) GetAdmin() (app.Admin, error) { return nil, nil }

// Users returns the UserProfileService for use in other packages (e.g. main.go).
func (a *App) Users() *UserProfileService { return a.users }

// Invites returns the InviteService.
func (a *App) Invites() *InviteService { return a.invites }

// renderInviteForm renders the invite form, optionally with an error message and field highlighting.
func (a *App) renderInviteForm(w http.ResponseWriter, r *http.Request, token, email, errMsg string, pwError bool) {
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "auth/invite.html", mtr.Ctx{
		"title":   "accept invitation",
		"token":   token,
		"email":   email,
		"error":   errMsg,
		"pwError": pwError,
	}); err != nil {
		app.Http500("rendering invite form", w, err)
	}
}

// handleInviteForm renders the set-password form for a token.
func (a *App) handleInviteForm(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	inv, err := a.invites.GetByToken(token)
	if err != nil {
		http.Error(w, "invalid or expired invitation", http.StatusNotFound)
		return
	}
	a.renderInviteForm(w, r, token, inv.Email, "", false)
}

// handleInviteSubmit consumes the token and creates the user account.
func (a *App) handleInviteSubmit(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	confirm := r.FormValue("confirm")

	inv, err := a.invites.GetByToken(token)
	if err != nil {
		http.Error(w, "invalid or expired invitation", http.StatusNotFound)
		return
	}

	if password == "" {
		a.renderInviteForm(w, r, token, inv.Email, "password is required", true)
		return
	}
	if password != confirm {
		a.renderInviteForm(w, r, token, inv.Email, "passwords do not match", true)
		return
	}

	inv, err = a.invites.Consume(token)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := a.users.CreateUser(inv.Email, inv.Email, password, RoleReadonly, GravatarURI(inv.Email)); err != nil {
		slog.Error("creating user from invite", "err", err)
		http.Error(w, "could not create account", http.StatusInternalServerError)
		return
	}

	sm := mauth.SessionFromContext(r.Context())
	session := sm.Session(r)
	session.Values["authenticated"] = true
	session.Values["user"] = inv.Email
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleUserList renders the admin user list.
func (a *App) handleUserList(w http.ResponseWriter, r *http.Request) {
	a.renderUserList(w, r, "")
}

func (a *App) renderUserList(w http.ResponseWriter, r *http.Request, inviteLink string) {
	users, err := a.users.List()
	if err != nil {
		app.Http500("listing users", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "auth/users.html", mtr.Ctx{
		"title":      "users",
		"users":      users,
		"user":       UserFromContext(r.Context()),
		"inviteLink": inviteLink,
	}); err != nil {
		app.Http500("rendering user list", w, err)
	}
}

// handleInviteUser creates an invitation and re-renders the user list with the link.
func (a *App) handleInviteUser(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := r.FormValue("email")
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	u := UserFromContext(r.Context())
	token, err := a.invites.Create(email, u.ID)
	if err != nil {
		app.Http500("creating invitation", w, err)
		return
	}
	link := fmt.Sprintf("%s/invite/%s", strings.TrimRight(a.baseURL, "/"), token)
	a.renderUserList(w, r, link)
}

// handleSetRole updates a user's role.
func (a *App) handleSetRole(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if _, err := fmt.Sscan(chi.URLParam(r, "id"), &userID); err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	role := r.FormValue("role")
	switch role {
	case RoleReadonly, RoleAdmin, RoleSuperAdmin:
	default:
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	if err := a.users.SetRole(userID, role); err != nil {
		app.Http500("setting role", w, err)
		return
	}
	http.Redirect(w, r, "/admin/users/", http.StatusSeeOther)
}

// handleDeleteUser deletes a user account.
func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	var userID int64
	if _, err := fmt.Sscan(chi.URLParam(r, "id"), &userID); err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	if u := UserFromContext(r.Context()); u != nil && u.ID == userID {
		http.Error(w, "cannot delete your own account", http.StatusBadRequest)
		return
	}
	target, err := a.users.GetByID(userID)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if target.Role == RoleSuperAdmin {
		http.Error(w, "cannot delete superadmin accounts", http.StatusBadRequest)
		return
	}
	if err := a.users.DeleteUser(userID); err != nil {
		app.Http500("deleting user", w, err)
		return
	}
	http.Redirect(w, r, "/admin/users/", http.StatusSeeOther)
}
