package runs_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

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
	action      string
	environment string
	playbookTag string
	digest      string
	finalStatus *string
}

func auditEvents(t *testing.T, pool *pgxpool.Pool, runID int64) []auditRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT action, environment, playbook_tag, params_digest, final_status
		FROM audit_event WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var a auditRow
		require.NoError(t, rows.Scan(&a.action, &a.environment, &a.playbookTag, &a.digest, &a.finalStatus))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// SPEC-012 behaviors 1 + 3: happy path to success with artifact metadata,
// env + playbook stamps on run and both audit events, nonprod routing.
func TestStartHappyPath(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), "billing-test", "dump", "CHG-1 nightly check", nil)
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
	}
	require.Equal(t, events[0].digest, events[1].digest)
}

// SPEC-012 behavior 3: a prod instance resolves the prod-class adapter.
func TestStartProdRoutesToProdAdapter(t *testing.T) {
	svc, _ := newService(t)

	run, err := svc.Start(context.Background(), "billing-prod", "dump", "", nil)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(*run.JobID, "mock-prod"), "got %s", *run.JobID)
	waitTerminal(t, svc, run.ID)
}

// SPEC-012 behavior 2: injected engine failure lands as a failed run with
// the error preserved and a failed final audit event.
func TestStartFailurePath(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), "billing-test", "dump", "",
		map[string]string{"mock_fail_at": "1"})
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

	_, err := svc.Start(ctx, "nope", "dump", "", nil)
	require.ErrorIs(t, err, runs.ErrUnknownInstance)

	_, err = svc.Start(ctx, "billing-test", "reindex-the-moon", "", nil)
	require.ErrorIs(t, err, runs.ErrUnknownOperation)

	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM run`).Scan(&count))
	require.Zero(t, count, "no run rows on rejected requests")
}

// SPEC-012 behavior 6: engine refusal (no adapter registered for the class)
// still leaves a complete audit trail — submitted AND finished/failed.
func TestStartEngineRefusalIsAudited(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	reg := engine.NewRegistry() // nothing registered: every class fails closed
	svc := runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	run, err := svc.Start(context.Background(), "billing-test", "dump", "", nil)
	require.ErrorIs(t, err, runs.ErrEngine)
	require.Equal(t, "failed", run.State)
	require.NotNil(t, run.Error)

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	require.Equal(t, "run.submitted", events[0].action)
	require.Equal(t, "failed", *events[1].finalStatus)
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
		VALUES ('local-dev', 'run.submitted', $1, $2, 'test', 'dump', 'digest')`, runID, instanceID)
	require.NoError(t, err)

	n, err := svc.SweepOrphans(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	run, err := svc.Get(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.State)
	require.Contains(t, *run.Error, "engine job lost")
	require.Equal(t, "failed", *auditEvents(t, pool, runID)[1].finalStatus)
}

// SPEC-012 behavior 8: newest-first list, instance filter, unknown filter
// matches nothing.
func TestList(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	first, err := svc.Start(ctx, "billing-test", "dump", "", nil)
	require.NoError(t, err)
	waitTerminal(t, svc, first.ID)
	second, err := svc.Start(ctx, "crm-test", "dump", "", nil)
	require.NoError(t, err)
	waitTerminal(t, svc, second.ID)

	all, err := svc.List(ctx, "")
	require.NoError(t, err)
	require.Len(t, all, 2)
	require.Equal(t, second.ID, all[0].ID, "newest first")

	filtered, err := svc.List(ctx, "crm-test")
	require.NoError(t, err)
	require.Len(t, filtered, 1)
	require.Equal(t, "crm-test", filtered[0].Instance)

	none, err := svc.List(ctx, "ghost-instance")
	require.NoError(t, err)
	require.Empty(t, none)

	_, err = svc.Get(ctx, 99999)
	require.ErrorIs(t, err, runs.ErrNotFound)
}
