package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var goldenTarget = InventoryHost{
	Name: "billing-test", Host: "10.0.0.5", Port: 5433,
	Env: "test", Platform: "vm", Cluster: "billing",
}

// goldenTargetJSON is the EXACT per-job inventory for goldenTarget (SPEC-050
// mini-ADR 5, WU-051 AC): one `target` group, one host, ansible_host +
// dbportal_* facts, keys in json.Marshal's sorted order. The adapter-level
// test asserts the on-disk file matches this byte-for-byte.
const goldenTargetJSON = `{
  "target": {
    "hosts": {
      "billing-test": {
        "ansible_host": "10.0.0.5",
        "dbportal_cluster": "billing",
        "dbportal_env": "test",
        "dbportal_instance": "billing-test",
        "dbportal_platform": "vm",
        "dbportal_port": 5433
      }
    }
  }
}
`

func TestRenderInventoryGolden(t *testing.T) {
	b, err := renderInventory(goldenTarget, nil)
	require.NoError(t, err)
	require.Equal(t, goldenTargetJSON, string(b))
}

func TestRenderInventoryPortDefaults(t *testing.T) {
	h := goldenTarget
	h.Port = 0 // unrecorded → the libpq default, never a silent zero
	b, err := renderInventory(h, nil)
	require.NoError(t, err)
	require.Contains(t, string(b), `"dbportal_port": 5432`)
}

// The cluster group renders only when members are passed (a manifest declares
// that from WU-053) — and it never smuggles extra groups in.
func TestRenderInventoryClusterGroup(t *testing.T) {
	peer := InventoryHost{
		Name: "billing-test-2", Host: "10.0.0.6", Port: 5433,
		Env: "test", Platform: "vm", Cluster: "billing",
	}
	b, err := renderInventory(goldenTarget, []InventoryHost{goldenTarget, peer})
	require.NoError(t, err)
	require.Contains(t, string(b), `"cluster"`)
	require.Contains(t, string(b), `"billing-test-2"`)
	require.Contains(t, string(b), `"10.0.0.6"`)
}
