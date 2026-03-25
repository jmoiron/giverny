package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	gauth "github.com/jmoiron/giverny/auth"
	"github.com/jmoiron/giverny/conf"
	"github.com/jmoiron/giverny/kanban"
	gsmtp "github.com/jmoiron/giverny/smtp"
	"github.com/jmoiron/monet/app"
	"github.com/jmoiron/monet/auth"
	"github.com/jmoiron/monet/db"
	"github.com/jmoiron/monet/db/monarch"
	"github.com/jmoiron/monet/mtr"
	"github.com/jmoiron/monet/pkg/hotswap"
	"github.com/jmoiron/sqlx"
	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/mattn/go-sqlite3"
)

const (
	cfgEnvVar      = "GIVERNY_CONFIG_PATH"
	givernyVersion = "0.0.1"
)

//go:embed static
var static embed.FS

//go:embed templates
var templates embed.FS

type options struct {
	ConfigPath string
	Debug      bool
	Version    bool

	AddUser      string
	Invite       string
	AddGravatars bool

	GenConf      string
	RegenSecrets bool

	ShowMigration bool
	Downgrade     string
}

var logLevel = new(slog.LevelVar)

func must(err error, msg string, args ...any) {
	if err != nil {
		args = append(args, "err", err)
		slog.Error(msg, args...)
		os.Exit(-1)
	}
}

func die[T any](v T, err error) func(string, ...any) T {
	return func(msg string, args ...any) T {
		if err != nil {
			args = append(args, "err", err)
			slog.Error(msg, args...)
			os.Exit(-1)
		}
		return v
	}
}

func try[T any](v T, err error) func(string, ...any) T {
	return func(msg string, args ...any) T {
		if err != nil {
			args = append(args, "err", err)
			slog.Warn(msg, args...)
		}
		return v
	}
}

func main() {
	slog.SetDefault(slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}),
	))

	var opts options
	parseOpts(&opts)

	if opts.Version {
		v, _, t := sqlite3.Version()
		fmt.Printf("Giverny v%s\n", givernyVersion)
		fmt.Printf("Built w/ SQLite %v (%s)\n", v, strings.Split(t, " ")[0])
		return
	}

	if opts.GenConf != "" {
		must(genConf(opts.GenConf), "generating config")
		return
	}

	if opts.RegenSecrets {
		if opts.ConfigPath == "" {
			slog.Error("--regen-secrets requires --config")
			os.Exit(-1)
		}
		must(regenSecrets(opts.ConfigPath), "regenerating secrets")
		return
	}

	cfg := die(conf.Load(opts.ConfigPath))("loading config")

	if opts.Debug {
		cfg.Debug = true
	}
	if cfg.Debug {
		slog.Info("debug enabled")
		logLevel.Set(slog.LevelDebug)
	}

	dbh := die(sqlx.Connect("sqlite3", cfg.DatabaseURI))("connecting to db", "uri", cfg.DatabaseURI)

	if opts.ShowMigration {
		showMigration(dbh)
		return
	}

	if opts.Downgrade != "" {
		downgradeApp(dbh, opts.Downgrade)
		return
	}

	// monet's auth app creates the base user table and provides login/logout
	authApp := auth.NewApp(&cfg.Config, dbh)
	// giverny's auth app extends monet's with user_profile + invitations
	gauthApp := gauth.NewApp(dbh, cfg.BaseURL)
	// smtp app manages email config and sending
	smtpApp := die(gsmtp.NewApp(dbh, cfg.Secret))("initializing smtp app")

	kanbanApp := kanban.NewApp(dbh)

	// apps is the ordered list of sub-applications. Auth must come first
	// since other tables reference user(id).
	apps := []app.App{authApp, gauthApp, smtpApp, kanbanApp}

	reg := mtr.NewRegistry()
	reg.AddBaseFS("base", "templates/base.html", templates)
	reg.AddPathFS("templates/index.html", templates)

	for _, a := range apps {
		must(a.Migrate(), "migrating app", "name", a.Name())
		a.Register(reg)
	}

	if runUtil(&opts, gauthApp, smtpApp, cfg.BaseURL) {
		return
	}

	must(reg.Build(), "building templates")

	r := chi.NewRouter()

	r.Use(authApp.Sessions.AddSessionMiddleware)
	r.Use(cfg.AddConfigMiddleware)
	r.Use(db.AddDbMiddleware(dbh))
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(mtr.AddRegistryMiddleware(reg))
	r.Use(gauth.AddUserMiddleware(gauthApp.Users()))

	for _, a := range apps {
		a.Bind(r)
	}

	r.Get("/", index)

	staticFS := die(fs.Sub(static, "static"))("initializing static fs")
	swp := hotswap.NewSwapper(staticFS)
	if cfg.Debug {
		swp.Swap()
	}

	cacheStack := []func(http.Handler) http.Handler{
		middleware.Compress(5),
		middleware.SetHeader("Cache-Control", "max-age=120"),
	}
	if cfg.Debug {
		cacheStack[1] = middleware.NoCache
	}

	r.With(cacheStack...).Handle("/static/*", http.StripPrefix("/static", http.FileServer(http.FS(swp))))

	slog.Info("listening", "addr", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, r); err != nil {
		slog.Error("server error", "err", err)
	}
}

func index(w http.ResponseWriter, r *http.Request) {
	reg := mtr.RegistryFromContext(r.Context())
	u := gauth.UserFromContext(r.Context())
	err := reg.RenderWithBase(w, "base", "templates/index.html", mtr.Ctx{
		"title": "",
		"user":  u,
	})
	if err != nil {
		app.Http500("rendering index", w, err)
	}
}

func showMigration(dbh db.DB) {
	m, err := monarch.NewManager(dbh)
	if err != nil {
		slog.Error("initializing monarch", "err", err)
		return
	}
	latest, err := m.LatestVersions()
	if err != nil {
		slog.Error("fetching versions", "err", err)
		return
	}
	for _, v := range latest {
		fmt.Printf("app=%s version=%d applied-at=%s\n", v.Name, v.Version, v.AppliedAt)
	}
}

func downgradeApp(dbh db.DB, name string) {
	m, err := monarch.NewManager(dbh)
	if err != nil {
		slog.Error("initializing monarch", "err", err)
		return
	}
	if err := m.Downgrade(name); err != nil {
		slog.Error("downgrading app", "app", name, "err", err)
	}
}

func parseOpts(opts *options) {
	pflag.StringVarP(&opts.ConfigPath, "config", "c", os.Getenv(cfgEnvVar), "path to a json config file")
	pflag.BoolVarP(&opts.Debug, "debug", "d", false, "enable debug mode")
	pflag.BoolVarP(&opts.Version, "version", "v", false, "show version info")
	pflag.StringVar(&opts.AddUser, "add-user", "", "create a super-admin user with the given username (email = username, prompted for password)")
	pflag.BoolVar(&opts.AddGravatars, "add-gravatars", false, "look up and set gravatar URIs for users with no profile image")
	pflag.StringVar(&opts.Invite, "invite", "", "send an invitation email to the given address")
	pflag.StringVar(&opts.GenConf, "gen-conf", "", "generate a config file with random secrets at the given path")
	pflag.BoolVar(&opts.RegenSecrets, "regen-secrets", false, "regenerate SessionSecret and Secret in the config file (requires --config)")
	pflag.BoolVar(&opts.ShowMigration, "migrations", false, "show migration state for each app")
	pflag.StringVar(&opts.Downgrade, "downgrade", "", "downgrade a named app by one migration version")
	pflag.Parse()
}

// randomHex returns n cryptographically random bytes encoded as a hex string.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		slog.Error("reading random bytes", "err", err)
		os.Exit(-1)
	}
	return hex.EncodeToString(b)
}

// genConf writes a default config with fresh random secrets to path.
// Fails if the file already exists to avoid clobbering a working config.
func genConf(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg := conf.Default()
	cfg.SessionSecret = randomHex(32)
	cfg.Secret = randomHex(32)

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

// regenSecrets reads the config at path, replaces SessionSecret and Secret
// with fresh random values, and writes the file back in place.
func regenSecrets(path string) error {
	cfg, err := conf.Load(path)
	if err != nil {
		return err
	}

	cfg.SessionSecret = randomHex(32)
	cfg.Secret = randomHex(32)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(cfg)
}

func runUtil(opts *options, gauthApp *gauth.App, smtpApp *gsmtp.App, baseURL string) bool {
	switch {
	case opts.AddUser != "":
		if err := addUser(opts.AddUser, gauthApp.Users()); err != nil {
			slog.Error("adding user", "err", err)
			os.Exit(1)
		}
	case opts.Invite != "":
		if err := sendInvite(opts.Invite, gauthApp, smtpApp, baseURL); err != nil {
			slog.Error("sending invite", "err", err)
			os.Exit(1)
		}
	case opts.AddGravatars:
		if err := addGravatars(gauthApp.Users()); err != nil {
			slog.Error("adding gravatars", "err", err)
			os.Exit(1)
		}
	default:
		return false
	}
	return true
}

func sendInvite(email string, gauthApp *gauth.App, smtpApp *gsmtp.App, baseURL string) error {
	// Use a zero ID for CLI-created invites (no logged-in user).
	token, err := gauthApp.Invites().Create(email, 0)
	if err != nil {
		return fmt.Errorf("creating invitation: %w", err)
	}
	link := fmt.Sprintf("%s/invite/%s", strings.TrimRight(baseURL, "/"), token)
	fmt.Printf("invitation link for %s:\n  %s\n", email, link)

	smtpCfg, err := smtpApp.Service().Get()
	if err != nil || smtpCfg.Host == "" {
		fmt.Println("(smtp not configured — send the link above manually)")
		return nil
	}
	body := fmt.Sprintf("You have been invited to Giverny.\n\nAccept your invitation here:\n%s\n\nThis link expires in 72 hours.\n", link)
	if err := smtpApp.Service().Send(email, "You're invited to Giverny", body); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	fmt.Println("invitation email sent.")
	return nil
}

func addUser(username string, users *gauth.UserProfileService) error {
	existing, err := users.GetByUsername(username)
	if err == nil {
		fmt.Printf("user %q already exists. replace? [y/N]: ", username)
		var answer string
		fmt.Scan(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			return nil
		}
		if err := users.DeleteUser(existing.ID); err != nil {
			return fmt.Errorf("deleting existing user: %w", err)
		}
	}

	fmt.Print("password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return err
	}

	return users.CreateUser(username, username, string(passwordBytes), gauth.RoleSuperAdmin, gauth.GravatarURI(username))
}

func addGravatars(users *gauth.UserProfileService) error {
	all, err := users.List()
	if err != nil {
		return err
	}
	for _, u := range all {
		if u.ProfileImageURI != "" {
			continue
		}
		uri := gauth.GravatarURI(u.Email)
		if uri == "" {
			fmt.Printf("no gravatar: %s\n", u.Email)
			continue
		}
		if err := users.SetProfileImageURI(u.ID, uri); err != nil {
			return fmt.Errorf("setting gravatar for %s: %w", u.Email, err)
		}
		fmt.Printf("set gravatar: %s\n", u.Email)
	}
	return nil
}
