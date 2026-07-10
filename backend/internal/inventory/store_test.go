package inventory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// fixtureStore imports the fixture into a scratch DB and returns a Store on it.
func fixtureStore(t *testing.T) *inventory.Store {
	t.Helper()
	pool := testutil.MigratedDB(t)
	importCSV(t, pool, "instances.csv", fixtureCSV(t))
	return inventory.NewStore(pool)
}

func TestListInstancesAll(t *testing.T) {
	s := fixtureStore(t)

	instances, err := s.ListInstances(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, instances, 8)

	// Ordered by name; joined cluster fields present.
	require.Equal(t, "analytics-dev", instances[0].Name)
	require.Equal(t, "analytics", instances[0].Cluster)
	require.Equal(t, "k8s_patroni", instances[0].Platform)
	require.Equal(t, "billing-prod", instances[1].Name)
	require.NotNil(t, instances[1].SizeGB)
	require.InDelta(t, 412, *instances[1].SizeGB, 0.001)
	require.NotNil(t, instances[1].MaintenanceWindow)
	require.Equal(t, "Sat 02:00-06:00", *instances[1].MaintenanceWindow)
}

func TestListInstancesEnvFilter(t *testing.T) {
	s := fixtureStore(t)

	for env, want := range map[string]int{"prod": 4, "test": 3, "dev": 1} {
		instances, err := s.ListInstances(context.Background(), env)
		require.NoError(t, err)
		require.Len(t, instances, want, "env %s", env)
		for _, in := range instances {
			require.Equal(t, env, in.Env)
		}
	}
}

func TestListInstancesEmptyDB(t *testing.T) {
	s := inventory.NewStore(testutil.MigratedDB(t))

	instances, err := s.ListInstances(context.Background(), "")
	require.NoError(t, err)
	require.NotNil(t, instances)
	require.Empty(t, instances)
}

func TestGetInstance(t *testing.T) {
	s := fixtureStore(t)

	in, err := s.GetInstance(context.Background(), "hr-test")
	require.NoError(t, err)
	require.Equal(t, "hr-test", in.Name)
	require.Equal(t, "hr", in.Cluster)
	require.Equal(t, "test", in.Env)
	require.Equal(t, "vm", in.Platform)
	require.Equal(t, "16.4", in.PGVersion)
	require.Nil(t, in.MaintenanceWindow, "empty CSV field must come back as NULL")
}

func TestGetInstanceNotFound(t *testing.T) {
	s := fixtureStore(t)

	_, err := s.GetInstance(context.Background(), "nope")
	require.ErrorIs(t, err, inventory.ErrNotFound)
}

// WU-011R: "Last backup" on the fleet views = finished_at of the newest
// SUCCESSFUL dump run. Failed/canceled runs and other operations don't count;
// instances the portal never dumped stay NULL (the UI renders "—").
func TestLastBackupAt(t *testing.T) {
	pool := testutil.MigratedDB(t)
	importCSV(t, pool, "instances.csv", fixtureCSV(t))
	s := inventory.NewStore(pool)

	seedRun := func(instance, op, state, finishedAt string) {
		t.Helper()
		_, err := pool.Exec(context.Background(), `
			INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state, finished_at)
			SELECT id, $2, env, 'nonprod', 'pg_dump', $3, $4::timestamptz
			FROM instance WHERE name = $1`,
			instance, op, state, finishedAt)
		require.NoError(t, err)
	}
	seedRun("billing-test", "dump", "success", "2026-07-09T10:00:00Z")
	seedRun("billing-test", "dump", "success", "2026-07-10T04:30:00Z") // newest success
	seedRun("billing-test", "dump", "failed", "2026-07-10T06:00:00Z")  // newer, but not a backup
	seedRun("billing-test", "dump", "canceled", "2026-07-10T07:00:00Z")
	seedRun("hr-test", "restore", "success", "2026-07-10T05:00:00Z") // not a dump

	in, err := s.GetInstance(context.Background(), "billing-test")
	require.NoError(t, err)
	require.NotNil(t, in.LastBackupAt)
	require.Equal(t, "2026-07-10T04:30:00Z", in.LastBackupAt.UTC().Format(time.RFC3339))

	list, err := s.ListInstances(context.Background(), "")
	require.NoError(t, err)
	byName := map[string]inventory.Instance{}
	for _, i := range list {
		byName[i.Name] = i
	}
	require.NotNil(t, byName["billing-test"].LastBackupAt)
	require.Nil(t, byName["hr-test"].LastBackupAt, "a restore is not a backup")
	require.Nil(t, byName["billing-prod"].LastBackupAt, "no runs at all")
}
