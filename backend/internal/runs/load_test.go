package runs_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// SPEC-043: the load-test harness. It drives 25–50 concurrent mock dumps
// through the REAL runs.Service against a seeded estate (WU-041) and asserts,
// under contention, the invariants WU-042 (instance lock) and WU-016
// (single-finalizer) promise: exactly-once finalize, per-instance
// serialization, cross-instance parallelism, and zero orphaned runs. Like
// every runs test it rides a scratch DB via testutil.MigratedDB, so it runs
// wherever a Postgres is reachable and skips otherwise. (SPEC-043 mini-ADR 1
// said "skips in CI"; WU-045 added a Postgres service to check.yml, so it now
// runs in CI too — its assertions are runner-speed-robust: parallelism is only
// asserted >= 2, never a hard peak.) The full-scale 500-instance/50-concurrent
// run is a live drill of this same code via PORTAL_LOADTEST_INSTANCES.

// newLoadService seeds a realistic estate of nInstances (SPEC-041 generator)
// into a fresh scratch DB and returns a runs.Service over fast mock engines.
// Unlike newServiceWithDelay it imports the generated estate, not the 8-row
// fixture, so there are enough distinct instances to spread contention across.
func newLoadService(t *testing.T, nInstances int, delay time.Duration) (*runs.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	// The gate seeds a fast ~80-instance estate; a live drill runs the SAME
	// harness at pilot scale (500) via PORTAL_LOADTEST_INSTANCES (mini-ADR 1).
	// The override only grows the estate — it never drops below a scenario's
	// launch count, so distinctInstances still finds enough rows.
	if v := os.Getenv("PORTAL_LOADTEST_INSTANCES"); v != "" {
		if got, err := strconv.Atoi(v); err == nil && got > nInstances {
			nInstances = got
		}
	}

	csv := inventory.GenerateEstate(nInstances, 43)
	rep, err := inventory.Import(context.Background(), pool, "estate.csv", strings.NewReader(csv))
	require.NoError(t, err)
	require.Zero(t, rep.Quarantined, "the generated estate imports clean")
	t.Logf("seeded estate: %d instances (0 quarantined)", rep.New)

	reg := engine.NewRegistry()
	reg.Register(engine.ClassProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-prod", StepDelay: delay}))
	reg.Register(engine.ClassNonProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod", StepDelay: delay}))

	svc := runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.PollInterval = 2 * time.Millisecond
	t.Cleanup(svc.Wait) // watchers drain before the pool closes
	return svc, pool
}

// distinctInstances returns up to n instance names from the seeded estate.
func distinctInstances(t *testing.T, pool *pgxpool.Pool, n int) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT name FROM instance ORDER BY name LIMIT $1`, n)
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	require.Len(t, names, n, "the estate holds at least n instances")
	return names
}

// auditActionCount returns how many audit rows a run has for one action —
// the exactly-once check (WU-016): one run.submitted, one run.finished.
func auditActionCount(t *testing.T, pool *pgxpool.Pool, runID int64, action string) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_event WHERE run_id = $1 AND action = $2`,
		runID, action).Scan(&n))
	return n
}

// orphanCount is the load test's headline invariant: rows still live after the
// harness settles. Must be zero once every watcher has drained (svc.Wait).
func orphanCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM run WHERE state IN ('queued', 'running')`).Scan(&n))
	return n
}

func lockCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM instance_lock`).Scan(&n))
	return n
}

// peakConcurrency sweep-lines the runs' [started_at, finished_at] intervals for
// the maximum number simultaneously live — a deterministic parallelism measure
// read from committed data, never a racy live sample (SPEC-043 mini-ADR 3).
func peakConcurrency(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT started_at, finished_at FROM run
		 WHERE started_at IS NOT NULL AND finished_at IS NOT NULL`)
	require.NoError(t, err)
	defer rows.Close()

	type ev struct {
		at    time.Time
		delta int
	}
	var evs []ev
	for rows.Next() {
		var start, fin time.Time
		require.NoError(t, rows.Scan(&start, &fin))
		evs = append(evs, ev{start, +1}, ev{fin, -1})
	}
	require.NoError(t, rows.Err())
	// Sort by time; on ties process ends (-1) before starts (+1) so two runs
	// that merely abut are not counted as overlapping.
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].at.Equal(evs[j].at) {
			return evs[i].delta < evs[j].delta
		}
		return evs[i].at.Before(evs[j].at)
	})
	cur, peak := 0, 0
	for _, e := range evs {
		cur += e.delta
		if cur > peak {
			peak = cur
		}
	}
	return peak
}

// B1 (SPEC-043): 50 dumps across 50 DISTINCT instances. Every launch wins its
// own instance lock, so all 50 succeed; each is finalized exactly once (one
// submitted + one finished audit row); the runs overlap in wall-clock
// (cross-instance parallelism); nothing is left orphaned. This is the
// exactly-once-under-load + parallelism proof.
func TestLoadConcurrentDistinctInstances(t *testing.T) {
	const n = 50
	svc, pool := newLoadService(t, 80, 15*time.Millisecond)
	ctx := context.Background()
	names := distinctInstances(t, pool, n)

	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wallStart := time.Now()
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := svc.Start(ctx, testReq(names[i], "dump", "load", nil))
			ids[i], errs[i] = run.ID, err
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "a distinct-instance dump never conflicts (%s)", names[i])
		require.NotZero(t, ids[i])
	}
	for i := range n {
		waitTerminal(t, svc, ids[i])
	}
	svc.Wait() // every watcher drained

	// Exactly-once finalize: one submitted + one finished per run, all success.
	for i := range n {
		run, err := svc.Get(ctx, ids[i])
		require.NoError(t, err)
		require.Equal(t, "success", run.State, "run %d for %s", ids[i], names[i])
		require.Equal(t, 1, auditActionCount(t, pool, ids[i], "run.submitted"))
		require.Equal(t, 1, auditActionCount(t, pool, ids[i], "run.finished"),
			"exactly one finalize per run (WU-016)")
	}

	require.Zero(t, orphanCount(t, pool), "no run left queued/running after settle")
	require.Zero(t, lockCount(t, pool), "every instance lock released on finalize")

	peak := peakConcurrency(t, pool)
	require.GreaterOrEqual(t, peak, 2, "distinct-instance dumps ran in parallel, not serially")
	t.Logf("B1: %d distinct-instance dumps in %s — peak concurrency %d, throughput %.0f runs/s",
		n, time.Since(wallStart).Round(time.Millisecond), peak,
		float64(n)/time.Since(wallStart).Seconds())
}

// B2 (SPEC-043): 40 concurrent dumps at ONE instance — the WU-042 lock test's
// shape at load. Exactly one wins; the other 39 get ErrInstanceLocked and leave
// no run row; the lock frees on the winner's finalize; a later Start succeeds.
func TestLoadSameInstanceContentionAtScale(t *testing.T) {
	const n = 40
	svc, pool := newLoadService(t, 40, 150*time.Millisecond)
	ctx := context.Background()
	target := distinctInstances(t, pool, 1)[0]

	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := svc.Start(ctx, testReq(target, "dump", "load", nil))
			ids[i], errs[i] = run.ID, err
		}()
	}
	close(start)
	wg.Wait()

	wins, conflicts, winner := 0, 0, int64(0)
	for i, err := range errs {
		if err == nil {
			wins++
			winner = ids[i]
			require.NotZero(t, ids[i])
		} else {
			require.ErrorIs(t, err, runs.ErrInstanceLocked)
			conflicts++
			require.Zero(t, ids[i], "a conflicted launch creates no run")
		}
	}
	require.Equal(t, 1, wins, "exactly one Start proceeds on a single instance")
	require.Equal(t, n-1, conflicts, "the other %d get ErrInstanceLocked", n-1)

	var runRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM run WHERE instance_id = (SELECT id FROM instance WHERE name = $1)`,
		target).Scan(&runRows))
	require.Equal(t, 1, runRows, "only the winner's run row exists")

	waitTerminal(t, svc, winner)
	require.Zero(t, lockCount(t, pool), "the lock is gone after the holder finalizes")

	next, err := svc.Start(ctx, testReq(target, "dump", "load", nil))
	require.NoError(t, err, "a freed instance accepts the next op")
	waitTerminal(t, svc, next.ID)
	svc.Wait()
	require.Zero(t, orphanCount(t, pool))
}

// B3 (SPEC-043): the mixed batch — M instances, K concurrent dumps each — proves
// serialization and parallelism together in one run: per instance exactly one
// success + K-1 conflicts, and over the whole batch every terminal run is
// finalized exactly once with zero orphans and zero leaked locks.
func TestLoadMixedContentionAndParallelism(t *testing.T) {
	const m, k = 20, 3 // 60 launch attempts, 20 winners
	svc, pool := newLoadService(t, 60, 20*time.Millisecond)
	ctx := context.Background()
	names := distinctInstances(t, pool, m)

	type res struct {
		id  int64
		err error
	}
	results := make([]res, m*k)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range m * k {
		inst := names[i%m]
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			run, err := svc.Start(ctx, testReq(inst, "dump", "load", nil))
			results[i] = res{run.ID, err}
		}()
	}
	close(start)
	wg.Wait()

	wins, conflicts := 0, 0
	var winners []int64
	for _, r := range results {
		if r.err == nil {
			wins++
			winners = append(winners, r.id)
		} else {
			require.ErrorIs(t, r.err, runs.ErrInstanceLocked)
			conflicts++
		}
	}
	require.Equal(t, m, wins, "one winner per instance")
	require.Equal(t, m*(k-1), conflicts, "the rest conflict")

	for _, id := range winners {
		waitTerminal(t, svc, id)
	}
	svc.Wait()

	for _, id := range winners {
		require.Equal(t, 1, auditActionCount(t, pool, id, "run.finished"),
			"each winner finalized exactly once")
	}
	var runRows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&runRows))
	require.Equal(t, m, runRows, "exactly one run row per instance — the conflicts left nothing")
	require.Zero(t, orphanCount(t, pool), "no orphaned runs after the mixed batch settles")
	require.Zero(t, lockCount(t, pool), "no leaked locks")
}
