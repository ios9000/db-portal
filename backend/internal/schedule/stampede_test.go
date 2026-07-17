package schedule_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/schedule"
)

// SPEC-043 B4 (mini-ADR 5): the scheduler boot-stampede. After long downtime
// many schedules come due at once; `fireDue` fires them SEQUENTIALLY (one
// bounded fire at a time), so a stampede is naturally rate-limited rather than
// a thundering herd of parallel Starts. This proves it: seed a realistic
// estate, make M distinct-instance schedules all due, run ONE fireDue, and
// assert all M fire and every run settles with zero orphans. The verdict —
// no spreading needed for the pilot — is recorded in SPEC-043 F2.
func TestLoadSchedulerStampede(t *testing.T) {
	const m = 25
	svc, runSvc, pool := newExecutor(t, 10*time.Millisecond)
	ctx := context.Background()

	// A realistic estate on top of the fixture (WU-041), so there are enough
	// distinct non-prod instances to give every schedule its own target.
	csv := inventory.GenerateEstate(80, 43)
	_, err := inventory.Import(ctx, pool, "estate.csv", strings.NewReader(csv))
	require.NoError(t, err)

	targets := nonProdInstances(t, pool, m)
	ids := make([]int64, m)
	for i, inst := range targets {
		sc, err := svc.Create(ctx, schedule.CreateRequest{
			Instance: inst, Operation: "dump", CronSpec: "@daily",
			Reason: "stampede", Confirm: inst, CreatedBy: testOwner,
		})
		require.NoError(t, err)
		ids[i] = sc.ID
		backdate(t, pool, sc.ID, 6) // all due, as after 6h of downtime
	}

	// ONE tick catches every due schedule up, coalesced (mini-ADR 2).
	tickStart := time.Now()
	svc.FireDue(ctx)
	tickDur := time.Since(tickStart)

	// Every distinct-instance schedule fired — none skipped (no overlap) — and
	// points at a real run.
	fired := 0
	for _, id := range ids {
		sc := getSchedule(t, svc, id)
		require.NotNil(t, sc.LastFireStatus, "schedule %d recorded a fire", id)
		require.Equal(t, "fired", *sc.LastFireStatus,
			"distinct-instance schedule %d fires, not skipped", id)
		require.NotNil(t, sc.LastRunID)
		waitTerminal(t, runSvc, *sc.LastRunID)
		fired++
	}
	require.Equal(t, m, fired)

	require.Equal(t, m, runCount(t, pool), "one run per due schedule — no double fire")

	runSvc.Wait()
	var orphans int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM run WHERE state IN ('queued', 'running')`).Scan(&orphans))
	require.Zero(t, orphans, "the stampede settles with no orphaned runs")

	// The lock releases everywhere (WU-042) — a stampede leaks nothing.
	var locks int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM instance_lock`).Scan(&locks))
	require.Zero(t, locks)

	t.Logf("B4: %d coalesced catch-up fires in one sequential tick took %s",
		m, tickDur.Round(time.Millisecond))
}

// nonProdInstances returns n distinct non-prod instance names — dump targets
// that never hit the prod ritual, drawn from the seeded estate.
func nonProdInstances(t *testing.T, pool *pgxpool.Pool, n int) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT name FROM instance WHERE env <> 'prod' ORDER BY name LIMIT $1`, n)
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	require.Len(t, names, n, "the estate holds at least n non-prod instances")
	return names
}
