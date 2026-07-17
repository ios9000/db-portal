package schedule_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// SPEC-042 mini-ADR 4: the instance lock is the scheduler's real overlap
// authority. Here the run-table probe sees nothing (no live run), but the
// instance is held by a lock — a fire that races through the probe still gets
// ErrInstanceLocked from Start, and the scheduler maps it to the same
// skip-visibly stamp instead of an error or a second concurrent op.
func TestFireSkipsWhenInstanceLocked(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	var iid int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM instance WHERE name = 'billing-test'`).Scan(&iid))
	// A held lock whose holder run is NOT live (so the probe passes) and NOT
	// expired (so it is not reaped): exactly the window the probe cannot see.
	var holder int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'success') RETURNING id`, iid).Scan(&holder))
	_, err := pool.Exec(ctx, `
		INSERT INTO instance_lock (instance_id, run_id, actor, expires_at)
		VALUES ($1, $2, 'other', now() + interval '1 hour')`, iid, holder)
	require.NoError(t, err)

	sc, err := svc.Create(ctx, createReq("billing-test", "* * * * *"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	before := runCount(t, pool)

	svc.FireDue(ctx)

	got := getSchedule(t, svc, sc.ID)
	require.NotNil(t, got.LastFireStatus)
	require.Equal(t, "skipped_overlap", *got.LastFireStatus,
		"a locked instance skips visibly, not errors")
	require.Equal(t, before, runCount(t, pool), "no run is created for a skipped fire")
	require.True(t, got.NextFireAt.After(time.Now()), "a skip still advances the clock")
}
