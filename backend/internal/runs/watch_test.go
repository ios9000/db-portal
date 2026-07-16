package runs_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

// flakyAdapter is an engine.Adapter whose Status is scripted to exercise the
// watcher's transient-error resilience (WU-037 / m3-gate finding 1). StartJob
// hands back a fixed job id so watch() has something to poll; Status returns
// failErr for the first failFirst calls (or forever, if alwaysFail), then the
// terminal status.
type flakyAdapter struct {
	mu         sync.Mutex
	failFirst  int
	failErr    error
	alwaysFail bool
	terminal   engine.JobStatus
	calls      int
}

func (a *flakyAdapter) StartJob(context.Context, string, map[string]string) (engine.JobID, error) {
	return "flaky-1", nil
}

func (a *flakyAdapter) Status(context.Context, engine.JobID) (engine.JobStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if a.alwaysFail || a.calls <= a.failFirst {
		return engine.JobStatus{}, a.failErr
	}
	return a.terminal, nil
}

func (a *flakyAdapter) StreamLogs(context.Context, engine.JobID) (<-chan engine.LogLine, error) {
	return nil, engine.ErrUnknownJob
}
func (a *flakyAdapter) Cancel(context.Context, engine.JobID) error { return nil }

func (a *flakyAdapter) statusCalls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// newWatchService builds a Service whose non-prod class is the given adapter,
// with a fast poll cadence and watcher draining wired — Start spawns the real
// watch goroutine.
func newWatchService(t *testing.T, nonprod engine.Adapter) *runs.Service {
	t.Helper()
	svc, _ := newReconcileService(t, nonprod)
	svc.PollInterval = time.Millisecond
	t.Cleanup(svc.Wait)
	return svc
}

// WU-037 / m3-gate finding 1: a TRANSIENT Status error (not ErrUnknownJob) must
// NOT permanently fail the run. The watcher retries with backoff and recovers
// once the engine is reachable again — the mainline behavior a real engine
// behind the unchanged seam needs (MockEngine's only error was "job lost").
func TestWatchRetriesTransientStatusError(t *testing.T) {
	adapter := &flakyAdapter{
		failFirst: 3,
		failErr:   errors.New("connection refused"),
		terminal:  engine.JobStatus{ID: "flaky-1", State: engine.StateSuccess},
	}
	svc := newWatchService(t, adapter)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State, "a transient blip must not falsely fail the run")
	require.Nil(t, final.Error)
	require.GreaterOrEqual(t, adapter.statusCalls(), 4, "the watcher retried through the transient errors")
}

// WU-037: ErrUnknownJob is still a permanent, correct failure — the job the
// engine genuinely forgot — finalized immediately with the honest "engine lost
// the job" message (which now means EXACTLY that, not "any error").
func TestWatchUnknownJobFinalizesFailed(t *testing.T) {
	adapter := &flakyAdapter{alwaysFail: true, failErr: engine.ErrUnknownJob}
	svc := newWatchService(t, adapter)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "failed", final.State)
	require.NotNil(t, final.Error)
	require.Contains(t, *final.Error, "engine lost the job")
	require.Equal(t, 1, adapter.statusCalls(), "a lost job fails on the first poll, no retry")
}

// WU-037: a genuinely unreachable engine (transient errors that never clear)
// still surfaces — the watcher gives up after MaxStatusErrors and finalizes
// failed, but HONESTLY ("status unavailable after N attempts"), never with the
// lying "engine lost the job".
func TestWatchGivesUpAfterSustainedOutage(t *testing.T) {
	adapter := &flakyAdapter{alwaysFail: true, failErr: errors.New("connection refused")}
	svc := newWatchService(t, adapter)
	svc.MaxStatusErrors = 4 // keep the give-up fast and deterministic

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)

	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "failed", final.State)
	require.NotNil(t, final.Error)
	require.Contains(t, *final.Error, "engine status unavailable after 4 attempts")
	require.NotContains(t, *final.Error, "engine lost the job",
		"a transient outage must not be reported as a lost job")
	require.Equal(t, 4, adapter.statusCalls(), "gave up exactly at the ceiling")
}
