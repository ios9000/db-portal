package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/db"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// TestMigrateUpDown runs the embedded migrations up and down against a
// scratch database created on the dev Postgres (skips if unreachable).
func TestMigrateUpDown(t *testing.T) {
	admin := testutil.DB(t)
	ctx := context.Background()

	suffix := make([]byte, 4)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	scratch := "portal_test_" + hex.EncodeToString(suffix)

	_, err = admin.Exec(ctx, "CREATE DATABASE "+scratch)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.Exec(context.Background(), "DROP DATABASE "+scratch+" WITH (FORCE)")
		require.NoError(t, err)
	})

	cfg := testutil.Config(t)
	cfg.DBName = scratch
	dsn := cfg.DSN()

	require.NoError(t, db.Migrate(ctx, dsn, "up"))

	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	var baseline string
	err = pool.QueryRow(ctx,
		"SELECT value FROM app_meta WHERE key = 'schema_baseline'").Scan(&baseline)
	require.NoError(t, err)
	require.Equal(t, "0001", baseline)
	require.True(t, tableExists(t, pool, "instance"), "0002 up must create the inventory tables")
	require.True(t, tableExists(t, pool, "audit_event"), "0003 up must create the runs/audit tables")
	require.True(t, columnExists(t, pool, "audit_event", "job_id"), "0004 up must add audit_event.job_id")
	require.True(t, tableExists(t, pool, "session"), "0005 up must create the session table")
	require.True(t, tableExists(t, pool, "auth_event"), "0005 up must create the auth trail")

	// goose down reverts one migration at a time; walk back to zero and
	// check each Down does its job.
	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "session"), "0005 down must remove the session table")
	require.False(t, tableExists(t, pool, "auth_event"), "0005 down must remove the auth trail")
	require.True(t, columnExists(t, pool, "audit_event", "job_id"))

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, columnExists(t, pool, "audit_event", "job_id"), "0004 down must remove audit_event.job_id")
	require.True(t, tableExists(t, pool, "audit_event"))

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "audit_event"), "0003 down must remove the runs/audit tables")
	require.True(t, tableExists(t, pool, "instance"))

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "instance"), "0002 down must remove the inventory tables")
	require.True(t, tableExists(t, pool, "app_meta"))

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "app_meta"), "0001 down must remove the baseline table")
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = $1)", name).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func columnExists(t *testing.T, pool *pgxpool.Pool, table, column string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns
		 WHERE table_name = $1 AND column_name = $2)`, table, column).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func TestMigrateUnknownCommand(t *testing.T) {
	err := db.Migrate(context.Background(), "postgres://localhost/x", "sideways")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}
