package chain_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

// flakyRuns wraps a real runs.Service and injects transient errors into the
// driver's step-run read (driver.go's inner watch — m3-gate finding 2). Start
// delegates so the real run + watcher still exist; Get fails failFirst times
// (or forever, if alwaysFail) before delegating.
type flakyRuns struct {
	inner      *runs.Service
	mu         sync.Mutex
	failFirst  int
	alwaysFail bool
	err        error
	getCalls   int
}

func (f *flakyRuns) Start(ctx context.Context, req runs.StartRequest) (runs.Run, error) {
	return f.inner.Start(ctx, req)
}

func (f *flakyRuns) Get(ctx context.Context, id int64) (runs.Run, error) {
	f.mu.Lock()
	f.getCalls++
	fail := f.alwaysFail || f.getCalls <= f.failFirst
	f.mu.Unlock()
	if fail {
		return runs.Run{}, f.err
	}
	return f.inner.Get(ctx, id)
}

// newFlakyChain builds a chain.Service driving through the flaky Runs wrapper,
// on the harness's real DB + engines + chain-mail recorder.
func newFlakyChain(t *testing.T, h *harness, fr *flakyRuns) *chain.Service {
	t.Helper()
	fr.inner = h.runs
	cs := chain.New(h.pool, fr, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cs.PollInterval = time.Millisecond
	cs.Notifier = h.chainMail
	t.Cleanup(cs.Wait)
	return cs
}

// WU-037 / m3-gate finding 2: a TRANSIENT read error while watching a step run
// must NOT wedge the chain 'running'. The driver retries and the chain drives
// to success once the read recovers.
func TestDriveRetriesTransientRunRead(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	fr := &flakyRuns{failFirst: 3, err: errors.New("read timeout")}
	cs := newFlakyChain(t, h, fr)

	c, err := cs.Create(ctx, createReq("billing-test", []chain.StepSpec{{Operation: "dump"}}))
	require.NoError(t, err)

	c = waitState(t, cs, c.ID, "success")
	require.Equal(t, "success", c.Steps[0].Status, "the chain self-healed through the transient read errors")
	require.Empty(t, h.chainMail.all(), "a self-healed chain does not mail")
}

// WU-037: a sustained read outage (errors that never clear) must not wedge the
// chain either — the driver gives up after MaxReadErrors and HALTS honestly
// (notifying, and re-enabling Resume), never a silent exit that leaves the
// chain 'running' forever with no alert and no recovery path.
func TestDriveHaltsOnSustainedReadOutage(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	fr := &flakyRuns{alwaysFail: true, err: errors.New("read timeout")}
	cs := newFlakyChain(t, h, fr)
	cs.MaxReadErrors = 3 // keep the give-up fast and deterministic

	c, err := cs.Create(ctx, createReq("billing-test", []chain.StepSpec{{Operation: "dump"}}))
	require.NoError(t, err)

	c = waitState(t, cs, c.ID, "halted")
	cs.Wait() // driver + notify goroutines settle before asserting mail
	require.NotNil(t, c.HaltedAt)

	halts := h.chainMail.all()
	require.Len(t, halts, 1, "the honest give-up halts loudly, exactly once")
	require.Equal(t, c.ID, halts[0].ID)

	// The chain is now resumable — the wedge finding's 'no recovery path' leg
	// is closed (Resume guards state='halted').
	_, err = cs.Resume(ctx, c.ID, testActor)
	require.NoError(t, err, "a honestly-halted chain can be resumed")
}
