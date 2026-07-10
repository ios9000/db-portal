package authn_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/ios9000/db-portal/backend/internal/authn"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

func newService(t *testing.T, opts ...func(*svcConfig)) (*authn.Service, *pgxpool.Pool) {
	t.Helper()
	cfg := svcConfig{dir: authn.DevDirectory(), ttl: time.Hour}
	for _, o := range opts {
		o(&cfg)
	}
	pool := testutil.MigratedDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := authn.NewService(pool, cfg.dir, log, cfg.ttl, cfg.breakglass)
	svc.SetFailDelay(time.Millisecond)
	return svc, pool
}

type svcConfig struct {
	dir        authn.Directory
	ttl        time.Duration
	breakglass string
}

func withTTL(d time.Duration) func(*svcConfig) { return func(c *svcConfig) { c.ttl = d } }
func withBreakglass(hash string) func(*svcConfig) {
	return func(c *svcConfig) { c.breakglass = hash }
}

func withDirectory(d authn.Directory) func(*svcConfig) {
	return func(c *svcConfig) { c.dir = d }
}

// downDirectory refuses every bind — AD is unreachable.
type downDirectory struct{}

func (downDirectory) Bind(context.Context, string, string) (authn.Identity, error) {
	return authn.Identity{}, authn.ErrBadCredentials
}

func authEvents(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT actor || '/' || action FROM auth_event ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		require.NoError(t, rows.Scan(&s))
		out = append(out, s)
	}
	require.NoError(t, rows.Err())
	return out
}

// SPEC-020 behavior 2: good creds → session works, auth.login row written.
func TestLoginValidateRoundTrip(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	sess, err := svc.Login(ctx, "dba1", "dba1", "10.0.0.1:1234")
	require.NoError(t, err)
	require.NotEmpty(t, sess.Token)
	require.Equal(t, "Dana Baker", sess.Identity.DisplayName)

	id, err := svc.Validate(ctx, sess.Token)
	require.NoError(t, err)
	require.Equal(t, "dba1", id.Username)

	require.Equal(t, []string{"dba1/auth.login"}, authEvents(t, pool))
}

// Ground rule: the DB stores sha256(token), never the token itself.
func TestSessionStoresHashNotToken(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	sess, err := svc.Login(ctx, "dba1", "dba1", "")
	require.NoError(t, err)

	var stored string
	require.NoError(t, pool.QueryRow(ctx, `SELECT token_hash FROM session`).Scan(&stored))
	require.NotEqual(t, sess.Token, stored)
	want := sha256.Sum256([]byte(sess.Token))
	require.Equal(t, hex.EncodeToString(want[:]), stored)
}

// SPEC-020 behavior 3: failures are uniform and land in the trail —
// username + remote, never the password.
func TestLoginFailureUniformAndAudited(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	for _, tc := range []struct{ user, pass string }{
		{"dba1", "wrong"},
		{"nobody", "whatever"},
		{"dba1", ""}, // empty password must never authenticate
	} {
		_, err := svc.Login(ctx, tc.user, tc.pass, "10.0.0.9:1")
		require.ErrorIs(t, err, authn.ErrBadCredentials, "user=%s", tc.user)
	}

	events := authEvents(t, pool)
	require.Len(t, events, 3)
	for _, e := range events {
		require.Contains(t, e, "auth.login_failed")
	}
	var leaked int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM auth_event
		WHERE detail LIKE '%wrong%' OR detail LIKE '%whatever%'`).Scan(&leaked))
	require.Zero(t, leaked, "passwords must never reach the auth trail")
}

// SPEC-020 behavior 4: logout revokes the row, not just the cookie.
func TestLogoutRevokesServerSide(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	sess, err := svc.Login(ctx, "dba1", "dba1", "")
	require.NoError(t, err)
	require.NoError(t, svc.Logout(ctx, sess.Token))

	_, err = svc.Validate(ctx, sess.Token)
	require.ErrorIs(t, err, authn.ErrNoSession)
	require.Equal(t, []string{"dba1/auth.login", "dba1/auth.logout"}, authEvents(t, pool))

	// Idempotent: a second logout with the dead token is a quiet no-op.
	require.NoError(t, svc.Logout(ctx, sess.Token))
	require.Len(t, authEvents(t, pool), 2)
}

// SPEC-020 behavior 5: expiry is enforced and the dead row lazily deleted.
func TestValidateExpiredSession(t *testing.T) {
	svc, pool := newService(t, withTTL(-time.Second)) // born expired
	ctx := context.Background()

	sess, err := svc.Login(ctx, "dba1", "dba1", "")
	require.NoError(t, err)

	_, err = svc.Validate(ctx, sess.Token)
	require.ErrorIs(t, err, authn.ErrNoSession)

	var rows int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM session`).Scan(&rows))
	require.Zero(t, rows, "expired row must be lazily deleted")
}

// SPEC-020 behavior 6: break-glass works while the directory is down and
// is alarmed; an empty hash disables the account entirely.
func TestBreakGlass(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("open-sesame"), bcrypt.MinCost)
	require.NoError(t, err)
	svc, pool := newService(t, withDirectory(downDirectory{}), withBreakglass(string(hash)))
	ctx := context.Background()

	// The directory refuses everyone…
	_, err = svc.Login(ctx, "dba1", "dba1", "")
	require.ErrorIs(t, err, authn.ErrBadCredentials)

	// …but break-glass still opens, alarmed.
	sess, err := svc.Login(ctx, "break-glass", "open-sesame", "10.0.0.2:9")
	require.NoError(t, err)
	id, err := svc.Validate(ctx, sess.Token)
	require.NoError(t, err)
	require.Equal(t, "break-glass", id.Username)
	require.Contains(t, authEvents(t, pool), "break-glass/auth.break_glass")

	// Wrong password still fails.
	_, err = svc.Login(ctx, "break-glass", "nope", "")
	require.ErrorIs(t, err, authn.ErrBadCredentials)
}

func TestBreakGlassDisabledByDefault(t *testing.T) {
	svc, _ := newService(t) // no hash configured
	_, err := svc.Login(context.Background(), "break-glass", "anything", "")
	require.ErrorIs(t, err, authn.ErrBadCredentials)
}

// SPEC-020 behavior 8: the auth trail is append-only, same enforcement as
// audit_event (UPDATE / DELETE / TRUNCATE all raise).
func TestAuthEventIsAppendOnly(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()
	_, err := svc.Login(ctx, "dba1", "dba1", "")
	require.NoError(t, err)

	for _, stmt := range []string{
		`UPDATE auth_event SET actor = 'tampered'`,
		`DELETE FROM auth_event`,
		`TRUNCATE auth_event`,
	} {
		_, err := pool.Exec(ctx, stmt)
		require.ErrorContains(t, err, "append-only", stmt)
	}
}
