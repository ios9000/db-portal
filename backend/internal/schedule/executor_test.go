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

// blockingStarter parks every Start call until released, returning a
// pre-inserted real run id (the stamp's last_run_id FK needs one).
type blockingStarter struct {
	runID   int64
	started chan string // receives the instance name of each call
	release chan struct{}
}

func (b *blockingStarter) Start(ctx context.Context, req runs.StartRequest) (runs.Run, error) {
	b.started <- req.Instance
	select {
	case <-b.release:
		return runs.Run{ID: b.runID}, nil
	case <-ctx.Done():
		return runs.Run{}, ctx.Err()
	}
}

// seedRun hand-inserts a terminal run row so fakes can hand out a real id.
func seedRun(t *testing.T, pool *pgxpool.Pool, instance, state string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		SELECT id, 'dump', env, 'nonprod', 'dump', $2 FROM instance WHERE name = $1
		RETURNING id`, instance, state).Scan(&id))
	return id
}

func newFakeExecutor(t *testing.T, starter schedule.Starter) (*schedule.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)
	svc := schedule.New(pool, starter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Jitter = 0
	return svc, pool
}

// M2-gate finding 1 (the HIGH): an instance promoted to prod AFTER an
// unconfirmed schedule creation must fail the ritual visibly at fire time —
// never silently dump prod.
func TestPromotedInstanceFailsRitual(t *testing.T) {
	svc, _, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	// Created on a TEST instance the honest way: no confirm typed.
	sc, err := svc.Create(ctx, schedule.CreateRequest{
		Instance: "billing-test", Operation: "dump", CronSpec: "@daily", CreatedBy: testOwner,
	})
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `UPDATE instance SET env = 'prod' WHERE name = 'billing-test'`)
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	svc.FireDue(ctx)
	require.Zero(t, runCount(t, pool), "an unconfirmed schedule must NEVER fire on a promoted instance")
	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "error", *after.LastFireStatus, "the refusal is visible, not silent")
	require.True(t, after.NextFireAt.After(time.Now()), "the loop moves on (mini-ADR 10)")

	svc.FireDue(ctx)
	require.Zero(t, runCount(t, pool), "still refused on later fires")
}

// The counterpart: a schedule whose creator DID perform the ritual keeps
// firing after promotion — the stored evidence stays valid.
func TestConfirmedScheduleSurvivesPromotion(t *testing.T) {
	svc, runSvc, pool := newExecutor(t, time.Millisecond)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-prod", "@daily")) // confirm = instance name
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastRunID)
	run := waitTerminal(t, runSvc, *after.LastRunID)
	require.Equal(t, "success", run.State)
}

// M2-gate finding 2: a disable landing while the fire is in flight keeps
// next_fire_at frozen (NULL) — the outcome is recorded, the clock is not
// resurrected.
func TestDisableDuringFireKeepsNextFireNull(t *testing.T) {
	starter := &blockingStarter{started: make(chan string), release: make(chan struct{})}
	svc, pool := newFakeExecutor(t, starter)
	ctx := context.Background()
	starter.runID = seedRun(t, pool, "billing-test", "success")

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	done := make(chan struct{})
	go func() { svc.FireDue(ctx); close(done) }()
	require.Equal(t, "billing-test", <-starter.started)
	_, err = svc.SetEnabled(ctx, sc.ID, false) // next_fire_at -> NULL
	require.NoError(t, err)
	close(starter.release)
	<-done

	after := getSchedule(t, svc, sc.ID)
	require.False(t, after.Enabled)
	require.Nil(t, after.NextFireAt, "the stamp must not overwrite a concurrent disable's NULL")
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "fired", *after.LastFireStatus, "the fire that DID happen is recorded honestly")
	require.NotNil(t, after.LastRunID)

	// And it stays silent for good: nothing is due anymore.
	svc.FireDue(ctx)
	require.Nil(t, getSchedule(t, svc, sc.ID).NextFireAt)
}

// M2-gate finding 2 (entry half): a schedule disabled after the due
// snapshot but before its turn in the tick never starts a run at all.
func TestDisableBetweenSnapshotAndFireSkips(t *testing.T) {
	starter := &blockingStarter{started: make(chan string), release: make(chan struct{})}
	svc, pool := newFakeExecutor(t, starter)
	ctx := context.Background()
	starter.runID = seedRun(t, pool, "billing-test", "success")

	first, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	second, err := svc.Create(ctx, createReq("crm-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, first.ID, 2) // fires first (ORDER BY next_fire_at)
	backdate(t, pool, second.ID, 1)

	done := make(chan struct{})
	go func() { svc.FireDue(ctx); close(done) }()
	require.Equal(t, "billing-test", <-starter.started)
	_, err = svc.SetEnabled(ctx, second.ID, false) // while the first fire is in flight
	require.NoError(t, err)
	close(starter.release)
	<-done

	select {
	case got := <-starter.started:
		t.Fatalf("disabled schedule fired anyway (instance %s)", got)
	default:
	}
	after := getSchedule(t, svc, second.ID)
	require.Nil(t, after.LastFireStatus, "never attempted — the re-check caught the disable")
}

// M2-gate finding 3 (amended mini-ADR 4): overlap is an instance property —
// sibling schedules never dump one instance concurrently.
func TestSiblingSchedulesDontOverlap(t *testing.T) {
	svc, _, pool := newExecutor(t, 300*time.Millisecond)
	ctx := context.Background()

	a, err := svc.Create(ctx, createReq("billing-test", "* * * * *"))
	require.NoError(t, err)
	b, err := svc.Create(ctx, createReq("billing-test", "*/2 * * * *"))
	require.NoError(t, err)
	backdate(t, pool, a.ID, 2)
	backdate(t, pool, b.ID, 1)

	svc.FireDue(ctx)
	require.Equal(t, 1, runCount(t, pool), "one dump per instance at a time")
	require.Equal(t, "fired", *getSchedule(t, svc, a.ID).LastFireStatus)
	require.Equal(t, "skipped_overlap", *getSchedule(t, svc, b.ID).LastFireStatus)
}

// Same probe, other launcher: a live button-press run blocks a scheduled
// fire too — the scheduler never piles onto a busy instance.
func TestManualRunBlocksScheduledFire(t *testing.T) {
	svc, runSvc, pool := newExecutor(t, 300*time.Millisecond)
	ctx := context.Background()

	_, err := runSvc.Start(ctx, runs.StartRequest{
		Actor: "dba-test", Instance: "billing-test", Operation: "dump", Confirm: "billing-test",
	})
	require.NoError(t, err)

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)
	svc.FireDue(ctx)

	require.Equal(t, 1, runCount(t, pool), "no scheduled pile-on while a manual run is live")
	require.Equal(t, "skipped_overlap", *getSchedule(t, svc, sc.ID).LastFireStatus)
}

// M2-gate finding 5: a hung fire is bounded by FireTimeout — the loop
// records 'error' and moves on instead of wedging forever.
type stuckStarter struct{}

func (stuckStarter) Start(ctx context.Context, _ runs.StartRequest) (runs.Run, error) {
	<-ctx.Done()
	return runs.Run{}, ctx.Err()
}

func TestStuckFireIsBounded(t *testing.T) {
	svc, pool := newFakeExecutor(t, stuckStarter{})
	svc.FireTimeout = 30 * time.Millisecond
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	backdate(t, pool, sc.ID, 1)

	start := time.Now()
	svc.FireDue(ctx)
	require.Less(t, time.Since(start), 5*time.Second, "the loop must not wedge on a stuck fire")

	after := getSchedule(t, svc, sc.ID)
	require.NotNil(t, after.LastFireStatus)
	require.Equal(t, "error", *after.LastFireStatus)
	require.True(t, after.NextFireAt.After(time.Now()), "the stamp survives the burned fire deadline")
}
