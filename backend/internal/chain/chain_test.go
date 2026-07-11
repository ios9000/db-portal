package chain_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

const testActor = "dba-test"

// harness is the SPEC-032 service-seam rig: real migrated DB, real
// runs.Service on mock engines, real chain.Service — plus recording
// notifiers at both granularities so halt-mail exactly-once AND step-run
// suppression are assertable.
type harness struct {
	pool      *pgxpool.Pool
	runs      *runs.Service
	chains    *chain.Service
	chainMail *recordingChainNotifier
	runMail   *recordingRunNotifier
}

func newHarness(t *testing.T, delay time.Duration) *harness {
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

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runSvc := runs.NewService(pool, reg, log)
	runSvc.PollInterval = time.Millisecond
	t.Cleanup(runSvc.Wait)

	h := &harness{
		pool: pool, runs: runSvc,
		chainMail: &recordingChainNotifier{},
		runMail:   &recordingRunNotifier{},
	}
	// Production's notifier shape (main.go): run mail behind the step
	// filter, chain mail direct.
	runSvc.Notifier = chain.StepRunFilter{Next: h.runMail}

	h.chains = chain.New(pool, runSvc, log)
	h.chains.PollInterval = time.Millisecond
	h.chains.Notifier = h.chainMail
	t.Cleanup(h.chains.Wait)
	return h
}

// threeSteps is the ROADMAP exit drill's shape; failAt2 injects a mock
// failure into the second step only.
func threeSteps(failAt2 bool) []chain.StepSpec {
	step2 := chain.StepSpec{Operation: "dump"}
	if failAt2 {
		step2.Params = map[string]string{"mock_fail_at": "1"}
	}
	return []chain.StepSpec{{Operation: "dump"}, step2, {Operation: "dump"}}
}

func createReq(instance string, steps []chain.StepSpec) chain.CreateRequest {
	return chain.CreateRequest{
		Kind: "test-chain", Instance: instance, Actor: testActor,
		Confirm: instance, Reason: "drill", Steps: steps,
	}
}

func waitState(t *testing.T, svc *chain.Service, id int64, want string) chain.Chain {
	t.Helper()
	var c chain.Chain
	require.Eventually(t, func() bool {
		var err error
		c, err = svc.Get(context.Background(), id)
		require.NoError(t, err)
		return c.State == want
	}, 10*time.Second, 2*time.Millisecond, "chain %d never reached %q", id, want)
	return c
}

type recordingChainNotifier struct {
	mu    sync.Mutex
	halts []chain.Chain
}

func (n *recordingChainNotifier) ChainHalted(_ context.Context, c chain.Chain) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.halts = append(n.halts, c)
	return nil
}

func (n *recordingChainNotifier) all() []chain.Chain {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]chain.Chain{}, n.halts...)
}

type recordingRunNotifier struct {
	mu   sync.Mutex
	runs []runs.Run
}

func (n *recordingRunNotifier) RunEnded(_ context.Context, r runs.Run) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.runs = append(n.runs, r)
	return nil
}

func (n *recordingRunNotifier) all() []runs.Run {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]runs.Run{}, n.runs...)
}

func auditActors(t *testing.T, pool *pgxpool.Pool, runID int64) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT actor FROM audit_event WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		require.NoError(t, rows.Scan(&a))
		out = append(out, a)
	}
	require.NoError(t, rows.Err())
	return out
}

// Behavior 1: three steps fire strictly sequentially through Start, each
// fully audited, and the chain finishes success.
func TestChainHappyPath(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("billing-test", threeSteps(false)))
	require.NoError(t, err)
	require.Equal(t, "running", c.State)
	require.Len(t, c.Steps, 3)

	c = waitState(t, h.chains, c.ID, "success")
	require.NotNil(t, c.FinishedAt)
	require.Nil(t, c.HaltedAt)

	var lastRunID int64
	for _, st := range c.Steps {
		require.Equal(t, "success", st.Status)
		require.NotNil(t, st.RunID)
		require.Greater(t, *st.RunID, lastRunID, "steps must fire in order")
		lastRunID = *st.RunID
		// Full audit attribution through Start (SPEC-032 mini-ADR 1): the
		// submitted + finished pair, both as chain:<initiator>.
		require.Equal(t, []string{"chain:" + testActor, "chain:" + testActor},
			auditActors(t, h.pool, *st.RunID))
	}

	// ForRun finds the chain from any step run; a non-step run answers
	// ErrNotFound (the strip simply isn't shown).
	got, err := h.chains.ForRun(ctx, *c.Steps[1].RunID)
	require.NoError(t, err)
	require.Equal(t, c.ID, got.ID)

	lone, err := h.runs.Start(ctx, runs.StartRequest{
		Actor: testActor, Instance: "billing-test", Operation: "dump",
	})
	require.NoError(t, err)
	_, err = h.chains.ForRun(ctx, lone.ID)
	require.ErrorIs(t, err, chain.ErrNotFound)

	require.Empty(t, h.chainMail.all(), "a successful chain must not mail")
}

// Behavior 2 (the ROADMAP exit drill, halt half): injected failure at step
// 2 leaves step 1 intact, halts the chain, mails EXACTLY once at chain
// granularity — the step run's own mail is suppressed — and never fires
// step 3.
func TestChainHaltsOnStepFailure(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("billing-test", threeSteps(true)))
	require.NoError(t, err)
	c = waitState(t, h.chains, c.ID, "halted")
	h.chains.Wait() // driver + notify goroutines settle before asserting mail

	require.NotNil(t, c.HaltedAt)
	require.Equal(t, "success", c.Steps[0].Status, "step 1 run stands")
	require.Equal(t, "failed", c.Steps[1].Status)
	require.Equal(t, "pending", c.Steps[2].Status, "step 3 must never fire")
	require.Nil(t, c.Steps[2].RunID)

	halts := h.chainMail.all()
	require.Len(t, halts, 1, "exactly ONE mail per halt")
	require.Equal(t, c.ID, halts[0].ID)
	require.Empty(t, h.runMail.all(),
		"the step run's own failure mail is suppressed (mini-ADR 5)")
}

// Behavior 3 (the drill's resume half): resume re-fires the failed step as
// a NEW run attributed to the RESUMER, keeps the superseded run's audit
// trail, and continues to success.
func TestChainResume(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("billing-test", threeSteps(true)))
	require.NoError(t, err)
	c = waitState(t, h.chains, c.ID, "halted")
	failedRunID := *c.Steps[1].RunID

	// Clear the injected failure: the resume must re-run the step as-is
	// after the human "fixed the cause".
	_, err = h.pool.Exec(ctx,
		`UPDATE chain_step SET params = '{}' WHERE chain_id = $1 AND seq = 2`, c.ID)
	require.NoError(t, err)

	resumed, err := h.chains.Resume(ctx, c.ID, "dba-resumer")
	require.NoError(t, err)
	require.Equal(t, "running", resumed.State)
	require.Nil(t, resumed.HaltedAt, "resume clears the halt stamp")

	c = waitState(t, h.chains, c.ID, "success")
	require.NotEqual(t, failedRunID, *c.Steps[1].RunID, "the failed step re-fires as a NEW run")

	// History kept: the superseded run and its audit rows survive, still
	// attributed to the original pass.
	old, err := h.runs.Get(ctx, failedRunID)
	require.NoError(t, err)
	require.Equal(t, "failed", old.State)
	require.Equal(t, []string{"chain:" + testActor, "chain:" + testActor},
		auditActors(t, h.pool, failedRunID))

	// The new pass audits as the resumer (mini-ADR 1) — steps 2 AND 3.
	for _, st := range c.Steps[1:] {
		require.Equal(t, []string{"chain:dba-resumer", "chain:dba-resumer"},
			auditActors(t, h.pool, *st.RunID))
	}
}

// Behavior 4: the guarded flip is the single-flight lock — resume admits
// exactly one caller, and only on a halted chain.
func TestResumeSingleFlight(t *testing.T) {
	h := newHarness(t, 20*time.Millisecond) // slow enough to observe 'running'
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("billing-test",
		[]chain.StepSpec{{Operation: "dump"}}))
	require.NoError(t, err)

	_, err = h.chains.Resume(ctx, c.ID, testActor)
	require.ErrorIs(t, err, chain.ErrNotResumable, "a running chain must not resume")

	_, err = h.chains.Resume(ctx, 99999, testActor)
	require.ErrorIs(t, err, chain.ErrNotFound)

	waitState(t, h.chains, c.ID, "success")
	_, err = h.chains.Resume(ctx, c.ID, testActor)
	require.ErrorIs(t, err, chain.ErrNotResumable, "a finished chain must not resume")

	// Concurrent double-resume on a genuinely halted chain: exactly one wins.
	halted, err := h.chains.Create(ctx, createReq("billing-test", []chain.StepSpec{
		{Operation: "dump", Params: map[string]string{"mock_fail_at": "1"}},
	}))
	require.NoError(t, err)
	waitState(t, h.chains, halted.ID, "halted")

	_, err = h.pool.Exec(ctx,
		`UPDATE chain_step SET params = '{}' WHERE chain_id = $1`, halted.ID)
	require.NoError(t, err)

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := h.chains.Resume(ctx, halted.ID, testActor)
			errs <- err
		}()
	}
	got := []error{<-errs, <-errs}
	winners := 0
	for _, err := range got {
		if err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, chain.ErrNotResumable)
		}
	}
	require.Equal(t, 1, winners, "exactly one concurrent resume may proceed")
	waitState(t, h.chains, halted.ID, "success")
}

// Behavior 5: canceling a live step run halts the chain (one mail),
// resumable like any halt.
func TestCancelHaltsChain(t *testing.T) {
	h := newHarness(t, 20*time.Millisecond)
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("billing-test", threeSteps(false)))
	require.NoError(t, err)

	// Cancel step 1's run while it is live.
	var runID int64
	require.Eventually(t, func() bool {
		got, err := h.chains.Get(ctx, c.ID)
		require.NoError(t, err)
		if got.Steps[0].RunID == nil {
			return false
		}
		runID = *got.Steps[0].RunID
		return true
	}, 5*time.Second, time.Millisecond)
	require.NoError(t, h.runs.Cancel(ctx, testActor, runID))

	c = waitState(t, h.chains, c.ID, "halted")
	h.chains.Wait()
	require.Equal(t, "canceled", c.Steps[0].Status)
	require.Len(t, h.chainMail.all(), 1)
	require.Empty(t, h.runMail.all(), "canceled step runs are chain-mail territory too")

	// Resumable (mini-ADR 3): the canceled step re-fires as a new run.
	_, err = h.chains.Resume(ctx, c.ID, testActor)
	require.NoError(t, err)
	waitState(t, h.chains, c.ID, "success")
}

// Behavior 6: the boot sweep halts chains a dead process left running —
// but never one with a genuinely live run.
func TestSweepOrphans(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	// Fabricate the crash shape directly: a running chain whose only step
	// never fired (the driver died between INSERT and Start).
	var instanceID int64
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT id FROM instance WHERE name = 'billing-test'`).Scan(&instanceID))
	var orphanID int64
	require.NoError(t, h.pool.QueryRow(ctx, `
		INSERT INTO chain (kind, instance_id, created_by, confirm)
		VALUES ('test-chain', $1, $2, '') RETURNING id`, instanceID, testActor).Scan(&orphanID))
	_, err := h.pool.Exec(ctx, `
		INSERT INTO chain_step (chain_id, seq, operation) VALUES ($1, 1, 'dump')`, orphanID)
	require.NoError(t, err)

	// A finished chain the sweep must ignore…
	live, err := h.chains.Create(ctx, createReq("crm-test",
		[]chain.StepSpec{{Operation: "dump"}}))
	require.NoError(t, err)
	// …and a running chain whose step run IS live, pinned as raw rows so no
	// watcher can finish it under the sweep.
	var pinnedChainID, pinnedRunID int64
	require.NoError(t, h.pool.QueryRow(ctx, `
		INSERT INTO chain (kind, instance_id, created_by, confirm)
		VALUES ('test-chain', $1, $2, '') RETURNING id`, instanceID, testActor).Scan(&pinnedChainID))
	require.NoError(t, h.pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'running') RETURNING id`,
		instanceID).Scan(&pinnedRunID))
	_, err = h.pool.Exec(ctx, `
		INSERT INTO chain_step (chain_id, seq, operation, run_id)
		VALUES ($1, 1, 'dump', $2)`, pinnedChainID, pinnedRunID)
	require.NoError(t, err)

	waitState(t, h.chains, live.ID, "success")

	swept, err := h.chains.SweepOrphans(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, swept, "only the orphan halts")
	h.chains.Wait()

	orphan, err := h.chains.Get(ctx, orphanID)
	require.NoError(t, err)
	require.Equal(t, "halted", orphan.State)

	pinned, err := h.chains.Get(ctx, pinnedChainID)
	require.NoError(t, err)
	require.Equal(t, "running", pinned.State, "a chain with a live run is not orphaned")

	halts := h.chainMail.all()
	require.Len(t, halts, 1)
	require.Equal(t, orphanID, halts[0].ID)

	// Idempotent: a second sweep finds nothing new.
	swept, err = h.chains.SweepOrphans(ctx)
	require.NoError(t, err)
	require.Zero(t, swept)
}

// Behavior 7: an instance promoted to prod mid-chain fails the stored
// ritual at the next fire — no run row, a visible halt, never an
// auto-confirm. Plus the creation-time ritual itself.
func TestEnvPromotionHaltsChain(t *testing.T) {
	h := newHarness(t, 30*time.Millisecond) // step 1 slow enough to promote under it
	ctx := context.Background()

	// Creation-time ritual: prod target demands the typed name.
	_, err := h.chains.Create(ctx, chain.CreateRequest{
		Kind: "test-chain", Instance: "billing-prod", Actor: testActor,
		Steps: []chain.StepSpec{{Operation: "dump"}},
	})
	require.ErrorIs(t, err, runs.ErrProdUnconfirmed)
	var chains int
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT count(*) FROM chain`).Scan(&chains))
	require.Zero(t, chains, "a refused ritual writes nothing")

	// Non-prod creation (empty confirm is fine), then promote mid-chain —
	// only after step 1's run is linked, so the promotion deterministically
	// lands between the two fires (step 1 already passed the ritual).
	c, err := h.chains.Create(ctx, chain.CreateRequest{
		Kind: "test-chain", Instance: "crm-test", Actor: testActor,
		Steps: []chain.StepSpec{{Operation: "dump"}, {Operation: "dump"}},
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		got, err := h.chains.Get(ctx, c.ID)
		require.NoError(t, err)
		return got.Steps[0].RunID != nil
	}, 5*time.Second, time.Millisecond)
	_, err = h.pool.Exec(ctx,
		`UPDATE instance SET env = 'prod' WHERE name = 'crm-test'`)
	require.NoError(t, err)

	c = waitState(t, h.chains, c.ID, "halted")
	h.chains.Wait()
	require.Equal(t, "success", c.Steps[0].Status)
	require.Equal(t, "pending", c.Steps[1].Status, "the refused fire leaves NO run row")
	require.Nil(t, c.Steps[1].RunID)
	require.Len(t, h.chainMail.all(), 1, "the ritual refusal halts loudly")
}

// Create's validation vocabulary matches the runs/schedule seams.
func TestCreateValidation(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	_, err := h.chains.Create(ctx, createReq("billing-test", nil))
	require.ErrorIs(t, err, chain.ErrNoSteps)

	_, err = h.chains.Create(ctx, createReq("billing-test",
		[]chain.StepSpec{{Operation: "explode"}}))
	require.ErrorIs(t, err, runs.ErrUnknownOperation)

	_, err = h.chains.Create(ctx, createReq("no-such-instance",
		[]chain.StepSpec{{Operation: "dump"}}))
	require.ErrorIs(t, err, runs.ErrUnknownInstance)
}

// The mini-ADR 5 filter in isolation: chain-attributed runs are quiet,
// everything else passes through.
func TestStepRunFilter(t *testing.T) {
	rec := &recordingRunNotifier{}
	f := chain.StepRunFilter{Next: rec}

	require.NoError(t, f.RunEnded(context.Background(),
		runs.Run{ID: 1, RequestedBy: "chain:dba1"}))
	require.Empty(t, rec.all())

	require.NoError(t, f.RunEnded(context.Background(),
		runs.Run{ID: 2, RequestedBy: "dba1"}))
	require.NoError(t, f.RunEnded(context.Background(),
		runs.Run{ID: 3, RequestedBy: "schedule:dba1"}))
	require.Len(t, rec.all(), 2, "human and scheduled runs still mail")
}
