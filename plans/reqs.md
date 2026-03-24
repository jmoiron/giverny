# Multi-User Kanban Board — Requirements

## Core Concepts

A kanban board is a visual project management tool where work items (cards) move
through columns representing stages of a workflow. A multi-user kanban board adds
collaboration, shared state, and access control on top of this.

---

## 1. Board Management

- **Create boards** — users can create new boards with a name and optional description
- **Default columns** — new boards come with sensible defaults (e.g. To Do, In Progress, Done)
- **Custom columns** — add, rename, reorder, and delete columns on a board
- **Column WIP limits** — optionally set a max number of cards per column
- **Archive/delete boards** — boards can be archived (soft delete) or permanently removed
- **Board listing** — users see a list of boards they have access to

## 2. Cards

- **Create cards** — add a card to any column with a title and optional description
- **Move cards** — drag-and-drop or explicit move between columns
- **Reorder cards** — change position within a column
- **Card details** — title, description (markdown), assignee(s), labels, due date
- **Labels/tags** — create and assign colored labels for categorization
- **Due dates** — optional due date with visual indicator when overdue
- **Checklists** — sub-tasks within a card with progress tracking
- **Comments** — threaded discussion on each card
- **Attachments** — file or image attachments on cards
- **Archive cards** — remove from active view without permanent deletion

## 3. Users & Authentication

- **Registration & login** — email/password or OAuth (Google, GitHub)
- **User profiles** — display name, avatar
- **Session management** — secure token-based sessions (JWT or session cookies)
- **Password reset** — email-based recovery flow

## 4. Collaboration & Permissions

- **Invite members** — invite users to a board by email or username
- **Roles** — at minimum: Owner, Member, Viewer
  - **Owner** — full control including board deletion and member management
  - **Member** — create/edit/move cards, add comments
  - **Viewer** — read-only access
- **Assignment** — assign one or more members to a card
- **Activity feed** — per-board log of who did what and when

## 5. Real-Time Updates

- **Live sync** — changes made by one user appear for all other users without refresh
- **Presence indicators** — show who is currently viewing the board
- **Conflict handling** — graceful resolution when two users edit the same card simultaneously
- **Notifications** — in-app (and optionally email) notifications for:
  - Being assigned to a card
  - Comments on cards you're assigned to or watching
  - Cards approaching or past due date

## 6. Search & Filtering

- **Full-text search** — find cards by title, description, or comment content
- **Filter by label** — show only cards matching selected labels
- **Filter by assignee** — show cards assigned to a specific user
- **Filter by due date** — overdue, due today, due this week, no date

## 7. Data & API

- **RESTful API** — all board/card/user operations available via API
- **Pagination** — paginated listing endpoints for boards, cards, activity
- **Audit trail** — record of all mutations for compliance/debugging
- **Data export** — export board data as JSON or CSV

## 8. Non-Functional Requirements

- **Responsive UI** — usable on desktop and mobile browsers
- **Performance** — board loads in < 1s for boards with up to 500 cards
- **Availability** — target 99.9% uptime for hosted deployments
- **Security** — HTTPS, CSRF protection, input sanitization, rate limiting
- **Accessibility** — keyboard navigation, screen reader support, WCAG 2.1 AA

---

## Open Questions

- Should boards support multiple "views" (kanban, list, calendar)?
- Is there a need for cross-board card linking or dependencies?
- Should there be workspace/organization-level grouping above boards?
- What is the expected scale (number of users, boards, cards)?
- Is offline support or a native mobile app in scope?
- Should there be integrations (Slack, GitHub, webhooks)?
