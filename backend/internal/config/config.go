// Package config loads portal configuration from the environment,
// optionally overlaid on a dotenv file (12-factor: process env wins).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the full portal configuration. Precedence per field:
// process env > dotenv file > default.
type Config struct {
	HTTPAddr   string `env:"PORTAL_HTTP_ADDR" envDefault:":8080"`
	DBHost     string `env:"PORTAL_DB_HOST"   envDefault:"127.0.0.1"`
	DBPort     int    `env:"PORTAL_DB_PORT"   envDefault:"5432"`
	DBUser     string `env:"PORTAL_DB_USER"   envDefault:"portal"`
	DBPassword string `env:"PORTAL_DB_PASSWORD"`
	DBName     string `env:"PORTAL_DB_NAME"   envDefault:"portal"`

	// Notification mail (SPEC-014). NotifyTo empty = notifications off.
	SMTPHost string `env:"PORTAL_SMTP_HOST" envDefault:"127.0.0.1"`
	SMTPPort int    `env:"PORTAL_SMTP_PORT" envDefault:"1025"`
	SMTPFrom string `env:"PORTAL_SMTP_FROM" envDefault:"portal@db-portal.local"`
	NotifyTo string `env:"PORTAL_NOTIFY_TO"`
	BaseURL  string `env:"PORTAL_BASE_URL"  envDefault:"http://localhost:8080"`

	// AuthN (SPEC-020). AuthMode defaults to ldap so a misconfigured portal
	// fails closed (no LDAP URL → every login fails, API stays 401).
	// BreakglassHash empty = the break-glass account is disabled.
	AuthMode         string        `env:"PORTAL_AUTH_MODE"          envDefault:"ldap"`
	LDAPURL          string        `env:"PORTAL_LDAP_URL"`
	LDAPBindTemplate string        `env:"PORTAL_LDAP_BIND_TEMPLATE"`
	LDAPInsecure     bool          `env:"PORTAL_LDAP_INSECURE"      envDefault:"false"`
	BreakglassHash   string        `env:"PORTAL_BREAKGLASS_HASH"`
	SessionTTL       time.Duration `env:"PORTAL_SESSION_TTL"        envDefault:"12h"`
	CookieSecure     bool          `env:"PORTAL_COOKIE_SECURE"      envDefault:"false"`
}

// Load builds a Config. dotenvPath may be "" (no file) or point at a dotenv
// file; a missing file is not an error. The process environment is never
// mutated — file values are merged below process env before parsing.
func Load(dotenvPath string) (Config, error) {
	vals := map[string]string{}
	if dotenvPath != "" {
		fileVals, err := godotenv.Read(dotenvPath)
		switch {
		case err == nil:
			vals = fileVals
		case errors.Is(err, fs.ErrNotExist):
			// no dotenv file — env + defaults only
		default:
			return Config{}, fmt.Errorf("config: read %s: %w", dotenvPath, err)
		}
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			vals[k] = v
		}
	}

	var cfg Config
	if err := env.ParseWithOptions(&cfg, env.Options{Environment: vals}); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

// LocateDotenv walks up from the CWD looking for a .env file and returns
// its path, or "" if none exists (Load treats "" as "no dotenv file").
// Dev runs the binary from backend/ (or tests from their package dir)
// while .env lives at the repo root; in prod there is no .env at all.
func LocateDotenv() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		path := filepath.Join(dir, ".env")
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// SMTPAddr returns the notification SMTP endpoint as host:port.
func (c Config) SMTPAddr() string {
	return net.JoinHostPort(c.SMTPHost, strconv.Itoa(c.SMTPPort))
}

// NotifyRecipients parses PORTAL_NOTIFY_TO (comma-separated). An empty
// result means notifications are disabled (SPEC-014).
func (c Config) NotifyRecipients() []string {
	out := []string{}
	for _, s := range strings.Split(c.NotifyTo, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// DSN returns the portal database connection string.
func (c Config) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DBUser, c.DBPassword),
		Host:   net.JoinHostPort(c.DBHost, strconv.Itoa(c.DBPort)),
		Path:   c.DBName,
	}
	return u.String()
}
