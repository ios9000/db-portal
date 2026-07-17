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
