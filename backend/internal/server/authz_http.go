package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ios9000/db-portal/backend/internal/authn"
	"github.com/ios9000/db-portal/backend/internal/authz"
)

// RoleGuard is what the route guards need from authz; satisfied by
// *authz.Store, stubbed in handler tests. A nil return means proceed;
// authz.ErrDenied means the denial is already on the auth trail.
type RoleGuard interface {
	Require(ctx context.Context, username, role, remote, detail string) error
}

// requireRole guards the mutating subgroup (SPEC-021 mini-ADR 2). It runs
// inside requireSession, so an absent identity is a wiring bug, not a user
// error. A guard failure is a 500, never a 403 — "denied" must mean denied.
func requireRole(log *slog.Logger, guard RoleGuard, role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actor, ok := actorFrom(w, r)
			if !ok {
				return
			}
			err := guard.Require(r.Context(), actor, role, r.RemoteAddr, r.Method+" "+r.URL.Path)
			switch {
			case errors.Is(err, authz.ErrDenied):
				writeJSONError(w, http.StatusForbidden, role+" role required")
			case err != nil:
				log.Error("role check failed", "role", role, "err", err.Error())
				writeJSONError(w, http.StatusInternalServerError, "authorization unavailable")
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// actorFrom yields the session identity's username for audit attribution.
// requireSession guarantees it; the 500 arm exists so a future mis-mounted
// route fails loudly instead of auditing "".
func actorFrom(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := authn.From(r.Context())
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "no identity on request")
		return "", false
	}
	return id.Username, true
}
