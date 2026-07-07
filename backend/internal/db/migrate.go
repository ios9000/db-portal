package db

import (
	"context"
	"embed"
	"errors"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate runs the given goose command ("up", "down" or "status") against
// the database at dsn, using the migrations embedded in the binary.
func Migrate(ctx context.Context, dsn, command string) (err error) {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("migrate: set dialect: %w", err)
	}

	sqldb, err := goose.OpenDBWithDriver("pgx", dsn)
	if err != nil {
		return fmt.Errorf("migrate: open db: %w", err)
	}
	defer func() { err = errors.Join(err, sqldb.Close()) }()

	switch command {
	case "up":
		err = goose.UpContext(ctx, sqldb, "migrations")
	case "down":
		err = goose.DownContext(ctx, sqldb, "migrations")
	case "status":
		err = goose.StatusContext(ctx, sqldb, "migrations")
	default:
		err = fmt.Errorf("migrate: unknown command %q (want up, down or status)", command)
	}
	return err
}
