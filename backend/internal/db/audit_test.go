package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// seedAuditEvent inserts the FK chain cluster -> instance -> run -> one
// audit_event and returns the event id.
func seedAuditEvent(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()

	var clusterID, instanceID, runID, eventID int64
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO cluster (name, platform) VALUES ('audit-c', 'vm') RETURNING id`).Scan(&clusterID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO instance (name, cluster_id, env, pg_version, owner)
		 VALUES ('audit-i', $1, 'test', '16.3', 'team') RETURNING id`, clusterID).Scan(&instanceID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		 VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'queued') RETURNING id`, instanceID).Scan(&runID))
	require.NoError(t, pool.QueryRow(ctx,
		`INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		 VALUES ('local-dev', 'run.submitted', $1, $2, 'test', 'dump', 'digest') RETURNING id`,
		runID, instanceID).Scan(&eventID))
	return eventID
}

// SPEC-012 behavior 4 (and WU-014's verify line, pinned early): audit rows
// are append-only — UPDATE and DELETE fail for the app role, the trigger
// enforcing it even though the app owns the table in dev.
func TestAuditEventIsAppendOnly(t *testing.T) {
	pool := testutil.MigratedDB(t)
	id := seedAuditEvent(t, pool)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `UPDATE audit_event SET actor = 'tamperer' WHERE id = $1`, id)
	require.ErrorContains(t, err, "append-only")

	_, err = pool.Exec(ctx, `DELETE FROM audit_event WHERE id = $1`, id)
	require.ErrorContains(t, err, "append-only")

	// TRUNCATE bypasses row-level triggers; 0004's statement trigger covers
	// it (m1-gate item 4).
	_, err = pool.Exec(ctx, `TRUNCATE audit_event`)
	require.ErrorContains(t, err, "append-only")

	// Row is intact.
	var actor string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT actor FROM audit_event WHERE id = $1`, id).Scan(&actor))
	require.Equal(t, "local-dev", actor)

	// Inserts still work (append is the point).
	var runID, instanceID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT run_id, instance_id FROM audit_event WHERE id = $1`, id).Scan(&runID, &instanceID))
	_, err = pool.Exec(ctx,
		`INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest, final_status)
		 VALUES ('local-dev', 'run.finished', $1, $2, 'test', 'dump', 'digest', 'success')`,
		runID, instanceID)
	require.NoError(t, err)
}

// The run table stays mutable — it is operational state, not audit.
func TestRunRowIsMutable(t *testing.T) {
	pool := testutil.MigratedDB(t)
	seedAuditEvent(t, pool)
	ctx := context.Background()

	tag, err := pool.Exec(ctx, `UPDATE run SET state = 'running', updated_at = now() WHERE state = 'queued'`)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}
