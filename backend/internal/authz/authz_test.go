package authz_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/authz"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

func newStore(t *testing.T) (*authz.Store, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.MigratedDB(t)
	return authz.NewStore(pool, slog.New(slog.NewTextHandler(io.Discard, nil))), pool
}

func TestGrantAndHasRole(t *testing.T) {
	ctx := context.Background()
	store, _ := newStore(t)

	held, err := store.HasRole(ctx, "dana", authz.RoleDBA)
	require.NoError(t, err)
	require.False(t, held, "ungranted user must not hold the role")

	require.NoError(t, store.Grant(ctx, authz.RoleDBA, "dana", "devon"))
	// Idempotent: dev/demo boot re-grants on every start (SPEC-021 mini-ADR 8).
	require.NoError(t, store.Grant(ctx, authz.RoleDBA, "dana"))

	for _, u := range []string{"dana", "devon"} {
		held, err = store.HasRole(ctx, u, authz.RoleDBA)
		require.NoError(t, err)
		require.True(t, held, u)
	}
}

func TestGrantUnknownRoleIsAnError(t *testing.T) {
	store, _ := newStore(t)
	require.Error(t, store.Grant(context.Background(), "warlock", "dana"),
		"a silent no-op grant would read as success")
}

// SPEC-021 mini-ADR 1: the migration seeds the one standing grant —
// break-glass must be able to act the moment it can log in.
func TestBreakGlassIsSeededDBA(t *testing.T) {
	store, _ := newStore(t)
	held, err := store.HasRole(context.Background(), "break-glass", authz.RoleDBA)
	require.NoError(t, err)
	require.True(t, held)
}

// SPEC-021 behavior 1 + mini-ADR 3: a denial answers ErrDenied AND lands on
// the auth trail; a held role answers nil and writes nothing.
func TestRequireRecordsDenials(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)

	err := store.Require(ctx, "intruder", authz.RoleDBA, "10.0.0.9:1234", "POST /api/runs")
	require.ErrorIs(t, err, authz.ErrDenied)

	var actor, remote, detail string
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT actor, remote, COALESCE(detail, '') FROM auth_event
		WHERE action = 'authz.denied'`).Scan(&actor, &remote, &detail))
	require.Equal(t, "intruder", actor)
	require.Equal(t, "10.0.0.9:1234", remote)
	require.Equal(t, "POST /api/runs", detail)

	require.NoError(t, store.Grant(ctx, authz.RoleDBA, "dana"))
	require.NoError(t, store.Require(ctx, "dana", authz.RoleDBA, "", "POST /api/runs"))

	var denials int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM auth_event WHERE action = 'authz.denied'`).Scan(&denials))
	require.Equal(t, 1, denials, "a held role must not write a denial")
}

// Denial rows inherit auth_event's append-only enforcement (0005 triggers).
func TestDenialRowsAreImmutable(t *testing.T) {
	ctx := context.Background()
	store, pool := newStore(t)

	require.ErrorIs(t,
		store.Require(ctx, "intruder", authz.RoleDBA, "", "POST /api/runs"), authz.ErrDenied)
	_, err := pool.Exec(ctx, `UPDATE auth_event SET actor = 'nobody' WHERE action = 'authz.denied'`)
	require.ErrorContains(t, err, "append-only")
}
