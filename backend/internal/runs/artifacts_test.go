package runs_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/runs"
)

// registryRow is the raw artifact table shape, fetched by origin run.
type registryRow struct {
	name           string
	sizeBytes      int64
	checksum       string
	retentionClass string
	location       *string
	createdAt      time.Time
}

func registryRowsFor(t *testing.T, pool *pgxpool.Pool, runID int64) []registryRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT name, size_bytes, checksum, retention_class, location, created_at
		FROM artifact WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()
	var out []registryRow
	for rows.Next() {
		var r registryRow
		require.NoError(t, rows.Scan(&r.name, &r.sizeBytes, &r.checksum,
			&r.retentionClass, &r.location, &r.createdAt))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

// SPEC-030 behavior 1: a successful dump registers exactly one artifact row,
// atomically carrying the same values as the run's own columns (mini-ADR 1),
// class 'standard', location dormant.
func TestArtifactRegisteredOnSuccess(t *testing.T) {
	svc, pool := newService(t)

	run, err := svc.Start(context.Background(), testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State)
	require.NotNil(t, final.Artifact)

	got := registryRowsFor(t, pool, run.ID)
	require.Len(t, got, 1, "exactly one registry row per successful dump")
	require.Equal(t, final.Artifact.Name, got[0].name, "registry mirrors the run columns (same tx)")
	require.Equal(t, final.Artifact.SizeBytes, got[0].sizeBytes)
	require.Equal(t, final.Artifact.Checksum, got[0].checksum)
	require.Equal(t, "standard", got[0].retentionClass, "WU-030 only ever writes 'standard'")
	require.Nil(t, got[0].location, "location stays dormant until WU-034/035")
	require.False(t, got[0].createdAt.IsZero())
}

// SPEC-030 behavior 2: non-success outcomes and artifact-less successes
// never register.
func TestArtifactNotRegisteredOnNonSuccess(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	failed, err := svc.Start(ctx, testReq("billing-test", "dump", "",
		map[string]string{"mock_fail_at": "1"}))
	require.NoError(t, err)
	waitTerminal(t, svc, failed.ID)
	require.Empty(t, registryRowsFor(t, pool, failed.ID), "failed runs never register")

	// A success finalized without an engine artifact (the shape of future
	// artifact-less ops, and of the sweep/refusal paths): nothing to register.
	svcSlow, poolSlow := newServiceWithDelay(t, 100*time.Millisecond)
	live, err := svcSlow.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	require.NoError(t, svcSlow.Finalize(ctx, live.ID, "success", ""))
	svcSlow.Wait()
	require.Empty(t, registryRowsFor(t, poolSlow, live.ID),
		"success without an engine artifact registers nothing")
}

// SPEC-030 behavior 3 + mini-ADR 3: exactly-once is structural twice over —
// a losing finalizer never reaches the INSERT (guarded terminal transition),
// and UNIQUE (run_id) makes a duplicate physically impossible.
func TestArtifactExactlyOnceOnDoubleFinalize(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	run, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	final := waitTerminal(t, svc, run.ID)
	require.Equal(t, "success", final.State)

	// Late losers of the watcher-vs-cancel-vs-sweep race, both flavors.
	require.NoError(t, svc.Finalize(ctx, run.ID, "failed", "late loser"))
	require.NoError(t, svc.Finalize(ctx, run.ID, "success", ""))
	require.Len(t, registryRowsFor(t, pool, run.ID), 1,
		"double finalize must not double register")

	// The schema backstop: even a bypassing writer cannot duplicate.
	_, err = pool.Exec(ctx, `
		INSERT INTO artifact (run_id, name, size_bytes, checksum)
		VALUES ($1, 'dupe', 1, 'x')`, run.ID)
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23505", pgErr.Code) // unique_violation on run_id
}

// SPEC-030 behavior 5 (service half) + mini-ADR 4: newest-first per
// instance, unknown instance is an error, empty is a non-nil empty list.
func TestListArtifacts(t *testing.T) {
	svc, _ := newService(t)
	ctx := context.Background()

	first, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, first.ID)
	second, err := svc.Start(ctx, testReq("billing-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, second.ID)
	other, err := svc.Start(ctx, testReq("crm-test", "dump", "", nil))
	require.NoError(t, err)
	waitTerminal(t, svc, other.ID)

	list, err := svc.ListArtifacts(ctx, "billing-test")
	require.NoError(t, err)
	require.Len(t, list, 2, "only the asked instance's artifacts")
	require.Equal(t, second.ID, list[0].RunID, "newest first")
	require.Equal(t, first.ID, list[1].RunID)
	require.Equal(t, "standard", list[0].RetentionClass)
	require.NotEmpty(t, list[0].Checksum)

	_, err = svc.ListArtifacts(ctx, "no-such-instance")
	require.ErrorIs(t, err, runs.ErrUnknownInstance,
		"unknown instance is 404 material, not an empty list (mini-ADR 4)")

	empty, err := svc.ListArtifacts(ctx, "billing-prod")
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty, "known instance with no dumps lists empty")
}
