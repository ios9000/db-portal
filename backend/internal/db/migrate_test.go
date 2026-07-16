package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/db"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// TestMigrateUpDown runs the embedded migrations up and down against a
// scratch database created on the dev Postgres (skips if unreachable).
func TestMigrateUpDown(t *testing.T) {
	ctx := context.Background()
	dsn := scratchDSN(t)

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
	require.True(t, tableExists(t, pool, "user_role"), "0006 up must create the role store")
	var seeded bool
	require.NoError(t, pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM user_role ur JOIN role r ON r.id = ur.role_id
		WHERE ur.username = 'break-glass' AND r.name = 'dba')`).Scan(&seeded))
	require.True(t, seeded, "0006 up must seed the break-glass dba grant")
	require.True(t, tableExists(t, pool, "schedule"), "0007 up must create the schedule table")
	require.True(t, columnExists(t, pool, "schedule", "confirm"),
		"0008 up must add the stored ritual evidence (M2-gate finding 1)")
	require.True(t, tableExists(t, pool, "artifact"), "0009 up must create the artifact registry")
	require.True(t, tableExists(t, pool, "chain"), "0010 up must create the chain table")
	require.True(t, tableExists(t, pool, "chain_step"), "0010 up must create the chain_step table")
	require.True(t, indexExists(t, pool, "run_job_id_unique"), "0011 up must add the partial job_id unique index")

	// goose down reverts one migration at a time; walk back to zero and
	// check each Down does its job.
	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, indexExists(t, pool, "run_job_id_unique"), "0011 down must drop the job_id unique index")
	require.True(t, tableExists(t, pool, "chain"), "0010 must survive 0011 down")

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "chain"), "0010 down must remove the chain tables")
	require.False(t, tableExists(t, pool, "chain_step"))
	require.True(t, tableExists(t, pool, "artifact"), "0009 must survive 0010 down")

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "artifact"), "0009 down must remove the artifact registry")
	require.True(t, columnExists(t, pool, "schedule", "confirm"), "0008 must survive 0009 down")

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, columnExists(t, pool, "schedule", "confirm"), "0008 down must remove the column")
	require.True(t, tableExists(t, pool, "schedule"), "0007 must survive 0008 down")

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "schedule"), "0007 down must remove the schedule table")
	require.True(t, tableExists(t, pool, "role"), "0006 must survive 0007 down")

	require.NoError(t, db.Migrate(ctx, dsn, "down"))
	require.False(t, tableExists(t, pool, "role"), "0006 down must remove the role store")
	require.False(t, tableExists(t, pool, "user_role"))
	require.True(t, tableExists(t, pool, "auth_event"), "0005 must survive 0006 down")

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

// scratchDSN creates a throwaway database on the dev Postgres (skips if
// unreachable) and returns its DSN; the database drops on test cleanup.
func scratchDSN(t *testing.T) string {
	t.Helper()
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
	return cfg.DSN()
}

// TestArtifactBackfillWalk pins 0009's backfill (SPEC-030 behavior 4):
// every historical successful dump registers exactly once, artifact-less
// and non-success runs never do, and the up→down→up walk lands the
// identical set each time.
func TestArtifactBackfillWalk(t *testing.T) {
	ctx := context.Background()
	dsn := scratchDSN(t)
	require.NoError(t, db.Migrate(ctx, dsn, "up"))

	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	// A pre-registry estate: the runs exist, the artifact table (created
	// empty above) has never seen them — the walk's re-up must backfill.
	var clusterID, instanceID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO cluster (name, platform) VALUES ('c1', 'vm') RETURNING id`).Scan(&clusterID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO instance (name, cluster_id, env, pg_version, owner)
		VALUES ('billing-test', $1, 'test', '16.3', 'team') RETURNING id`,
		clusterID).Scan(&instanceID))

	finished := time.Date(2026, 7, 1, 3, 0, 0, 0, time.UTC)
	insertRun := func(operation, state string, withArtifact bool) int64 {
		t.Helper()
		var name, checksum *string
		var size *int64
		if withArtifact {
			n, sz, sum := "billing-test."+operation+".tgz", int64(1234), "abc123"
			name, size, checksum = &n, &sz, &sum
		}
		var id int64
		require.NoError(t, pool.QueryRow(ctx, `
			INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag,
				state, artifact_name, artifact_size_bytes, artifact_checksum, finished_at)
			VALUES ($1, $2, 'test', 'nonprod', 'dump', $3, $4, $5, $6, $7)
			RETURNING id`,
			instanceID, operation, state, name, size, checksum, finished).Scan(&id))
		return id
	}
	dumped := insertRun("dump", "success", true)
	// A safety dump's class must survive the down→up walk (M3-gate item 6):
	// the backfill derives it from run.operation, not the column DEFAULT.
	safety := insertRun("safety_dump", "success", true)
	insertRun("dump", "success", false) // artifact-less success: nothing to register
	insertRun("dump", "failed", true)   // non-success never registers

	for range 2 {
		// goose down steps one migration; 0011 (job_id index) + 0010 (chains)
		// sit above 0009 now, so reaching below the registry takes three.
		require.NoError(t, db.Migrate(ctx, dsn, "down"))
		require.NoError(t, db.Migrate(ctx, dsn, "down"))
		require.NoError(t, db.Migrate(ctx, dsn, "down"))
		require.False(t, tableExists(t, pool, "artifact"))
		require.NoError(t, db.Migrate(ctx, dsn, "up"))

		rows, err := pool.Query(ctx,
			`SELECT run_id, name, retention_class, created_at FROM artifact ORDER BY id`)
		require.NoError(t, err)
		type row struct {
			runID       int64
			name, class string
			createdAt   time.Time
		}
		var got []row
		for rows.Next() {
			var r row
			require.NoError(t, rows.Scan(&r.runID, &r.name, &r.class, &r.createdAt))
			got = append(got, r)
		}
		rows.Close()
		require.NoError(t, rows.Err())

		require.Len(t, got, 2, "the historical successful dump + safety dump, once each")
		require.Equal(t, dumped, got[0].runID)
		require.Equal(t, "billing-test.dump.tgz", got[0].name)
		require.Equal(t, "standard", got[0].class)
		require.Equal(t, finished, got[0].createdAt.UTC(),
			"backfilled created_at is the run's finished_at, not migration time")
		require.Equal(t, safety, got[1].runID)
		require.Equal(t, "safety", got[1].class,
			"the safety dump keeps its class across a down→up walk (M3-gate item 6)")
	}
}

// TestJobIDUniqueConstraint pins 0011: the partial UNIQUE index rejects a
// reused non-null job_id (hardening ReconcileByJobID against a Semaphore
// task-id wipe replaying ids while a run is in-flight) while permitting many
// NULLs, so queued runs — which carry no id yet — are unaffected.
func TestJobIDUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	dsn := scratchDSN(t)
	require.NoError(t, db.Migrate(ctx, dsn, "up"))

	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()

	var clusterID, instanceID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO cluster (name, platform) VALUES ('c1', 'vm') RETURNING id`).Scan(&clusterID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO instance (name, cluster_id, env, pg_version, owner)
		VALUES ('billing-test', $1, 'test', '16.3', 'team') RETURNING id`,
		clusterID).Scan(&instanceID))

	insert := func(jobID *string) error {
		_, err := pool.Exec(ctx, `
			INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, job_id)
			VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'running', $2)`,
			instanceID, jobID)
		return err
	}

	// Multiple NULL job_ids coexist — PG treats NULLs as distinct, so queued
	// runs (no id assigned yet) are never blocked.
	require.NoError(t, insert(nil))
	require.NoError(t, insert(nil))

	// The first non-null id inserts; a second run reusing it is rejected by
	// the partial unique index.
	id := "task-77"
	require.NoError(t, insert(&id))
	err = insert(&id)
	require.Error(t, err, "a duplicate non-null job_id must violate run_job_id_unique")
	require.Contains(t, err.Error(), "run_job_id_unique")

	// A different non-null id is still free to insert.
	other := "task-78"
	require.NoError(t, insert(&other))
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

func indexExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)", name).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func TestMigrateUnknownCommand(t *testing.T) {
	err := db.Migrate(context.Background(), "postgres://localhost/x", "sideways")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown command")
}
