package runs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

func instanceID(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT id FROM instance WHERE name = $1`, name).Scan(&id))
	return id
}

// SPEC-042 AC 1: with an operation live on an instance, only ONE of N racing
// Starts takes the lock; the rest get ErrInstanceLocked. No second run row is
// even created — a conflicted launch rolls its whole tx back.
func TestInstanceLockSerializesConcurrentStarts(t *testing.T) {
	// A generous mock delay keeps the winner's run live while every racer
	// attempts, so the contention is real rather than a timing artifact.
	svc, pool := newServiceWithDelay(t, 300*time.Millisecond)
	ctx := context.Background()

	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	ids := make([]int64, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release all goroutines together
			run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
			results[i], ids[i] = err, run.ID
		}()
	}
	close(start)
	wg.Wait()

	wins, conflicts := 0, 0
	for i, err := range results {
		if err == nil {
			wins++
			require.NotZero(t, ids[i], "the winner carries a real run id")
		} else {
			require.ErrorIs(t, err, runs.ErrInstanceLocked)
			conflicts++
			require.Zero(t, ids[i], "a conflicted launch creates no run")
		}
	}
	require.Equal(t, 1, wins, "exactly one Start proceeds")
	require.Equal(t, n-1, conflicts, "the rest get ErrInstanceLocked")

	// Only the winner's run exists — the N-1 conflicts left nothing behind.
	var runRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM run WHERE instance_id = $1`,
		instanceID(t, pool, "billing-test")).Scan(&runRows))
	require.Equal(t, 1, runRows)

	// Once the winner finalizes the lock frees, and a fresh Start succeeds —
	// release rides finalize's own transaction (mini-ADR 2).
	waitTerminal(t, svc, ids[indexOfWinner(results)])
	var held int
	require.NoError(t, ctxCount(ctx, pool, "billing-test", &held))
	require.Equal(t, 0, held, "the lock is gone after the holder finalizes")

	next, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err, "a freed instance accepts the next op")
	waitTerminal(t, svc, next.ID)
}

func indexOfWinner(results []error) int {
	for i, err := range results {
		if err == nil {
			return i
		}
	}
	return -1
}

func ctxCount(ctx context.Context, pool *pgxpool.Pool, instance string, out *int) error {
	return pool.QueryRow(ctx, `
		SELECT count(*) FROM instance_lock l JOIN instance i ON i.id = l.instance_id
		WHERE i.name = $1`, instance).Scan(out)
}

// SPEC-042 mini-ADR 3: an expired lock whose holder run is no longer live is
// reaped by the next acquire (the TTL backstop). A crashed holder never wedges
// the instance permanently. The generous mock delay keeps the new run LIVE
// while the lock row is asserted — at the 1ms delay the run can finalize (and
// finalize releases the lock) before the SELECT on a slow CI runner
// (flaked in CI run 31531854861, s35).
func TestInstanceLockReapsExpiredDeadHolder(t *testing.T) {
	svc, pool := newServiceWithDelay(t, 300*time.Millisecond)
	ctx := context.Background()
	iid := instanceID(t, pool, "billing-test")

	// A dead holder: a terminal run with an already-expired lock row.
	var deadRun int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'failed') RETURNING id`, iid).Scan(&deadRun))
	_, err := pool.Exec(ctx, `
		INSERT INTO instance_lock (instance_id, run_id, actor, expires_at)
		VALUES ($1, $2, 'ghost', now() - interval '1 hour')`, iid, deadRun)
	require.NoError(t, err)

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err, "the expired dead holder's lock must be reaped")

	var holder int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT run_id FROM instance_lock WHERE instance_id = $1`, iid).Scan(&holder))
	require.Equal(t, run.ID, holder, "the lock now belongs to the new run")
	waitTerminal(t, svc, run.ID)
}

// SPEC-042 mini-ADR 3 (the load-bearing safety property): an expired lock whose
// holder run is STILL LIVE is never stolen — a long-running op is never joined
// by a second, whatever the TTL.
func TestInstanceLockNeverStealsFromLiveHolder(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	iid := instanceID(t, pool, "billing-test")

	var liveRun int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'running') RETURNING id`, iid).Scan(&liveRun))
	// Expired on the clock, but the holder is genuinely still running.
	_, err := pool.Exec(ctx, `
		INSERT INTO instance_lock (instance_id, run_id, actor, expires_at)
		VALUES ($1, $2, 'busy', now() - interval '1 hour')`, iid, liveRun)
	require.NoError(t, err)

	_, err = svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrInstanceLocked,
		"a live holder's lock is never stolen, expired or not")

	var holder int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT run_id FROM instance_lock WHERE instance_id = $1`, iid).Scan(&holder))
	require.Equal(t, liveRun, holder, "the live holder keeps the lock")
}

// SPEC-042 mini-ADR 5: a self-target op is refused before any run row, and the
// denial is recorded on the auth_event security ledger.
func TestSelfTargetRefused(t *testing.T) {
	svc, pool := newService(t)
	svc.Protected = map[string]bool{"billing-test": true}
	ctx := context.Background()

	_, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrSelfTarget)

	var runRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM run WHERE instance_id = $1`,
		instanceID(t, pool, "billing-test")).Scan(&runRows))
	require.Zero(t, runRows, "a refused self-target never becomes a run")

	var actor, detail string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT actor, detail FROM auth_event WHERE action = 'guardrail.denied'`).
		Scan(&actor, &detail))
	require.Equal(t, testActor, actor)
	require.Equal(t, "self-target: billing-test", detail)
}
