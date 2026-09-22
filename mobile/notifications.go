package mobile

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

func (a *App) handleNotifications(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	page := 1
	if parsed, parseErr := strconv.Atoi(r.URL.Query().Get("page")); parseErr == nil && parsed > 0 {
		page = parsed
	}
	const pageSize = 20
	notifications, err := a.kanban.Notifications().RecentForUserPage(user.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		app.Http500("loading mobile notifications", w, err)
		return
	}
	total, err := a.kanban.Notifications().CountForUser(user.ID)
	if err != nil {
		app.Http500("counting mobile notifications", w, err)
		return
	}
	a.render(w, r, "mobile/notifications.html", mtr.Ctx{
		"title":         "notifications",
		"user":          user,
		"notifications": notifications,
		"page":          page,
		"previousPage":  page - 1,
		"nextPage":      page + 1,
		"hasPrevious":   page > 1,
		"hasNext":       page*pageSize < total,
	})
}

func (a *App) handleNotificationFragment(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	notificationID, err := strconv.ParseInt(chi.URLParam(r, "notificationID"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	notification, err := a.kanban.Notifications().GetForUser(user.ID, notificationID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctx := mtr.Ctx{
		"ID": notification.ID, "Type": notification.Type, "URL": notification.URL,
		"CreatedAt": notification.CreatedAt, "ReadAt": notification.ReadAt,
		"OldTitle": notification.OldTitle, "NewTitle": notification.NewTitle,
		"OldContent": notification.OldContent, "NewContent": notification.NewContent,
		"TitleDiff": notification.TitleDiff, "ContentDiff": notification.ContentDiff,
		"Card": notification.Card, "Board": notification.Board, "Actor": notification.Actor,
	}
	if err := mtr.RegistryFromContext(r.Context()).Render(w, "mobile/notification_item.html", ctx); err != nil {
		app.Http500("rendering mobile notification", w, err)
	}
}
