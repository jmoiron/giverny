package kanban

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
)

//go:embed kanban/*.html
var templates embed.FS

// App is the kanban sub-application.
type App struct {
	db      db.DB
	hub     *Hub
	boards  *BoardService
	columns *ColumnService
	cards   *CardService
	labels  *LabelService
	users   *gauth.UserProfileService
}

func NewApp(dbh db.DB) *App {
	hub := NewHub()
	go hub.Run()
	return &App{
		db:      dbh,
		hub:     hub,
		boards:  NewBoardService(dbh),
		columns: NewColumnService(dbh),
		cards:   NewCardService(dbh),
		labels:  NewLabelService(dbh),
		users:   gauth.NewUserProfileService(dbh),
	}
}

func (a *App) Name() string { return "kanban" }

func (a *App) RecentBoards(limit int, user *gauth.User) ([]*Board, error) {
	return a.boards.RecentByCardActivity(limit, user != nil && user.IsAdmin())
}

func (a *App) RecentCards(limit int, user *gauth.User) ([]*DashboardCard, error) {
	return a.cards.Recent(limit, user != nil && user.IsAdmin())
}

func (a *App) RenderDashboardCards(r *http.Request, cards []*DashboardCard) ([]*RenderedDashboardCard, error) {
	return a.renderDashboardCards(r, cards)
}

func (a *App) Migrate() error {
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
	reg.AddPathFS("kanban/labels.html", templates)
}

func (a *App) GetAdmin() (app.Admin, error) { return nil, nil }

func (a *App) Bind(r chi.Router) {
	r.Route("/labels", func(r chi.Router) {
		r.Use(gauth.RequireAuth)
		r.Get("/", a.handleLabelListPage)
		r.Post("/", a.handleCreateLabelPage)
		r.Post("/quick", a.handleCreateLabel)
		r.Post("/{labelID}/edit", a.handleUpdateLabelPage)
		r.Post("/{labelID}/delete", a.handleDeleteLabelPage)
	})

	r.Route("/boards", func(r chi.Router) {
		r.Use(gauth.RequireAuth)
		r.Get("/", a.handleBoardList)
		r.Post("/", a.handleCreateBoard)

		r.Route("/{slug}", func(r chi.Router) {
			r.Get("/", a.handleBoardDetail)
			r.Get("/ws", a.handleWS)
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

			r.Route("/cards/{cardID}", func(r chi.Router) {
				r.Get("/", a.handleGetCard)
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
				r.Post("/labels", a.handleAddCardLabel)
				r.Post("/labels/{labelID}/delete", a.handleRemoveCardLabel)
				r.Post("/move", a.handleMoveCard)
				r.Post("/archive", a.handleArchiveCard)
				r.Post("/delete", a.handleDeleteCard)
				r.Post("/unarchive", a.handleUnarchiveCard)
			})
			r.Post("/cards/archived/delete", a.handleDeleteArchivedCards)
		})
	})
}

// handleWS upgrades the request to a WebSocket connection and registers the
// client with the hub for the given board slug.
func (a *App) handleWS(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
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

func (a *App) buildRenderedColumns(r *http.Request, cols []*Column, cards []*Card) ([]*ColumnWithRenderedCards, error) {
	cardsByCol := make(map[int64][]*Card)
	for _, c := range cards {
		cardsByCol[c.ColumnID] = append(cardsByCol[c.ColumnID], c)
	}
	result := make([]*ColumnWithRenderedCards, len(cols))
	for i, col := range cols {
		renderedCards, err := a.renderCards(r, cardsByCol[col.ID], true)
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

	columnsWithCards, err := a.buildRenderedColumns(r, cols, cards)
	if err != nil {
		app.Http500("rendering board cards", w, err)
		return
	}
	renderedArchived, err := a.renderCards(r, archivedCards, false)
	if err != nil {
		app.Http500("rendering archived cards", w, err)
		return
	}

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/board.html", mtr.Ctx{
		"title":     board.Name,
		"board":     board,
		"columns":   columnsWithCards,
		"archived":  renderedArchived,
		"user":      user,
		"isAdmin":   user.IsAdmin(),
		"mainClass": "board-main",
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

	reg := mtr.RegistryFromContext(r.Context())
	var buf bytes.Buffer
	if err := reg.Render(&buf, "kanban/card_snippet.html", mtr.Ctx{
		"card":  card,
		"board": slug,
	}); err != nil {
		slog.Error("rendering card snippet", "err", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	a.publishBoardEvent(slug, EventCardCreated, CardCreatedPayload{
		CardID:   card.ID,
		ColumnID: colID,
		HTML:     buf.String(),
	})
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write(buf.Bytes()); err != nil {
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
		"title":  "labels",
		"labels": labels,
		"user":   gauth.UserFromContext(r.Context()),
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
	cols, err := a.columns.ListByBoard(board.ID)
	if err != nil {
		app.Http500("listing columns", w, err)
		return
	}
	knownLabels, err := a.labels.List()
	if err != nil {
		app.Http500("listing labels", w, err)
		return
	}
	users, err := a.users.List()
	if err != nil {
		app.Http500("listing users", w, err)
		return
	}
	doneCol, err := a.columns.DoneByBoard(board.ID)
	if err != nil {
		app.Http500("loading done column", w, err)
		return
	}
	subscribed, err := a.cards.IsSubscribed(card.ID, user.ID)
	if err != nil {
		app.Http500("loading subscription state", w, err)
		return
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
		"createdAtDisplay":    formatTimestampForUser(r.Context(), card.CreatedAt),
		"updatedAtDisplay":    formatTimestampForUser(r.Context(), card.UpdatedAt),
		"checklist":           a.cardChecklistPayload(card.ID, card.Checklist),
	}); err != nil {
		slog.Error("rendering card", "err", err)
	}
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
	color := r.FormValue("color")
	labelIDs := make([]int64, 0, len(r.Form["label_ids"]))
	for _, rawID := range r.Form["label_ids"] {
		labelID, err := parseID(rawID)
		if err != nil {
			apiErr(w, http.StatusBadRequest, "invalid label id")
			return
		}
		labelIDs = append(labelIDs, labelID)
	}

	if err := a.cards.Update(cardID, title, content, color, labelIDs); err != nil {
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
