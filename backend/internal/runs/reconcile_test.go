package runs_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// scriptedAdapter is a fully controllable engine.Adapter: Status returns
// exactly what the test set for a job id (or ErrUnknownJob if unset). It lets
// ReconcileByJobID's Status→finalize logic be driven with zero timing races —
// no live mock, no watcher goroutine (these tests seed the run row directly).
type scriptedAdapter struct {
	status map[engine.JobID]engine.JobStatus
}

func (a *scriptedAdapter) StartJob(context.Context, string, map[string]string) (engine.JobID, error) {
	return "", nil
}

func (a *scriptedAdapter) Status(_ context.Context, id engine.JobID) (engine.JobStatus, error) {
	st, ok := a.status[id]
	if !ok {
		return engine.JobStatus{}, engine.ErrUnknownJob
	}
	return st, nil
}

func (a *scriptedAdapter) StreamLogs(context.Context, engine.JobID) (<-chan engine.LogLine, error) {
	return nil, engine.ErrUnknownJob
}
func (a *scriptedAdapter) Cancel(context.Context, engine.JobID) error { return nil }

// newReconcileService builds a Service whose NON-PROD class is the scripted
// adapter, on a migrated scratch DB with the fixture imported. No watchers are
// started (the tests seed runs directly), so nothing to Wait on.
func newReconcileService(t *testing.T, nonprod engine.Adapter) (*runs.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	reg := engine.NewRegistry()
	reg.Register(engine.ClassProd, engine.NewMockEngine(engine.MockConfig{Name: "mock-prod"}))
	reg.Register(engine.ClassNonProd, nonprod)
	return runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil))), pool
}

// seedRunningRun inserts a run in 'running' with the given job_id plus the
// `run.submitted` audit row Start would have left — exactly the shape
// ReconcileByJobID resolves. instance must be a non-prod fixture row.
func seedRunningRun(t *testing.T, pool *pgxpool.Pool, instance, jobID string) int64 {
	t.Helper()
	ctx := context.Background()
	var instanceID int64
	var env string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id, env FROM instance WHERE name = $1`, instance).Scan(&instanceID, &env))
	class, err := engine.ClassForEnv(env)
	require.NoError(t, err)

	var runID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, job_id)
		VALUES ($1, 'dump', $2, $3, 'dump', 'running', $4) RETURNING id`,
		instanceID, env, string(class), jobID).Scan(&runID))
	_, err = pool.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		VALUES ($1, 'run.submitted', $2, $3, $4, 'dump', 'seed')`,
		testActor, runID, instanceID, env)
	require.NoError(t, err)
	return runID
}

func countFinished(t *testing.T, pool *pgxpool.Pool, runID int64) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_event WHERE run_id = $1 AND action = 'run.finished'`, runID).Scan(&n))
	return n
}

// A terminal engine job finalizes the run through ReconcileByJobID — the
// webhook's acceleration path (SPEC-033 mini-ADR 2), the same guarded
// Status→finalize the periodic watcher would eventually run.
func TestReconcileFinalizesTerminalJob(t *testing.T) {
	started := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	finished := time.Now().UTC().Truncate(time.Second)
	adapter := &scriptedAdapter{status: map[engine.JobID]engine.JobStatus{
		"task-77": {ID: "task-77", State: engine.StateSuccess, Started: started, Finished: finished},
	}}
	svc, pool := newReconcileService(t, adapter)
	runID := seedRunningRun(t, pool, "billing-test", "task-77")

	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-77"))

	run, err := svc.Get(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, "success", run.State)
	require.Equal(t, 1, countFinished(t, pool, runID), "exactly one run.finished audit row")
	// Audit parity: the finished row inherits the submitter as actor.
	rows := auditEvents(t, pool, runID)
	require.Equal(t, "run.finished", rows[len(rows)-1].action)
	require.Equal(t, testActor, rows[len(rows)-1].actor)
	require.Equal(t, "task-77", *rows[len(rows)-1].jobID)
}

// A payload naming a task no run owns is a silent no-op — a stale or spoofed
// task id can't error the endpoint, and touches nothing.
func TestReconcileUnknownTaskIDNoOp(t *testing.T) {
	svc, pool := newReconcileService(t, &scriptedAdapter{})
	runID := seedRunningRun(t, pool, "billing-test", "task-1")

	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-does-not-exist"))
	require.NoError(t, svc.ReconcileByJobID(context.Background(), ""))

	run, err := svc.Get(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, "running", run.State, "no run was touched")
	require.Equal(t, 0, countFinished(t, pool, runID))
}

// A run already terminal is a no-op — the first outcome stands, no duplicate
// finalize / audit / notify (the WU-016 single-finalizer contract).
func TestReconcileAlreadyTerminalNoOp(t *testing.T) {
	adapter := &scriptedAdapter{status: map[engine.JobID]engine.JobStatus{
		"task-9": {ID: "task-9", State: engine.StateSuccess},
	}}
	svc, pool := newReconcileService(t, adapter)
	runID := seedRunningRun(t, pool, "billing-test", "task-9")

	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-9")) // first: finalizes
	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-9")) // second: no-op

	require.Equal(t, 1, countFinished(t, pool, runID), "the second reconcile added no audit row")
}

// The engine reporting a job it no longer knows is a no-op, NOT a failed
// finalize: a genuinely lost job is the poll authority's call (the watcher
// finalizes it failed), never the webhook's (SPEC-033 mini-ADR 2).
func TestReconcileUnknownJobDefersToPoll(t *testing.T) {
	svc, pool := newReconcileService(t, &scriptedAdapter{}) // Status → ErrUnknownJob
	runID := seedRunningRun(t, pool, "billing-test", "task-lost")

	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-lost"))

	run, err := svc.Get(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, "running", run.State, "the webhook must not finalize a job the engine forgot")
	require.Equal(t, 0, countFinished(t, pool, runID))
}

// A job still running is a no-op — the accelerator only finalizes terminal
// tasks; the periodic watcher keeps polling the rest.
func TestReconcileNonTerminalNoOp(t *testing.T) {
	adapter := &scriptedAdapter{status: map[engine.JobID]engine.JobStatus{
		"task-run": {ID: "task-run", State: engine.StateRunning},
	}}
	svc, pool := newReconcileService(t, adapter)
	runID := seedRunningRun(t, pool, "billing-test", "task-run")

	require.NoError(t, svc.ReconcileByJobID(context.Background(), "task-run"))

	run, err := svc.Get(context.Background(), runID)
	require.NoError(t, err)
	require.Equal(t, "running", run.State)
	require.Equal(t, 0, countFinished(t, pool, runID))
}

// ADR-002 fallback, made explicit: with NO webhook ever fired, the periodic
// watcher still drives a run to terminal on its own. "Webhook down → poll
// finalizes" is the DEFAULT, not a special path.
func TestPollFinalizesWithoutWebhook(t *testing.T) {
	svc, _ := newService(t) // real mock engines + watcher, no reconcile calls
	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "fallback", nil))
	require.NoError(t, err)
	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State, "the watcher's poll finalized the run with no webhook in play")
}
