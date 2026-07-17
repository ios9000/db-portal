package maintenance_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/maintenance"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

// newMaint builds a maintenance.Service on a migrated scratch DB with the
// fixture imported (runs need an instance FK). Retention ages are left at the
// caller's discretion — tests dial them tight.
func newMaint(t *testing.T) (*maintenance.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	svc := maintenance.New(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, pool
}

func instanceID(t *testing.T, pool *pgxpool.Pool, name string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT id FROM instance WHERE name = $1`, name).Scan(&id))
	return id
}

// seedArtifact inserts a successful run, its immutable run.submitted audit row,
// and one registry artifact aged `age` in the past with the given class and
// (optional) object-store location. Returns the run id so tests can assert the
// artifact.reaped audit row that references it.
func seedArtifact(t *testing.T, pool *pgxpool.Pool, instance, class string, age time.Duration, location string) int64 {
	t.Helper()
	ctx := context.Background()
	iid := instanceID(t, pool, instance)

	var runID int64
	require.NoError(t, pool.QueryRow(ctx, `
		INSERT INTO run (instance_id, operation, environment, engine_class, playbook_tag, state)
		VALUES ($1, 'dump', 'test', 'nonprod', 'dump', 'success') RETURNING id`, iid).Scan(&runID))
	_, err := pool.Exec(ctx, `
		INSERT INTO audit_event (actor, action, run_id, instance_id, environment, playbook_tag, params_digest)
		VALUES ('dba-test', 'run.submitted', $1, $2, 'test', 'dump', 'digest')`, runID, iid)
	require.NoError(t, err)

	var loc *string
	if location != "" {
		loc = &location
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO artifact (run_id, name, size_bytes, checksum, retention_class, location, created_at)
		VALUES ($1, $2, 100, 'sum', $3, $4, now() - make_interval(secs => $5))`,
		runID, instance+"-artifact", class, loc, age.Seconds())
	require.NoError(t, err)
	return runID
}

func artifactCount(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM artifact`).Scan(&n))
	return n
}

// SPEC-044 B3: expired sessions are reaped each sweep; a live session
// (expires_at > now()) is never touched.
func TestReapExpiredSessions(t *testing.T) {
	svc, pool := newMaint(t)
	ctx := context.Background()

	// Two expired sessions, one live.
	_, err := pool.Exec(ctx, `
		INSERT INTO session (token_hash, username, display_name, expires_at) VALUES
			('dead1', 'a', 'A', now() - interval '1 hour'),
			('dead2', 'b', 'B', now() - interval '1 minute'),
			('live1', 'c', 'C', now() + interval '1 hour')`)
	require.NoError(t, err)

	r := svc.Sweep(ctx)
	require.Equal(t, int64(2), r.SessionsReaped)

	var remaining, liveKept int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM session`).Scan(&remaining))
	require.Equal(t, 1, remaining, "only the live session survives")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM session WHERE token_hash = 'live1'`).Scan(&liveKept))
	require.Equal(t, 1, liveKept, "a live session is never reaped")
}

// SPEC-044 B2: standard artifacts past retention are removed from the registry
// and an artifact.reaped audit row is written; safety artifacts are NEVER
// reaped; younger standard artifacts stay.
func TestReapStandardArtifactsPastRetention(t *testing.T) {
	svc, pool := newMaint(t)
	svc.ArtifactRetention = 24 * time.Hour
	ctx := context.Background()

	oldStd := seedArtifact(t, pool, "billing-test", "standard", 48*time.Hour, "")
	seedArtifact(t, pool, "crm-test", "standard", 1*time.Hour, "")   // young — kept
	seedArtifact(t, pool, "hr-test", "safety", 100*24*time.Hour, "") // ancient safety — kept

	r := svc.Sweep(ctx)
	require.Equal(t, int64(1), r.ArtifactsReaped, "only the aged standard artifact is reaped")
	require.Equal(t, 2, artifactCount(t, pool), "the young standard and the safety artifact survive")

	// The safety artifact is structurally untouchable.
	var safety int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM artifact WHERE retention_class = 'safety'`).Scan(&safety))
	require.Equal(t, 1, safety, "a safety artifact is never reaped, whatever its age")

	// The deletion is audited, linked to the reaped artifact's origin run.
	var reapActor string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT actor FROM audit_event
		WHERE action = 'artifact.reaped' AND run_id = $1`, oldStd).Scan(&reapActor))
	require.Equal(t, "maintenance", reapActor)
}

// SPEC-044 mini-ADR 5: a non-positive retention DISABLES the artifact sweep — a
// zero age must never be read as "reap everything".
func TestArtifactRetentionDisabledWhenNonPositive(t *testing.T) {
	svc, pool := newMaint(t)
	svc.ArtifactRetention = 0
	ctx := context.Background()

	seedArtifact(t, pool, "billing-test", "standard", 1000*time.Hour, "")
	r := svc.Sweep(ctx)
	require.Equal(t, int64(0), r.ArtifactsReaped, "a disabled sweep reaps nothing")
	require.Equal(t, 1, artifactCount(t, pool))
}

// SPEC-044 mini-ADR 3: a reaped standard artifact that still carried an
// object-store location is counted + logged (bytes the store's lifecycle must
// reclaim), never silently orphaned.
func TestArtifactReapCountsOrphanedObjects(t *testing.T) {
	svc, pool := newMaint(t)
	svc.ArtifactRetention = 24 * time.Hour
	ctx := context.Background()

	seedArtifact(t, pool, "billing-test", "standard", 48*time.Hour, "s3://dbportal-artifacts/old.dump")
	r := svc.Sweep(ctx)
	require.Equal(t, int64(1), r.ArtifactsReaped)
	require.Equal(t, int64(1), r.OrphanedObjects, "the non-NULL location is surfaced")
}

// SPEC-044 B1: the audit pass reports age but mutates NOTHING — the append-only
// ledgers are respected. Row counts are unchanged and a direct DELETE still
// raises (the 0003 trigger holds).
func TestObserveAuditAgeNoMutation(t *testing.T) {
	svc, pool := newMaint(t)
	ctx := context.Background()

	// Seed audit history (each seedArtifact writes a run.submitted row).
	seedArtifact(t, pool, "billing-test", "standard", 72*time.Hour, "")
	seedArtifact(t, pool, "crm-test", "standard", 1*time.Hour, "")

	var before int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM audit_event`).Scan(&before))
	require.Positive(t, before)

	r := svc.Sweep(ctx)
	require.Positive(t, r.OldestAuditAge, "the oldest event's age is reported")

	// The audit reap of the aged standard artifact ADDS one artifact.reaped row;
	// nothing is ever deleted or updated. Assert the submitted rows all survive.
	var submitted int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit_event WHERE action = 'run.submitted'`).Scan(&submitted))
	require.Equal(t, 2, submitted, "no audit row is ever mutated or deleted")

	// The trigger still forbids deletion — audit integrity is intact.
	_, err := pool.Exec(ctx, `DELETE FROM audit_event`)
	require.Error(t, err, "audit_event is append-only — the sweep never bypasses the trigger")
}

// SPEC-044 mini-ADR 1 + 5: Run sweeps once at boot; a non-positive interval
// disables the loop and returns immediately.
func TestRunBootSweepsThenRespectsCancel(t *testing.T) {
	svc, pool := newMaint(t)
	svc.Interval = time.Hour // long — only the boot sweep runs in this test
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO session (token_hash, username, display_name, expires_at)
		VALUES ('dead', 'a', 'A', now() - interval '1 hour')`)
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { svc.Run(runCtx); close(done) }()

	require.Eventually(t, func() bool {
		var n int
		require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM session`).Scan(&n))
		return n == 0
	}, 3*time.Second, 5*time.Millisecond, "the boot sweep reaps the expired session")

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRunDisabledReturnsImmediately(t *testing.T) {
	svc, _ := newMaint(t)
	svc.Interval = 0

	done := make(chan struct{})
	go func() { svc.Run(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a disabled maintenance loop must return immediately")
	}
}
