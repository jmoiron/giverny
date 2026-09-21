package mobile

import (
	"net/http"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	notifications, err := a.gauth.Users().RecentNotifications(user.ID, 50)
	if err != nil {
		app.Http500("loading mobile notifications", w, err)
		return
	}
	a.render(w, r, "mobile/notifications.html", mtr.Ctx{
		"title":         "notifications",
		"user":          user,
		"notifications": notifications,
	})
}
