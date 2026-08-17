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

// WU-018 (m1-gate item 8): exotic-but-valid size_gb forms import canonically —
// hex floats no longer abort the whole import at the ::numeric cast, and forms
// Postgres normalizes ('1e2' -> '100') no longer re-import as "updated"
// forever: the identical file is all-unchanged (SPEC-010 behavior 2).
func TestImportSizeGBFormsIdempotent(t *testing.T) {
	pool := testutil.MigratedDB(t)

	csv := goodHeader + "\n" +
		"a-1,c-1,dev,vm,16.3,1e2,team,\n" +
		"a-2,c-1,dev,vm,16.3,.5,team,\n" +
		"a-3,c-1,dev,vm,16.3,0120,team,\n" +
		"a-4,c-1,dev,vm,16.3,0x1p4,team,\n"

	rep := importCSV(t, pool, "sizes.csv", csv)
	require.Equal(t, "imported 4 new, updated 0, unchanged 0, quarantined 0", rep.String())

	rows, err := pool.Query(context.Background(),
		`SELECT name, size_gb::text FROM instance ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var name, size string
		require.NoError(t, rows.Scan(&name, &size))
		got[name] = size
	}
	require.NoError(t, rows.Err())
	require.Equal(t, map[string]string{
		"a-1": "100", "a-2": "0.5", "a-3": "120", "a-4": "16",
	}, got, "stored form must equal the parser's canonical form")

	rep = importCSV(t, pool, "sizes.csv", csv)
	require.Equal(t, "imported 0 new, updated 0, unchanged 4, quarantined 0", rep.String())
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

// SPEC-050 mini-ADR 5 (WU-051): the connection tuple round-trips through
// import, stays idempotent, updates in place under the natural key, and an
// old-format file NULLs it (the CSV is the source of truth for the whole row
// — never a stale address).
func TestImportConnectionTuple(t *testing.T) {
	pool := testutil.MigratedDB(t)
	connHeader := "instance_name,cluster_name,env,platform,pg_version,size_gb,owner,maintenance_window,host,port"

	readTuple := func() (host *string, port *int) {
		t.Helper()
		err := pool.QueryRow(context.Background(),
			`SELECT host, port FROM instance WHERE name = 'conn-test'`).Scan(&host, &port)
		require.NoError(t, err)
		return host, port
	}

	// Round-trip + idempotency.
	withTuple := connHeader + "\nconn-test,conn,test,vm,16.3,40,team,,10.0.0.5,5433\n"
	rep := importCSV(t, pool, "conn.csv", withTuple)
	require.Equal(t, 1, rep.New)
	host, port := readTuple()
	require.NotNil(t, host)
	require.Equal(t, "10.0.0.5", *host)
	require.NotNil(t, port)
	require.Equal(t, 5433, *port)

	rep = importCSV(t, pool, "conn.csv", withTuple)
	require.Equal(t, 1, rep.Unchanged, "tuple-bearing re-import is idempotent")

	// Tuple change updates in place.
	rep = importCSV(t, pool, "conn.csv",
		connHeader+"\nconn-test,conn,test,vm,16.3,40,team,,10.0.0.6,5433\n")
	require.Equal(t, 1, rep.Updated)
	host, _ = readTuple()
	require.Equal(t, "10.0.0.6", *host)

	// An old-format file imports fine — and NULLs the tuple (absent → NULL).
	oldFormat := "instance_name,cluster_name,env,platform,pg_version,size_gb,owner,maintenance_window\n" +
		"conn-test,conn,test,vm,16.3,40,team,\n"
	rep = importCSV(t, pool, "conn.csv", oldFormat)
	require.Equal(t, 1, rep.Updated, "dropping the tuple is a real change")
	host, port = readTuple()
	require.Nil(t, host)
	require.Nil(t, port)

	rep = importCSV(t, pool, "conn.csv", oldFormat)
	require.Equal(t, 1, rep.Unchanged, "old-format re-import is idempotent")
}

// The shipped 8-column fixture must import byte-untouched, exactly as before
// the tuple columns existed — every host/port NULL.
func TestImportFixtureHasNoTuple(t *testing.T) {
	pool := testutil.MigratedDB(t)
	importCSV(t, pool, "instances.csv", fixtureCSV(t))
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM instance WHERE host IS NOT NULL OR port IS NOT NULL`).Scan(&n))
	require.Zero(t, n)
}
