// Package testutil provides helpers for DB-backed tests. Tests that need
// the dev database skip (not fail) when it is unreachable, so the suite
// stays green in environments without the compose stack (e.g. CI).
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ios9000/db-portal/backend/internal/config"
)

// Config loads the portal config exactly the way the binary does: process
// env over the repo-root .env (found by upward search) over defaults.
func Config(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Load(config.LocateDotenv())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// DB returns a pool connected to the dev database, or skips the test if it
// is unreachable. The pool is closed automatically when the test ends.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := Config(t)

	pool, err := pgxpool.New(context.Background(), cfg.DSN())
	if err != nil {
		t.Skipf("dev database unavailable (bad DSN): %v", err)
	}
	t.Cleanup(pool.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("dev database unreachable (is the compose env up?): %v", err)
	}
	return pool
}
