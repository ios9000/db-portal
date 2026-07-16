package schedule_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

const testOwner = "dba-sched"

// newStore builds a Service on a migrated scratch DB with the fixture
// imported and no starter — store tests never fire.
func newStore(t *testing.T) (*schedule.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)

	f, err := os.Open("../../../infra/fixtures/instances.csv")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	_, err = inventory.Import(context.Background(), pool, "instances.csv", f)
	require.NoError(t, err)

	svc := schedule.New(pool, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Jitter = 0
	return svc, pool
}

func createReq(instance, spec string) schedule.CreateRequest {
	return schedule.CreateRequest{
		Instance: instance, Operation: "dump", CronSpec: spec,
		Reason: "nightly", Confirm: instance, CreatedBy: testOwner,
	}
}

// SPEC-022 behavior 1: creation validates everything before writing, and a
// valid ask lands with its first fire computed.
func TestCreateValidation(t *testing.T) {
	svc, pool := newStore(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, schedule.CreateRequest{
		Instance: "billing-test", Operation: "dump", CronSpec: "not cron", CreatedBy: testOwner,
	})
	require.ErrorIs(t, err, schedule.ErrBadSpec)

	_, err = svc.Create(ctx, schedule.CreateRequest{
		Instance: "billing-test", Operation: "explode", CronSpec: "* * * * *", CreatedBy: testOwner,
	})
	require.ErrorIs(t, err, runs.ErrUnknownOperation)

	// WU-038: an EXISTING but non-launchable op (a restore-chain step) is not
	// schedulable — it looks like an unknown operation, same as the button
	// path (restore.md behavior 5). Existence alone would have written a
	// schedule that errors on every tick with no run and no mail.
	for _, op := range []string{"restore", "verify", "safety_dump"} {
		_, err = svc.Create(ctx, schedule.CreateRequest{
			Instance: "billing-test", Operation: op, CronSpec: "* * * * *", CreatedBy: testOwner,
		})
		require.ErrorIs(t, err, runs.ErrUnknownOperation, "operation=%q", op)
	}

	_, err = svc.Create(ctx, createReq("nope-db", "* * * * *"))
	require.ErrorIs(t, err, runs.ErrUnknownInstance)

	// The prod ritual, once, at creation (mini-ADR 8) — same predicate as
	// launch: exact, case-sensitive instance name.
	for _, confirm := range []string{"", "BILLING-PROD", "billing-pro"} {
		_, err = svc.Create(ctx, schedule.CreateRequest{
			Instance: "billing-prod", Operation: "dump", CronSpec: "@daily",
			Confirm: confirm, CreatedBy: testOwner,
		})
		require.ErrorIs(t, err, runs.ErrProdUnconfirmed, "confirm=%q", confirm)
	}
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM schedule`).Scan(&count))
	require.Zero(t, count, "no failed create may leave a row")

	before := time.Now()
	sc, err := svc.Create(ctx, createReq("billing-prod", "* * * * *"))
	require.NoError(t, err)
	require.Equal(t, "billing-prod", sc.Instance)
	require.Equal(t, "prod", sc.Env)
	require.True(t, sc.Enabled)
	require.Equal(t, testOwner, sc.CreatedBy)
	require.NotNil(t, sc.Reason)
	require.Equal(t, "nightly", *sc.Reason)
	require.NotNil(t, sc.NextFireAt, "enabled schedules always know their next fire")
	require.True(t, sc.NextFireAt.After(before), "next fire lies in the future")
	require.True(t, sc.NextFireAt.Before(before.Add(61*time.Second)),
		"an every-minute spec with zero jitter fires within the minute")
	require.Nil(t, sc.LastFiredAt)
	require.Nil(t, sc.LastRunID)
	require.Nil(t, sc.LastFireStatus)
}

// SPEC-022 mini-ADR 6: jitter is bounded and baked into the computed fire
// time — (cron next, cron next + jitter], exact when jitter is zero.
func TestNextFireJitterBounds(t *testing.T) {
	svc, _ := newStore(t)
	spec, err := cron.ParseStandard("30 2 * * *")
	require.NoError(t, err)
	from := time.Date(2026, 7, 10, 12, 0, 0, 0, time.Local)
	base := spec.Next(from)

	require.Equal(t, base, svc.NextFire(spec, from), "zero jitter means exact cron times")

	svc.Jitter = time.Hour
	for range 100 {
		got := svc.NextFire(spec, from)
		require.False(t, got.Before(base), "jitter never fires early")
		require.False(t, got.After(base.Add(time.Hour)), "jitter stays within its bound")
	}
}

func TestListNewestFirst(t *testing.T) {
	svc, _ := newStore(t)
	ctx := context.Background()

	first, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	second, err := svc.Create(ctx, createReq("crm-test", "@hourly"))
	require.NoError(t, err)

	list, err := svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Equal(t, second.ID, list[0].ID)
	require.Equal(t, first.ID, list[1].ID)
	require.Equal(t, "crm-test", list[0].Instance)
	require.Equal(t, "test", list[0].Env)
}

// SPEC-022 behavior 5 (store half): disabling freezes, re-enabling computes
// from now — the disabled gap is never made up.
func TestSetEnabled(t *testing.T) {
	svc, _ := newStore(t)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "* * * * *"))
	require.NoError(t, err)

	off, err := svc.SetEnabled(ctx, sc.ID, false)
	require.NoError(t, err)
	require.False(t, off.Enabled)
	require.Nil(t, off.NextFireAt, "disabled schedules have no next fire")

	before := time.Now()
	on, err := svc.SetEnabled(ctx, sc.ID, true)
	require.NoError(t, err)
	require.True(t, on.Enabled)
	require.NotNil(t, on.NextFireAt)
	require.True(t, on.NextFireAt.After(before), "re-enable computes from now, not the gap")

	_, err = svc.SetEnabled(ctx, 99999, false)
	require.ErrorIs(t, err, schedule.ErrNotFound)
}

func TestDelete(t *testing.T) {
	svc, pool := newStore(t)
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, sc.ID))
	list, err := svc.List(ctx)
	require.NoError(t, err)
	require.Empty(t, list)
	require.ErrorIs(t, svc.Delete(ctx, sc.ID), schedule.ErrNotFound)

	// SPEC-022 behavior 9: a schedule cannot outlive its instance.
	sc, err = svc.Create(ctx, createReq("crm-test", "@daily"))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM instance WHERE name = 'crm-test'`)
	require.NoError(t, err)
	_, err = svc.List(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, svc.Delete(ctx, sc.ID), schedule.ErrNotFound,
		"instance deletion cascades to its schedules")
}

// M2-gate finding 8: a redundant toggle is a true no-op — no jitter
// re-roll, no next_fire_at drift.
func TestRedundantToggleIsNoOp(t *testing.T) {
	svc, _ := newStore(t)
	svc.Jitter = time.Hour // any recompute would (almost surely) move the time
	ctx := context.Background()

	sc, err := svc.Create(ctx, createReq("billing-test", "@daily"))
	require.NoError(t, err)

	again, err := svc.SetEnabled(ctx, sc.ID, true)
	require.NoError(t, err)
	require.Equal(t, *sc.NextFireAt, *again.NextFireAt,
		"enabling an enabled schedule must not touch next_fire_at")

	off, err := svc.SetEnabled(ctx, sc.ID, false)
	require.NoError(t, err)
	offAgain, err := svc.SetEnabled(ctx, sc.ID, false)
	require.NoError(t, err)
	require.Equal(t, off.Enabled, offAgain.Enabled)
	require.Nil(t, offAgain.NextFireAt)
}
