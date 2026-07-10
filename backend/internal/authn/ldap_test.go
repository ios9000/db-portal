package authn_test

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/authn"
)

// fakeConn records what the LDAP directory does with a connection. A live
// AD is out of test scope — the dialer seam keeps these tests offline.
type fakeConn struct {
	bindDN, bindPW string
	startTLS       bool
	bindErr        error
	closed         bool
}

func (c *fakeConn) Bind(dn, pw string) error {
	c.bindDN, c.bindPW = dn, pw
	return c.bindErr
}
func (c *fakeConn) StartTLS(*tls.Config) error { c.startTLS = true; return nil }
func (c *fakeConn) Close() error               { c.closed = true; return nil }

func dialTo(conn *fakeConn) func(context.Context) (authn.LDAPConn, error) {
	return func(context.Context) (authn.LDAPConn, error) { return conn, nil }
}

func TestLDAPBindRendersTemplate(t *testing.T) {
	conn := &fakeConn{}
	dir := authn.LDAP{URL: "ldaps://ad.example.com:636", BindTemplate: "%s@corp.example.com"}
	dir.SetDialer(dialTo(conn))

	id, err := dir.Bind(context.Background(), "dana.baker", "pw")
	require.NoError(t, err)
	require.Equal(t, "dana.baker@corp.example.com", conn.bindDN)
	require.Equal(t, "dana.baker", id.Username)
	require.True(t, conn.closed)
	require.False(t, conn.startTLS, "ldaps:// is already TLS — no StartTLS")
}

// An empty password is an "unauthenticated bind" — LDAP servers report
// success for it. It must be rejected before any network I/O.
func TestLDAPRejectsEmptyPassword(t *testing.T) {
	dir := authn.LDAP{URL: "ldaps://x", BindTemplate: "%s"}
	dir.SetDialer(func(context.Context) (authn.LDAPConn, error) {
		t.Fatal("must not dial on an empty password")
		return nil, nil
	})
	_, err := dir.Bind(context.Background(), "dana", "")
	require.ErrorIs(t, err, authn.ErrBadCredentials)
}

// Usernames that could rewrite the DN/UPN never reach the template.
func TestLDAPRejectsInjectionUsernames(t *testing.T) {
	dir := authn.LDAP{URL: "ldaps://x", BindTemplate: "uid=%s,ou=people,dc=corp"}
	dir.SetDialer(func(context.Context) (authn.LDAPConn, error) {
		t.Fatal("must not dial on a rejected username")
		return nil, nil
	})
	for _, user := range []string{"", "a,ou=admins", "a)(uid=*", "a b", `a\b`, "a=b"} {
		_, err := dir.Bind(context.Background(), user, "pw")
		require.ErrorIs(t, err, authn.ErrBadCredentials, "username %q", user)
	}
}

// Plaintext ldap:// must upgrade to TLS before credentials travel.
func TestLDAPPlaintextEnforcesStartTLS(t *testing.T) {
	conn := &fakeConn{}
	dir := authn.LDAP{URL: "ldap://ad.example.com:389", BindTemplate: "%s@corp"}
	dir.SetDialer(dialTo(conn))

	_, err := dir.Bind(context.Background(), "dana", "pw")
	require.NoError(t, err)
	require.True(t, conn.startTLS, "ldap:// without Insecure must StartTLS")
}

// A refused bind is a uniform ErrBadCredentials — no directory detail leaks.
func TestLDAPBindRefusedIsUniform(t *testing.T) {
	conn := &fakeConn{bindErr: errors.New("LDAP Result Code 49: invalid credentials")}
	dir := authn.LDAP{URL: "ldaps://x", BindTemplate: "%s"}
	dir.SetDialer(dialTo(conn))

	_, err := dir.Bind(context.Background(), "dana", "wrong")
	require.ErrorIs(t, err, authn.ErrBadCredentials)
}
