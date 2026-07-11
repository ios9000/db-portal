package schedule_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// newExecutor builds a schedule Service wired to a REAL runs.Service over
// mock engines — scheduled fires must ride the identical guardrail + audit
// path as button presses (ADR-003), so most executor tests avoid fakes.
func newExecutor(t *testing.T, delay time.Duration) (*schedule.Service, *runs.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	reg := engine.NewRegistry()
	reg.Register(engine.ClassProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-prod", StepDelay: delay}))
	reg.Register(engine.ClassNonProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod", StepDelay: delay}))
	runSvc := runs.NewService(pool, reg, log)
	runSvc.PollInterval = time.Millisecond
	t.Cleanup(runSvc.Wait)

	svc := schedule.New(pool, runSvc, log)
	svc.Jitter = 0
	return svc, runSvc, pool
}

// backdate makes a schedule due as of hours ago — the misfire shape.
func backdate(t *testing.T, pool *pgxpool.Pool, id int64, hours int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`UPDATE schedule SET next_fire_at = now() - make_interval(hours => $2) WHERE id = $1`,
		id, hours)
	require.NoError(t, err)
}

func getSchedule(t *testing.T, svc *schedule.Service, id int64) schedule.Schedule {
	t.Helper()
	list, err := svc.List(context.Background())
	require.NoError(t, err)
	for _, sc := range list {
		if sc.ID == id {
			return sc
		}
	}
	t.Fatalf("schedule %d not in list", id)
	return schedule.Schedule{}
}

func runCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM run`).Scan(&n))
	return n
}

func waitTerminal(t *testing.T, rs *runs.Service, id int64) runs.Run {
	t.Helper()
	var run runs.Run
	require.Eventually(t, func() bool {
		var err error
		run, err = rs.Get(context.Background(), id)
		require.NoError(t, err)
		return run.State == "success" || run.State == "failed" || run.State == "canceled"
	}, 5*time.Second, 2*time.Millisecond)
	return run
}

// SPEC-022 behavior 2 — the M2 exit criterion: a due schedule fires through
// the real runs path with `schedule:<owner>` on BOTH audit rows, and the
// schedule row records the fire.
func TestFireAttribution(t *testing.T) {
	svc, runSvc, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	svc.FireDue(ctx)

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastRunID, "a fired schedule points at its run")
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "fired", *after.LastFireStatus)
	require.NotNil(t, after.LastFiredAt)
	require.NotNil(t, after.NextFireAt)
	require.True(t, after.NextFireAt.After(time.Now()), "next fire moved to the future")

	run := waitTerminal(t, runSvc, *after.LastRunID)
	require.Equal(t, "success", run.State)
	require.Equal(t, "schedule:"+testOwner, run.RequestedBy)
	require.NotNil(t, run.Reason)
	require.Equal(t, "nightly", *run.Reason, "the schedule's reason rides every fire")

	rows, err := pool.Query(ctx, `SELECT actor, action FROM audit_event
		WHERE run_id = $1 ORDER BY id`, run.ID)
	require.NoError(t, err)
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var actor, action string
		require.NoError(t, rows.Scan(&actor, &action))
		require.Equal(t, "schedule:"+testOwner, actor,
			"every audit row names the schedule's owner (ADR-003)")
		actions = append(actions, action)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []string{"run.submitted", "run.finished"}, actions)
}

// SPEC-022 behavior 3 / mini-ADR 2: a schedule missed many times over
// coalesces into ONE catch-up fire, and the loop is then quiet.
func TestMisfireCoalesces(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 72) // three missed daily fires

	svc.FireDue(ctx)
	require.Equal(t, 1, runCount(t, pool), "72h of missed dailies coalesce into one fire")

	svc.FireDue(ctx)
	svc.FireDue(ctx)
	require.Equal(t, 1, runCount(t, pool), "a caught-up schedule is no longer due")
}

// SPEC-022 behavior 4 / mini-ADR 4: while the previous run is live the fire
// is skipped visibly; once it finishes, firing resumes.
func TestOverlapSkips(t *testing.T) {
	// A slow engine keeps the first run alive while the second fire comes due.
	svc, runSvc, pool := newExecutor(t, 300*time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "* * * * *"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	first := getSchedule(t, svc, sc.ID)
	require.NotNil(t, first.LastRunID)

	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)
	skipped := getSchedule(t, svc, sc.ID)
	require.Equal(t, 1, runCount(t, pool), "no second run while the first is live")
	require.NotNil(t, skipped.LastFireStatus)
	require.Equal(t, "skipped_overlap", *skipped.LastFireStatus)
	require.Equal(t, *first.LastRunID, *skipped.LastRunID, "the skip keeps the run pointer")
	require.True(t, skipped.NextFireAt.After(time.Now()), "a skip still advances the clock")

	waitTerminal(t, runSvc, *first.LastRunID)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)
	require.Equal(t, 2, runCount(t, pool), "firing resumes once the previous run is terminal")
}

// SPEC-022 behavior 5: disabled means silent, even when due; re-enabling
// never fires the gap.
func TestDisabledNeverFires(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "* * * * *"))
	require.NoError(t, err)
	_, err = svc.SetEnabled(ctx, sc.ID, false)
	require.NoError(t, err)
	// Belt and braces: even a due next_fire_at (hand-corrupted row) must
	// not fire while disabled — the query keys on enabled.
	backdate(t, pool, sc.ID, 24)

	svc.FireDue(ctx)
	require.Zero(t, runCount(t, pool), "disabled schedules never fire")

	on, err := svc.SetEnabled(ctx, sc.ID, true)
	require.NoError(t, err)
	require.True(t, on.NextFireAt.After(time.Now()))
	svc.FireDue(ctx)
	require.Zero(t, runCount(t, pool), "re-enable computes from now — the gap is not fired")
}

// SPEC-022 behavior 6: a prod schedule fires unattended — the creation-time
// ritual authorizes every future fire (mini-ADR 8).
func TestProdScheduleFiresUnattended(t *testing.T) {
	svc, runSvc, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-prod", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastRunID)
	run := waitTerminal(t, runSvc, *after.LastRunID)
	require.Equal(t, "success", run.State)
	require.Equal(t, "prod", run.Environment)
	require.Equal(t, "schedule:"+testOwner, run.RequestedBy)
}

// SPEC-022 behavior 7 (engine half): a refused fire still counts as fired —
// the run exists, finalized failed, and the mail path owns the alarm.
func TestEngineRefusalStampsFired(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// An empty registry refuses every class: Start returns ErrEngine with
	// the run already finalized failed.
	runSvc := runs.NewService(pool, engine.NewRegistry(), log)
	t.Cleanup(runSvc.Wait)
	svc := schedule.New(pool, runSvc, log)
	svc.Jitter = 0
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "fired", *after.LastFireStatus)
	require.NotNil(t, after.LastRunID)
	run, err := runSvc.Get(ctx, *after.LastRunID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.State)
	require.True(t, after.NextFireAt.After(time.Now()))
}

// erringStarter fails every Start before any run row exists.
type erringStarter struct{ calls atomic.Int32 }

func (e *erringStarter) Start(context.Context, runs.StartRequest) (runs.Run, error) {
	e.calls.Add(1)
	return runs.Run{}, errors.New("boom")
}

// SPEC-022 behavior 7 (pre-run half) / mini-ADR 10: a Start error with no
// run still stamps 'error' and advances the clock — the loop never wedges.
func TestStartErrorStampsErrorAndAdvances(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	starter := &erringStarter{}
	svc := schedule.New(pool, starter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Jitter = 0
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	svc.FireDue(ctx)
	require.Equal(t, int32(1), starter.calls.Load())
	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "error", *after.LastFireStatus)
	require.Nil(t, after.LastRunID, "no run to point at")
	require.True(t, after.NextFireAt.After(time.Now()), "the failure advanced the clock")

	svc.FireDue(ctx)
	require.Equal(t, int32(1), starter.calls.Load(), "an advanced schedule is not retried this cron cycle")
}

// SPEC-023 behavior 5: a scheduled fire outside the maintenance window
// carries window_warned on its submitted row — drawer and scheduler share
// the stamp point (runs.Start), proven here through the executor.
func TestScheduledFireStampsWindowWarned(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	// A window three days from now: the fire is guaranteed outside it.
	day := time.Now().AddDate(0, 0, 3).Weekday().String()[:3]
	_, err := pool.Exec(ctx,
		`UPDATE instance SET maintenance_window = $1 WHERE name = 'billing-test'`,
		day+" 00:00-01:00")
	require.NoError(t, err)

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastRunID)
	var warned bool
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT window_warned FROM audit_event
		WHERE run_id = $1 AND action = 'run.submitted'`, *after.LastRunID).Scan(&warned))
	require.True(t, warned, "the scheduler path must carry the window stamp too")
}

// SPEC-022 behavior 2 (loop half): the ticker loop itself picks up a due
// schedule — Run(ctx) is what production executes.
func TestRunLoopFires(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	svc.Tick = time.Millisecond
	done := make(chan struct{})
	go func() { svc.Run(ctx); close(done) }()

	require.Eventually(t, func() bool {
		sc := getSchedule(t, svc, sc.ID)
		return sc.LastFireStatus != nil && *sc.LastFireStatus == "fired"
	}, 5*time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run must return when its context is canceled")
	}
}
