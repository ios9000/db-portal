package catalog_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/catalog"
)

// SPEC-031 mini-ADR 3: All() is the user-facing catalog — only launchable
// operations. The restore-chain steps exist in the catalog (ByID finds them
// for Start/chain validation) but never reach the launch drawer or scheduler.
func TestAllReturnsOnlyLaunchable(t *testing.T) {
	ids := map[string]bool{}
	for _, op := range catalog.All() {
		ids[op.ID] = true
		require.True(t, op.Launchable, "All() must return only launchable ops, got %q", op.ID)
	}
	require.True(t, ids["dump"], "the one run-now/schedulable op")
	for _, internal := range []string{"verify", "safety_dump", "restore"} {
		require.False(t, ids[internal], "%q is a chain step, never independently launchable", internal)
	}
}

// ByID finds every operation — the validation path (Start, chain assembly)
// must resolve the internal steps even though the user can't launch them.
func TestByIDFindsInternalSteps(t *testing.T) {
	cases := []struct {
		id             string
		launchable     bool
		retentionClass string
	}{
		{"dump", true, "standard"},
		{"safety_dump", false, "safety"},
		{"verify", false, ""},
		{"restore", false, ""},
	}
	for _, tc := range cases {
		op, ok := catalog.ByID(tc.id)
		require.True(t, ok, "ByID must resolve %q", tc.id)
		require.Equal(t, tc.launchable, op.Launchable, tc.id)
		require.Equal(t, tc.retentionClass, op.RetentionClass,
			"%q stamps retention_class %q at registration (mini-ADR 2)", tc.id, tc.retentionClass)
	}

	_, ok := catalog.ByID("no-such-op")
	require.False(t, ok)
}
