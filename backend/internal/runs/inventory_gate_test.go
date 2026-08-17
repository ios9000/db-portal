package runs_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// SPEC-050 mini-ADR 5 (WU-051 AC): a launch whose target has no recorded
// connection tuple finalizes FAILED with the clear message, through the REAL
// local adapter wired to the REAL inventory store — the fixture carries no
// host,port, and StartJob fails closed before ansible-playbook could ever
// spawn (which is why no stub binary is needed here). The audit trail stays
// intact: run.submitted AND run.finished/failed.
func TestStartMissingConnectionInfoFinalizesFailed(t *testing.T) {
	pool := testutil.MigratedDB(t)
	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	lib := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(lib, "dump.yml"), []byte("# never runs\n"), 0o600))
	adapter, err := engine.NewLocalAdapter(engine.LocalConfig{
		AnsibleBin: "sh", // resolvable, never spawned: StartJob fails closed first
		Library:    lib,
		Workdir:    t.TempDir(),
		Inventory:  inventory.NewStore(pool),
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	require.NoError(t, err)

	reg := engine.NewRegistry()
	reg.Register(engine.ClassNonProd, adapter)
	svc := runs.NewService(pool, reg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.ErrorIs(t, err, runs.ErrEngine)
	require.Equal(t, "failed", run.State)
	require.NotNil(t, run.Error)
	require.Contains(t, *run.Error, `instance "billing-test" has no connection info`)
	require.Contains(t, *run.Error, "re-import the inventory with host,port")

	events := auditEvents(t, pool, run.ID)
	require.Len(t, events, 2)
	require.Equal(t, "run.submitted", events[0].action)
	require.Equal(t, "run.finished", events[1].action)
	require.Equal(t, "failed", *events[1].finalStatus)
}
