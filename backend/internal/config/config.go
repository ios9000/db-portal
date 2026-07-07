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
