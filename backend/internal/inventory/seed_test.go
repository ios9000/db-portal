package inventory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// GenerateEstate is a pure function of (n, seed): identical arguments yield
// byte-identical output, a different seed yields a different estate
// (SPEC-041 behavior 1).
func TestGenerateEstateDeterministic(t *testing.T) {
	a := inventory.GenerateEstate(500, 41)
	require.Equal(t, a, inventory.GenerateEstate(500, 41), "same (n, seed) is byte-identical")
	require.NotEqual(t, a, inventory.GenerateEstate(500, 42), "a different seed changes the estate")
}

// Every generated row is accepted by the SPEC-010 parser: zero rejects,
// exactly n rows — the generator IS the import contract (SPEC-041 behavior 2).
func TestGenerateEstateParsesClean(t *testing.T) {
	res, err := inventory.Parse(strings.NewReader(inventory.GenerateEstate(500, 41)))
	require.NoError(t, err)
	require.Empty(t, res.Rejects, "a clean estate quarantines nothing")
	require.Equal(t, 500, res.Total)
	require.Len(t, res.Rows, 500)
}

// The estate has the realistic shape the load test needs: all three envs, a
// non-empty prod minority dominated by non-prod, ≥5 clusters each on one
// platform, and every prod instance carrying a window (SPEC-041 behaviors 4-6).
func TestGenerateEstateDistribution(t *testing.T) {
	res, err := inventory.Parse(strings.NewReader(inventory.GenerateEstate(500, 41)))
	require.NoError(t, err)

	envCount := map[string]int{}
	clusterPlatform := map[string]string{}
	for _, r := range res.Rows {
		envCount[r.Env]++
		if p, ok := clusterPlatform[r.ClusterName]; ok {
			require.Equal(t, p, r.Platform, "cluster %q must stay on one platform", r.ClusterName)
		} else {
			clusterPlatform[r.ClusterName] = r.Platform
		}
		if r.Env == "prod" {
			require.NotNil(t, r.MaintenanceWindow, "prod instance %q carries a window", r.InstanceName)
		}
	}

	require.Positive(t, envCount["prod"], "prod slice is non-empty (ritual/window coverage)")
	require.Positive(t, envCount["test"])
	require.Positive(t, envCount["dev"])
	require.Greater(t, envCount["dev"]+envCount["test"], envCount["prod"], "non-prod dominates")
	require.GreaterOrEqual(t, len(clusterPlatform), 5, "at least 5 distinct clusters")
}

// The seed rides the real Import path, so re-seeding the same estate is a
// no-op: the natural key makes every row unchanged the second time
// (SPEC-041 behavior 3). Skips without the dev Postgres.
func TestSeedImportIdempotent(t *testing.T) {
	pool := testutil.MigratedDB(t)
	csv := inventory.GenerateEstate(500, 41)

	first := importCSV(t, pool, "seed", csv)
	require.Equal(t, 500, first.New)
	require.Zero(t, first.Quarantined)
	require.Equal(t, 500, countRows(t, pool, "instance"))
	require.GreaterOrEqual(t, countRows(t, pool, "cluster"), 5)

	var prod int
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT count(*) FROM instance WHERE env = 'prod'").Scan(&prod))
	require.Positive(t, prod, "a prod slice loaded for ritual/load coverage")

	second := importCSV(t, pool, "seed", csv)
	require.Zero(t, second.New, "re-seeding the same estate adds no rows")
	require.Equal(t, 500, second.Unchanged)
	require.Equal(t, 500, countRows(t, pool, "instance"), "no duplicates")
}
