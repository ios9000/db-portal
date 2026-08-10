package chain_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/runs"
)

func chainCount(t *testing.T, h *harness) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM chain`).Scan(&n))
	return n
}

func denialDetail(t *testing.T, h *harness) string {
	t.Helper()
	var detail string
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT detail FROM auth_event WHERE action = 'guardrail.denied'`).Scan(&detail))
	return detail
}

// restoreSteps mirrors the restore recipe's shape — the middle safety_dump and
// the final restore are what a real restore chain carries.
func restoreSteps() []chain.StepSpec {
	return []chain.StepSpec{
		{Operation: "verify"}, {Operation: "safety_dump"}, {Operation: "restore"},
	}
}

// SPEC-042 mini-ADR 6: a chain with a restore step targeting a Patroni-managed
// cluster is refused synchronously — no chain row, denial audited. billing-test
// is on the k8s_patroni "billing" cluster in the fixture.
func TestPatroniRestoreRefused(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	_, err := h.chains.Create(ctx, createReq("billing-test", restoreSteps()))
	require.ErrorIs(t, err, runs.ErrPatroniRestore)
	require.Zero(t, chainCount(t, h), "a refused restore never becomes a chain")
	require.Equal(t, "patroni-restore: billing-test", denialDetail(t, h))
}

// The block is narrow: a DUMP chain on Patroni is fine (dumps are safe, even
// desirable from a replica), and a restore onto a VM cluster is fine. crm-test
// is on the vm "crm" cluster.
func TestPatroniAllowsDumpAndVMRestore(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	dumpChain, err := h.chains.Create(ctx,
		createReq("billing-test", []chain.StepSpec{{Operation: "dump"}}))
	require.NoError(t, err, "a dump on a Patroni target is not blocked")
	require.NotZero(t, dumpChain.ID)

	vmRestore, err := h.chains.Create(ctx, createReq("crm-test", restoreSteps()))
	require.NoError(t, err, "a restore onto a VM cluster is allowed")
	require.NotZero(t, vmRestore.ID)
}

// denialRow returns the single guardrail.denied auth_event row's actor and
// detail — the fire-time refusal tests assert attribution too.
func denialRow(t *testing.T, h *harness) (actor, detail string) {
	t.Helper()
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT actor, detail FROM auth_event WHERE action = 'guardrail.denied'`).
		Scan(&actor, &detail))
	return actor, detail
}

// rePlatform models the m4-gate finding-1 leg: an ordinary estate re-import
// listing the instance under a k8s_patroni cluster UPDATEs cluster_id in
// place (inventory.upsertInstance) — no quarantine, no trace on the chain.
func rePlatform(t *testing.T, h *harness, instance string) {
	t.Helper()
	_, err := h.pool.Exec(context.Background(), `
		UPDATE instance SET cluster_id = (SELECT id FROM cluster WHERE name = 'billing')
		WHERE name = $1`, instance)
	require.NoError(t, err)
}

// WU-048 (m4-gate finding 1): the Patroni block is re-validated at FIRE time,
// not only at Create. A restore chain created while the target was vm, halted,
// then re-platformed to k8s_patroni must NOT fire its restore step on Resume —
// runs.Start reads the platform fresh (the same per-fire treatment as the
// self-target ban and the prod ritual), the driver halts visibly, and the
// denial is audited. The earlier steps prove the block stays narrow: verify
// and the safety dump re-fire and SUCCEED on the Patroni target.
func TestRePlatformHaltsResumedRestore(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	ctx := context.Background()

	steps := restoreSteps()
	steps[0].Params = map[string]string{"mock_fail_at": "1"}
	c, err := h.chains.Create(ctx, createReq("crm-test", steps))
	require.NoError(t, err, "crm-test is vm at create — the door check passes")
	c = waitState(t, h.chains, c.ID, "halted")

	rePlatform(t, h, "crm-test")
	// The DBA "fixed the cause" and resumes — the product's advertised flow.
	_, err = h.pool.Exec(ctx,
		`UPDATE chain_step SET params = '{}' WHERE chain_id = $1 AND seq = 1`, c.ID)
	require.NoError(t, err)
	_, err = h.chains.Resume(ctx, c.ID, "dba-resumer")
	require.NoError(t, err)

	c = waitState(t, h.chains, c.ID, "halted")
	h.chains.Wait()

	require.Equal(t, "success", c.Steps[0].Status, "verify re-fires fine on Patroni")
	require.Equal(t, "success", c.Steps[1].Status, "the safety dump stays allowed on Patroni")
	require.Equal(t, "pending", c.Steps[2].Status, "the restore step must never fire")
	require.Nil(t, c.Steps[2].RunID, "the refusal leaves NO run row")

	actor, detail := denialRow(t, h)
	require.Equal(t, "chain:dba-resumer", actor, "the denial attributes the resuming pass")
	require.Equal(t, "patroni-restore: crm-test", detail)
	require.Len(t, h.chainMail.all(), 2, "the refusal halts loudly (original halt + patroni halt)")
}

// The same gap without a resume: a mid-chain re-platform (while an earlier
// step is still running) is caught when the restore step comes up to fire.
func TestRePlatformMidChainBlocksRestore(t *testing.T) {
	h := newHarness(t, 30*time.Millisecond) // step 1 slow enough to re-platform under it
	ctx := context.Background()

	c, err := h.chains.Create(ctx, createReq("crm-test", restoreSteps()))
	require.NoError(t, err)
	// Re-platform only after step 1's run is linked, so the flip
	// deterministically lands before the restore step fires (the
	// TestEnvPromotionHaltsChain pattern).
	require.Eventually(t, func() bool {
		got, err := h.chains.Get(ctx, c.ID)
		require.NoError(t, err)
		return got.Steps[0].RunID != nil
	}, 5*time.Second, time.Millisecond)
	rePlatform(t, h, "crm-test")

	c = waitState(t, h.chains, c.ID, "halted")
	h.chains.Wait()

	require.Equal(t, "success", c.Steps[0].Status)
	require.Equal(t, "success", c.Steps[1].Status, "the safety dump stays allowed on Patroni")
	require.Equal(t, "pending", c.Steps[2].Status, "the restore step must never fire")
	require.Nil(t, c.Steps[2].RunID)

	actor, detail := denialRow(t, h)
	require.Equal(t, "chain:"+testActor, actor)
	require.Equal(t, "patroni-restore: crm-test", detail)
	require.Len(t, h.chainMail.all(), 1, "exactly one mail for the one halt")
}

// SPEC-042 mini-ADR 5: the restore path refuses a self-target synchronously in
// Create (so the handler answers with the distinct error, not an async halt),
// and audits the denial.
func TestChainSelfTargetRefused(t *testing.T) {
	h := newHarness(t, time.Millisecond)
	h.chains.Protected = map[string]bool{"crm-test": true}
	ctx := context.Background()

	_, err := h.chains.Create(ctx, createReq("crm-test", restoreSteps()))
	require.ErrorIs(t, err, runs.ErrSelfTarget)
	require.Zero(t, chainCount(t, h), "a refused self-target never becomes a chain")
	require.Equal(t, "self-target: crm-test", denialDetail(t, h))
}
