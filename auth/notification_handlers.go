package auth

import (
	"net/http"

	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	notifications, err := a.users.RecentNotifications(user.ID, 50)
	if err != nil {
		app.Http500("loading notifications", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "auth/notifications.html", mtr.Ctx{
		"title":         "notifications",
		"user":          user,
		"notifications": notifications,
	}); err != nil {
		app.Http500("rendering notifications", w, err)
	}
}
