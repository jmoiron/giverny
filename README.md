Giverny is a kanban web app that is built on top of the library developed in [monet](/jmoiron/monet/).

It compiles to a single Go binary and uses sqlite3 for data storage.

### features

It's kanban.

* There is a basic user system w/ 3 tiers of access.
* There is a user invite link system for creating new users.
* There is a command line helper for adding your first superadmin.
* There are boards. Boards have columns. Columns have cards.

A lot of the basics for this are there already (users, boards, columns and cards) but they don't work well enough yet to be used.

### planned

Some things I'd like:

I'd like pretty good simultaneous usersupport:

* all backend mutations result in a websocket message sent to all subscribers of a page
* all front-end mutations are based on handling a websocket message

I'd like some good non-kanban views:

* list of tasks with various board/state/content filters
* gantt/calendar view

I'd like decent mobile web support. This _probably_ doesn't mean a mobile kanban view, but maybe a mobile list view.

### not planned

* boards with user-specific permissions or visibility

Probably a lot of other stuff.


### why another kanban

Several reasons, none of them particularly interesting:

* I wanted to see how portable the library substrate of _monet_ was
* I wanted to run this on my own (self-hosted)
* I wanted to be able to add my own features somewhat easily
* I wanted something with a small memory footprint (Go or Rust)
* I wanted something that was easy to back up

I don't imagine it will ever be very good or featureful as kanban software, but it will be mine!