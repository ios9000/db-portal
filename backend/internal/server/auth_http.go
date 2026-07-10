package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ios9000/db-portal/backend/internal/authn"
)

// Authenticator is what the HTTP layer needs from authn (SPEC-020);
// satisfied by *authn.Service and authn.Bypass. Handler tests stub it.
type Authenticator interface {
	Login(ctx context.Context, username, password, remote string) (authn.Session, error)
	Validate(ctx context.Context, token string) (authn.Identity, error)
	Logout(ctx context.Context, token string) error
}

// sessionCookie carries the opaque session token. httpOnly + SameSite=Lax
// is the SPEC-020 CSRF posture: cross-site POSTs don't send it, and every
// mutating endpoint requires a JSON body a cross-site form can't produce.
const sessionCookie = "portal_session"

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// requireSession guards the API subtree: no valid session → 401. The
// identity rides the request context for handlers (and WU-021's authz).
func requireSession(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, err := auth.Validate(r.Context(), sessionToken(r))
			if err != nil {
				writeJSONError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.With(r.Context(), id)))
		})
	}
}

// login answers POST /api/auth/login — the one API route outside the
// session guard. Failures are uniform 401s (no user-exists oracle).
func login(log *slog.Logger, d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10) // two short fields (SPEC-021 mini-ADR 7)
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" {
			writeJSONError(w, http.StatusBadRequest, "invalid body")
			return
		}
		sess, err := d.Auth.Login(r.Context(), body.Username, body.Password, r.RemoteAddr)
		switch {
		case errors.Is(err, authn.ErrBadCredentials):
			writeJSONError(w, http.StatusUnauthorized, "sign-in failed")
			return
		case err != nil:
			log.Error("login", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "sign-in unavailable")
			return
		}
		if sess.Token != "" { // Bypass mints no cookie
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    sess.Token,
				Path:     "/",
				Expires:  sess.ExpiresAt,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   d.SecureCookies,
			})
		}
		writeJSON(w, http.StatusOK, sess.Identity)
	}
}

// logout answers POST /api/auth/logout (inside the guard): revokes the
// session row and expires the cookie.
func logout(log *slog.Logger, d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Auth.Logout(r.Context(), sessionToken(r)); err != nil {
			log.Error("logout", "err", err.Error())
			writeJSONError(w, http.StatusInternalServerError, "sign-out failed")
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   d.SecureCookies,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// me answers GET /api/auth/me — the SPA's bootstrap probe. Reaching it
// means the guard already validated the session.
func me() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := authn.From(r.Context())
		writeJSON(w, http.StatusOK, id)
	}
}
