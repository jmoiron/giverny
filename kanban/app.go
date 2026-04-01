package kanban

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	gauth "github.com/jmoiron/giverny/auth"
	gconf "github.com/jmoiron/giverny/conf"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/vfs"
)

//go:embed kanban/*.html
var templates embed.FS

// App is the kanban sub-application.
type App struct {
	db       db.DB
	hub      *Hub
	boards   *BoardService
	columns  *ColumnService
	cards    *CardService
	labels   *LabelService
	comments *CommentService
	users    *gauth.UserProfileService
	views    *ViewService
	fss      vfs.Registry
}

func NewApp(dbh db.DB, fss vfs.Registry) *App {
	hub := NewHub()
	go hub.Run()
	return &App{
		db:       dbh,
		hub:      hub,
		boards:   NewBoardService(dbh),
		columns:  NewColumnService(dbh),
		cards:    NewCardService(dbh),
		labels:   NewLabelService(dbh),
		comments: NewCommentService(dbh),
		users:    gauth.NewUserProfileService(dbh),
		views:    NewViewService(dbh),
		fss:      fss,
	}
}

// CardListRow is the view model for a single row in the card list view.
type CardListRow struct {
	*DashboardCard
	CreatedAtDisplay string
	UpdatedAtDisplay string
	DueDateDisplay   string
	StartDateDisplay string
}

// CommentDisplay wraps CommentWithAuthor with pre-rendered HTML for template use.
type CommentDisplay struct {
	*CommentWithAuthor
	BodyHTML         template.HTML
	CreatedAtDisplay string
}

func (a *App) Name() string { return "kanban" }

func (a *App) RecentBoards(limit int, user *gauth.User) ([]*Board, error) {
	return a.boards.RecentByCardActivity(limit, user != nil && user.IsAdmin())
}

func (a *App) PublicBoards() ([]*Board, error) {
	return a.boards.ListPublic()
}

func (a *App) RecentCards(limit int, user *gauth.User) ([]*DashboardCard, error) {
	return a.cards.Recent(limit, user != nil && user.IsAdmin())
}

func (a *App) RenderDashboardCards(r *http.Request, cards []*DashboardCard) ([]*RenderedDashboardCard, error) {
	return a.renderDashboardCards(r, cards)
}

func (a *App) repairLegacyCardViewSchema() error {
	var tableCount int
	if err := a.db.Get(&tableCount, `SELECT COUNT(1) FROM sqlite_master WHERE type = 'table' AND name = 'card_view'`); err != nil {
		return err
	}
	if tableCount == 0 {
		return nil
	}

	type pragmaColumn struct {
		CID        int            `db:"cid"`
		Name       string         `db:"name"`
		Type       string         `db:"type"`
		NotNull    int            `db:"notnull"`
		Default    sql.NullString `db:"dflt_value"`
		PrimaryKey int            `db:"pk"`
	}
	var cols []pragmaColumn
	if err := a.db.Select(&cols, `PRAGMA table_info(card_view)`); err != nil {
		return err
	}
	hasSlug := false
	hasDescription := false
	for _, col := range cols {
		switch col.Name {
		case "slug":
			hasSlug = true
		case "description":
			hasDescription = true
		}
	}
	if !hasSlug {
		if _, err := a.db.Exec(`ALTER TABLE card_view ADD COLUMN slug TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !hasDescription {
		if _, err := a.db.Exec(`ALTER TABLE card_view ADD COLUMN description TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !hasSlug {
		if _, err := a.db.Exec(`UPDATE card_view SET slug = 'view-' || id WHERE slug = ''`); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Migrate() error {
	if err := a.repairLegacyCardViewSchema(); err != nil {
		return fmt.Errorf("repair legacy card_view schema: %w", err)
	}
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	sets := []monarch.Set{
		BoardMigrations,
		ColumnMigrations,
		CardMigrations,
		CardAssigneeMigrations,
		LabelMigrations,
		ChecklistMigrations,
		CommentMigrations,
		AttachmentMigrations,
		ActivityMigrations,
		SubscriptionMigrations,
		CardFTSMigrations,
		CommentFTSMigrations,
		CardViewMigrations,
	}
	for _, s := range sets {
		if err := m.Upgrade(s); err != nil {
			return fmt.Errorf("%s: %w", s.Name, err)
		}
	}
	return nil
}

func (a *App) Register(reg *mtr.Registry) {
	reg.AddPathFS("kanban/board_list.html", templates)
	reg.AddPathFS("kanban/board.html", templates)
	reg.AddPathFS("kanban/board_edit.html", templates)
	reg.AddPathFS("kanban/card.html", templates)
	reg.AddPathFS("kanban/card_snippet.html", templates)
	reg.AddPathFS("kanban/card_modal_shell.html", templates)
	reg.AddPathFS("kanban/labels.html", templates)
	reg.AddPathFS("kanban/card_list.html", templates)
	reg.AddPathFS("kanban/view_list.html", templates)
}

func (a *App) GetAdmin() (app.Admin, error) { return nil, nil }

func (a *App) Bind(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		r.Use(gauth.RequireAuth)
		r.Get("/nav-boards/", a.handleNavBoards)
		r.Get("/nav-views/", a.handleNavViews)
		r.Post("/views/", a.handleSaveView)
		r.Post("/views/{viewID}/edit", a.handleEditView)
		r.Post("/views/{viewID}/delete", a.handleDeleteView)
	})

	r.Route("/cards", func(r chi.Router) {
		r.Use(gauth.RequireAuth)
		r.Get("/", a.handleCardList)
		r.Get("/my-tasks/", a.handleMyTasks)
		r.Get("/subscribed/", a.handleSubscribedCards)
		r.Get("/in-progress/", a.handleInProgressCards)
		r.Get("/views/", a.handleViewList)
		r.Get("/views/{slug}/", a.handleViewDetail)
		r.Get("/{cardID}/", a.handleGetCardByID)
	})

	r.Route("/labels", func(r chi.Router) {
		r.Use(gauth.RequireAuth)
		r.Get("/", a.handleLabelListPage)
		r.Post("/", a.handleCreateLabelPage)
		r.Post("/quick", a.handleCreateLabel)
		r.Post("/{labelID}/color", a.handleUpdateLabelColor)
		r.Post("/{labelID}/edit", a.handleUpdateLabelPage)
		r.Post("/{labelID}/delete", a.handleDeleteLabelPage)
	})

	r.Route("/boards", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(gauth.RequireAuth)
			r.Get("/", a.handleBoardList)
			r.Post("/", a.handleCreateBoard)
		})

		r.Route("/{slug}", func(r chi.Router) {
			r.Get("/", a.handleBoardDetail)
			r.Get("/ws", a.handleWS)

			r.Group(func(r chi.Router) {
				r.Use(a.requireBoardWrite)
				r.Get("/edit", a.handleBoardEditForm)
				r.Post("/edit", a.handleBoardEditSubmit)
				r.Post("/delete", a.handleDeleteBoard)

				r.Route("/columns", func(r chi.Router) {
					r.Post("/", a.handleCreateColumn)
					r.Post("/reorder", a.handleReorderColumns)
					r.Post("/{colID}/edit", a.handleEditColumn)
					r.Post("/{colID}/delete", a.handleDeleteColumn)
				})

				r.Route("/columns/{colID}/cards", func(r chi.Router) {
					r.Post("/", a.handleCreateCard)
					r.Post("/reorder", a.handleReorderCards)
				})

				r.Post("/cards/archived/delete", a.handleDeleteArchivedCards)
			})

			r.Route("/cards/{cardID}", func(r chi.Router) {
				r.Get("/", a.handleGetCard)

				r.Group(func(r chi.Router) {
					r.Use(a.requireBoardWrite)
					r.Post("/", a.handleUpdateCard)
					r.Post("/done", a.handleMarkDone)
					r.Post("/subscribe", a.handleToggleSubscription)
					r.Post("/color", a.handleSetCardColor)
					r.Post("/assign", a.handleSetCardAssignee)
					r.Post("/assignees/{userID}/delete", a.handleRemoveCardAssignee)
					r.Post("/start-date", a.handleSetCardStartDate)
					r.Post("/due-date", a.handleSetCardDueDate)
					r.Post("/checklist/items", a.handleAddChecklistItem)
					r.Post("/checklist/items/{itemID}/done", a.handleSetChecklistItemDone)
					r.Post("/checklist/items/{itemID}/delete", a.handleDeleteChecklistItem)
					r.Post("/checklist/reorder", a.handleReorderChecklistItems)
					r.Post("/checklist/delete", a.handleDeleteChecklist)
					r.Post("/attachments", a.handleUploadAttachment)
					r.Post("/attachments/{attachmentID}/rename", a.handleRenameAttachment)
					r.Post("/attachments/{attachmentID}/delete", a.handleDeleteAttachment)
					r.Post("/labels", a.handleAddCardLabel)
					r.Post("/labels/{labelID}/delete", a.handleRemoveCardLabel)
					r.Post("/comments", a.handleCreateComment)
					r.Post("/comments/{commentID}/edit", a.handleEditComment)
					r.Post("/comments/{commentID}/delete", a.handleDeleteComment)
					r.Post("/move", a.handleMoveCard)
					r.Post("/archive", a.handleArchiveCard)
					r.Post("/delete", a.handleDeleteCard)
					r.Post("/unarchive", a.handleUnarchiveCard)
				})
			})
		})
	})
}

// handleCardList renders the cross-board card list view with sortable columns and filters.
func (a *App) buildCardListPage(r *http.Request, user *gauth.User, q url.Values, listPath string, activeView *CardView) (mtr.Ctx, error) {
	sortCol := q.Get("sort")
	sortDir := q.Get("dir")
	validSorts := map[string]bool{"title": true, "board": true, "created": true, "updated": true, "due": true, "column": true, "done": true, "start": true}
	if !validSorts[sortCol] {
		sortCol = "updated"
	}
	if sortDir != "asc" {
		sortDir = "desc"
	}

	parseInt := func(s string) int64 { v, _ := strconv.ParseInt(s, 10, 64); return v }
	parseIDs := func(values []string) []int64 {
		ids := make([]int64, 0, len(values))
		for _, raw := range values {
			if id := parseInt(raw); id > 0 {
				ids = append(ids, id)
			}
		}
		return ids
	}
	validState := func(value string, allowed ...string) string {
		for _, candidate := range allowed {
			if value == candidate {
				return value
			}
		}
		return "all"
	}
	rawAssigned := strings.TrimSpace(q.Get("filter_user"))
	rawSubscribed := strings.TrimSpace(q.Get("filter_subscribed"))
	filter := CardListFilter{
		Query:           q.Get("q"),
		BoardID:         parseInt(q.Get("filter_board")),
		LabelIDs:        parseIDs(q["filter_label"]),
		LabelMatchAll:   q.Get("filter_label_match") != "any",
		UserID:          parseInt(rawAssigned),
		UserUnassigned:  rawAssigned == "__unassigned__",
		SubUserID:       parseInt(rawSubscribed),
		SubUnsubscribed: rawSubscribed == "__unsubscribed__",
		ColumnName:      strings.TrimSpace(q.Get("filter_col")),
		DoneState:       validState(q.Get("filter_done_state"), "all", "done", "not_done"),
	}
	filterActive := filter.Query != "" || filter.BoardID != 0 || len(filter.LabelIDs) > 0 ||
		filter.UserID != 0 || filter.UserUnassigned || filter.SubUserID != 0 || filter.SubUnsubscribed ||
		filter.ColumnName != "" || filter.DoneState != "all"

	cards, err := a.cards.ListAll(user.IsAdmin(), sortCol, sortDir, filter, user.ID)
	if err != nil {
		return nil, err
	}

	rows := make([]*CardListRow, 0, len(cards))
	for _, c := range cards {
		row := &CardListRow{
			DashboardCard:    c,
			CreatedAtDisplay: formatTimestampForUser(r.Context(), c.CreatedAt),
			UpdatedAtDisplay: formatTimestampForUser(r.Context(), c.UpdatedAt),
		}
		if c.DueDate != nil {
			row.DueDateDisplay = formatTimestampForUser(r.Context(), *c.DueDate)
		}
		if c.StartDate != nil {
			row.StartDateDisplay = formatTimestampForUser(r.Context(), *c.StartDate)
		}
		rows = append(rows, row)
	}

	// Build sort link URLs that preserve all current filter/cols params.
	flipDir := func(col string) string {
		if sortCol == col && sortDir == "asc" {
			return "desc"
		}
		if sortCol == col {
			return "asc"
		}
		return "desc"
	}
	baseQ := url.Values{}
	for k, v := range q {
		if k != "sort" && k != "dir" {
			baseQ[k] = v
		}
	}
	sortURL := func(col string) template.URL {
		sq := url.Values{}
		for k, v := range baseQ {
			sq[k] = v
		}
		sq.Set("sort", col)
		sq.Set("dir", flipDir(col))
		return template.URL(listPath + "?" + sq.Encode())
	}
	sortURLs := map[string]template.URL{
		"title":   sortURL("title"),
		"board":   sortURL("board"),
		"created": sortURL("created"),
		"updated": sortURL("updated"),
		"due":     sortURL("due"),
		"column":  sortURL("column"),
		"done":    sortURL("done"),
		"start":   sortURL("start"),
	}
	// Sort direction indicators for current sort column.
	sortDirs := map[string]string{
		"title": flipDir("title"), "board": flipDir("board"),
		"created": flipDir("created"), "updated": flipDir("updated"), "due": flipDir("due"),
		"column": flipDir("column"), "done": flipDir("done"), "start": flipDir("start"),
	}

	// Clear URL preserves sort but removes all filters.
	clearURL := template.URL(listPath + "?sort=" + sortCol + "&dir=" + sortDir)

	// Fetch filter dropdown data.
	allUsers, _ := a.users.List()
	allLabels, _ := a.labels.List()
	allBoards, _ := a.boards.List(user.IsAdmin())
	allColumns, _ := a.columns.ListAllWithBoards(user.IsAdmin())
	cardModalShell, err := a.renderCardModalShell(r)
	if err != nil {
		return nil, err
	}
	pageTitle := "cards"
	if activeView != nil && strings.TrimSpace(activeView.Name) != "" {
		pageTitle = activeView.Name
	}
	filterPanelOpen := filterActive && activeView == nil
	return mtr.Ctx{
		"title":                 pageTitle,
		"user":                  user,
		"pageTitle":             pageTitle,
		"cards":                 rows,
		"sort":                  sortCol,
		"dir":                   sortDir,
		"sortURLs":              sortURLs,
		"sortDirs":              sortDirs,
		"clearURL":              clearURL,
		"filterActive":          filterActive,
		"filterPanelOpen":       filterPanelOpen,
		"listPath":              listPath,
		"filterBoard":           filter.BoardID,
		"filterLabels":          filter.LabelIDs,
		"filterLabelMatchAll":   filter.LabelMatchAll,
		"filterUser":            filter.UserID,
		"filterUserUnassigned":  filter.UserUnassigned,
		"filterSub":             filter.SubUserID,
		"filterSubUnsubscribed": filter.SubUnsubscribed,
		"filterCol":             filter.ColumnName,
		"filterQuery":           filter.Query,
		"filterDoneState":       filter.DoneState,
		"users":                 allUsers,
		"labels":                allLabels,
		"boards":                allBoards,
		"columns":               allColumns,
		"cardModalShell":        cardModalShell,
		"currentQueryString":    q.Encode(),
		"activeView":            activeView,
	}, nil
}

func (a *App) handleCardList(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	ctx, err := a.buildCardListPage(r, user, r.URL.Query(), "/cards/", nil)
	if err != nil {
		app.Http500("listing cards", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/card_list.html", ctx); err != nil {
		app.Http500("rendering card list", w, err)
	}
}

func mergeQueryValues(base, override url.Values) url.Values {
	merged := url.Values{}
	for k, v := range base {
		cp := make([]string, len(v))
		copy(cp, v)
		merged[k] = cp
	}
	for k, v := range override {
		cp := make([]string, len(v))
		copy(cp, v)
		merged[k] = cp
	}
	return merged
}

func (a *App) renderPresetCardList(w http.ResponseWriter, r *http.Request, title, path string, preset url.Values) {
	user := gauth.UserFromContext(r.Context())
	q := mergeQueryValues(preset, r.URL.Query())
	ctx, err := a.buildCardListPage(r, user, q, path, nil)
	if err != nil {
		app.Http500("listing cards", w, err)
		return
	}
	ctx["title"] = title
	ctx["pageTitle"] = title
	ctx["filterPanelOpen"] = false
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/card_list.html", ctx); err != nil {
		app.Http500("rendering card list", w, err)
	}
}

func (a *App) handleMyTasks(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	a.renderPresetCardList(w, r, "my tasks", "/cards/my-tasks/", url.Values{
		"filter_user":       {strconv.FormatInt(user.ID, 10)},
		"filter_done_state": {"not_done"},
	})
}

func (a *App) handleSubscribedCards(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	a.renderPresetCardList(w, r, "subscribed", "/cards/subscribed/", url.Values{
		"filter_subscribed": {strconv.FormatInt(user.ID, 10)},
		"filter_done_state": {"not_done"},
	})
}

func (a *App) handleInProgressCards(w http.ResponseWriter, r *http.Request) {
	a.renderPresetCardList(w, r, "in progress", "/cards/in-progress/", url.Values{
		"filter_col": {"In Progress"},
	})
}

func (a *App) handleViewList(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	views, err := a.views.ListForUser(user.ID, 0)
	if err != nil {
		app.Http500("loading views", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/view_list.html", mtr.Ctx{
		"title": "views",
		"user":  user,
		"views": views,
	}); err != nil {
		app.Http500("rendering view list", w, err)
	}
}

func (a *App) handleViewDetail(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	view, err := a.views.GetBySlugForUser(chi.URLParam(r, "slug"), user.ID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	baseQ, err := url.ParseQuery(view.QueryString)
	if err != nil {
		baseQ = url.Values{}
	}
	q := mergeQueryValues(baseQ, r.URL.Query())
	ctx, err := a.buildCardListPage(r, user, q, fmt.Sprintf("/cards/views/%s/", view.Slug), view)
	if err != nil {
		app.Http500("loading view cards", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/card_list.html", ctx); err != nil {
		app.Http500("rendering view detail", w, err)
	}
}

// handleNavViews returns the user's saved views as JSON for the side nav.
func (a *App) handleNavViews(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	views, err := a.views.ListForUser(user.ID, 10)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading views")
		return
	}
	type viewItem struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		QueryString string `json:"qs"`
	}
	items := make([]viewItem, 0, len(views))
	for _, v := range views {
		items = append(items, viewItem{ID: v.ID, Name: v.Name, Slug: v.Slug, Description: v.Description, QueryString: v.QueryString})
	}
	writeJSON(w, http.StatusOK, items)
}

// handleSaveView creates a new saved view for the current user.
func (a *App) handleSaveView(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	queryString := r.FormValue("query_string")
	if name == "" {
		apiErr(w, http.StatusBadRequest, "name required")
		return
	}
	view, err := a.views.Create(user.ID, name, description, queryString)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "saving view")
		return
	}
	type viewItem struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		QueryString string `json:"qs"`
		CreatedAt   string `json:"created_at"`
	}
	writeJSON(w, http.StatusOK, viewItem{
		ID:          view.ID,
		Name:        view.Name,
		Slug:        view.Slug,
		Description: view.Description,
		QueryString: view.QueryString,
		CreatedAt:   formatTimestampForUser(r.Context(), view.CreatedAt),
	})
}

func (a *App) handleEditView(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	viewID, err := strconv.ParseInt(chi.URLParam(r, "viewID"), 10, 64)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid view id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "invalid form")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	description := strings.TrimSpace(r.FormValue("description"))
	queryString := r.FormValue("query_string")
	if name == "" {
		apiErr(w, http.StatusBadRequest, "name required")
		return
	}
	view, err := a.views.Update(viewID, user.ID, name, description, queryString)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "saving view")
		return
	}
	type viewItem struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		QueryString string `json:"qs"`
		CreatedAt   string `json:"created_at"`
	}
	writeJSON(w, http.StatusOK, viewItem{
		ID:          view.ID,
		Name:        view.Name,
		Slug:        view.Slug,
		Description: view.Description,
		QueryString: view.QueryString,
		CreatedAt:   formatTimestampForUser(r.Context(), view.CreatedAt),
	})
}

// handleDeleteView deletes a saved view owned by the current user.
func (a *App) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	viewID, err := strconv.ParseInt(chi.URLParam(r, "viewID"), 10, 64)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid view id")
		return
	}
	if err := a.views.Delete(viewID, user.ID); err != nil {
		apiErr(w, http.StatusInternalServerError, "deleting view")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleNavBoards returns a JSON list of up to 8 boards for the side nav,
// ordered by most recent card activity with user-assigned boards first.
func (a *App) handleNavBoards(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	boards, err := a.boards.NavBoards(user.ID, user.IsAdmin())
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading boards")
		return
	}
	type boardItem struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	items := make([]boardItem, 0, len(boards))
	for _, b := range boards {
		items = append(items, boardItem{Name: b.Name, Slug: b.Slug})
	}
	writeJSON(w, http.StatusOK, items)
}

// handleWS upgrades the request to a WebSocket connection and registers the
// client with the hub for the given board slug.
func (a *App) handleWS(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !canViewBoard(board, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		slog.Error("websocket accept", "err", err)
		return
	}
	defer conn.CloseNow()

	client := &Client{
		board: slug,
		send:  make(chan []byte, 64),
		conn:  conn,
	}
	a.hub.Register(client)
	defer a.hub.Unregister(client)

	ctx := r.Context()
	go client.writePump(ctx)

	// Ping the client periodically to keep the connection alive and detect drops.
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.Ping(ctx); err != nil {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Drain incoming messages. coder/websocket requires continuous reads to
	// process internal protocol frames (pong, close, etc.).
	conn.SetReadLimit(512)
	for {
		if _, _, err := conn.Read(ctx); err != nil {
			return
		}
	}
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writing json response", "err", err)
	}
}

func apiErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (a *App) publishBoardEvent(board, eventType string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		slog.Error("marshal websocket event payload", "type", eventType, "board", board, "err", err)
		return
	}
	a.hub.Publish(Event{
		Type:    eventType,
		Board:   board,
		Payload: data,
	})
}

func parseID(s string) (int64, error) {
	var id int64
	_, err := fmt.Sscan(s, &id)
	return id, err
}

func parseOptionalDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeUploadFilename(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

func mimeExtension(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain; charset=utf-8", "text/plain":
		return ".txt"
	default:
		return ""
	}
}

func userLocation(ctx context.Context) *time.Location {
	u := gauth.UserFromContext(ctx)
	if u != nil && strings.TrimSpace(u.Timezone) != "" {
		if loc, err := time.LoadLocation(strings.TrimSpace(u.Timezone)); err == nil {
			return loc
		}
	}
	return time.UTC
}

func formatTimestampForUser(ctx context.Context, t time.Time) string {
	return t.In(userLocation(ctx)).Format("15:04 Jan 2")
}

func (a *App) renderCardSnippet(r *http.Request, card *Card) (string, error) {
	return a.renderCardSnippetWithOptions(r, card, true)
}

func (a *App) renderCardModalShell(r *http.Request) (template.HTML, error) {
	reg := mtr.RegistryFromContext(r.Context())
	var buf bytes.Buffer
	if err := reg.Render(&buf, "kanban/card_modal_shell.html", nil); err != nil {
		return "", err
	}
	return template.HTML(buf.String()), nil
}

func (a *App) renderCardSnippetWithOptions(r *http.Request, card *Card, draggable bool) (string, error) {
	reg := mtr.RegistryFromContext(r.Context())
	var buf bytes.Buffer
	if err := reg.Render(&buf, "kanban/card_snippet.html", mtr.Ctx{
		"card":      card,
		"draggable": draggable,
	}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type RenderedCard struct {
	Card *Card
	HTML template.HTML
}

type ColumnWithRenderedCards struct {
	*Column
	Cards []*RenderedCard
}

type RenderedDashboardCard struct {
	*DashboardCard
	HTML template.HTML
}

func (a *App) renderCards(r *http.Request, cards []*Card, draggable bool) ([]*RenderedCard, error) {
	rendered := make([]*RenderedCard, 0, len(cards))
	for _, card := range cards {
		html, err := a.renderCardSnippetWithOptions(r, card, draggable)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, &RenderedCard{
			Card: card,
			HTML: template.HTML(html),
		})
	}
	return rendered, nil
}

func (a *App) buildRenderedColumns(r *http.Request, cols []*Column, cards []*Card, draggable bool) ([]*ColumnWithRenderedCards, error) {
	cardsByCol := make(map[int64][]*Card)
	for _, c := range cards {
		cardsByCol[c.ColumnID] = append(cardsByCol[c.ColumnID], c)
	}
	result := make([]*ColumnWithRenderedCards, len(cols))
	for i, col := range cols {
		renderedCards, err := a.renderCards(r, cardsByCol[col.ID], draggable)
		if err != nil {
			return nil, err
		}
		result[i] = &ColumnWithRenderedCards{
			Column: col,
			Cards:  renderedCards,
		}
	}
	return result, nil
}

func (a *App) renderDashboardCards(r *http.Request, cards []*DashboardCard) ([]*RenderedDashboardCard, error) {
	rendered := make([]*RenderedDashboardCard, 0, len(cards))
	for _, card := range cards {
		html, err := a.renderCardSnippetWithOptions(r, &card.Card, false)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, &RenderedDashboardCard{
			DashboardCard: card,
			HTML:          template.HTML(html),
		})
	}
	return rendered, nil
}

func (a *App) cardResponse(r *http.Request, card *Card) map[string]any {
	html, err := a.renderCardSnippet(r, card)
	if err != nil {
		slog.Error("rendering card snippet", "err", err)
	}
	assignees := make([]map[string]any, 0, len(card.Assignees))
	for _, assignee := range card.Assignees {
		assignees = append(assignees, map[string]any{
			"id":                assignee.ID,
			"username":          assignee.Username,
			"profile_image_uri": assignee.ProfileImageURI,
		})
	}
	var startDateValue, dueDateValue string
	if card.StartDate != nil {
		startDateValue = card.StartDate.Format("2006-01-02")
	}
	if card.DueDate != nil {
		dueDateValue = card.DueDate.Format("2006-01-02")
	}
	return map[string]any{
		"ok":                 true,
		"id":                 card.ID,
		"title":              card.Title,
		"content":            card.Content,
		"content_rendered":   card.ContentRendered,
		"color":              card.Color,
		"column_id":          card.ColumnID,
		"assignees":          assignees,
		"start_date":         card.StartDate,
		"start_date_value":   startDateValue,
		"due_date":           card.DueDate,
		"due_date_value":     dueDateValue,
		"updated_at_value":   card.UpdatedAt.Format(time.RFC3339),
		"updated_at_display": formatTimestampForUser(r.Context(), card.UpdatedAt),
		"checklist":          a.cardChecklistPayload(card.ID, card.Checklist),
		"attachments":        attachmentResponseData(card.Attachments),
		"html":               html,
	}
}

func (a *App) cardDateUpdatedPayload(card *Card) CardDateUpdatedPayload {
	var startDateValue, dueDateValue string
	if card.StartDate != nil {
		startDateValue = card.StartDate.Format("2006-01-02")
	}
	if card.DueDate != nil {
		dueDateValue = card.DueDate.Format("2006-01-02")
	}
	return CardDateUpdatedPayload{
		CardID:           card.ID,
		StartDateValue:   startDateValue,
		DueDateValue:     dueDateValue,
		UpdatedAtValue:   card.UpdatedAt.Format(time.RFC3339),
		UpdatedAtDisplay: "",
	}
}

func (a *App) cardChecklistPayload(cardID int64, checklist *Checklist) CardChecklistUpdatedPayload {
	payload := CardChecklistUpdatedPayload{
		CardID: cardID,
		Exists: checklist != nil,
	}
	if checklist == nil {
		return payload
	}
	payload.ChecklistID = checklist.ID
	payload.Items = make([]ChecklistItemPayload, 0, len(checklist.Items))
	for _, item := range checklist.Items {
		if item == nil {
			continue
		}
		payload.Items = append(payload.Items, ChecklistItemPayload{
			ID:       item.ID,
			Text:     item.Text,
			Done:     item.Done,
			Position: item.Position,
		})
		if item.Done {
			payload.CompletedCount++
		}
	}
	payload.TotalCount = len(payload.Items)
	if payload.TotalCount > 0 {
		payload.PercentComplete = int(float64(payload.CompletedCount) / float64(payload.TotalCount) * 100)
	}
	return payload
}

func attachmentResponseData(attachments []*Attachment) []map[string]any {
	data := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		data = append(data, map[string]any{
			"id":         attachment.ID,
			"filename":   attachment.Filename,
			"filepath":   attachment.Filepath,
			"mime_type":  attachment.MimeType,
			"icon_class": attachment.IconClass,
		})
	}
	return data
}

func attachmentEventPayload(attachments []*Attachment) []EventAttachmentPayload {
	data := make([]EventAttachmentPayload, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment == nil {
			continue
		}
		data = append(data, EventAttachmentPayload{
			ID:        attachment.ID,
			Filename:  attachment.Filename,
			Filepath:  attachment.Filepath,
			MimeType:  attachment.MimeType,
			IconClass: attachment.IconClass,
		})
	}
	return data
}

func (a *App) cardAttachmentsUpdatedPayload(r *http.Request, card *Card) CardAttachmentsUpdatedPayload {
	return CardAttachmentsUpdatedPayload{
		CardID:           card.ID,
		Attachments:      attachmentEventPayload(card.Attachments),
		UpdatedAtValue:   card.UpdatedAt.Format(time.RFC3339),
		UpdatedAtDisplay: formatTimestampForUser(r.Context(), card.UpdatedAt),
	}
}

func canViewBoard(board *Board, user *gauth.User) bool {
	switch board.Visibility {
	case VisibilityPublic:
		return true
	case VisibilityOpen:
		return user != nil
	case VisibilityPrivate:
		return user != nil && user.IsAdmin()
	}
	return false
}

// Board access policy:
// - superadmins can view and modify every board and can also manage install-wide settings elsewhere
// - admins can view and modify every board, but not install-wide settings
// - readonly/normal authenticated users can view public and open boards, and can modify public boards only
// - anonymous users can view public boards only and cannot modify any board
func canModifyBoard(board *Board, user *gauth.User) bool {
	if user == nil {
		return false
	}
	if user.IsAdmin() {
		return true
	}
	return board.Visibility == VisibilityPublic
}

func (a *App) requireBoardWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := gauth.UserFromContext(r.Context())
		board, err := a.boards.GetBySlug(chi.URLParam(r, "slug"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if !canModifyBoard(board, user) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Handlers ---

func (a *App) handleBoardList(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	boards, err := a.boards.List(user.IsAdmin())
	if err != nil {
		app.Http500("listing boards", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/board_list.html", mtr.Ctx{
		"title":   "boards",
		"boards":  boards,
		"user":    user,
		"isAdmin": user.IsAdmin(),
	}); err != nil {
		app.Http500("rendering board list", w, err)
	}
}

func (a *App) handleCreateBoard(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	slug := r.FormValue("slug")
	description := r.FormValue("description")
	visibility := r.FormValue("visibility")
	if visibility == "" {
		visibility = VisibilityPrivate
	}

	board, err := a.boards.Create(name, slug, description, visibility, user.ID)
	if err != nil {
		app.Http500("creating board", w, err)
		return
	}
	for _, spec := range []struct {
		Name string
		Done bool
		Late bool
	}{
		{Name: "Todo"},
		{Name: "In Progress"},
		{Name: "Done", Done: true},
	} {
		if _, err := a.columns.Create(board.ID, spec.Name, "", spec.Done, spec.Late); err != nil {
			slog.Error("creating default column", "board", board.Slug, "name", spec.Name, "err", err)
		}
	}
	http.Redirect(w, r, "/boards/"+board.Slug, http.StatusSeeOther)
}

func (a *App) handleBoardEditForm(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	board, err := a.boards.GetBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/board_edit.html", mtr.Ctx{
		"title": "edit " + board.Name,
		"board": board,
		"user":  user,
	}); err != nil {
		app.Http500("rendering board edit", w, err)
	}
}

func (a *App) handleBoardEditSubmit(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	board, err := a.boards.GetBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	newSlug := r.FormValue("slug")
	description := r.FormValue("description")
	visibility := r.FormValue("visibility")
	if err := a.boards.Update(board.ID, name, newSlug, description, visibility); err != nil {
		app.Http500("updating board", w, err)
		return
	}
	http.Redirect(w, r, "/boards/"+newSlug, http.StatusSeeOther)
}

func (a *App) handleDeleteBoard(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	board, err := a.boards.GetBySlug(chi.URLParam(r, "slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := a.boards.Delete(board.ID); err != nil {
		app.Http500("deleting board", w, err)
		return
	}
	http.Redirect(w, r, "/boards/", http.StatusSeeOther)
}

func (a *App) handleBoardDetail(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	slug := chi.URLParam(r, "slug")

	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if !canViewBoard(board, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	cols, err := a.columns.ListByBoard(board.ID)
	if err != nil {
		app.Http500("listing columns", w, err)
		return
	}

	cards, err := a.cards.ListByBoard(board.ID)
	if err != nil {
		app.Http500("listing cards", w, err)
		return
	}
	archivedCards, err := a.cards.ListArchivedByBoard(board.ID)
	if err != nil {
		app.Http500("listing archived cards", w, err)
		return
	}

	canEdit := canModifyBoard(board, user)
	columnsWithCards, err := a.buildRenderedColumns(r, cols, cards, canEdit)
	if err != nil {
		app.Http500("rendering board cards", w, err)
		return
	}
	renderedArchived, err := a.renderCards(r, archivedCards, false)
	if err != nil {
		app.Http500("rendering archived cards", w, err)
		return
	}
	cardModalShell, err := a.renderCardModalShell(r)
	if err != nil {
		app.Http500("rendering card modal shell", w, err)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/board.html", mtr.Ctx{
		"title":          board.Name,
		"board":          board,
		"columns":        columnsWithCards,
		"archived":       renderedArchived,
		"user":           user,
		"isAdmin":        user != nil && user.IsAdmin(),
		"canEdit":        canEdit,
		"cardModalShell": cardModalShell,
		"mainClass":      "board-main",
	}); err != nil {
		app.Http500("rendering board", w, err)
	}
}

func (a *App) handleCreateColumn(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	slug := chi.URLParam(r, "slug")
	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	color := r.FormValue("color")
	done := r.FormValue("done") == "1"
	late := r.FormValue("late") == "1"
	if _, err := a.columns.Create(board.ID, name, color, done, late); err != nil {
		app.Http500("creating column", w, err)
		return
	}
	http.Redirect(w, r, "/boards/"+slug, http.StatusSeeOther)
}

func (a *App) handleDeleteColumn(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	slug := chi.URLParam(r, "slug")
	colIDStr := chi.URLParam(r, "colID")
	colID, err := parseID(colIDStr)
	if err != nil {
		http.Error(w, "invalid column id", http.StatusBadRequest)
		return
	}
	if err := a.columns.Delete(colID); err != nil {
		if errors.Is(err, ErrDoneColumnRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		app.Http500("deleting column", w, err)
		return
	}
	http.Redirect(w, r, "/boards/"+slug, http.StatusSeeOther)
}

func (a *App) handleEditColumn(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	slug := chi.URLParam(r, "slug")
	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	colID, err := parseID(chi.URLParam(r, "colID"))
	if err != nil {
		http.Error(w, "invalid column id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "column name is required", http.StatusBadRequest)
		return
	}
	var wipLimit int
	if raw := r.FormValue("wip_limit"); raw != "" {
		if _, err := fmt.Sscan(raw, &wipLimit); err != nil {
			http.Error(w, "invalid wip_limit", http.StatusBadRequest)
			return
		}
	}
	color := r.FormValue("color")
	done := r.FormValue("done") == "1"
	late := r.FormValue("late") == "1"
	if err := a.columns.Update(colID, name, wipLimit, color, done, late); err != nil {
		if errors.Is(err, ErrDoneColumnRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		app.Http500("editing column", w, err)
		return
	}
	cols, err := a.columns.ListByBoard(board.ID)
	if err == nil {
		payload := ColumnChangedPayload{
			Columns: make([]EventColumnPayload, 0, len(cols)),
		}
		for _, col := range cols {
			payload.Columns = append(payload.Columns, EventColumnPayload{
				ID:       col.ID,
				Name:     col.Name,
				WIPLimit: col.WIPLimit,
				Color:    col.Color,
				Done:     col.Done,
				Late:     col.Late,
			})
		}
		a.publishBoardEvent(slug, EventColumnChanged, payload)
	}
	http.Redirect(w, r, "/boards/"+slug, http.StatusSeeOther)
}

func (a *App) handleReorderColumns(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if !user.IsAdmin() {
		apiErr(w, http.StatusForbidden, "forbidden")
		return
	}
	slug := chi.URLParam(r, "slug")
	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		apiErr(w, http.StatusNotFound, "board not found")
		return
	}
	var ids []int64
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.columns.Reorder(board.ID, ids); err != nil {
		apiErr(w, http.StatusInternalServerError, "reorder failed")
		return
	}
	orderedIDs, err := a.columns.IDsByBoard(board.ID)
	if err == nil {
		a.publishBoardEvent(slug, EventColumnReordered, ColumnReorderedPayload{
			ColumnIDs: orderedIDs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleCreateCard(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	colIDStr := chi.URLParam(r, "colID")

	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	colID, err := parseID(colIDStr)
	if err != nil {
		http.Error(w, "invalid column id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	title := r.FormValue("title")
	content := r.FormValue("content")
	color := r.FormValue("color")

	card, err := a.cards.Create(colID, board.ID, user.ID, title, content, color)
	if err != nil {
		app.Http500("creating card", w, err)
		return
	}
	if user.AutoAssignCards {
		if err := a.cards.AddAssignee(card.ID, user.ID); err != nil {
			app.Http500("assigning created card", w, err)
			return
		}
		card, err = a.cards.Get(card.ID)
		if err != nil {
			app.Http500("reloading created card", w, err)
			return
		}
	}

	html, err := a.renderCardSnippet(r, card)
	if err != nil {
		slog.Error("rendering card snippet", "err", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	a.publishBoardEvent(slug, EventCardCreated, CardCreatedPayload{
		CardID:   card.ID,
		ColumnID: colID,
		HTML:     html,
	})
	w.Header().Set("Content-Type", "text/html")
	if _, err := io.WriteString(w, html); err != nil {
		slog.Error("writing card snippet response", "err", err)
	}
}

func (a *App) handleListLabels(w http.ResponseWriter, r *http.Request) {
	labels, err := a.labels.List()
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "listing labels failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"labels": labels})
}

func (a *App) renderLabelListPage(w http.ResponseWriter, r *http.Request) {
	labels, err := a.labels.List()
	if err != nil {
		app.Http500("listing labels", w, err)
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/labels.html", mtr.Ctx{
		"title":         "labels",
		"labels":        labels,
		"paletteColors": labelPalette,
		"user":          gauth.UserFromContext(r.Context()),
	}); err != nil {
		app.Http500("rendering labels", w, err)
	}
}

func (a *App) handleLabelListPage(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	a.renderLabelListPage(w, r)
}

func (a *App) handleCreateLabelPage(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if _, _, err := a.labels.CreateOrGet(
		r.FormValue("title"),
		r.FormValue("description"),
		r.FormValue("color"),
	); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/labels/", http.StatusSeeOther)
}

func (a *App) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	label, created, err := a.labels.CreateOrGet(
		r.FormValue("title"),
		r.FormValue("description"),
		r.FormValue("color"),
	)
	if err != nil {
		apiErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"created": created,
		"label":   label,
	})
}

func (a *App) handleUpdateLabelPage(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	labelID, err := parseID(chi.URLParam(r, "labelID"))
	if err != nil {
		http.Error(w, "invalid label id", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := a.labels.Update(
		labelID,
		r.FormValue("title"),
		r.FormValue("description"),
		r.FormValue("color"),
	); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/labels/", http.StatusSeeOther)
}

func (a *App) handleUpdateLabelColor(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	labelID, err := parseID(chi.URLParam(r, "labelID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid label id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := a.labels.UpdateColor(labelID, r.FormValue("color")); err != nil {
		apiErr(w, http.StatusBadRequest, err.Error())
		return
	}
	label, err := a.labels.Get(labelID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading label failed")
		return
	}
	a.publishBoardEvent(BoardGlobal, EventLabelColorChanged, LabelColorChangedPayload{
		LabelID:   label.ID,
		Color:     label.Color,
		TextClass: label.TextClass,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"label_id":   label.ID,
		"color":      label.Color,
		"text_class": label.TextClass,
	})
}

func (a *App) handleDeleteLabelPage(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	if user == nil || !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	labelID, err := parseID(chi.URLParam(r, "labelID"))
	if err != nil {
		http.Error(w, "invalid label id", http.StatusBadRequest)
		return
	}
	if err := a.labels.Delete(labelID); err != nil {
		app.Http500("deleting label", w, err)
		return
	}
	http.Redirect(w, r, "/labels/", http.StatusSeeOther)
}

func (a *App) renderCardModal(w http.ResponseWriter, r *http.Request, board *Board, card *Card, user *gauth.User) {
	if !canViewBoard(board, user) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	canEdit := canModifyBoard(board, user)
	cols, err := a.columns.ListByBoard(board.ID)
	if err != nil {
		app.Http500("listing columns", w, err)
		return
	}
	doneCol, err := a.columns.DoneByBoard(board.ID)
	if err != nil {
		app.Http500("loading done column", w, err)
		return
	}
	knownLabels := []*Label{}
	users := []*gauth.User{}
	subscribed := false
	if canEdit {
		knownLabels, err = a.labels.List()
		if err != nil {
			app.Http500("listing labels", w, err)
			return
		}
		users, err = a.users.List()
		if err != nil {
			app.Http500("listing users", w, err)
			return
		}
		subscribed, err = a.cards.IsSubscribed(card.ID, user.ID)
		if err != nil {
			app.Http500("loading subscription state", w, err)
			return
		}
	}

	rawComments, err := a.comments.ListByCard(card.ID)
	if err != nil {
		app.Http500("listing comments", w, err)
		return
	}
	commentDisplays := make([]*CommentDisplay, 0, len(rawComments))
	for _, c := range rawComments {
		commentDisplays = append(commentDisplays, &CommentDisplay{
			CommentWithAuthor: c,
			BodyHTML:          template.HTML(c.BodyRendered),
			CreatedAtDisplay:  formatTimestampForUser(r.Context(), c.CreatedAt),
		})
	}

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.Render(w, "kanban/card.html", mtr.Ctx{
		"card":                card,
		"cardRenderedContent": template.HTML(card.ContentRendered),
		"columns":             cols,
		"board":               board,
		"knownLabels":         knownLabels,
		"users":               users,
		"paletteColors":       labelPalette,
		"isSubscribed":        subscribed,
		"isDone":              card.ColumnID == doneCol.ID,
		"currentUser":         user,
		"canEdit":             canEdit,
		"createdAtDisplay":    formatTimestampForUser(r.Context(), card.CreatedAt),
		"updatedAtDisplay":    formatTimestampForUser(r.Context(), card.UpdatedAt),
		"checklist":           a.cardChecklistPayload(card.ID, card.Checklist),
		"comments":            commentDisplays,
	}); err != nil {
		slog.Error("rendering card", "err", err)
	}
}

func (a *App) handleGetCard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	cardIDStr := chi.URLParam(r, "cardID")
	user := gauth.UserFromContext(r.Context())

	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cardID, err := parseID(cardIDStr)
	if err != nil {
		http.Error(w, "invalid card id", http.StatusBadRequest)
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if card.BoardID != board.ID {
		http.NotFound(w, r)
		return
	}
	a.renderCardModal(w, r, board, card, user)
}

func (a *App) handleGetCardByID(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		http.Error(w, "invalid card id", http.StatusBadRequest)
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	board, err := a.boards.Get(card.BoardID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.renderCardModal(w, r, board, card, user)
}

func (a *App) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	board := chi.URLParam(r, "slug")
	cardIDStr := chi.URLParam(r, "cardID")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(cardIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	prevCard, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading current card failed")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	title := r.FormValue("title")
	content := r.FormValue("content")

	if err := a.cards.Update(cardID, title, content); err != nil {
		apiErr(w, http.StatusInternalServerError, "update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	var buf bytes.Buffer
	if err := reg.Render(&buf, "kanban/card_snippet.html", mtr.Ctx{
		"card": card,
	}); err != nil {
		apiErr(w, http.StatusInternalServerError, "rendering card failed")
		return
	}
	if prevCard.Title != card.Title {
		a.publishBoardEvent(board, EventCardTitleModified, CardTitleModifiedPayload{
			CardID: card.ID,
			Title:  card.Title,
		})
		_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" updated the card title")
	}
	if prevCard.Content != card.Content {
		a.publishBoardEvent(board, EventCardDescriptionModified, CardDescriptionModifiedPayload{
			CardID:          card.ID,
			Content:         card.Content,
			ContentRendered: card.ContentRendered,
		})
		_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" updated the card description")
	}
	resp := a.cardResponse(r, card)
	resp["html"] = buf.String()
	writeJSON(w, http.StatusOK, resp)
}

func (a *App) handleMarkDone(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	prevCard, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading current card failed")
		return
	}
	if err := a.cards.MarkDone(cardID); err != nil {
		apiErr(w, http.StatusInternalServerError, "mark done failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	fromCardIDs, fromErr := a.cards.IDsByColumn(prevCard.ColumnID)
	toCardIDs, toErr := a.cards.IDsByColumn(card.ColumnID)
	if fromErr == nil && toErr == nil && prevCard.ColumnID != card.ColumnID {
		payload := CardMovedPayload{
			CardID:       card.ID,
			FromColumnID: prevCard.ColumnID,
			ToColumnID:   card.ColumnID,
			FromCardIDs:  fromCardIDs,
			ToCardIDs:    toCardIDs,
		}
		a.publishBoardEvent(slug, EventCardMoved, payload)
		writeJSON(w, http.StatusOK, payload)
	} else {
		writeJSON(w, http.StatusOK, a.cardResponse(r, card))
	}
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" marked the card done")
}

func (a *App) handleToggleSubscription(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	enabled := r.FormValue("subscribed") == "1"
	if enabled {
		err = a.cards.Subscribe(cardID, user.ID)
	} else {
		err = a.cards.Unsubscribe(cardID, user.ID)
	}
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "subscription update failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "subscribed": enabled})
}

func (a *App) handleSetCardColor(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	slug := chi.URLParam(r, "slug")
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := a.cards.SetColor(cardID, r.FormValue("color")); err != nil {
		apiErr(w, http.StatusInternalServerError, "color update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	a.publishBoardEvent(slug, EventCardColorChanged, CardColorChangedPayload{
		CardID: card.ID,
		Color:  card.Color,
	})
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" changed the card color")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleSetCardAssignee(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	raw := strings.TrimSpace(r.FormValue("assignee_id"))
	assigneeID, err := parseID(raw)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid assignee id")
		return
	}
	if _, err := a.users.GetByID(assigneeID); err != nil {
		apiErr(w, http.StatusBadRequest, "assignee not found")
		return
	}
	if err := a.cards.AddAssignee(cardID, assigneeID); err != nil {
		apiErr(w, http.StatusInternalServerError, "assignee update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" changed the card assignee")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleRemoveCardAssignee(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	assigneeID, err := parseID(chi.URLParam(r, "userID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid assignee id")
		return
	}
	if err := a.cards.RemoveAssignee(cardID, assigneeID); err != nil {
		apiErr(w, http.StatusInternalServerError, "assignee remove failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" removed a card assignee")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleSetCardStartDate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	startDate, err := parseOptionalDate(r.FormValue("date"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid date")
		return
	}
	if err := a.cards.SetStartDate(cardID, startDate); err != nil {
		apiErr(w, http.StatusInternalServerError, "start date update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" changed the card start date")
	a.publishBoardEvent(slug, EventCardDateUpdated, a.cardDateUpdatedPayload(card))
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleSetCardDueDate(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	dueDate, err := parseOptionalDate(r.FormValue("date"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid date")
		return
	}
	if err := a.cards.SetDueDate(cardID, dueDate); err != nil {
		apiErr(w, http.StatusInternalServerError, "due date update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" changed the card due date")
	a.publishBoardEvent(slug, EventCardDateUpdated, a.cardDateUpdatedPayload(card))
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleAddChecklistItem(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	checklist, err := a.cards.AddChecklistItem(cardID, r.FormValue("text"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "add checklist item failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err == nil {
		_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" added a checklist item")
		a.publishBoardEvent(slug, EventCardChecklistUpdated, a.cardChecklistPayload(cardID, checklist))
		writeJSON(w, http.StatusOK, a.cardResponse(r, card))
		return
	}
	apiErr(w, http.StatusInternalServerError, "loading updated card failed")
}

func (a *App) handleSetChecklistItemDone(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	itemID, err := parseID(chi.URLParam(r, "itemID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid checklist item id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	checklist, err := a.cards.SetChecklistItemDone(cardID, itemID, r.FormValue("done") == "1")
	if err != nil {
		apiErr(w, http.StatusBadRequest, "checklist update failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err == nil {
		_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" updated the checklist")
		a.publishBoardEvent(slug, EventCardChecklistUpdated, a.cardChecklistPayload(cardID, checklist))
		writeJSON(w, http.StatusOK, a.cardResponse(r, card))
		return
	}
	apiErr(w, http.StatusInternalServerError, "loading updated card failed")
}

func (a *App) handleDeleteChecklistItem(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	itemID, err := parseID(chi.URLParam(r, "itemID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid checklist item id")
		return
	}
	checklist, err := a.cards.DeleteChecklistItem(cardID, itemID)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "delete failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err == nil {
		_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" deleted a checklist item")
		a.publishBoardEvent(slug, EventCardChecklistUpdated, a.cardChecklistPayload(cardID, checklist))
		writeJSON(w, http.StatusOK, a.cardResponse(r, card))
		return
	}
	apiErr(w, http.StatusInternalServerError, "loading updated card failed")
}

func (a *App) handleReorderChecklistItems(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	var ids []int64
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	checklist, err := a.cards.ReorderChecklistItems(cardID, ids)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "reorder failed")
		return
	}
	a.publishBoardEvent(slug, EventCardChecklistUpdated, a.cardChecklistPayload(cardID, checklist))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDeleteChecklist(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := a.cards.DeleteChecklist(cardID); err != nil {
		apiErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" deleted the checklist")
	a.publishBoardEvent(slug, EventCardChecklistUpdated, a.cardChecklistPayload(cardID, nil))
	card, err := a.cards.Get(cardID)
	if err == nil {
		writeJSON(w, http.StatusOK, a.cardResponse(r, card))
		return
	}
	apiErr(w, http.StatusInternalServerError, "loading updated card failed")
}

func (a *App) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if a.fss == nil {
		apiErr(w, http.StatusInternalServerError, "attachment storage is not configured")
		return
	}
	uploader, err := a.fss.CreateUploader("attachments")
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "attachment storage is not writable")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		apiErr(w, http.StatusBadRequest, "failed to parse upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		apiErr(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 32<<20))
	if err != nil || len(data) == 0 {
		apiErr(w, http.StatusBadRequest, "failed to read upload")
		return
	}
	contentType := http.DetectContentType(data[:minInt(len(data), 512)])
	filename := sanitizeUploadFilename(header.Filename)
	if ext := strings.ToLower(filepath.Ext(filename)); ext == "" {
		if guessed := mimeExtension(contentType); guessed != "" {
			filename += guessed
		}
	}
	storedName := fmt.Sprintf("card-%d-%d%s", cardID, time.Now().UnixNano(), strings.ToLower(filepath.Ext(filename)))
	basePath, err := a.fss.GetPath("attachments")
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "attachment storage path missing")
		return
	}
	destPath := filepath.Join(basePath, storedName)
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		apiErr(w, http.StatusInternalServerError, "failed to write attachment")
		return
	}
	fileURL := uploader.GetFileURL(storedName)
	if _, err := a.cards.AddAttachment(cardID, user.ID, filename, fileURL, contentType, int64(len(data))); err != nil {
		_ = os.Remove(destPath)
		apiErr(w, http.StatusInternalServerError, "failed to save attachment")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	a.publishBoardEvent(slug, EventCardAttachmentsUpdated, a.cardAttachmentsUpdatedPayload(r, card))
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" added an attachment")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleDeleteAttachment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	attachmentID, err := parseID(chi.URLParam(r, "attachmentID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid attachment id")
		return
	}
	attachment, err := a.cards.Attachment(cardID, attachmentID)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "attachment not found")
		return
	}
	if err := a.cards.DeleteAttachment(cardID, attachmentID); err != nil {
		apiErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	cfg := gconf.ConfigFromContext(r.Context())
	if a.fss != nil {
		if basePath, err := a.fss.GetPath("attachments"); err == nil {
			if prefix := strings.TrimRight(cfg.FSS.URLs["attachments"], "/"); prefix != "" && strings.HasPrefix(attachment.Filepath, prefix+"/") {
				oldFilename := strings.TrimPrefix(attachment.Filepath, prefix+"/")
				_ = os.Remove(filepath.Join(basePath, filepath.Base(oldFilename)))
			}
		}
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	a.publishBoardEvent(slug, EventCardAttachmentsUpdated, a.cardAttachmentsUpdatedPayload(r, card))
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" removed an attachment")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleRenameAttachment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	attachmentID, err := parseID(chi.URLParam(r, "attachmentID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid attachment id")
		return
	}
	filename := sanitizeUploadFilename(r.FormValue("filename"))
	if filename == "" || filename == "attachment" {
		filename = strings.TrimSpace(r.FormValue("filename"))
	}
	if err := a.cards.RenameAttachment(cardID, attachmentID, filename); err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrInvalid) {
			apiErr(w, http.StatusBadRequest, "invalid filename")
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			apiErr(w, http.StatusBadRequest, "attachment not found")
			return
		}
		if strings.Contains(err.Error(), "required") {
			apiErr(w, http.StatusBadRequest, "filename is required")
			return
		}
		apiErr(w, http.StatusInternalServerError, "rename failed")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading updated card failed")
		return
	}
	a.publishBoardEvent(slug, EventCardAttachmentsUpdated, a.cardAttachmentsUpdatedPayload(r, card))
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" renamed an attachment")
	writeJSON(w, http.StatusOK, a.cardResponse(r, card))
}

func (a *App) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	cardIDStr := chi.URLParam(r, "cardID")
	cardID, err := parseID(cardIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	prevCard, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading current card failed")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	colIDStr := r.FormValue("column_id")
	posStr := r.FormValue("position")

	colID, err := parseID(colIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid column_id")
		return
	}
	var pos int
	if posStr != "" {
		fmt.Sscan(posStr, &pos)
	}

	if err := a.cards.Move(cardID, colID, pos); err != nil {
		apiErr(w, http.StatusInternalServerError, "move failed")
		return
	}
	if prevCard.ColumnID == colID {
		cardIDs, err := a.cards.IDsByColumn(colID)
		if err == nil {
			a.publishBoardEvent(slug, EventCardReordered, CardReorderedPayload{
				ColumnID: colID,
				CardIDs:  cardIDs,
			})
		}
	} else {
		fromCardIDs, fromErr := a.cards.IDsByColumn(prevCard.ColumnID)
		toCardIDs, toErr := a.cards.IDsByColumn(colID)
		if fromErr == nil && toErr == nil {
			a.publishBoardEvent(slug, EventCardMoved, CardMovedPayload{
				CardID:       cardID,
				FromColumnID: prevCard.ColumnID,
				ToColumnID:   colID,
				FromCardIDs:  fromCardIDs,
				ToCardIDs:    toCardIDs,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleArchiveCard(w http.ResponseWriter, r *http.Request) {
	user := gauth.UserFromContext(r.Context())
	cardIDStr := chi.URLParam(r, "cardID")
	cardID, err := parseID(cardIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := a.cards.Archive(cardID); err != nil {
		apiErr(w, http.StatusInternalServerError, "archive failed")
		return
	}
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" archived the card")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleUnarchiveCard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardIDStr := chi.URLParam(r, "cardID")
	cardID, err := parseID(cardIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	card, err := a.cards.Unarchive(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "unarchive failed")
		return
	}
	reg := mtr.RegistryFromContext(r.Context())
	var buf bytes.Buffer
	if err := reg.Render(&buf, "kanban/card_snippet.html", mtr.Ctx{
		"card":  card,
		"board": slug,
	}); err != nil {
		apiErr(w, http.StatusInternalServerError, "rendering card failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"card_id":   card.ID,
		"column_id": card.ColumnID,
		"html":      buf.String(),
	})
	_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" unarchived the card")
}

func (a *App) handleDeleteCard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	cardIDStr := chi.URLParam(r, "cardID")
	cardID, err := parseID(cardIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	card, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	if err := a.cards.Delete(cardID); err != nil {
		apiErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if card.BoardID != 0 {
		a.publishBoardEvent(slug, EventCardDeleted, CardDeletedPayload{CardID: cardID})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleDeleteArchivedCards(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	board, err := a.boards.GetBySlug(slug)
	if err != nil {
		apiErr(w, http.StatusNotFound, "board not found")
		return
	}
	archivedCards, err := a.cards.ListArchivedByBoard(board.ID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "load failed")
		return
	}
	if err := a.cards.DeleteArchivedByBoard(board.ID); err != nil {
		apiErr(w, http.StatusInternalServerError, "delete failed")
		return
	}
	for _, card := range archivedCards {
		a.publishBoardEvent(slug, EventCardDeleted, CardDeletedPayload{CardID: card.ID})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleAddCardLabel(w http.ResponseWriter, r *http.Request) {
	board := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	if err := r.ParseForm(); err != nil {
		apiErr(w, http.StatusBadRequest, "bad request")
		return
	}
	cardBefore, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading card failed")
		return
	}
	labelID, err := parseID(r.FormValue("label_id"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid label id")
		return
	}
	if err := a.cards.AddLabel(cardID, labelID); err != nil {
		apiErr(w, http.StatusInternalServerError, "adding label failed")
		return
	}
	label, err := a.labels.Get(labelID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading label failed")
		return
	}
	cardAfter, err := a.cards.Get(cardID)
	if err != nil {
		apiErr(w, http.StatusInternalServerError, "loading card failed")
		return
	}
	a.publishBoardEvent(board, EventCardLabelAdded, CardLabelAddedPayload{
		CardID: cardID,
		Label: EventLabelPayload{
			ID:          label.ID,
			Title:       label.Title,
			Color:       label.Color,
			TextClass:   label.TextClass,
			Description: label.Description,
		},
	})
	if cardBefore.Color != cardAfter.Color {
		a.publishBoardEvent(board, EventCardColorChanged, CardColorChangedPayload{
			CardID: cardID,
			Color:  cardAfter.Color,
		})
	}
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" added label "+label.Title)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleRemoveCardLabel(w http.ResponseWriter, r *http.Request) {
	board := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	labelID, err := parseID(chi.URLParam(r, "labelID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid label id")
		return
	}
	if err := a.cards.RemoveLabel(cardID, labelID); err != nil {
		apiErr(w, http.StatusInternalServerError, "removing label failed")
		return
	}
	a.publishBoardEvent(board, EventCardLabelRemoved, CardLabelRemovedPayload{
		CardID:  cardID,
		LabelID: labelID,
	})
	if label, err := a.labels.Get(labelID); err == nil {
		_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" removed label "+label.Title)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) commentPayload(r *http.Request, c *CommentWithAuthor) CardCommentPayload {
	return CardCommentPayload{
		CommentID:        c.ID,
		CardID:           c.CardID,
		AuthorID:         c.AuthorID,
		AuthorUsername:   c.AuthorUsername,
		AuthorImage:      c.AuthorProfileImage,
		Body:             c.Body,
		BodyRendered:     c.BodyRendered,
		CreatedAt:        c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        c.UpdatedAt.Format(time.RFC3339),
		CreatedAtDisplay: formatTimestampForUser(r.Context(), c.CreatedAt),
	}
}

func (a *App) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		apiErr(w, http.StatusBadRequest, "body is required")
		return
	}
	comment, err := a.comments.Create(cardID, user.ID, body)
	if err != nil {
		app.Http500("creating comment", w, err)
		return
	}
	payload := a.commentPayload(r, comment)
	a.publishBoardEvent(slug, EventCardCommentAdded, payload)
	_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" added a comment")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "comment": payload})
}

func (a *App) handleEditComment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	commentID, err := parseID(chi.URLParam(r, "commentID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	existing, err := a.comments.Get(commentID)
	if err != nil || existing.CardID != cardID {
		http.NotFound(w, r)
		return
	}
	if existing.AuthorID != user.ID && !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" {
		apiErr(w, http.StatusBadRequest, "body is required")
		return
	}
	comment, err := a.comments.Update(commentID, body)
	if err != nil {
		app.Http500("updating comment", w, err)
		return
	}
	payload := a.commentPayload(r, comment)
	a.publishBoardEvent(slug, EventCardCommentEdited, payload)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "comment": payload})
}

func (a *App) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	user := gauth.UserFromContext(r.Context())
	cardID, err := parseID(chi.URLParam(r, "cardID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid card id")
		return
	}
	commentID, err := parseID(chi.URLParam(r, "commentID"))
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid comment id")
		return
	}
	existing, err := a.comments.Get(commentID)
	if err != nil || existing.CardID != cardID {
		http.NotFound(w, r)
		return
	}
	if existing.AuthorID != user.ID && !user.IsAdmin() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := a.comments.Delete(commentID); err != nil {
		app.Http500("deleting comment", w, err)
		return
	}
	a.publishBoardEvent(slug, EventCardCommentDeleted, CardCommentDeletedPayload{
		CommentID: commentID,
		CardID:    cardID,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleReorderCards(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	colIDStr := chi.URLParam(r, "colID")
	colID, err := parseID(colIDStr)
	if err != nil {
		apiErr(w, http.StatusBadRequest, "invalid column id")
		return
	}
	var ids []int64
	if err := json.NewDecoder(r.Body).Decode(&ids); err != nil {
		apiErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := a.cards.Reorder(colID, ids); err != nil {
		apiErr(w, http.StatusInternalServerError, "reorder failed")
		return
	}
	cardIDs, err := a.cards.IDsByColumn(colID)
	if err == nil {
		a.publishBoardEvent(slug, EventCardReordered, CardReorderedPayload{
			ColumnID: colID,
			CardIDs:  cardIDs,
		})
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
