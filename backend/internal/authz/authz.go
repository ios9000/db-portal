// Package authz answers "may this user do that" from the portal-DB role
// store (SPEC-021). The directory (authn) says who you are; these tables
// say what you may do. MVP knows one role: dba (D2).
package authz

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RoleDBA is the only role the MVP grants or checks (D2; D3 defers the rest).
const RoleDBA = "dba"

// ErrDenied means the user is authenticated but lacks the required role.
// The denial is already on the auth trail when this is returned.
var ErrDenied = errors.New("authz: role required")

// Store is the pool-backed role store.
type Store struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	return &Store{pool: pool, log: log}
}

// HasRole reports whether username holds role. Unknown users and unknown
// roles are simply false — never an error.
func (s *Store) HasRole(ctx context.Context, username, role string) (bool, error) {
	var held bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_role ur
			JOIN role r ON r.id = ur.role_id
			WHERE ur.username = $1 AND r.name = $2)`, username, role).Scan(&held)
	if err != nil {
		return false, fmt.Errorf("authz: role lookup: %w", err)
	}
	return held, nil
}

// Grant gives every username the role, idempotently — dev/demo boot calls
// this on every start (SPEC-021 mini-ADR 8). Unknown role is an error:
// a silent no-op grant would read as success.
func (s *Store) Grant(ctx context.Context, role string, usernames ...string) error {
	for _, u := range usernames {
		tag, err := s.pool.Exec(ctx, `
			INSERT INTO user_role (username, role_id)
			SELECT $1, id FROM role WHERE name = $2
			ON CONFLICT DO NOTHING`, u, role)
		if err != nil {
			return fmt.Errorf("authz: grant %s to %q: %w", role, u, err)
		}
		// INSERT…SELECT with zero source rows means the role doesn't exist;
		// a conflict (already granted) still reports via the SELECT arm, so
		// distinguish by re-checking only on the suspicious case.
		if tag.RowsAffected() == 0 {
			held, herr := s.HasRole(ctx, u, role)
			if herr != nil {
				return herr
			}
			if !held {
				return fmt.Errorf("authz: grant to %q: unknown role %q", u, role)
			}
		}
	}
	return nil
}

// Require is the route-guard seam: nil when username holds role; otherwise
// the denial is recorded (authz.denied on the auth trail — SPEC-021
// mini-ADR 3) and ErrDenied returned. A store failure is neither: the
// caller must answer 500, not 403 — "denied" has to mean denied.
func (s *Store) Require(ctx context.Context, username, role, remote, detail string) error {
	held, err := s.HasRole(ctx, username, role)
	if err != nil {
		return err
	}
	if held {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO auth_event (actor, action, remote, detail)
		VALUES ($1, 'authz.denied', $2, $3)`, username, remote, detail); err != nil {
		// Same posture as authn's trail writes: log, never take the
		// request down — and the 403 stands regardless.
		s.log.Error("auth_event write failed", "action", "authz.denied", "err", err.Error())
	}
	return fmt.Errorf("%w: %s needs %s", ErrDenied, username, role)
}
