## Giverny

Giverny is a kanban web app that is built on top of the library developed in [monet](https://github.com/jmoiron/monet/).

It compiles to a single Go binary and uses sqlite3 for data storage.

### features

It's kanban.

* There is a basic user system w/ 3 tiers of access.
* There is a user invite link system for creating new users.
* There is a command line helper for adding your first superadmin.
* There are boards. Boards have columns. Columns have cards.
* Cards and columns can be moved around.
* You can assign users and due dates to cards.
* You can attach files to cards.
* You can add a checklist to cards.

All modifications go through a websocket so all users get live updates from every other user.

Giverny is meant to be a personal/family organizational tool, and might not have features that you'd want for large teams or multi-team organizations. These omissions are deliberate, as it simplifies the use and management of the board.

### planned

Some things I'd like:

I'd like some good non-kanban views:

* list of tasks with various board/state/content filters
* gantt/calendar view

I'd like decent mobile web support. This _probably_ doesn't mean a mobile kanban view, but maybe a mobile list view.

### not planned

* boards with user-specific permissions or visibility
* complex user management, eg. teams

Probably a lot of other stuff.


### why another kanban

Several reasons, none of them particularly interesting:

* I wanted to see how portable the library substrate of _monet_ was
* I wanted to run this on my own (self-hosted)
* I wanted to be able to add my own features somewhat easily
* I wanted something with a small memory footprint (Go or Rust)
* I wanted something that was easy to back up

I don't imagine it will ever be very good or featureful as kanban software, but it will be mine!
