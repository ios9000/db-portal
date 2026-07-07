// Package db owns the portal's database access: the pgx pool and
// goose migrations (embedded in the binary).
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates a pgx connection pool. It does not connect eagerly:
// the server must start (and report degraded health) even when the
// database is down.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}
	return pool, nil
}
