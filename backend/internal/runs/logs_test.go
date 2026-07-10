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

func collectLines(t *testing.T, ch <-chan engine.LogLine) []string {
	t.Helper()
	var out []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case l, open := <-ch:
			if !open {
				return out
			}
			out = append(out, l.Line)
		case <-deadline:
			t.Fatalf("log stream never closed; got %d lines", len(out))
		}
	}
}

// SPEC-013 behavior 1: a finished run replays its full history, in order,
// then the channel closes.
func TestStreamLogsReplaysFinishedRun(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, run.ID)

	lines := collectLines(t, mustStream(t, svc, run.ID))
	require.NotEmpty(t, lines)
	require.Contains(t, lines[0], "PLAY [dump]", "replay starts at the beginning")
	require.Contains(t, lines[len(lines)-1], "PLAY RECAP", "replay ends at the recap")
}

// SPEC-013 behavior 2: subscribing to a live run follows it to completion —
// the stream ends with the recap and the channel closes on job end.
func TestStreamLogsFollowsLiveRun(t *testing.T) {
	svc, _ := newServiceWithDelay(t, 20*time.Millisecond)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	lines := collectLines(t, mustStream(t, svc, run.ID)) // job still running here
	require.Contains(t, lines[len(lines)-1], "PLAY RECAP")
	require.Equal(t, "success", waitTerminal(t, svc, run.ID).State)
}

// SPEC-013 behavior 7: a client hanging up detaches the subscription (the
// channel closes) without disturbing the job.
func TestStreamLogsClientDisconnect(t *testing.T) {
	svc, _ := newServiceWithDelay(t, 20*time.Millisecond)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	streamCtx, cancel := context.WithCancel(context.Background())
	ch, err := svc.StreamLogs(streamCtx, run.ID)
	require.NoError(t, err)
	cancel()
	require.Eventually(t, func() bool {
		for {
			select {
			case _, open := <-ch:
				if !open {
					return true
				}
			default:
				return false
			}
		}
	}, 2*time.Second, 5*time.Millisecond, "cancelled stream must close")

	require.Equal(t, "success", waitTerminal(t, svc, run.ID).State,
		"the job must outlive its log subscribers")
}

// SPEC-013 behavior 4: unknown run vs. runs whose logs are gone.
func TestStreamLogsUnavailable(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	_, err := svc.StreamLogs(ctx, 99999)
	require.ErrorIs(t, err, runs.ErrNotFound)

	// A swept orphan: the run row exists, the engine never heard of the job.
	ghostID := insertGhostRun(t, pool, "failed")
	_, err = svc.StreamLogs(ctx, ghostID)
	require.ErrorIs(t, err, runs.ErrNoLogs)
}

// SPEC-013 behavior 4: an engine-refused run never had a job — no logs,
// but the run itself still serves.
func TestStreamLogsEngineRefusedRun(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	svc := runs.NewService(pool, engine.NewRegistry(), // fails closed
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrEngine)

	_, err = svc.StreamLogs(context.Background(), run.ID)
	require.ErrorIs(t, err, runs.ErrNoLogs)
	got, err := svc.Get(context.Background(), run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.State)
}

// SPEC-013 behavior 5 (as amended by SPEC-021 mini-ADR 5): cancel drives
// the run to `canceled` through the normal watcher/finalize path — no new
// finalization path, just the run.cancel_requested attribution row.
func TestCancelRunningJob(t *testing.T) {
	svc, pool := newServiceWithDelay(t, 50*time.Millisecond)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.NoError(t, svc.Cancel(ctx, testActor, run.ID))

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "canceled", final.State)
	require.NotNil(t, final.Error)
	require.Contains(t, *final.Error, "canceled by operator")

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 3)
	require.Equal(t, "run.cancel_requested", events[1].action)
	require.Equal(t, "run.finished", events[2].action)
	require.Equal(t, "canceled", *events[2].finalStatus)
}

// SPEC-013 behavior 6: terminal, ghost and unknown runs are not cancelable.
func TestCancelNotCancelable(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	require.ErrorIs(t, svc.Cancel(ctx, testActor, 99999), runs.ErrNotFound)

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	final := waitTerminal(t, svc, run.ID)
	require.ErrorIs(t, svc.Cancel(ctx, testActor, run.ID), runs.ErrNotCancelable)
	unchanged, err := svc.Get(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, final.State, unchanged.State)

	ghostID := insertGhostRun(t, pool, "running")
	require.ErrorIs(t, svc.Cancel(ctx, testActor, ghostID), runs.ErrNotCancelable)
}

func mustStream(t *testing.T, svc *runs.Service, id int64) <-chan engine.LogLine {
	t.Helper()
	ch, err := svc.StreamLogs(context.Background(), id)
	require.NoError(t, err)
	return ch
}

// insertGhostRun writes a run row whose job_id no engine knows — the
// post-restart shape (before or after the orphan sweep, per state).
func insertGhostRun(t *testing.T, pool *pgxpool.Pool, state string) int64 {
	t.Helper()
	var instanceID, runID int64
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT id FROM instance WHERE name = 'billing-test'`).Scan(&instanceID))
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, job_id)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', $2, 'mock-nonprod-ghost')
		RETURNING id`, instanceID, state).Scan(&runID))
	return runID
}
