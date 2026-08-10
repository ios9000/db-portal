package runs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// internalReq is testReq plus the chain driver's Internal flag — the only
// caller allowed to fire a non-launchable operation (SPEC-031 mini-ADR 3).
func internalReq(instance, operation string) runs.StartRequest {
	r := testReq(instance, operation, "", nil)
	r.Internal = true
	return r
}

// The launchable gate is the single choke point behind AC-4: a bare
// restore/verify/safety_dump over the button or the scheduler (both leave
// Internal false) looks exactly like an unknown operation, so the ONLY way to
// run one is the assembled chain (which always contains the safety dump).
func TestStartRefusesBareInternalOps(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	for _, op := range []string{"restore", "verify", "safety_dump"} {
		_, err := svc.Start(ctx, testReq("billing-test", op, "", nil))
		require.ErrorIs(t, err, runs.ErrUnknownOperation,
			"%q must be unreachable without the chain driver's Internal flag", op)
	}

	// The launchable op is unaffected — the gate only bites internal steps.
	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.Equal(t, "dump", waitTerminal(t, svc, run.ID).Operation)
}

// With Internal set (the driver's posture), the same operations run through
// the identical Start path — full guardrail + audit — and finalize normally.
// verify and restore produce no artifact (only the dump template does), so
// they register nothing. crm-test (vm): restore on the Patroni fixture
// instance is refused at fire time since WU-048 — that path has its own test.
func TestStartAcceptsInternalOps(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	for _, op := range []string{"verify", "restore"} {
		run, err := svc.Start(ctx, internalReq("crm-test", op))
		require.NoError(t, err, op)
		final := waitTerminal(t, svc, run.ID)
		require.Equal(t, "success", final.State, op)
		require.Nil(t, final.Artifact, "%q is artifact-less — registers nothing", op)
		require.Empty(t, registryRowsFor(t, pool, run.ID), op)
	}
}

// WU-048 (m4-gate finding 1): the Patroni-restore guardrail is enforced at
// runs.Start — fire time — reading the target's CURRENT cluster platform
// fresh, so every fire path (create-drive, Resume re-drive, mid-chain step)
// inherits it exactly like the self-target ban and the prod ritual. Refused
// before any row: no run, no lock, denial on the security ledger. The block
// stays narrow — verify and the safety dump run fine on the same Patroni
// target, and a restore onto a vm target is untouched.
func TestPatroniRestoreRefusedAtStart(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	_, err := svc.Start(ctx, internalReq("billing-test", "restore"))
	require.ErrorIs(t, err, runs.ErrPatroniRestore)

	var runRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM run WHERE instance_id = $1`,
		instanceID(t, pool, "billing-test")).Scan(&runRows))
	require.Zero(t, runRows, "a refused Patroni restore never becomes a run")

	var actor, detail string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT actor, detail FROM auth_event WHERE action = 'guardrail.denied'`).
		Scan(&actor, &detail))
	require.Equal(t, testActor, actor)
	require.Equal(t, "patroni-restore: billing-test", detail)

	for _, tc := range []struct{ instance, op string }{
		{"billing-test", "verify"},
		{"billing-test", "safety_dump"},
		{"crm-test", "restore"},
	} {
		run, err := svc.Start(ctx, internalReq(tc.instance, tc.op))
		require.NoError(t, err, "%s on %s must stay allowed", tc.op, tc.instance)
		require.Equal(t, "success", waitTerminal(t, svc, run.ID).State)
	}
}

// SPEC-031 mini-ADR 2: the safety dump is a distinct operation whose catalog
// entry carries RetentionClass "safety"; finalize reads the run's operation
// and stamps the class. A plain dump still stamps "standard" (the WU-030
// default), so the two are distinguishable in the registry — which is what
// keeps a pre-restore snapshot out of the target's backup regime.
func TestSafetyDumpStampsSafetyClass(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	safety, err := svc.Start(ctx, internalReq("billing-test", "safety_dump"))
	require.NoError(t, err)
	require.Equal(t, "success", waitTerminal(t, svc, safety.ID).State)
	safetyRows := registryRowsFor(t, pool, safety.ID)
	require.Len(t, safetyRows, 1, "the safety dump produces the dump template's artifact")
	require.Equal(t, "safety", safetyRows[0].retentionClass)

	plain, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.Equal(t, "success", waitTerminal(t, svc, plain.ID).State)
	require.Equal(t, "standard", registryRowsFor(t, pool, plain.ID)[0].retentionClass)
}

// GetArtifact is the restore assembler's by-id source lookup: it returns the
// registry row, or ErrArtifactNotFound for an id that was never registered.
func TestGetArtifact(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.Equal(t, "success", waitTerminal(t, svc, run.ID).State)

	list, err := svc.ListArtifacts(ctx, "billing-test")
	require.NoError(t, err)
	require.Len(t, list, 1)
	want := list[0]

	got, err := svc.GetArtifact(ctx, want.ID)
	require.NoError(t, err)
	require.Equal(t, want.ID, got.ID)
	require.Equal(t, run.ID, got.RunID, "the row is keyed to its origin run")
	require.Equal(t, want.Checksum, got.Checksum)
	require.Equal(t, "standard", got.RetentionClass)

	_, err = svc.GetArtifact(ctx, 9_999_999)
	require.ErrorIs(t, err, runs.ErrArtifactNotFound, "an unknown id is a 404, not an empty row")
}
