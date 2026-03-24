This board should be implemented in Go and deployable as a single binary.

All data storage should be in SQLite, except for images, which can be on the filesystem.

The project should use the same basic 'app' structure as monet, my blogging software. We should import github.com/jmoiron/monet and reuse as many of its components as possible, including its database migration management, its template registration system, auth, the admin system, etc.

The intent behind monet is that it is both an inverted framework (a collection of libraries where the entry points are all controlled by user application code) and a collection of apps built on that set of libraries. It should be possible to build upon it from outside code.

Card data should be stored as markdown with pre-renedring, as in monet. We should be able to reuse goldmark and the goldmark WASM bundle to provide live editing previews.

We should stick to basic jQuery+vanilla JS, as with monet. We should build abstractions that help us deal with the event oriented nature of a kanban system, but we shouldn't adopt something heavy like next or react.

We will have custom user auth like with Monet. We need to have an email based invitation system where I can create an account and invite the user to create their password via an email link with a one-time-use expiring token. I want to make it easy for my wife to be able to make an account.

User access should be read-only, admin, and super-admin. Super-admins can alter/delete/manage user accounts. Admins can alter/delete/manage all of the Kanban data; create boards, cards, etc. Boards have three basic security settings: private, open, and public. A read-only user can view boards that have been set to "open" and anonymous visitors can read boards that have been set to "public".

The site should be usable on mobile. This may entail developing some different UI flows. Lets consider mobile to be secondary.

The site should be able to send email over SMTP. It will require a server, username, and password to do this. These should be stored in the db and configurable via the app. The password can be encrypted at rest with the SECRET specified in the config file to prevent a compromised DB from compromising the password, but this must be reversable encryption as we need to use the password to log into the SMTP server.