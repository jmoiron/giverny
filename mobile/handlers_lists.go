package mobile

import (
	"net/http"
	"net/url"
	"strconv"

	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/mtr"
)

func (a *App) renderPresetCards(w http.ResponseWriter, r *http.Request, title, path string, preset url.Values) {
	user := gauth.UserFromContext(r.Context())
	ctx, err := a.kanban.PresetCardListPage(r, user, title, path, preset)
	if err != nil {
		app.Http500("loading mobile cards", w, err)
		return
	}
	ctx["user"] = user
	if err := mtr.RegistryFromContext(r.Context()).RenderWithBase(w, "mobile-base", "mobile/cards.html", ctx); err != nil {
		app.Http500("rendering mobile cards", w, err)
	}
}

func (a *App) handleMyTasks(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	a.renderPresetCards(w, r, "my tasks", "/mobile/cards/my-tasks/", url.Values{
		"filter_user":       {strconv.FormatInt(user.ID, 10)},
		"filter_done_state": {"not_done"},
	})
}

func (a *App) handleSubscribedCards(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	a.renderPresetCards(w, r, "subscribed", "/mobile/cards/subscribed/", url.Values{
		"filter_subscribed": {strconv.FormatInt(user.ID, 10)},
		"filter_done_state": {"not_done"},
	})
}

func (a *App) handleInProgressCards(w http.ResponseWriter, r *http.Request) {
	a.renderPresetCards(w, r, "in progress", "/mobile/cards/in-progress/", url.Values{
		"filter_col": {"In Progress"},
	})
}
