# Giverny Mobile PWA — Implementation Plan

## Context

`plans/mobile.md` outlines a mobile PWA for giverny anchored at `/mobile/`, sharing as much
code/design as possible with the desktop app. Giverny is a server-rendered Go app (chi router,
`html/template` via the `monet` framework's `mtr` registry, cookie-session auth, a websocket hub
broadcasting typed events per board) with an explicit project philosophy of staying lightweight —
"stick to basic jQuery+vanilla JS... shouldn't adopt something heavy like next or react" — and no
JS build step at all (LESS→CSS via `lessc` is the only build tooling). No PWA scaffolding
(manifest, service worker, mobile routes) exists yet.

Four scope decisions were confirmed with the project owner before designing this plan:
1. **Packaging**: pure installable PWA ("Add to Home Screen"), no Capacitor/native wrapper, no
   Node build pipeline, no App Store distribution.
2. **Push notifications v1 triggers**: (a) card assigned to you, (b) new comment on a card you're
   subscribed to or assigned to. Due-date reminders are out of v1 (would need a cron job).
3. **WebAuthn/passkey login is in v1 scope** (Face ID/Touch ID/Android biometric via passkeys).
4. **Mobile home page**: all accessible boards, sorted by recent card activity (same ordering as
   desktop's "recent boards", just unlimited instead of capped at 3).

The goal of this plan is to fill in every technical detail mobile.md leaves open — package
structure, routing, template/CSS/JS reuse strategy, WebAuthn and Web Push backends, and rollout
order — so implementation can start directly from it.

## Architecture overview

Mobile is a **new presentation layer**, not a new data/business-logic layer. It reuses
`kanban.Service`/`kanban.App` for all board/card data and mutations, and `gauth`/monet `auth` for
sessions — mobile handlers mostly render new templates and mobile JS calls the **same** desktop
POST mutation endpoints (e.g. `POST /boards/{slug}/cards/{cardID}/move`) via `fetch`, reusing the
existing cookie session. WebAuthn and Web Push are the only genuinely new domains, and even those
plug into the existing session and subscription-notification mechanisms rather than building
parallel systems.

## 1. New Go package: `mobile`

New directory `/mnt/omocha/jmoiron/dev/giverny/mobile/`, implementing `app.App` exactly like
`kanban`/`gauth`/`smtp` and added to the `apps` slice in `main.go`. It owns mobile templates/routes
plus push-subscription storage/sending (mobile-specific). WebAuthn lives in giverny's own `auth`
package instead (see §9) since credentials are 1:1 with the `user` table and the endpoint should be
reusable by a future desktop login page.

**One upstream change required** — monet's `auth.App` keeps its `*UserService` in an unexported
`serv` field, so giverny code can't call `Validate(username, password)` for a `/mobile/login/`
handler (which must redirect to `/mobile/` instead of `/`, so it can't reuse monet's own handler).
Add one accessor mirroring the existing exported `Sessions` field:
```go
// /mnt/omocha/jmoiron/dev/monet/auth/auth.go
func (a *App) Users() *UserService { return a.serv }
```

**Changes to `/mnt/omocha/jmoiron/dev/giverny/kanban/app.go`** (small, deliberate exported surface,
mirroring the `gauthApp.Users()`/`Invites()` pattern already used elsewhere):
- Rename `canViewBoard`→`CanViewBoard`, `canModifyBoard`→`CanModifyBoard` (mechanical; update the
  ~6 internal call sites in the same file).
- Add `func (a *App) Boards() *BoardService`, `Cards() *CardService`, `Columns() *ColumnService`.
- Add `func (a *App) RenderedColumns(r *http.Request, board *Board, canEdit bool) ([]*ColumnWithRenderedCards, error)`
  — thin wrapper around the same calls `handleBoardDetail` already makes
  (`a.columns.ListByBoard`, `a.cards.ListByBoard`, `a.buildRenderedColumns`), giving mobile's board
  page identically-rendered `.kanban-card` HTML (from `kanban/kanban/card_snippet.html`) for free.
- Add `func (a *App) RenderColumnCards(r *http.Request, colID int64, draggable bool) (template.HTML, error)`
  for the single-column partial-refresh endpoint mobile's websocket handler uses (§5).
- Add `func (a *App) SetPushNotifier(n PushNotifier)` and interface
  `type PushNotifier interface { NotifyCardAssigned(cardID, assigneeID int64); NotifyNewComment(cardID, actorID int64) }`
  (kanban defines the interface, `mobile.PushService` implements it — avoids an import cycle since
  `mobile` already imports `kanban`).
- `BoardService.RecentByCardActivity` (`kanban/service.go:294`): treat `limit <= 0` as "no LIMIT
  clause" instead of always appending `LIMIT ?`. This lets mobile's home page reuse the existing
  `kanban.App.RecentBoards(0, user)` (already at `app.go:88`) — satisfies decision #4 with a
  one-line query change, no new method needed.
- Add `func (s *CardService) NotificationRecipients(cardID int64) ([]int64, error)` in
  `kanban/service.go` (near `Subscribe`/`Unsubscribe`, ~line 1003) — union of `card_subscription`
  and card-assignee user IDs, deduplicated. `RecordSubscriptionMessage` (service.go:1013, the
  existing in-app-notification fan-out already called after nearly every card mutation in
  `app.go`) only covers subscribers, not assignees, so push needs this new method per decision #2.

**New files in `mobile/`:**

| File | Responsibility |
|---|---|
| `mobile/app.go` | `App` struct + `NewApp(dbh db.DB, cfg *conf.Config, kanbanApp *kanban.App, gauthApp *gauth.App, monetAuth *mauth.App, fss vfs.Registry) *App`. Implements `Name()="mobile"`, `Migrate()`, `Register(reg)`, `Bind(r)`, `GetAdmin()`. |
| `mobile/middleware.go` | `RequireAuth` — same check as `gauth.RequireAuth` but redirects to `/mobile/login/?redirect=...`. |
| `mobile/handlers_pages.go` | `handleLogin` (GET/POST, password path — WebAuthn ceremony is pure JS+JSON against `/auth/webauthn/...`), `handleHome`, `handleBoardDetail`, `handleCardDetail`, `handleColumnCardsPartial`. |
| `mobile/push.go` | `PushSubscription` model + migration, `PushSubscriptionService`, VAPID key model + migration + `VAPIDService` (generate-once, AES-256-GCM encrypt/decrypt using `cfg.Secret`, mirroring `smtp/service.go`'s `encrypt`/`decrypt`). |
| `mobile/push_handlers.go` | `handleSubscribePush`, `handleUnsubscribePush`, `handleVAPIDPublicKey`. |
| `mobile/push_service.go` | `PushService` implementing `kanban.PushNotifier` via `github.com/SherClockHolmes/webpush-go`. |

Mobile does **not** get its own board/card/checklist/comment/assignment mutation handlers — mobile
JS calls the exact same desktop POST endpoints already bound in `kanban/app.go`.

## 2. Routing

All new routes in `mobile/app.go`'s `Bind(r chi.Router)`, mounted at `/mobile/`:

```go
func (a *App) Bind(r chi.Router) {
    r.Get("/mobile/login/", a.handleLogin)
    r.Post("/mobile/login/", a.handleLogin)
    r.Get("/mobile/logout/", a.handleLogout)

    r.Route("/mobile", func(r chi.Router) {
        r.Use(a.RequireAuth) // redirects to /mobile/login/
        r.Get("/", a.handleHome)
        r.Get("/boards/{slug}/", a.handleBoardDetail)
        r.Get("/boards/{slug}/columns/{colID}/cards", a.handleColumnCardsPartial)
        r.Get("/boards/{slug}/cards/{cardID}/", a.handleCardDetail)
        r.Get("/push/vapid-key", a.handleVAPIDPublicKey)
        r.Post("/push/subscribe", a.handleSubscribePush)
        r.Post("/push/unsubscribe", a.handleUnsubscribePush)
    })
}
```

Shared WebAuthn endpoints are mounted top-level (not under `/mobile/`) by giverny's `auth` package
so a future desktop login page can use them too (§9).

Column reorder, within-column card reorder, checklist/comment/label/attachment/date/color/assign
mutations all POST to the **existing** desktop routes already bound in `kanban/app.go` — no new
kanban mutation routes. Per mobile.md, mobile drag-and-drop is restricted to **within-column
reorder only**, so mobile JS only ever calls `POST /boards/{slug}/columns/{colID}/cards/reorder`
(identical payload shape to desktop — `handleReorderCards` needs zero changes), never the
cross-column `move` or column-reorder endpoints.

`/mobile/boards/{slug}/cards/{cardID}/` is a full server-rendered page (not an AJAX fragment
target) — see §3.

`mobile.RequireAuth`:
```go
func (a *App) RequireAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if gauth.UserFromContext(r.Context()) == nil {
            http.Redirect(w, r, "/mobile/login/?redirect="+url.QueryEscape(r.URL.Path), http.StatusFound)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```
`gauth.UserFromContext` is already populated by the existing global `gauth.AddUserMiddleware` in
`main.go`, so mobile only needs its own redirect target, not its own user-resolution middleware.

## 3. Templates

**New mobile base, not a flag on `templates/base.html`.** The desktop base has permanent top nav
and a persistent left drawer; mobile's chrome (full-screen drawers, top bar with X-toggle, swipe
paging) is different enough that branching it all behind `{{if .mobile}}` would make `base.html`
an unreadable dual-purpose template and risk desktop regressions. A second ~40-line base is
cleaner.

`main.go` registers it alongside the existing base: `reg.AddBaseFS("mobile-base", "templates/mobile_base.html", templates)`.

| File | Renders |
|---|---|
| `templates/mobile_base.html` | New base: `<head>` with manifest link, apple-touch-icon, theme-color meta, `viewport-fit=cover`; top bar (hamburger → left drawer; search omitted per mobile.md; user icon → full-screen right drawer, swaps to X via CSS class); left drawer reusing the **same** `#side-nav-boards`/`#side-nav-views` container IDs and `/api/nav-boards/`/`/api/nav-views/` fetch targets as desktop, plus "my tasks"/"subscribed"/"in progress" links; right drawer server-rendered inline from `.user` context (settings link, theme toggle, logout, add-passkey button — no AJAX fragment needed); `{{.body}}` slot; WS-disconnect banner placeholder `<div id="ws-banner" class="ws-banner is-hidden">reconnecting…</div>`. |
| `mobile/mobile/login.html` | Full-screen login: password form, plus a "Sign in with Face ID / Touch ID" button (shown when `PublicKeyCredential` is supported) above a "use password" toggle. |
| `mobile/mobile/home.html` | Scrollable list of all accessible boards, sorted by recent activity (`.homeBoards` from `kanbanApp.RecentBoards(0, user)`), linking to `/mobile/boards/{slug}/`. |
| `mobile/mobile/board.html` | Full-width swipeable columns, iterating `.columns` ([]`*kanban.ColumnWithRenderedCards` from `RenderedColumns`) — reuses the same per-card HTML fragments `kanban/kanban/card_snippet.html` produces, wrapped in mobile's own pager markup instead of desktop's `.board-columns` flex row. |
| (reused) `kanban/kanban/card.html` | Rendered directly as `/mobile/boards/{slug}/cards/{cardID}/`'s body via `reg.RenderWithBase(w, "mobile-base", "kanban/card.html", ctx)` — same template desktop uses inside its modal. The `.card-detail-layout` grid already collapses to a single stacked column at the existing 780px breakpoint (`kanban.less:2119`) — exactly the "quick controls stacked below main content" behavior mobile.md asks for, already built. `mobile.handleCardDetail` builds the same `ctx` (`board`, `card`, `canEdit`) `handleGetCard`/`renderCardModal` build today, using the new exported `kanbanApp.Boards()`/`Cards()`/`CanViewBoard`. The close control does `history.back()` on mobile instead of hiding a modal overlay (JS checks for a `.mobile-card-page` ancestor). |

`main.go` wiring: `mobileApp := mobile.NewApp(dbh, cfg, kanbanApp, gauthApp, authApp, fss)`, added
to the `apps` slice, `Migrate()`/`Bind()` called in the existing per-app loop.

## 4. CSS

New `static/mobile.less`, added to `static/style.less`'s `@import` chain — ships in the single
compiled `style.css`, no separate build artifact, `cfg.Debug`'s client-side-LESS dev path keeps
working unmodified. Shares `theme.less`'s CSS custom properties/dark-mode overrides (no forked
color system) and reuses `kanban.less` selectors for card/label/checklist visuals (`.kanban-card`,
`.label-pill`, `.card-detail`, `.card-main-pane`, `.card-quick-pane` — same classes, same partial
templates, zero duplication). `mobile.less` only adds:

- **Top bar / drawers**: `.mobile-topbar` (fixed, `env(safe-area-inset-top)` padding for the iOS
  notch); `.mobile-drawer-left`/`.mobile-drawer-right` as fixed-position, `transform: translateX(...)`
  sliding panels toggled via a body class (`mobile-left-open`/`mobile-right-open`), same pattern as
  desktop's `.side-nav-open`. Right drawer is full-screen per spec; left drawer can stay
  partial-width like desktop's side-nav.
- **Swipeable columns**: `.mobile-board-columns { display:flex; overflow-x:scroll; scroll-snap-type:x mandatory; scroll-behavior:smooth }`,
  `.mobile-board-column { flex: 0 0 92vw; scroll-snap-align:center; position:relative }` — the 8vw
  remainder naturally shows a sliver of the adjacent column (the "gradient faded edge" hint from
  mobile.md), reinforced with a `::after` pseudo-element (`linear-gradient(to right, transparent, var(--bg))`).
  A slim dot-pager (`.mobile-col-pager`) sits under the top bar, `.active` dot toggled by JS
  tracking scroll position.
- **Card detail fullscreen wrapper**: `.mobile-card-page { min-height: 100vh }` — the existing
  780px-breakpoint rules in `kanban.less` already apply (mobile viewports are always ≤780px);
  `mobile.less` only needs to override `.card-detail`'s modal-specific positioning back to normal
  in-flow sizing, and restyle the close control as a fixed top-right X.
- **WS banner** and **touch-drag ghost/placeholder** styles (`.touch-drag-ghost`, `.touch-drag-placeholder`).

## 5. JS architecture

**Extract two shared files out of `static/js/kanban.js`** (3293 lines today); leave desktop-only
native drag-and-drop and cross-board list/filter code in `kanban.js`.

**`static/js/ws-client.js`** (new, ~150 lines) — pure connection/backoff state machine, no DOM
coupling beyond callback hooks. Move out of `kanban.js` (currently ~lines 1334–1489): `ws`,
`wsState`, `reconnectTimer`, `reconnectDelay`, `MAX_RECONNECT_DELAY`, `connectTimer`,
`CONNECT_TIMEOUT_MS`, `clearReconnectTimer`, `clearConnectTimer`, `scheduleReconnect`, `connectWS`.
Refactor into `createBoardSocket(boardSlug, { onEvent(evt), onStateChange(state) })` returning
`{connect(), close()}` so desktop and mobile each own an independent socket. Desktop's debug
`#ws-status-box`/event-log UI stays in `kanban.js` as its `onStateChange`/`onEvent` callback bodies;
mobile's `onStateChange` instead toggles `#ws-banner` (§6).

`kanban.js` keeps `applyBoardEvent(evt)` and all `apply*` DOM-patch functions as-is (they already
select by class, not page-specific IDs).

**`static/js/card-detail.js`** (new — everything scoped to `.card-detail`, used both inside the
desktop modal and mobile's fullscreen page). Move out of `kanban.js`: card save/edit-warning state,
description editing, checklist functions, attachment functions, label functions, comment
functions, and the corresponding `applyCard*`/`applyLabelColorChanged`/`applyCardComment*`
event-apply functions. Export one entry point, `CardDetail.init($scope)`, called both by desktop's
`openCardModal()` (after AJAX swap) and by mobile's page-load bootstrap (card is already
server-rendered, no AJAX swap needed). `applyBoardEvent` in `kanban.js` calls into
`CardDetail.apply*` via the shared `window.CardDetail` namespace rather than closures, since the
files are now separately included.

`kanban.js` retains (does not move): native HTML5 drag-and-drop, column reorder, cross-column card
move, and cross-board card-list/filter/view code.

**`static/js/mobile.js`** (new, ~400–500 lines):
- Drawer open/close: left drawer toggle reuses the exact `localStorage['sideNavOpen']` key desktop
  uses; right drawer toggle swaps the user icon to an X. **Include `app.js` on mobile pages too** —
  its side-nav population (`/api/nav-boards/`/`/api/nav-views/` fetch, ID-selector-based), theme
  toggle, and confirm-modal helpers are directly reusable; its `#side-nav-toggle` desktop-only
  handler is simply inert (no such element on mobile).
- Swipe/scroll-snap column paging: native `scroll-snap-type: x mandatory` handles the swipe
  physics; JS only tracks `scrollLeft` (via `requestAnimationFrame`-debounced scroll listener) to
  update the pager dots.
- WebSocket: `createBoardSocket(slug, { onEvent: applyMobileBoardEvent, onStateChange: updateWsBanner })`.
  `applyMobileBoardEvent` is deliberately coarser than desktop's: for column/card-list-affecting
  events (`card.created`/`card.deleted`/`card.move`/`card.reorder`/`column.changed`), re-fetch the
  affected column via `GET /mobile/boards/{slug}/columns/{colID}/cards` and replace
  `.col-cards[data-column-id]` innerHTML, rather than replicating desktop's ~15 fine-grained
  DOM-splice functions for a very different column layout. For card-detail-scoped events
  (`card.title.modified`, `.description.modified`, `.checklist.updated`, `.color.changed`,
  `.date.updated`, `.attachments.updated`, `.label.*`, `.comment.*`), call the same
  `CardDetail.apply*` functions when the fullscreen card page for that card is open — full reuse.
- **Touch-based within-column reorder** (native HTML5 DnD has poor mobile Safari/Chrome-Android
  touch support — don't reuse `kanban.js`'s DnD code on mobile). ~80 lines of hand-written
  `touchstart`/`touchmove`/`touchend`, no external library:
  - `touchstart` on `.kanban-card`: 350ms long-press timer, cancelled by >~8px movement (scroll
    intent) before it fires; on fire, clone into a `position:fixed` `.touch-drag-ghost`, add
    `.touch-drag-placeholder` in the original slot, `navigator.vibrate?.(10)`.
  - `touchmove`: reposition ghost, compute insertion point among sibling `.kanban-card`s (reuse
    `kanban.js`'s existing small `findInsertBeforeByAxis`-style helper).
  - `touchend`: compute new card-ID order, `POST /boards/{slug}/columns/{colID}/cards/reorder`
    with the same `{card_ids: [...]}` payload desktop sends — `handleReorderCards` unchanged.
  - Explicitly no cross-column drag, no column reordering on touch (matches spec, avoids
    auto-scroll-while-dragging complexity).
- Service worker registration: `if ('serviceWorker' in navigator) navigator.serviceWorker.register('/static/sw.js', {scope: '/mobile/'})`.
- Push subscription bootstrap after login: if `'PushManager' in window` and no
  `localStorage['pushSubscribed']` flag, prompt `Notification.requestPermission()`, then
  `registration.pushManager.subscribe({userVisibleOnly:true, applicationServerKey: <from /mobile/push/vapid-key>})`,
  POST the resulting subscription to `/mobile/push/subscribe`.

## 6. WebSocket disconnect banner

Hooks into `createBoardSocket`'s `onStateChange` callback in `mobile.js`:
```js
function updateWsBanner(state) {
  document.getElementById('ws-banner').classList.toggle('is-hidden', state === 'connected');
}
```
`ws-client.js` calls `onStateChange('connecting'|'connected'|'error')` at the same points desktop's
existing state tracking already updates `#ws-indicator` — moving this into the shared file means
the banner works from day one with no bespoke mobile WS-state tracking.

## 7. PWA plumbing

- `static/manifest.webmanifest` (static — giverny is single-tenant, no per-user theming need):
  `name`, `short_name: "Giverny"`, `start_url: "/mobile/"`, `scope: "/mobile/"`,
  `display: "standalone"`, `background_color`/`theme_color` matching `theme.less`'s light-mode
  `--bg`, icons array.
- Icon set (new, under `static/icons/`, generated once from `static/img/logo.png` as a manual
  asset-prep step, not scripted into the build): `icon-192.png`, `icon-512.png`,
  `icon-maskable-192.png`, `icon-maskable-512.png` (~20% safe-area padding), `apple-touch-icon-180.png`.
- `mobile_base.html` `<head>` additions: manifest link, `theme-color` meta, apple-touch-icon link,
  `apple-mobile-web-app-capable`, `apple-mobile-web-app-status-bar-style`, viewport with
  `viewport-fit=cover`.
- `static/sw.js` (new, ~60 lines, hand-written, no build step):
  - `install`: cache-first app shell (`/static/style.css`, `/static/js/{app,cash.min,ws-client,card-detail,mobile}.js`,
    font files, manifest, icons) — only `/static/*` requests; everything else (HTML pages, API
    calls) goes straight to network since offline data caching is explicitly v2.
  - `push` event: `self.registration.showNotification(data.title, {body, icon: '/static/icons/icon-192.png', data: {url}})`.
  - `notificationclick`: focus an existing client showing that URL, or `clients.openWindow(url)`.
  - Placed at `static/sw.js` (not `static/js/sw.js`) so the response can carry a
    `Service-Worker-Allowed: /mobile/` header (added via a small `middleware.SetHeader` scoped to
    that one path in `main.go`) — required because the script's own path (`/static/sw.js`) is
    outside its registered scope (`/mobile/`); easy to silently get wrong.

## 8. Web Push backend

- **Library**: `github.com/SherClockHolmes/webpush-go` (pure Go, handles VAPID JWT signing + AES-GCM
  payload encryption internally, no external push-relay dependency). Confirmed via `go.sum`
  inspection that nothing webpush/webauthn-related is vendored yet.
- **VAPID keys**: generated once via `webpush.GenerateVAPIDKeys()`, stored in a new singleton
  table, following the exact `smtp_config` encrypt-at-rest pattern
  (`/mnt/omocha/jmoiron/dev/giverny/smtp/service.go`'s `encrypt`/`decrypt`, AES-256-GCM keyed by
  `cfg.Secret` — reused as-is, no new config fields):
  ```go
  // mobile/push.go
  var VAPIDMigrations = monarch.Set{
    Name: "push_vapid_key",
    Migrations: []monarch.Migration{{
      Up: `CREATE TABLE IF NOT EXISTS push_vapid_key (
          id INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
          public_key TEXT NOT NULL,
          encrypted_private_key TEXT NOT NULL,
          created_at DATETIME DEFAULT (datetime('now'))
      );`,
      Down: `DROP TABLE push_vapid_key;`,
    }},
  }
  ```
- **Subscription table**:
  ```go
  var PushSubscriptionMigrations = monarch.Set{
    Name: "push_subscription",
    Migrations: []monarch.Migration{{
      Up: `CREATE TABLE IF NOT EXISTS push_subscription (
          id INTEGER NOT NULL PRIMARY KEY,
          user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
          endpoint TEXT NOT NULL UNIQUE,
          p256dh TEXT NOT NULL,
          auth TEXT NOT NULL,
          user_agent TEXT NOT NULL DEFAULT '',
          created_at DATETIME DEFAULT (datetime('now'))
      );`,
      Down: `DROP TABLE push_subscription;`,
    }},
  }
  ```
- **Endpoints** (`mobile/push_handlers.go`, under `/mobile/push/...`, behind `mobile.RequireAuth`):
  `GET vapid-key` → `{"key": "<base64 public key>"}`; `POST subscribe` → upsert
  `PushSubscription` from `pushManager.subscribe()`'s JSON, keyed on `endpoint`; `POST unsubscribe`
  → delete by `endpoint`.
- **Trigger hook points** in `/mnt/omocha/jmoiron/dev/giverny/kanban/app.go`:
  - `handleSetCardAssignee` (line 1936): immediately after the existing
    `_ = a.cards.RecordSubscriptionMessage(card.ID, user.Username+" changed the card assignee")`,
    add `if a.pushNotifier != nil { go a.pushNotifier.NotifyCardAssigned(card.ID, assigneeID) }`
    (self-assignment guard lives inside `PushService.NotifyCardAssigned`, keeping kanban
    notifier-agnostic).
  - `handleCreateComment` (line 2604): immediately after
    `_ = a.cards.RecordSubscriptionMessage(cardID, user.Username+" added a comment")`, add
    `if a.pushNotifier != nil { go a.pushNotifier.NotifyNewComment(cardID, user.ID) }`.
  - `kanban.App` gains a `pushNotifier PushNotifier` field set via `SetPushNotifier`; wired in
    `main.go` after both `kanbanApp` and the push service exist:
    `kanbanApp.SetPushNotifier(pushService)`.
- **`mobile/push_service.go`** — `NotifyCardAssigned`/`NotifyNewComment` look up the card/board,
  build a `/mobile/boards/{slug}/cards/{id}/` URL, and call a shared `send(userIDs, title, body,
  url, excludeUserID)` that iterates each recipient's subscriptions and calls
  `webpush.SendNotification(...)` with the stored VAPID keys; a `404`/`410` response prunes the
  stale subscription row.

## 9. WebAuthn backend

Lives in `/mnt/omocha/jmoiron/dev/giverny/auth/` (not `mobile`) — credentials share the `user`
table's trust boundary, and the endpoints should be reusable by a future desktop login page.

- **Library**: `github.com/go-webauthn/webauthn` (the standard/maintained Go WebAuthn library;
  pulls in `github.com/go-webauthn/x` and `github.com/fxamacker/cbor/v2` transitively). Confirmed
  not already present in `go.sum`.
- **New files**:
  - `auth/webauthn.go` — `WebAuthnCredential` model + migration:
    ```go
    var WebAuthnMigrations = monarch.Set{
      Name: "webauthn_credential",
      Migrations: []monarch.Migration{{
        Up: `CREATE TABLE IF NOT EXISTS webauthn_credential (
            id INTEGER NOT NULL PRIMARY KEY,
            user_id INTEGER NOT NULL REFERENCES user(id) ON DELETE CASCADE,
            credential_id TEXT NOT NULL UNIQUE,
            public_key BLOB NOT NULL,
            attestation_type TEXT NOT NULL DEFAULT '',
            transports TEXT NOT NULL DEFAULT '',
            sign_count INTEGER NOT NULL DEFAULT 0,
            clone_warning BOOLEAN NOT NULL DEFAULT 0,
            aaguid TEXT NOT NULL DEFAULT '',
            name TEXT NOT NULL DEFAULT '',
            created_at DATETIME DEFAULT (datetime('now')),
            last_used_at DATETIME
        );`,
        Down: `DROP TABLE webauthn_credential;`,
      }},
    }
    ```
    plus `WebAuthnCredentialService` (`Create`/`ListByUser`/`UpdateSignCount`/`Delete`/
    `GetUserByCredentialUserHandle`) and a `webAuthnUser` adapter implementing `webauthn.User`.
  - `auth/webauthn_handlers.go` — `handleWebAuthnRegisterBegin`/`Finish` (behind `RequireAuth` — a
    passkey can only be added while already logged in via password, per decision #3),
    `handleWebAuthnLoginBegin`/`Finish` (unauthenticated — this *is* how you authenticate).
- `auth.App` gains a `webAuthn *webauthn.WebAuthn` field built from
  `webauthn.Config{RPDisplayName: "Giverny", RPID: <host from cfg.BaseURL>, RPOrigins: []string{cfg.BaseURL}}`
  (`go-webauthn` special-cases `http://localhost`, so dev works unmodified).
- **Routes** (top-level in `auth.App.Bind()`, so both `/mobile/login/` and a future desktop login
  can use them):
  ```go
  r.Route("/auth/webauthn", func(r chi.Router) {
      r.Post("/login/begin", a.handleWebAuthnLoginBegin)
      r.Post("/login/finish", a.handleWebAuthnLoginFinish)
      r.Group(func(r chi.Router) {
          r.Use(RequireAuth)
          r.Post("/register/begin", a.handleWebAuthnRegisterBegin)
          r.Post("/register/finish", a.handleWebAuthnRegisterFinish)
          r.Get("/credentials", a.handleWebAuthnListCredentials)
          r.Post("/credentials/{id}/delete", a.handleWebAuthnDeleteCredential)
      })
  })
  ```
- **Registration ceremony**: `BeginRegistration`/`FinishRegistration`, with the interim
  `*webauthn.SessionData` stashed in the existing gorilla session
  (`session.Values["webauthn_reg_session"] = data`) — no new server-side session store. Invoked
  from a new "passkeys" section in `/user/settings/` (`auth/auth/settings.html`) and a first-run
  prompt on `/mobile/` after password login if no credential exists yet.
- **Login ceremony**: `BeginDiscoverableLogin()`/`FinishDiscoverableLogin()` — usernameless/
  resident-key flow, required for a good Face ID/fingerprint UX (no username field on a phone). On
  success, the **exact same two lines used everywhere else in the codebase**
  (`auth/app.go`'s `handleInviteSubmit`, monet's `login`):
  ```go
  session := sm.Session(r)
  session.Values["authenticated"] = true
  session.Values["user"] = u.Username
  session.Save(r, w)
  ```
  No parallel auth/session system — every existing `RequireAuth`/`RequireAdmin` guard works
  unchanged for WebAuthn-authenticated requests, since they only ever check
  `session.Values["authenticated"]`/`["user"]`.
- `mobile/mobile/login.html`'s JS talks to `/auth/webauthn/login/begin` + `/login/finish` via
  `navigator.credentials.get({publicKey, mediation: 'optional'})`, base64url-encoding per the
  WebAuthn spec — standard ~40-line vanilla-JS boilerplate, no client-side library needed.

## 10. Rollout order

1. **Shell + read-only views**: `mobile` package skeleton, `mobile_base.html`, password-only
   `/mobile/login/` (using the new `monet.Users()` accessor), `/mobile/` home (needs the
   `RecentByCardActivity` limit-0 change), `/mobile/boards/{slug}/` (needs the `kanban.App`
   exported accessors + `RenderedColumns`), `/mobile/boards/{slug}/cards/{cardID}/` reusing
   `kanban/kanban/card.html`. Drawers wired to existing nav APIs. No mutations, no websocket yet —
   verifies routing, auth-guarding, and template reuse end to end.
2. **Real-time + mutations**: extract `ws-client.js`/`card-detail.js`; wire `mobile.js`'s socket +
   `applyMobileBoardEvent`; add the column-cards partial endpoint; implement touch long-press
   reorder; swipe/scroll-snap paging + pager dots + edge-gradient CSS. All other mutations
   (checklist, comments, labels, dates, color, assign, mark-done, subscribe, archive) come free via
   `card-detail.js` reuse.
3. **WebAuthn**: migration + handlers, "add a passkey" UI in `/user/settings/` and first-run mobile
   prompt, Face ID/fingerprint button on `/mobile/login/`.
4. **Web Push**: VAPID key generation/storage, subscription table+endpoints, `sw.js` push handling,
   the two `kanban/app.go` hook points, subscribe-on-login flow.
5. **Installability polish**: manifest, icon set, `Service-Worker-Allowed` header, app-shell
   cache-first SW handlers, iOS meta tags, safe-area CSS, Lighthouse PWA audit cleanup.

Phases 1→2 are a hard dependency chain; 3 and 4 are independent of each other (either order, or
parallel); 5 can start once phase 1's templates exist but is naturally last.

## 11. Testing / verification

- Extend `migrations_test.go` (currently the only test — spins up in-memory sqlite and calls
  `Migrate()` per app) with `mobile.NewApp(...).Migrate()` and the new `auth`/`kanban` migration
  sets as each phase lands, so `make test` stays the regression gate for schema changes.
- Manual verification via `make run` (reflex live-reload + `--debug`, which also keeps the
  client-side-LESS dev path working for `mobile.less` changes):
  - Chrome DevTools device emulation (iPhone/Pixel presets) for layout/swipe/drawer work in phases
    1–2 and 5.
  - A real device on the same LAN for touch-specific feel (long-press timing/threshold,
    scroll-snap momentum, `navigator.vibrate`) — DevTools touch emulation doesn't reproduce iOS
    Safari's rubber-banding reliably.
  - **WebAuthn** requires a real platform authenticator or Chrome's virtual authenticator
    (DevTools → WebAuthn panel) — note that CI can't exercise the real ceremony without a virtual
    authenticator, and cross-device passkeys (register on desktop, use on phone via hybrid/caBLE)
    are a good manual test but out of scope to automate.
  - **Web Push**: DevTools → Application → Service Workers → "Push" can simulate a push event
    without a real backend send, useful for iterating on `sw.js` before wiring the Go sender;
    end-to-end (real `webpush-go` → real push service → notification) needs HTTPS, so this leg
    needs either a trusted local cert or the nginx-fronted staging/production deployment.
  - `lighthouse` PWA audit after phase 5 (one-off CLI invocation, not a build dependency) to catch
    missing manifest fields, icon sizes, or `Service-Worker-Allowed` misconfiguration.
  - A real-iPhone "Add to Home Screen" smoke test at least once per phase-5 milestone — iOS PWA
    installability/standalone-mode quirks can't be verified in any emulator.

### Critical files
- `/mnt/omocha/jmoiron/dev/giverny/kanban/app.go` — exported accessors, `RenderedColumns`,
  `RenderColumnCards`, `PushNotifier` hook, two push trigger call sites (lines ~1936, ~2604).
- `/mnt/omocha/jmoiron/dev/giverny/kanban/service.go` — `RecentByCardActivity` limit-0 change
  (line ~294), new `NotificationRecipients` (near line ~1003).
- `/mnt/omocha/jmoiron/dev/giverny/main.go` — wire `mobileApp` into `apps`, register `mobile-base`,
  `SetPushNotifier`.
- `/mnt/omocha/jmoiron/dev/giverny/auth/app.go` + new `auth/webauthn.go`/`webauthn_handlers.go` —
  WebAuthn ceremonies + session integration.
- `/mnt/omocha/jmoiron/dev/giverny/static/js/kanban.js` — source of the `ws-client.js`/
  `card-detail.js` extraction.
- `/mnt/omocha/jmoiron/dev/giverny/templates/base.html` — reference for `templates/mobile_base.html`.
- `/mnt/omocha/jmoiron/dev/monet/auth/auth.go` — one-line `Users()` accessor for mobile password login.
