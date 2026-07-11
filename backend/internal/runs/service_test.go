package runs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// testActor is every test's audit identity (SPEC-021: the actor is an
// explicit Start/Cancel argument; authz grants are the server's concern).
const testActor = "dba-test"

// testReq wraps the common launch ask. Confirm is pre-set to the instance
// name so prod launches pass the ritual — the ritual itself has its own
// tests.
func testReq(instance, operation, reason string, params map[string]string) runs.StartRequest {
	return runs.StartRequest{
		Actor: testActor, Instance: instance, Operation: operation,
		Reason: reason, Confirm: instance, EngineParams: params,
	}
}

// newService builds a Service on a migrated scratch DB with the fixture
// imported and fast mock engines for both env classes.
func newService(t *testing.T) (*runs.Service, *pgxpool.Pool) {
	t.Helper()
	return newServiceWithDelay(t, time.Millisecond)
}

// newServiceWithDelay is newService with a chosen mock step delay — slower
// engines give tests a window to act while a job is still running.
func newServiceWithDelay(t *testing.T, delay time.Duration) (*runs.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	reg := engine.NewRegistry()
	reg.Register(engine.ClassProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-prod", StepDelay: delay}))
	reg.Register(engine.ClassNonProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod", StepDelay: delay}))

	svc := runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.PollInterval = time.Millisecond
	t.Cleanup(svc.Wait) // watchers drain before the pool closes
	return svc, pool
}

func waitTerminal(t *testing.T, svc *runs.Service, id int64) runs.Run {
	t.Helper()
	var run runs.Run
	require.Eventually(t, func() bool {
		var err error
		run, err = svc.Get(context.Background(), id)
		require.NoError(t, err)
		return run.State == "success" || run.State == "failed" || run.State == "canceled"
	}, 5*time.Second, 2*time.Millisecond)
	return run
}

type auditRow struct {
	actor        string
	action       string
	environment  string
	playbookTag  string
	digest       string
	finalStatus  *string
	jobID        *string
	windowWarned bool
}

func auditEvents(t *testing.T, pool *pgxpool.Pool, runID int64) []auditRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT actor, action, environment, playbook_tag, params_digest, final_status, job_id, window_warned
		FROM audit_event WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var a auditRow
		require.NoError(t, rows.Scan(&a.actor, &a.action, &a.environment, &a.playbookTag, &a.digest, &a.finalStatus, &a.jobID, &a.windowWarned))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// insideWindowNow / outsideWindowNow build deterministic window texts
// relative to the wall clock: the inside one starts 6h ago and ends 6h
// from now (wrap-capable, so midnight proximity can't flake it); the
// outside one sits three days away.
func insideWindowNow() string {
	base := time.Now().Add(-6 * time.Hour)
	return base.Weekday().String()[:3] + " " + base.Format("15:04") + "-" +
		time.Now().Add(6*time.Hour).Format("15:04")
}

func outsideWindowNow() string {
	day := time.Now().AddDate(0, 0, 3).Weekday()
	return day.String()[:3] + " 00:00-01:00"
}

func setWindow(t *testing.T, pool *pgxpool.Pool, instance, window string) {
	t.Helper()
	tag, err := pool.Exec(context.Background(),
		`UPDATE instance SET maintenance_window = $2 WHERE name = $1`, instance, window)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

// SPEC-023 behaviors 3+4: the window stamp on the submitted row — outside
// warns, inside doesn't, no window doesn't, and garbage NEVER blocks.
func TestWindowWarnedStamp(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	cases := []struct {
		name, window string
		warned       bool
	}{
		{"outside the window warns", outsideWindowNow(), true},
		{"inside the window is quiet", insideWindowNow(), false},
		{"garbage text never blocks", "every other fortnight", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setWindow(t, pool, "billing-test", tc.window)
			run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
			require.NoError(t, err, "a window must never block a launch")
			waitTerminal(t, svc, run.ID)

			events := auditEvents(t, pool, run.ID)
			require.Len(t, events, 2)
			require.Equal(t, tc.warned, events[0].windowWarned)
			require.False(t, events[1].windowWarned,
				"the warning qualifies the launch decision; finished doesn't carry it")
		})
	}

	// Empty window (the fixture's non-prod instances): quiet.
	run, err := svc.Start(ctx, testReq("crm-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, run.ID)
	require.False(t, auditEvents(t, pool, run.ID)[0].windowWarned)
}

// SPEC-012 behaviors 1 + 3: happy path to success with artifact metadata,
// env + playbook stamps on run and both audit events, nonprod routing.
func TestStartHappyPath(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "CHG-1 nightly check", nil))
	require.NoError(t, err)
	require.Equal(t, "billing-test", run.Instance)
	require.Equal(t, "test", run.Environment)
	require.Equal(t, "dump", run.Operation)
	require.NotNil(t, run.JobID)
	require.True(t, strings.HasPrefix(*run.JobID, "mock-nonprod"),
		"test env must route to the nonprod adapter, got %s", *run.JobID)

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State)
	require.NotNil(t, final.StartedAt)
	require.NotNil(t, final.FinishedAt)
	require.NotNil(t, final.Artifact)
	require.Contains(t, final.Artifact.Name, "billing-test")
	require.NotEmpty(t, final.Artifact.Checksum)
	require.Positive(t, final.Artifact.SizeBytes)
	require.NotNil(t, final.Reason)
	require.Equal(t, "CHG-1 nightly check", *final.Reason)

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	require.Equal(t, "run.submitted", events[0].action)
	require.Nil(t, events[0].finalStatus)
	require.Equal(t, "run.finished", events[1].action)
	require.NotNil(t, events[1].finalStatus)
	require.Equal(t, "success", *events[1].finalStatus)
	for _, e := range events {
		require.Equal(t, "test", e.environment, "guardrail: env stamped on every audit row")
		require.Equal(t, "dump", e.playbookTag)
		require.NotEmpty(t, e.digest)
		require.Equal(t, testActor, e.actor,
			"both rows carry the requesting identity (SPEC-021 mini-ADR 4)")
	}
	require.Equal(t, events[0].digest, events[1].digest)
}

// SPEC-012 behavior 3 + SPEC-015 behavior 7: a prod instance resolves the
// prod-class adapter, and BOTH its audit events carry environment='prod'
// (guardrail layer 4 — detection needs the stamp on every row).
func TestStartProdRoutesToProdAdapter(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), testReq("billing-prod", "dump", "", nil))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(*run.JobID, "mock-prod"), "got %s", *run.JobID)
	waitTerminal(t, svc, run.ID)

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	for _, e := range events {
		require.Equal(t, "prod", e.environment, "guardrail: env stamped on every audit row")
	}
}

// SPEC-015 behavior 6: the environment stamp is un-omittable — the schema
// itself refuses an audit row with NULL environment (not-null violation),
// so guardrail layer 4 cannot silently regress in application code.
func TestAuditEnvironmentNotNullable(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, run.ID)

	// Clone a real event's keys so ONLY the NULL environment can fail.
	_, err = pool.Exec(context.Background(), `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		SELECT actor, action, run_id, instance_id, NULL, playbook_tag, params_digest
		FROM audit_event WHERE run_id = $1 AND action = 'run.submitted'`, run.ID)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23502", pgErr.Code) // not_null_violation
	require.Equal(t, "environment", pgErr.ColumnName)
}

// SPEC-012 behavior 2: injected engine failure lands as a failed run with
// the error preserved and a failed final audit event.
func TestStartFailurePath(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "",
		map[string]string{"mock_fail_at": "1"}))
	require.NoError(t, err)

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "failed", final.State)
	require.NotNil(t, final.Error)
	require.Contains(t, *final.Error, "failed")
	require.Nil(t, final.Artifact)

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	require.Equal(t, "failed", *events[1].finalStatus)
}

// SPEC-012 behavior 5: bad requests write nothing.
func TestStartUnknownInstanceAndOperation(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	_, err := svc.Start(ctx, testReq("nope", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrUnknownInstance)

	_, err = svc.Start(ctx, testReq("billing-test", "reindex-the-moon", "", nil))
	require.ErrorIs(t, err, runs.ErrUnknownOperation)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&count))
	require.Zero(t, count, "no run rows on rejected requests")
}

// SPEC-012 behavior 6: engine refusal (no adapter registered for the class)
// still leaves a complete audit trail — submitted AND finished/failed.
// SPEC-014 behavior 3: refusal notifies (it finalizes through the same seam).
func TestStartEngineRefusalIsAudited(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	reg := engine.NewRegistry() // nothing registered: every class fails closed
	svc := runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rec := &recordingNotifier{}
	svc.Notifier = rec

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrEngine)
	require.Equal(t, "failed", run.State)
	require.NotNil(t, run.Error)

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	require.Equal(t, "run.submitted", events[0].action)
	require.Equal(t, "failed", *events[1].finalStatus)

	svc.Wait()
	require.Len(t, rec.all(), 1, "engine refusal must notify")
}

// SPEC-012 behavior 7: non-terminal runs from a dead process are finalized
// at startup.
func TestSweepOrphans(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	// A run stuck 'running' whose engine job no longer exists anywhere.
	var instanceID, runID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM instance WHERE name = 'billing-test'`).Scan(&instanceID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, job_id)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'running', 'mock-nonprod-ghost')
		RETURNING id`, instanceID).Scan(&runID))
	_, err := pool.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		VALUES ($3, 'run.submitted', $1, $2, 'test', 'dump', 'digest')`, runID, instanceID, testActor)
	require.NoError(t, err)

	rec := &recordingNotifier{}
	svc.Notifier = rec
	n, err := svc.SweepOrphans(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	run, err := svc.Get(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.State)
	require.Contains(t, *run.Error, "engine job lost")
	finished := auditEvents(t, pool, runID)[1]
	require.Equal(t, "failed", *finished.finalStatus)
	require.Equal(t, testActor, finished.actor,
		"a sweep finalization acts on the requester's behalf (SPEC-021 mini-ADR 4)")

	svc.Wait()
	require.Len(t, rec.all(), 1, "orphan sweep must notify (SPEC-014 behavior 3)")
}

// recordingNotifier is a runs.Notifier that captures every call; err, when
// set, is returned to exercise the log-only error contract.
type recordingNotifier struct {
	mu  sync.Mutex
	err error
	got []runs.Run
}

func (n *recordingNotifier) RunEnded(_ context.Context, r runs.Run) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.got = append(n.got, r)
	return n.err
}

func (n *recordingNotifier) all() []runs.Run {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]runs.Run(nil), n.got...)
}

// SPEC-014 behaviors 1, 2, 4: failed and canceled runs notify exactly once
// (even when the notifier itself errors — log-only); success stays silent.
func TestNotifierOnTerminalStates(t *testing.T) {
	svc, pool := newServiceWithDelay(t, 50*time.Millisecond)
	rec := &recordingNotifier{err: errors.New("smtp down")}
	svc.Notifier = rec
	ctx := context.Background()

	okRun, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.Equal(t, "success", waitTerminal(t, svc, okRun.ID).State)

	failRun, err := svc.Start(ctx, testReq("billing-test", "dump", "",
		map[string]string{"mock_fail_at": "1"}))
	require.NoError(t, err)
	require.Equal(t, "failed", waitTerminal(t, svc, failRun.ID).State)

	abortRun, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.NoError(t, svc.Cancel(ctx, testActor, abortRun.ID))
	require.Equal(t, "canceled", waitTerminal(t, svc, abortRun.ID).State)

	svc.Wait() // drain watcher + notifier goroutines before asserting

	got := rec.all()
	require.Len(t, got, 2, "success must not notify")
	states := map[int64]string{}
	for _, r := range got {
		states[r.ID] = r.State
		require.Equal(t, testActor, r.RequestedBy,
			"notification carries the who (SPEC-014 mini-ADR 3)")
		require.Equal(t, "billing-test", r.Instance)
	}
	require.Equal(t, "failed", states[failRun.ID])
	require.Equal(t, "canceled", states[abortRun.ID])

	// The notifier erroring every time changed nothing durable: runs are
	// finalized and the audit trail is complete (the aborted run carries
	// its extra run.cancel_requested row — SPEC-021 mini-ADR 5).
	require.Len(t, auditEvents(t, pool, failRun.ID), 2)
	require.Len(t, auditEvents(t, pool, abortRun.ID), 3)
}

// SPEC-012 behavior 8: newest-first list, instance filter, unknown filter
// matches nothing.
func TestList(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	first, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, first.ID)
	second, err := svc.Start(ctx, testReq("crm-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, second.ID)

	all, err := svc.List(ctx, runs.ListFilter{})
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, second.ID, all[0].ID, "newest first")
	for _, r := range all {
		require.Equal(t, testActor, r.RequestedBy,
			"SPEC-014: requester surfaces from the submitted audit event")
	}

	filtered, err := svc.List(ctx, runs.ListFilter{Instance: "crm-test"})
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "crm-test", filtered[0].Instance)

	none, err := svc.List(ctx, runs.ListFilter{Instance: "ghost-instance"})
	require.NoError(t, err)
	require.Empty(t, none)

	_, err = svc.Get(ctx, 99999)
	require.ErrorIs(t, err, runs.ErrNotFound)
}

// SPEC-014 behavior 6: state/env/operation filters compose (ANDed) and
// unknown values match nothing.
func TestListFilters(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	ok, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, ok.ID)
	bad, err := svc.Start(ctx, testReq("billing-prod", "dump", "",
		map[string]string{"mock_fail_at": "1"}))
	require.NoError(t, err)
	waitTerminal(t, svc, bad.ID)

	failed, err := svc.List(ctx, runs.ListFilter{State: "failed"})
	require.NoError(t, err)
	require.Len(t, failed, 1)
	require.Equal(t, bad.ID, failed[0].ID)

	prodDumps, err := svc.List(ctx, runs.ListFilter{Environment: "prod", Operation: "dump"})
	require.NoError(t, err)
	require.Len(t, prodDumps, 1)
	require.Equal(t, bad.ID, prodDumps[0].ID)

	disjoint, err := svc.List(ctx, runs.ListFilter{Instance: "billing-test", State: "failed"})
	require.NoError(t, err)
	require.Empty(t, disjoint, "filters must AND")

	unknown, err := svc.List(ctx, runs.ListFilter{State: "warp-drive"})
	require.NoError(t, err)
	require.Empty(t, unknown, "unknown value filters to empty, not an error")
}

// m1-gate item 1: when recording the job id fails after StartJob succeeded,
// the live engine job must not be stranded — Start cancels it (best effort)
// and finalizes the run failed with a complete audit trail (mini-ADR 7).
func TestStartRecordJobIDFailureRepairsRun(t *testing.T) {
	svc, pool := newService(t)
	svc.SetFailRecordJobID(errors.New("db hiccup"))

	_, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.ErrorContains(t, err, "record job id")

	var id int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT id FROM run ORDER BY id DESC LIMIT 1`).Scan(&id))
	run, gerr := svc.Get(context.Background(), id)
	require.NoError(t, gerr)
	require.Equal(t, "failed", run.State, "run must not stay 'queued' behind a live job")
	require.NotNil(t, run.Error)
	require.Contains(t, *run.Error, "failed to record the engine job")

	events := auditEvents(t, pool, id)
	require.Len(t, events, 2, "submitted + finished — trail complete either way")
	require.Equal(t, "run.submitted", events[0].action)
	require.Equal(t, "run.finished", events[1].action)
	require.NotNil(t, events[1].finalStatus)
	require.Equal(t, "failed", *events[1].finalStatus)
}

// m1-gate item 2: a second finalizer must neither overwrite the terminal
// outcome nor write a duplicate run.finished event (mini-ADR 8).
func TestFinalizeIdempotent(t *testing.T) {
	svc, pool := newService(t)
	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State)

	// The losing side of a watcher-vs-cancel-vs-sweep race arrives late.
	require.NoError(t, svc.Finalize(context.Background(), run.ID, "failed", "late loser"))

	again, err := svc.Get(context.Background(), run.ID)
	require.NoError(t, err)
	require.Equal(t, "success", again.State, "first terminal outcome stands")
	require.Nil(t, again.Error)
	require.Len(t, auditEvents(t, pool, run.ID), 2, "exactly one run.finished")
}

// Two finalizers racing on a live run: exactly one wins, exactly one
// run.finished event lands, and the watcher's own late finalize no-ops.
func TestFinalizeConcurrentSingleWinner(t *testing.T) {
	svc, pool := newServiceWithDelay(t, 100*time.Millisecond) // job stays live
	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	var wg sync.WaitGroup
	for _, st := range []string{"failed", "canceled"} {
		wg.Add(1)
		go func(st string) {
			defer wg.Done()
			require.NoError(t, svc.Finalize(context.Background(), run.ID, st, st))
		}(st)
	}
	wg.Wait()
	svc.Wait() // drain the watcher: its finalize must also lose quietly

	got, err := svc.Get(context.Background(), run.ID)
	require.NoError(t, err)
	require.Contains(t, []string{"failed", "canceled"}, got.State)
	require.Len(t, auditEvents(t, pool, run.ID), 2, "exactly one run.finished despite three finalizers")
}

// SPEC-021 mini-ADR 6: prod launches need Confirm == instance name, checked
// server-side before any row is written; non-prod ignores Confirm.
func TestProdRitualServerSide(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	for _, confirm := range []string{"", "billing-Prod", "billing-test"} {
		req := testReq("billing-prod", "dump", "", nil)
		req.Confirm = confirm
		_, err := svc.Start(ctx, req)
		require.ErrorIs(t, err, runs.ErrProdUnconfirmed, "confirm=%q", confirm)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&count))
	require.Zero(t, count, "a failed ritual must leave no run row")
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM audit_event`).Scan(&count))
	require.Zero(t, count, "a failed ritual must leave no audit row")

	// Exact match launches; non-prod launches with any Confirm at all.
	run, err := svc.Start(ctx, testReq("billing-prod", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, run.ID)
	nonprod := testReq("billing-test", "dump", "", nil)
	nonprod.Confirm = "whatever"
	run, err = svc.Start(ctx, nonprod)
	require.NoError(t, err)
	waitTerminal(t, svc, run.ID)
}

// SPEC-021 mini-ADR 5: cancel records run.cancel_requested with the
// canceling actor before the engine is asked; the finished row still
// inherits the SUBMITTING actor — three rows, two identities.
func TestCancelRequestIsAudited(t *testing.T) {
	svc, pool := newServiceWithDelay(t, 50*time.Millisecond)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.NoError(t, svc.Cancel(ctx, "dba-canceler", run.ID))
	require.Equal(t, "canceled", waitTerminal(t, svc, run.ID).State)
	svc.Wait()

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 3)
	require.Equal(t, "run.submitted", events[0].action)
	require.Equal(t, testActor, events[0].actor)

	cancelReq := events[1]
	require.Equal(t, "run.cancel_requested", cancelReq.action)
	require.Equal(t, "dba-canceler", cancelReq.actor, "the cancel row names who asked")
	require.Nil(t, cancelReq.finalStatus, "the outcome is not known at request time")
	require.NotNil(t, cancelReq.jobID, "the job being canceled is the forensic anchor")
	require.Equal(t, "test", cancelReq.environment)
	require.NotEmpty(t, cancelReq.digest)

	require.Equal(t, "run.finished", events[2].action)
	require.Equal(t, testActor, events[2].actor, "finished inherits the submitter")
	require.Equal(t, "canceled", *events[2].finalStatus)
}

// SPEC-021 behavior 5: requested_by filters on the submitted row's actor,
// composes with the other filters, and unknown usernames match nothing.
func TestListRequestedByFilter(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	mine := testReq("billing-test", "dump", "", nil)
	theirs := testReq("crm-test", "dump", "", nil)
	theirs.Actor = "dba-other"
	first, err := svc.Start(ctx, mine)
	require.NoError(t, err)
	waitTerminal(t, svc, first.ID)
	second, err := svc.Start(ctx, theirs)
	require.NoError(t, err)
	waitTerminal(t, svc, second.ID)

	got, err := svc.List(ctx, runs.ListFilter{RequestedBy: "dba-other"})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, second.ID, got[0].ID)
	require.Equal(t, "dba-other", got[0].RequestedBy)

	got, err = svc.List(ctx, runs.ListFilter{RequestedBy: "dba-other", Instance: "billing-test"})
	require.NoError(t, err)
	require.Empty(t, got, "filters AND together")

	got, err = svc.List(ctx, runs.ListFilter{RequestedBy: "nobody"})
	require.NoError(t, err)
	require.Empty(t, got, "unknown requester is a filter that matches nothing")
}
