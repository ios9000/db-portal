// Package authn implements SPEC-020: directory bind (AD LDAP in prod, an
// in-process fake for dev/CI — same seam pattern as engine.Adapter), server
// sessions in the portal DB, and the append-only auth_event trail. AuthZ
// lives in WU-021; everything here answers only "who is this?".
package authn

import (
	"context"
	"errors"
)

// Identity is an authenticated principal. DisplayName falls back to the
// username when the directory can't provide one (template bind has no
// search step — WU-021 may enrich this).
type Identity struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// Directory verifies credentials. Implementations must treat an empty
// password as a failure — LDAP servers accept empty passwords as an
// "unauthenticated bind" and report success, which must never authenticate
// a portal user.
type Directory interface {
	Bind(ctx context.Context, username, password string) (Identity, error)
}

// ErrBadCredentials is the uniform login failure: wrong password, unknown
// user, directory down — callers must not be able to tell which (no
// user-exists oracle).
var ErrBadCredentials = errors.New("authn: bad credentials")

// ErrNoSession means the presented token matches no live session.
var ErrNoSession = errors.New("authn: no session")

type ctxKey struct{}

// With stores the authenticated identity on the context (set by the HTTP
// middleware; read by WU-021's authz and, eventually, the audit actor).
func With(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// From returns the identity stored by With, if any.
func From(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// Bypass is PORTAL_AUTH_MODE=off: every request is local-dev, no login
// needed. Demo/dev only — main logs loudly when it is wired.
type Bypass struct{}

var bypassIdentity = Identity{Username: "local-dev", DisplayName: "Local Dev"}

func (Bypass) Login(context.Context, string, string, string) (Session, error) {
	// No cookie to set: Token stays empty and Validate accepts anything.
	return Session{Identity: bypassIdentity}, nil
}

func (Bypass) Validate(context.Context, string) (Identity, error) {
	return bypassIdentity, nil
}

func (Bypass) Logout(context.Context, string) error { return nil }
