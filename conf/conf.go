package conf

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	monet "github.com/jmoiron/monet/conf"
)

type configKey struct{}

// Config holds options for giverny. It embeds monet's Config so that monet
// sub-packages (auth, mtr, etc.) can receive *monet.Config directly via
// &cfg.Config.
type Config struct {
	monet.Config
	// Secret is used for reversible encryption of stored secrets (e.g. SMTP password).
	Secret string
}

// String returns the config as indented JSON (omits Secret).
func (c *Config) String() string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	_ = enc.Encode(c)
	return buf.String()
}

// FromPath loads and merges a JSON config file at path.
func (c *Config) FromPath(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return c.FromReader(f)
}

// FromReader decodes JSON config from r.
func (c *Config) FromReader(r io.Reader) error {
	return json.NewDecoder(r).Decode(c)
}

// AddConfigMiddleware adds this config to the request context.
func (c *Config) AddConfigMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), configKey{}, c)))
	})
}

// ConfigFromContext retrieves the giverny Config from ctx.
func ConfigFromContext(ctx context.Context) *Config {
	return ctx.Value(configKey{}).(*Config)
}

// Default returns a Config with sensible defaults.
func Default() *Config {
	c := &Config{}
	c.ListenAddr = "0.0.0.0:7100"
	c.SessionSecret = "SET-IN-CONFIG-FILE"
	c.DatabaseURI = "giverny.db"
	return c
}

// Load returns a Default config, optionally merged with the file at path.
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	return cfg, cfg.FromPath(path)
}
