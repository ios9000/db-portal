package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"

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

	require.NoError(t, db.Migrate(ctx, dsn, "down"))

	var exists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'app_meta')").Scan(&exists)
	require.NoError(t, err)
	require.False(t, exists, "goose down must remove the baseline table")
}

func TestMigrateUnknownCommand(t *testing.T) {
	err := db.Migrate(context.Background(), "postgres://localhost/x", "sideways")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}
