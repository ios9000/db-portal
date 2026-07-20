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

	// Concurrency locks (SPEC-042). LockTTL sizes the instance-lock backstop:
	// a lock whose holder run is still live is NEVER stolen (mini-ADR 3), so
	// this only bounds how long a genuinely-leaked lock lingers before a reap.
	// ProtectedInstances is the self-target ban's declared set (mini-ADR 5);
	// see ProtectedInstanceSet, which also seeds it with DBName.
	LockTTL            time.Duration `env:"PORTAL_LOCK_TTL"             envDefault:"30m"`
	ProtectedInstances string        `env:"PORTAL_PROTECTED_INSTANCES"`

	// Maintenance & retention (SPEC-044). MaintenanceInterval is the sweep
	// cadence; non-positive disables the loop. ArtifactRetention is the max age
	// of a `standard` registry artifact before it is reaped — non-positive
	// DISABLES the artifact sweep (a zero age would reap everything, mini-ADR 5);
	// `safety` artifacts are never reaped. AuditRetention is observational only:
	// the append-only ledgers are retained in-DB and the sweep logs when the
	// oldest event exceeds it (mini-ADR 2). Durations use Go units (e.g. 2160h
	// = 90d, 8760h = 365d) — the day unit is not supported.
	MaintenanceInterval time.Duration `env:"PORTAL_MAINTENANCE_INTERVAL" envDefault:"1h"`
	ArtifactRetention   time.Duration `env:"PORTAL_ARTIFACT_RETENTION"   envDefault:"2160h"`
	AuditRetention      time.Duration `env:"PORTAL_AUDIT_RETENTION"      envDefault:"8760h"`

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

	// Engine (SPEC-033). EngineNonProd picks the NON-PROD class adapter:
	// mock (default, forever per ADR-002) | semaphore (opt-in real engine).
	// Prod stays mock in dev. An unknown value fails at wiring (main), not
	// here. The Semaphore* fields are a disjoint config object (guardrail 3);
	// secrets live in .env only (ADR-004). ProjectID scopes the task API
	// (paths are /api/project/{id}/tasks).
	EngineNonProd          string        `env:"PORTAL_ENGINE_NONPROD"          envDefault:"mock"`
	SemaphoreURL           string        `env:"PORTAL_SEMAPHORE_URL"           envDefault:"http://127.0.0.1:3000"`
	SemaphoreAPIToken      string        `env:"PORTAL_SEMAPHORE_API_TOKEN"`
	SemaphoreProjectID     int           `env:"PORTAL_SEMAPHORE_PROJECT_ID"    envDefault:"1"`
	SemaphoreWebhookSecret string        `env:"PORTAL_SEMAPHORE_WEBHOOK_SECRET"`
	SemaphoreTemplates     string        `env:"PORTAL_SEMAPHORE_TEMPLATES"`
	SemaphorePollInterval  time.Duration `env:"PORTAL_SEMAPHORE_POLL_INTERVAL" envDefault:"3s"`
}

// SemaphoreTemplateMap parses PORTAL_SEMAPHORE_TEMPLATES ("tag:id,tag:id")
// into a playbook-tag → Semaphore-template-id map (SPEC-033 mini-ADR 3). An
// empty value yields an empty map — fail-closed then happens per-StartJob on
// an unmapped tag; a malformed entry is an error so a typo fails at boot, not
// at run time.
func (c Config) SemaphoreTemplateMap() (map[string]int, error) {
	out := map[string]int{}
	for _, part := range strings.Split(c.SemaphoreTemplates, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, idStr, ok := strings.Cut(part, ":")
		tag = strings.TrimSpace(tag)
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if !ok || tag == "" || err != nil {
			return nil, fmt.Errorf("config: bad PORTAL_SEMAPHORE_TEMPLATES entry %q (want tag:id)", part)
		}
		out[tag] = id
	}
	return out, nil
}

// ProtectedInstanceSet is the self-target ban's denylist (SPEC-042 mini-ADR 5):
// instance names that must never be a portal target, lowercased for
// case-insensitive match. It is PORTAL_PROTECTED_INSTANCES (comma-separated)
// unioned with the portal's own DBName, so an instance literally named the same
// as the portal DB is refused out of the box even with no explicit config.
func (c Config) ProtectedInstanceSet() map[string]bool {
	out := map[string]bool{}
	if n := strings.ToLower(strings.TrimSpace(c.DBName)); n != "" {
		out[n] = true
	}
	for _, part := range strings.Split(c.ProtectedInstances, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out[p] = true
		}
	}
	return out
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

// LocateDotenv returns the path to the .env file config.Load should read, or
// "" for none (Load treats "" as "no dotenv file"). Resolution:
//
//  1. PORTAL_DOTENV, if set, is honored verbatim — an explicit path, or "" to
//     disable the search. This is the escape hatch for a prod/systemd deploy,
//     where config comes from the process env / EnvironmentFile.
//  2. Otherwise walk UP from the CWD for a .env, but confine the search to the
//     repository: never look above the repo root (the nearest ancestor holding
//     .git). Dev runs the binary from backend/ (or a test from its package
//     dir) while .env lives at the repo root — both inside the repo, so the
//     bounded walk still finds it. Outside any repo (a standalone prod binary)
//     the boundary is the CWD itself, so only ./.env is considered and a stray
//     .env in an unrelated parent can never be silently adopted (M1-gate item
//     16). main logs the resolved path at startup.
func LocateDotenv() string {
	if p, ok := os.LookupEnv("PORTAL_DOTENV"); ok {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	boundary := repoBoundary(cwd)
	for dir := cwd; ; {
		if path := filepath.Join(dir, ".env"); isRegularFile(path) {
			return path
		}
		if dir == boundary {
			return ""
		}
		dir = filepath.Dir(dir)
	}
}

// repoBoundary returns the topmost directory LocateDotenv may search: the
// nearest ancestor of cwd (inclusive) that holds a .git entry — the repo root —
// or cwd itself when cwd is not inside a repository. .git may be a directory (a
// normal clone) or a file (a worktree/submodule), so existence, not dir-ness,
// is the test. go.mod is deliberately NOT a boundary: it lives in backend/, one
// level below the repo root where .env sits, so stopping there would miss it.
func repoBoundary(cwd string) string {
	for dir := cwd; ; {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

// isRegularFile reports whether path exists and is a regular file (not a dir).
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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
