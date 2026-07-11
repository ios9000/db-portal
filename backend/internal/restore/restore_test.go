package restore_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/restore"
)

// The recipe is the invariant this WU exists to institutionalize (D1): there
// is NO arrangement of inputs that omits the safety dump, because it is a
// literal in the returned slice, not a runtime-toggled option. This test is
// the "no skip affordance" guarantee at the code level.
func TestStepsRecipeIsFixedAndOrdered(t *testing.T) {
	steps := restore.Steps(42, "cafef00d", "billing-test-20260711.dump.tgz")

	require.Len(t, steps, 3, "always exactly verify → safety_dump → restore")
	require.Equal(t, restore.OpVerify, steps[0].Operation)
	require.Equal(t, restore.OpSafetyDump, steps[1].Operation,
		"the safety dump is unconditional and always present")
	require.Equal(t, restore.OpRestore, steps[2].Operation,
		"restore is last — verify precedes the safety dump precedes the restore (mini-ADR 1)")
}

// Lineage (id + checksum + name) rides verify and restore only — ids and
// checksums, never secrets (SPEC-032). The safety dump dumps the target
// as-is, so it carries none.
func TestStepsCarryArtifactLineage(t *testing.T) {
	steps := restore.Steps(42, "cafef00d", "billing-test-20260711.dump.tgz")

	want := map[string]string{
		"artifact_id":   strconv.FormatInt(42, 10),
		"checksum":      "cafef00d",
		"artifact_name": "billing-test-20260711.dump.tgz",
	}
	require.Equal(t, want, steps[0].Params, "verify pins the source")
	require.Equal(t, want, steps[2].Params, "restore pins the source")
	require.Empty(t, steps[1].Params, "the safety dump carries no source lineage")
}

// Each step gets its own params map (cloneParams): a test — or the chain
// driver — mutating one step's params must never bleed into another.
func TestStepsParamsAreIndependent(t *testing.T) {
	steps := restore.Steps(42, "cafef00d", "art")
	steps[0].Params["mock_fail_at"] = "1"
	require.NotContains(t, steps[2].Params, "mock_fail_at",
		"mutating the verify step must not touch the restore step")
}
