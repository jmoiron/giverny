# Giverny — Implementation Plan

## Summary

Single-binary Go kanban app built on monet's patterns: chi router, sqlx+SQLite, monarch
migrations, mtr templates, gorilla sessions. Reuses monet's auth session infrastructure
but provides its own user/invite model. jQuery + vanilla JS frontend with goldmark WASM
for markdown previews.

---

## Phase 1 — Project Scaffold

**Goal:** Compilable skeleton that boots, loads config, and connects to DB.

1. `go mod init github.com/jmoiron/giverny`; add dependencies:
   - `github.com/jmoiron/monet` (library)
   - `github.com/go-chi/chi/v5`
   - `github.com/jmoiron/sqlx` + `github.com/mattn/go-sqlite3` (CGO SQLite with FTS5)
   - `github.com/gorilla/sessions`
2. Create `main.go` with CLI flags mirroring monet: `--config`, `--debug`, `--add-user`,
   `--invite`, `--migrations`, `--downgrade`.
3. Create `conf/conf.go` — extend monet's `conf.Config` or define own struct with fields:
   `ListenAddr`, `DatabaseURI`, `SessionSecret`, `Secret` (for encryption), `Debug`,
   `BaseURL`, `FSS`.
4. Create `app/app.go` — define the `App` interface (same as monet's):
   `Name()`, `Migrate()`, `Register(*mtr.Registry)`, `Bind(chi.Router)`, `GetAdmin()`.
5. Wire up `main.go`: open DB, run migrations, set up chi router with middleware stack
   (session, config, db, registry), bind apps, `http.ListenAndServe`.
6. Embed static assets and base templates with `//go:embed`.

**Key files:** `main.go`, `conf/conf.go`, `app/app.go`

---

## Phase 2 — Database Models & Migrations (monarch.Set per app)

Define all models and migrations up front so later phases can build on them.

### `auth` app — Users & Invitations
```
users            id, username, email, password_hash, role, created_at, last_login_at
                 role: "readonly" | "admin" | "superadmin"
invitations      id, email, token (hex, 32 bytes), created_by, expires_at, used_at
```

### `kanban` app
```
boards           id, name, slug, description, visibility, created_by, created_at, updated_at
                 visibility: "private" | "open" | "public"
board_members    board_id, user_id, role ("viewer"|"member"|"owner"), added_at
columns          id, board_id, name, position, wip_limit, created_at
cards            id, column_id, board_id, title, content, content_rendered,
                 position, assignee_id, due_date, archived_at, created_by,
                 created_at, updated_at
labels           id, board_id, name, color
card_labels      card_id, label_id
checklists       id, card_id, title, position
checklist_items  id, checklist_id, text, done, position
comments         id, card_id, author_id, body, body_rendered, created_at, updated_at
attachments      id, card_id, uploaded_by, filename, filepath, mime_type, size, created_at
activity         id, board_id, card_id, user_id, action, detail (json), created_at
```

### `smtp` app
```
smtp_config      id, host, port, username, encrypted_password, from_address, updated_at
```

Use `monarch.Set` for each app. Add FTS5 virtual tables + triggers for cards and comments
(reuse monet's FTS5 helper pattern from `db/fts5.go`).

---

## Phase 3 — Auth App (`auth/`)

Monet's `auth.App` is already wired in — it owns the `user` table and provides
login/logout routes and `SessionManager`. Giverny's auth package extends this with
a `user_profile` table (1:1 with `user` via `user_id` FK) holding email, role,
created_at, and last_login_at. All user creation must insert both rows in a transaction.

**Files:** `auth/app.go`, `auth/service.go`, `auth/invite.go`,
`auth/templates/giverny-auth/*.html`

1. **UserProfileService** — `CreateUser(username, email, password, role)` wraps
   monet's `auth.UserService.CreateUser` + inserts `user_profile` in a transaction;
   `GetByUsername(username)` joins `user` + `user_profile`; `GetByEmail(email)`;
   `SetRole(userID, role)`; `RecordLogin(userID)` updates `last_login_at`.
   Validation (bcrypt) is delegated to monet's `auth.UserService.Validate` — no
   duplication needed.
2. **InviteService** — `CreateInvite(email, createdBy)` generates 32-byte random token,
   stores in DB, returns raw token for email link; `Consume(token)` validates expiry +
   marks used + returns email; `SetupPassword(token, password)` calls `CreateUser` to
   finalize account.
3. **Session helpers** — `RequireAdmin` and `RequireSuperAdmin` middleware look up the
   `user_profile` for the session username and check role. Use monet's
   `auth.SessionFromContext` to get the session manager.
4. **Routes** (giverny's auth app binds these alongside monet's login/logout):
   - `GET /invite/{token}` — show set-password form
   - `POST /invite/{token}` — consume token, call `CreateUser`, redirect to login
5. **Admin routes** (super-admin only):
   - `GET /admin/users/` — list users (join user + user_profile)
   - `POST /admin/users/{id}/role` — change role
   - `POST /admin/users/invite` — send invitation email

---

## Phase 4 — SMTP App (`smtp/`)

**Files:** `smtp/app.go`, `smtp/service.go`, `smtp/templates/smtp/*.html`

1. **Config model** — single-row table. `encrypted_password` stored with AES-GCM using
   `conf.Secret` as key (reversible encryption).
2. **SMTPService** — `Send(to, subject, body)` opens live SMTP connection per send
   (no pooling needed for low volume). Decrypts password at send time.
3. **Admin UI** (super-admin):
   - `GET/POST /admin/smtp/` — view/edit SMTP config with test-send button.
4. **Email templates** for invitation emails.

---

## Phase 5 — Kanban Core App (`kanban/`)

**Files:** `kanban/app.go`, `kanban/board.go`, `kanban/column.go`, `kanban/card.go`,
`kanban/service.go`, `kanban/admin.go`

### Board CRUD

Access is determined by user role + board visibility (no per-board membership):
- `public` boards — visible to anyone including anonymous
- `open` boards — visible to all logged-in users
- `private` boards — admin and superadmin only

- `GET /` — list accessible boards
- `GET /board/{slug}` — board detail (kanban view)
- `POST /board/` — create board (admin+)
- `PATCH /board/{slug}` — update name/description/visibility (admin+)
- `DELETE /board/{slug}` — soft-delete (admin+)

### Column CRUD
- `POST /board/{slug}/columns` — add column
- `PATCH /board/{slug}/columns/{id}` — rename, set WIP limit
- `DELETE /board/{slug}/columns/{id}` — delete (move cards to adjacent column or refuse)
- `POST /board/{slug}/columns/reorder` — update positions (array of ids)

### Card CRUD
- `POST /board/{slug}/columns/{colId}/cards` — create card (renders markdown on save)
- `GET /board/{slug}/cards/{id}` — card detail (JSON or HTML partial)
- `PATCH /board/{slug}/cards/{id}` — update card (re-render markdown)
- `PATCH /board/{slug}/cards/{id}/move` — move to different column + position
- `POST /board/{slug}/columns/{colId}/cards/reorder` — update positions within column
- `DELETE /board/{slug}/cards/{id}` — archive card

### Activity Logging
Write to `activity` table on every mutation: board/column/card creates, moves, edits,
membership changes. Include `user_id`, `action` string, `detail` JSON blob.

---

## Phase 6 — Card Feature Apps (within `kanban/`)

Add routes and models for richer card data (labels, checklists, comments, attachments).

1. **Labels** — `POST /board/{slug}/labels`, `DELETE .../labels/{id}`,
   `POST /cards/{id}/labels/{labelId}`, `DELETE /cards/{id}/labels/{labelId}`
2. **Checklists** — `POST /cards/{id}/checklists`, `PATCH .../checklists/{id}`,
   `POST .../checklists/{id}/items`, `PATCH .../items/{id}` (toggle done), `DELETE`
3. **Comments** — `POST /cards/{id}/comments`, `PATCH .../comments/{id}`,
   `DELETE .../comments/{id}`; render markdown on save (reuse goldmark)
4. **Attachments** — `POST /cards/{id}/attachments` (multipart upload, store in
   configured upload dir); `DELETE .../attachments/{id}`; serve via monet's VFS/hotswap pattern

---

## Phase 7 — Frontend

**Files:** `templates/base.html`, `kanban/templates/kanban/*.html`, `static/js/kanban.js`,
`static/css/kanban.css`

### Templates (MTR)
- `base.html` — HTML shell, nav, user info, links to static assets
- `kanban/board_list.html` — grid/list of boards
- `kanban/board_detail.html` — full kanban layout: columns + cards
- `kanban/card_detail.html` — modal or slide-out panel with all card fields
- `auth/login.html`, `auth/invite.html`
- `admin/*.html` — shared admin layouts (reuse monet admin patterns)

### JavaScript (`static/js/kanban.js`)
Keep vanilla JS + jQuery, no build step required. Key pieces:

1. **Card drag-and-drop** — use HTML5 drag events (or SortableJS if allowed as a
   small lib) to reorder within columns and move between columns. On drop, send
   `PATCH /cards/{id}/move` via `$.ajax`.
2. **Inline card creation** — click "+ Add card" at column bottom, show inline form,
   `POST` on submit, inject rendered card HTML into DOM.
3. **Card detail panel** — clicking a card opens a right-side panel (or modal).
   Load card HTML with `$.get`, attach sub-forms for comments/checklists.
4. **Live markdown preview** — use goldmark WASM bundle from monet
   (`static/goldmark/`) for textarea previews in card description and comments.
5. **Column reorder** — drag column headers to reorder.
6. **Activity feed** — simple polling (`setInterval`, 15s) of
   `GET /board/{slug}/activity.json?since={timestamp}` to refresh the activity list.
   (SSE is cleaner; evaluate after basic polling works.)

### CSS
- CSS Grid / Flexbox for kanban layout (horizontal scroll for many columns)
- Mobile: collapse to vertical column stack, hide drag handles

---

## Phase 8 — Admin Interface

Register all apps with monet's `admin.App`:

1. **Board admin** — list/edit/delete all boards, change visibility
2. **Card admin** — search cards, bulk archive
3. **User admin** (super-admin panel) — list users, change roles, send invites,
   deactivate accounts
4. **SMTP admin** — edit config, test send

Implement `GetAdmin() app.Admin` on each app returning admin panel definitions
(name, nav items, handler functions) following monet's admin pattern.

---

## Phase 9 — Search & Filtering

1. **FTS5 setup** — add FTS5 virtual table for cards (title + content) and comments
   (body) using monet's `db/fts5.go` helper. Attach triggers to keep FTS in sync.
2. **Search endpoint** — `GET /search?q=...&board={slug}` returns matching cards
   as HTML partials or JSON.
3. **Board filter bar** — frontend filter controls (label, assignee, due date) that
   hide non-matching cards client-side (no server round-trip for simple filters).

---

## Phase 10 — Polish & Hardening

1. **CSRF** — add gorilla/csrf middleware; include token in all forms and AJAX headers
2. **Rate limiting** — simple IP-based rate limiting on login and invite endpoints
3. **Input validation** — server-side validation on all mutation endpoints with
   structured error JSON responses
4. **Error pages** — custom 404/403/500 templates
5. **Pagination** — use `mtr.Pagination` for activity feed and card search results
6. **Config validation** — fail fast on startup if required config fields are missing
7. **Build & packaging** — Makefile targets: `build`, `dev` (reflex watch), `test`

---

## Implementation Order

```
Phase 1  → Phase 2 → Phase 3 → Phase 4
                              ↓
                       Phase 5 (boards/columns/cards)
                              ↓
                       Phase 6 (labels/checklists/comments/attachments)
                              ↓
                       Phase 7 (frontend)
                              ↓
                       Phase 8 (admin)   + Phase 9 (search) [parallel]
                              ↓
                       Phase 10 (hardening)
```

Phases 3 and 4 can proceed together once Phase 2 models exist.
Frontend (Phase 7) can be developed incrementally alongside Phase 5 & 6.

---

## Verification

- `go build ./...` succeeds from repo root
- `./giverny --migrations` shows all migrations applied
- `./giverny --invite user@example.com` sends email, link creates account, login works
- Manual walkthrough: create board → add columns → add/move cards → add comment → filter
- Admin panel accessible at `/admin/` for admin users, user management for super-admins
- Run with multiple browser tabs open; verify activity feed updates via polling
