package auth

import (
	"encoding/json"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	gconf "github.com/jmoiron/giverny/conf"
	"github.com/jmoiron/monet/app"
	mauth "github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
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
	fss     vfs.Registry
}

func NewApp(dbh db.DB, baseURL string, fss vfs.Registry) *App {
	return &App{
		db:      dbh,
		baseURL: baseURL,
		users:   NewUserProfileService(dbh),
		invites: NewInviteService(dbh),
		fss:     fss,
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
	reg.AddPathFS("auth/settings.html", templates)
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

	r.Route("/user/settings", func(r chi.Router) {
		r.Use(RequireAuth)
		r.Get("/", a.handleUserSettings)
		r.Post("/", a.handleUserSettingsSave)
		r.Post("/avatar-upload", a.handleAvatarUpload)
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

type timezoneOption struct {
	Value string
	Label string
}

var settingsTimezones = []timezoneOption{
	{Value: "UTC", Label: "UTC"},
	{Value: "America/New_York", Label: "America/New_York"},
	{Value: "America/Chicago", Label: "America/Chicago"},
	{Value: "America/Denver", Label: "America/Denver"},
	{Value: "America/Los_Angeles", Label: "America/Los_Angeles"},
	{Value: "America/Phoenix", Label: "America/Phoenix"},
	{Value: "America/Anchorage", Label: "America/Anchorage"},
	{Value: "Pacific/Honolulu", Label: "Pacific/Honolulu"},
	{Value: "America/Toronto", Label: "America/Toronto"},
	{Value: "America/Vancouver", Label: "America/Vancouver"},
	{Value: "America/Mexico_City", Label: "America/Mexico_City"},
	{Value: "America/Sao_Paulo", Label: "America/Sao_Paulo"},
	{Value: "Europe/London", Label: "Europe/London"},
	{Value: "Europe/Dublin", Label: "Europe/Dublin"},
	{Value: "Europe/Paris", Label: "Europe/Paris"},
	{Value: "Europe/Berlin", Label: "Europe/Berlin"},
	{Value: "Europe/Madrid", Label: "Europe/Madrid"},
	{Value: "Europe/Rome", Label: "Europe/Rome"},
	{Value: "Europe/Warsaw", Label: "Europe/Warsaw"},
	{Value: "Europe/Helsinki", Label: "Europe/Helsinki"},
	{Value: "Europe/Moscow", Label: "Europe/Moscow"},
	{Value: "Africa/Johannesburg", Label: "Africa/Johannesburg"},
	{Value: "Asia/Dubai", Label: "Asia/Dubai"},
	{Value: "Asia/Kolkata", Label: "Asia/Kolkata"},
	{Value: "Asia/Bangkok", Label: "Asia/Bangkok"},
	{Value: "Asia/Singapore", Label: "Asia/Singapore"},
	{Value: "Asia/Hong_Kong", Label: "Asia/Hong_Kong"},
	{Value: "Asia/Tokyo", Label: "Asia/Tokyo"},
	{Value: "Asia/Seoul", Label: "Asia/Seoul"},
	{Value: "Australia/Perth", Label: "Australia/Perth"},
	{Value: "Australia/Sydney", Label: "Australia/Sydney"},
	{Value: "Pacific/Auckland", Label: "Pacific/Auckland"},
}

func validTimezone(tz string) bool {
	if tz == "" {
		return false
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

func (a *App) renderUserSettings(w http.ResponseWriter, r *http.Request, user *User, errMsg string, saved bool) {
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "auth/settings.html", mtr.Ctx{
		"title":     "settings",
		"user":      user,
		"timezones": settingsTimezones,
		"saved":     saved,
		"error":     errMsg,
	}); err != nil {
		app.Http500("rendering settings", w, err)
	}
}

func (a *App) handleUserSettings(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	a.renderUserSettings(w, r, user, "", false)
}

func (a *App) handleUserSettingsSave(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	wantsJSON := strings.Contains(r.Header.Get("Accept"), "application/json")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	avatarChanged := r.FormValue("profile_image_uri_present") == "1"
	timezoneChanged := r.FormValue("timezone_present") == "1"
	autoAssignChanged := r.FormValue("auto_assign_cards_present") == "1"
	avatarURI := user.ProfileImageURI
	if avatarChanged {
		avatarURI = strings.TrimSpace(r.FormValue("profile_image_uri"))
	}
	timezone := user.Timezone
	if timezoneChanged {
		timezone = strings.TrimSpace(r.FormValue("timezone"))
		if timezone == "" {
			timezone = "UTC"
		}
	}
	if !validTimezone(timezone) {
		updated, err := a.users.GetByID(user.ID)
		if err != nil {
			app.Http500("loading user settings", w, err)
			return
		}
		updated.ProfileImageURI = avatarURI
		updated.Timezone = timezone
		updated.AutoAssignCards = r.FormValue("auto_assign_cards") != ""
		if wantsJSON {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":    false,
				"error": "invalid timezone",
			})
			return
		}
		a.renderUserSettings(w, r, updated, "invalid timezone", false)
		return
	}
	autoAssign := user.AutoAssignCards
	if autoAssignChanged {
		autoAssign = r.FormValue("auto_assign_cards") != ""
	}
	if err := a.users.UpdateSettings(user.ID, avatarURI, timezone, autoAssign); err != nil {
		app.Http500("saving user settings", w, err)
		return
	}
	if wantsJSON {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"profile_image_uri": avatarURI,
			"timezone":          timezone,
			"auto_assign_cards": autoAssign,
		})
		return
	}
	http.Redirect(w, r, "/user/settings/?saved=1", http.StatusSeeOther)
}

func (a *App) handleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	cfg := gconf.ConfigFromContext(r.Context())
	if a.fss == nil {
		http.Error(w, "avatar storage is not configured", http.StatusInternalServerError)
		return
	}
	uploader, err := a.fss.CreateUploader("avatars")
	if err != nil {
		http.Error(w, "avatar storage is not writable", http.StatusInternalServerError)
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		http.Error(w, "failed to parse upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 8<<20))
	if err != nil {
		http.Error(w, "failed to read upload", http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "empty upload", http.StatusBadRequest)
		return
	}
	contentType := http.DetectContentType(data[:min(len(data), 512)])
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "only image uploads are supported", http.StatusBadRequest)
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
	default:
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			http.Error(w, "unsupported image type", http.StatusBadRequest)
			return
		}
	}

	filename := fmt.Sprintf("user-%d-%d%s", user.ID, time.Now().UnixNano(), ext)
	basePath, err := a.fss.GetPath("avatars")
	if err != nil {
		http.Error(w, "avatar storage path missing", http.StatusInternalServerError)
		return
	}
	destPath := filepath.Join(basePath, filename)
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		http.Error(w, "failed to write avatar", http.StatusInternalServerError)
		return
	}
	fileURL := uploader.GetFileURL(filename)
	if err := a.users.SetProfileImageURI(user.ID, fileURL); err != nil {
		_ = os.Remove(destPath)
		http.Error(w, "failed to save avatar", http.StatusInternalServerError)
		return
	}

	// Remove the previous uploaded avatar file if it lived in the avatars filesystem.
	if prefix := strings.TrimRight(cfg.FSS.URLs["avatars"], "/"); prefix != "" {
		previous := strings.TrimSpace(user.ProfileImageURI)
		if previous != "" && strings.HasPrefix(previous, prefix+"/") {
			oldFilename := strings.TrimPrefix(previous, prefix+"/")
			if oldFilename != filename {
				_ = os.Remove(filepath.Join(basePath, filepath.Base(oldFilename)))
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":  true,
		"url": fileURL,
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
