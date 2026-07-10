package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// breakGlassUser is the one local account that works when AD is down
// (SPEC-020 mini-ADR 5). It exists only if PORTAL_BREAKGLASS_HASH is set,
// and every successful use is alarmed: auth.break_glass row + error log.
const breakGlassUser = "break-glass"

// Session is a freshly minted login: the opaque token goes into the
// cookie and is never stored — the DB holds sha256(token) only.
type Session struct {
	Token     string
	Identity  Identity
	ExpiresAt time.Time
}

// Service owns login (directory bind + break-glass), session validation
// and the append-only auth_event trail. It is the only thing the HTTP
// layer talks to (server.Authenticator seam).
type Service struct {
	pool           *pgxpool.Pool
	dir            Directory
	log            *slog.Logger
	ttl            time.Duration
	breakglassHash string
	failDelay      time.Duration // blunts online guessing; tests shrink it
}

func NewService(pool *pgxpool.Pool, dir Directory, log *slog.Logger, ttl time.Duration, breakglassHash string) *Service {
	return &Service{
		pool:           pool,
		dir:            dir,
		log:            log,
		ttl:            ttl,
		breakglassHash: breakglassHash,
		failDelay:      300 * time.Millisecond,
	}
}

// Login authenticates and mints a session. Failures are uniform
// (ErrBadCredentials) and delayed; the attempted username and remote land
// in auth_event — the password never does, anywhere.
func (s *Service) Login(ctx context.Context, username, password, remote string) (Session, error) {
	id, action, err := s.authenticate(ctx, username, password)
	if err != nil {
		s.event(ctx, username, "auth.login_failed", remote, "bad credentials")
		time.Sleep(s.failDelay)
		return Session{}, ErrBadCredentials
	}

	token, hash, err := newToken()
	if err != nil {
		return Session{}, fmt.Errorf("authn: mint token: %w", err)
	}
	expires := time.Now().Add(s.ttl)
	_, err = s.pool.Exec(ctx, `
		INSERT INTO session (token_hash, username, display_name, expires_at)
		VALUES ($1, $2, $3, $4)`,
		hash, id.Username, id.DisplayName, expires)
	if err != nil {
		return Session{}, fmt.Errorf("authn: store session: %w", err)
	}

	s.event(ctx, id.Username, action, remote, nil)
	if action == "auth.break_glass" {
		s.log.Error("BREAK-GLASS LOGIN — local account used, verify this was sanctioned",
			"remote", remote)
	}
	return Session{Token: token, Identity: id, ExpiresAt: expires}, nil
}

// authenticate resolves the principal: break-glass is checked before the
// directory (its whole point is the directory being down) and never falls
// through to it.
func (s *Service) authenticate(ctx context.Context, username, password string) (Identity, string, error) {
	if username == breakGlassUser {
		if s.breakglassHash == "" || password == "" {
			return Identity{}, "", ErrBadCredentials
		}
		if bcrypt.CompareHashAndPassword([]byte(s.breakglassHash), []byte(password)) != nil {
			return Identity{}, "", ErrBadCredentials
		}
		return Identity{Username: breakGlassUser, DisplayName: "Break Glass"}, "auth.break_glass", nil
	}
	id, err := s.dir.Bind(ctx, username, password)
	if err != nil {
		return Identity{}, "", err
	}
	return id, "auth.login", nil
}

// Validate exchanges a cookie token for its identity. One statement
// refreshes last_seen_at and checks expiry; a dead token's row is lazily
// deleted (session GC beyond this is icebox).
func (s *Service) Validate(ctx context.Context, token string) (Identity, error) {
	if token == "" {
		return Identity{}, ErrNoSession
	}
	hash := hashToken(token)
	var id Identity
	err := s.pool.QueryRow(ctx, `
		UPDATE session SET last_seen_at = now()
		WHERE token_hash = $1 AND expires_at > now()
		RETURNING username, display_name`, hash).
		Scan(&id.Username, &id.DisplayName)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		_, _ = s.pool.Exec(ctx,
			`DELETE FROM session WHERE token_hash = $1 AND expires_at <= now()`, hash)
		return Identity{}, ErrNoSession
	case err != nil:
		return Identity{}, fmt.Errorf("authn: validate session: %w", err)
	}
	return id, nil
}

// Logout revokes the session server-side (the row, not just the cookie).
// Unknown tokens are a no-op: logout is idempotent.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	var username string
	err := s.pool.QueryRow(ctx,
		`DELETE FROM session WHERE token_hash = $1 RETURNING username`, hashToken(token)).
		Scan(&username)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	case err != nil:
		return fmt.Errorf("authn: logout: %w", err)
	}
	s.event(ctx, username, "auth.logout", "", nil)
	return nil
}

// event appends to the auth trail; a failed write is logged, never fatal —
// the trail must not take the portal down, and the trigger tests pin that
// rows, once written, are immutable.
func (s *Service) event(ctx context.Context, actor, action, remote string, detail any) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_event (actor, action, remote, detail)
		VALUES ($1, $2, $3, $4)`, actor, action, remote, detail)
	if err != nil {
		s.log.Error("auth_event write failed", "action", action, "err", err.Error())
	}
}

func newToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
