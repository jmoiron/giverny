package kanban

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
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
	}
}

func (a *App) Name() string { return "kanban" }

func (a *App) Migrate() error {
	m, err := monarch.NewManager(a.db)
	if err != nil {
		return err
	}
	sets := []monarch.Set{
		BoardMigrations,
		ColumnMigrations,
		CardMigrations,
		LabelMigrations,
		ChecklistMigrations,
		CommentMigrations,
		AttachmentMigrations,
		ActivityMigrations,
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

	columnsWithCards := BuildColumns(cols, cards)

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.RenderWithBase(w, "base", "kanban/board.html", mtr.Ctx{
		"title":     board.Name,
		"board":     board,
		"columns":   columnsWithCards,
		"archived":  archivedCards,
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

	reg := mtr.RegistryFromContext(r.Context())
	if err := reg.Render(w, "kanban/card.html", mtr.Ctx{
		"card":                card,
		"cardRenderedContent": template.HTML(card.ContentRendered),
		"columns":             cols,
		"board":               board,
		"knownLabels":         knownLabels,
	}); err != nil {
		slog.Error("rendering card", "err", err)
	}
}

func (a *App) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	board := chi.URLParam(r, "slug")
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
	}
	if prevCard.Content != card.Content {
		a.publishBoardEvent(board, EventCardDescriptionModified, CardDescriptionModifiedPayload{
			CardID:          card.ID,
			Content:         card.Content,
			ContentRendered: card.ContentRendered,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":               cardID,
		"title":            card.Title,
		"content":          card.Content,
		"content_rendered": card.ContentRendered,
		"html":             buf.String(),
	})
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
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleUnarchiveCard(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
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
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *App) handleRemoveCardLabel(w http.ResponseWriter, r *http.Request) {
	board := chi.URLParam(r, "slug")
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
