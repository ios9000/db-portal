package engine_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
)

func newFastMock(t *testing.T) engine.Adapter {
	t.Helper()
	return engine.NewMockEngine(engine.MockConfig{Name: "mock-test", StepDelay: 2 * time.Millisecond})
}

// drain reads the stream until the engine closes it (job terminal).
func drain(t *testing.T, ch <-chan engine.LogLine) []string {
	t.Helper()
	var lines []string
	deadline := time.After(5 * time.Second)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				return lines
			}
			lines = append(lines, l.Line)
		case <-deadline:
			t.Fatalf("log stream did not close; got %d lines so far", len(lines))
		}
	}
}

func TestHappyPathDump(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	id, err := e.StartJob(ctx, "dump", map[string]string{"instance": "pg-alpha"})
	require.NoError(t, err)

	ch, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)
	lines := drain(t, ch)

	st, err := e.Status(ctx, id)
	require.NoError(t, err)
	require.Equal(t, engine.StateSuccess, st.State)
	require.False(t, st.Started.IsZero())
	require.False(t, st.Finished.IsZero())
	require.Empty(t, st.Error)

	require.NotNil(t, st.Artifact, "dump success must carry artifact metadata")
	require.Contains(t, st.Artifact.Name, "pg-alpha")
	require.Len(t, st.Artifact.Checksum, 64, "sha256 hex")
	require.Positive(t, st.Artifact.SizeBytes)

	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, "PLAY [dump]")
	require.Contains(t, joined, "run pg_dump")
	require.Contains(t, joined, "PLAY RECAP")
	require.Contains(t, joined, "failed=0")
}

func TestFailureInjection(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	id, err := e.StartJob(ctx, "dump", map[string]string{"mock_fail_at": "2"})
	require.NoError(t, err)

	ch, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)
	lines := drain(t, ch)

	st, err := e.Status(ctx, id)
	require.NoError(t, err)
	require.Equal(t, engine.StateFailed, st.State)
	require.Contains(t, st.Error, "step 2")
	require.Nil(t, st.Artifact, "failed jobs must not register artifacts")

	joined := strings.Join(lines, "\n")
	require.Contains(t, joined, "FAILED!")
	require.Contains(t, joined, "injected failure at step 2")
	require.Contains(t, joined, "failed=1")
}

func TestCancelMidRun(t *testing.T) {
	e := engine.NewMockEngine(engine.MockConfig{Name: "mock-cancel", StepDelay: 50 * time.Millisecond})
	ctx := context.Background()

	id, err := e.StartJob(ctx, "dump", nil)
	require.NoError(t, err)

	ch, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)

	// The PLAY banner is emitted as soon as the runner starts — once we
	// see it the job is mid-run (first TASK is still ~50ms away).
	select {
	case l, ok := <-ch:
		require.True(t, ok)
		require.Contains(t, l.Line, "PLAY [dump]")
	case <-time.After(5 * time.Second):
		t.Fatal("no log output")
	}

	require.NoError(t, e.Cancel(ctx, id))
	lines := drain(t, ch)

	st, err := e.Status(ctx, id)
	require.NoError(t, err)
	require.Equal(t, engine.StateCanceled, st.State)
	require.False(t, st.Finished.IsZero())
	require.Nil(t, st.Artifact)
	require.Contains(t, strings.Join(lines, "\n"), "RUNNING HANDLER")
}

func TestCancelAfterTerminalIsNoop(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	id, err := e.StartJob(ctx, "dump", nil)
	require.NoError(t, err)
	ch, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)
	drain(t, ch)

	require.NoError(t, e.Cancel(ctx, id))
	st, err := e.Status(ctx, id)
	require.NoError(t, err)
	require.Equal(t, engine.StateSuccess, st.State, "cancel after finish must not change state")
}

func TestConcurrentJobsAreIsolated(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	idA, err := e.StartJob(ctx, "dump", map[string]string{"instance": "pg-alpha"})
	require.NoError(t, err)
	idB, err := e.StartJob(ctx, "dump", map[string]string{"instance": "pg-beta", "mock_fail_at": "1"})
	require.NoError(t, err)
	require.NotEqual(t, idA, idB)

	chA, err := e.StreamLogs(ctx, idA)
	require.NoError(t, err)
	chB, err := e.StreamLogs(ctx, idB)
	require.NoError(t, err)

	var linesA, linesB []string
	done := make(chan struct{}, 2)
	go func() { linesA = drain(t, chA); done <- struct{}{} }()
	go func() { linesB = drain(t, chB); done <- struct{}{} }()
	<-done
	<-done

	stA, err := e.Status(ctx, idA)
	require.NoError(t, err)
	stB, err := e.Status(ctx, idB)
	require.NoError(t, err)

	require.Equal(t, engine.StateSuccess, stA.State)
	require.Equal(t, engine.StateFailed, stB.State)

	joinedA := strings.Join(linesA, "\n")
	joinedB := strings.Join(linesB, "\n")
	require.Contains(t, joinedA, "pg-alpha")
	require.NotContains(t, joinedA, "pg-beta", "job A's stream leaked job B's lines")
	require.Contains(t, joinedB, "pg-beta")
	require.NotContains(t, joinedB, "pg-alpha", "job B's stream leaked job A's lines")
}

func TestStreamReplayAfterFinish(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	id, err := e.StartJob(ctx, "restore", nil)
	require.NoError(t, err)
	first, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)
	live := drain(t, first)

	// A second subscriber attaching after the job finished gets the full
	// history and an immediately-closed channel.
	replayCh, err := e.StreamLogs(ctx, id)
	require.NoError(t, err)
	replay := drain(t, replayCh)
	require.Equal(t, live, replay)
	require.Contains(t, strings.Join(replay, "\n"), "pre-restore dump")
}

func TestStreamStopsWhenContextCancelled(t *testing.T) {
	e := engine.NewMockEngine(engine.MockConfig{Name: "mock-ctx", StepDelay: 50 * time.Millisecond})
	ctx := context.Background()

	id, err := e.StartJob(ctx, "dump", nil)
	require.NoError(t, err)

	streamCtx, cancelStream := context.WithCancel(ctx)
	ch, err := e.StreamLogs(streamCtx, id)
	require.NoError(t, err)
	cancelStream()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				// Channel closed by the ctx watcher; the JOB keeps running.
				st, err := e.Status(ctx, id)
				require.NoError(t, err)
				require.NotEqual(t, engine.StateCanceled, st.State,
					"dropping a log stream must not cancel the job")
				return
			}
		case <-deadline:
			t.Fatal("stream did not close after context cancellation")
		}
	}
}

func TestUnknownJobErrors(t *testing.T) {
	e := newFastMock(t)
	ctx := context.Background()

	_, err := e.Status(ctx, "nope-1")
	require.ErrorIs(t, err, engine.ErrUnknownJob)
	_, err = e.StreamLogs(ctx, "nope-1")
	require.ErrorIs(t, err, engine.ErrUnknownJob)
	err = e.Cancel(ctx, "nope-1")
	require.ErrorIs(t, err, engine.ErrUnknownJob)
}

// A restarted engine (same name, new instance) must never resolve the old
// instance's job ids — orphaned runs would stream another run's logs
// (WU-013 regression: found live, mock seq restarted at 1 every boot).
func TestJobIDsDoNotAliasAcrossInstances(t *testing.T) {
	ctx := context.Background()
	before := engine.NewMockEngine(engine.MockConfig{Name: "mock-test", StepDelay: time.Millisecond})
	after := engine.NewMockEngine(engine.MockConfig{Name: "mock-test", StepDelay: time.Millisecond})

	staleID, err := before.StartJob(ctx, "dump", nil)
	require.NoError(t, err)
	freshID, err := after.StartJob(ctx, "dump", nil)
	require.NoError(t, err)
	require.NotEqual(t, staleID, freshID)

	_, err = after.Status(ctx, staleID)
	require.ErrorIs(t, err, engine.ErrUnknownJob)
	_, err = after.StreamLogs(ctx, staleID)
	require.ErrorIs(t, err, engine.ErrUnknownJob)
}
