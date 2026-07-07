package inventory_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

const fixturePath = "../../../infra/fixtures/instances.csv"

func fixtureCSV(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(fixturePath)
	require.NoError(t, err)
	return string(b)
}

func importCSV(t *testing.T, pool *pgxpool.Pool, filename, csv string) inventory.Report {
	t.Helper()
	rep, err := inventory.Import(context.Background(), pool, filename, strings.NewReader(csv))
	require.NoError(t, err)
	return rep
}

func countRows(t *testing.T, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	require.NoError(t, err)
	return n
}

// SPEC-010 behavior 1: fixture imports clean — 8 instances, 6 auto-created
// clusters, a bookkeeping row with the counts.
func TestImportHappyPath(t *testing.T) {
	pool := testutil.MigratedDB(t)

	rep := importCSV(t, pool, "instances.csv", fixtureCSV(t))
	require.Equal(t, "imported 8 new, updated 0, unchanged 0, quarantined 0", rep.String())
	require.Equal(t, 8, rep.Total)

	require.Equal(t, 8, countRows(t, pool, "instance"))
	require.Equal(t, 6, countRows(t, pool, "cluster"))

	var total, newN, updated, unchanged, quarantined int
	err := pool.QueryRow(context.Background(), `
		SELECT rows_total, rows_new, rows_updated, rows_unchanged, rows_quarantined
		FROM inventory_import WHERE filename = 'instances.csv'`).
		Scan(&total, &newN, &updated, &unchanged, &quarantined)
	require.NoError(t, err)
	require.Equal(t, []int{8, 8, 0, 0, 0}, []int{total, newN, updated, unchanged, quarantined})
}

// SPEC-010 behavior 2: re-importing the same file changes nothing.
func TestImportIdempotent(t *testing.T) {
	pool := testutil.MigratedDB(t)
	csv := fixtureCSV(t)

	importCSV(t, pool, "instances.csv", csv)
	rep := importCSV(t, pool, "instances.csv", csv)

	require.Equal(t, "imported 0 new, updated 0, unchanged 8, quarantined 0", rep.String())
	require.Equal(t, 8, countRows(t, pool, "instance"))
	require.Equal(t, 6, countRows(t, pool, "cluster"))
}

// SPEC-010 behavior 3: same natural key with a changed value overwrites in
// place and bumps updated_at — no duplicate row.
func TestImportUpdate(t *testing.T) {
	pool := testutil.MigratedDB(t)
	csv := fixtureCSV(t)

	importCSV(t, pool, "instances.csv", csv)

	bumped := strings.Replace(csv,
		"billing-prod,billing,prod,k8s_patroni,16.3",
		"billing-prod,billing,prod,k8s_patroni,16.4", 1)
	require.NotEqual(t, csv, bumped, "fixture line moved — update the test")
	rep := importCSV(t, pool, "instances.csv", bumped)

	require.Equal(t, "imported 0 new, updated 1, unchanged 7, quarantined 0", rep.String())
	require.Equal(t, 8, countRows(t, pool, "instance"))

	var version string
	var createdAt, updatedAt time.Time
	err := pool.QueryRow(context.Background(),
		`SELECT pg_version, created_at, updated_at FROM instance WHERE name = 'billing-prod'`).
		Scan(&version, &createdAt, &updatedAt)
	require.NoError(t, err)
	require.Equal(t, "16.4", version)
	require.Greater(t, updatedAt, createdAt, "updated_at must move on a real change")
}

// SPEC-010 behaviors 4, 5, 7, 8: malformed rows are quarantined with all
// their reasons and the raw line verbatim; valid rows still import.
func TestImportQuarantine(t *testing.T) {
	pool := testutil.MigratedDB(t)

	b, err := os.ReadFile("testdata/malformed.csv")
	require.NoError(t, err)
	rep := importCSV(t, pool, "malformed.csv", string(b))

	require.Equal(t, "imported 2 new, updated 0, unchanged 0, quarantined 7", rep.String())
	require.Equal(t, 2, countRows(t, pool, "instance"))
	require.Equal(t, 1, countRows(t, pool, "cluster"))

	rows, err := pool.Query(context.Background(), `
		SELECT r.row_number, r.raw, r.reasons
		FROM inventory_import_reject r
		JOIN inventory_import i ON i.id = r.import_id
		WHERE i.filename = 'malformed.csv'
		ORDER BY r.row_number`)
	require.NoError(t, err)
	defer rows.Close()

	type reject struct {
		line    int
		raw     string
		reasons []string
	}
	var rejects []reject
	for rows.Next() {
		var r reject
		require.NoError(t, rows.Scan(&r.line, &r.raw, &r.reasons))
		rejects = append(rejects, r)
	}
	require.NoError(t, rows.Err())
	require.Len(t, rejects, 7)

	wantReason := map[int]string{ // file line -> reason substring
		3: "unknown env",
		4: "instance_name is required",
		5: "size_gb must be numeric",
		6: "wrong column count",
		7: "duplicate instance_name in file",
		8: "instance_name must match",
		9: "cluster platform conflict",
	}
	for _, r := range rejects {
		want, ok := wantReason[r.line]
		require.True(t, ok, "unexpected reject on line %d: %v", r.line, r.reasons)
		require.Contains(t, strings.Join(r.reasons, "; "), want)
		require.NotEmpty(t, r.raw)
	}
}

// SPEC-010 behavior 6: a file-level failure writes nothing at all.
func TestImportFileLevelFailureWritesNothing(t *testing.T) {
	pool := testutil.MigratedDB(t)

	bad := "cluster_name,instance_name,env,platform,pg_version,size_gb,owner,maintenance_window\n" +
		"billing,billing-prod,prod,k8s_patroni,16.3,412,team,\n"
	_, err := inventory.Import(context.Background(), pool, "bad.csv", strings.NewReader(bad))
	require.Error(t, err)

	require.Zero(t, countRows(t, pool, "inventory_import"))
	require.Zero(t, countRows(t, pool, "instance"))
	require.Zero(t, countRows(t, pool, "cluster"))
}

// SPEC-010 behavior 8 across imports: a row whose platform disagrees with a
// cluster created by an earlier import is quarantined, not applied.
func TestImportClusterConflictAcrossImports(t *testing.T) {
	pool := testutil.MigratedDB(t)

	importCSV(t, pool, "first.csv", goodHeader+"\n"+
		"shop-prod,shop,prod,k8s_patroni,16.2,280,shop-team,\n")
	rep := importCSV(t, pool, "second.csv", goodHeader+"\n"+
		"shop-test,shop,test,vm,16.2,20,shop-team,\n")

	require.Equal(t, "imported 0 new, updated 0, unchanged 0, quarantined 1", rep.String())
	require.Len(t, rep.Rejects, 1)
	require.Contains(t, rep.Rejects[0].Reasons[0], "cluster platform conflict")

	require.Equal(t, 1, countRows(t, pool, "instance"))
	var platform string
	err := pool.QueryRow(context.Background(),
		`SELECT platform FROM cluster WHERE name = 'shop'`).Scan(&platform)
	require.NoError(t, err)
	require.Equal(t, "k8s_patroni", platform, "existing cluster must be untouched")
}
