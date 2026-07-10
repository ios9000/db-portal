package authn

import (
	"context"
	"crypto/tls"
	"fmt"
	"regexp"
	"strings"
)

// ldapConn is the slice of go-ldap's *ldap.Conn this package needs; the
// dialer indirection keeps unit tests off the network (a live AD is out of
// test scope — the Directory seam isolates it like the real Ansible engine).
type ldapConn interface {
	Bind(username, password string) error
	StartTLS(*tls.Config) error
	Close() error
}

// usernameRE is the conservative allowlist for what may be interpolated
// into the bind template: sAMAccountName/UPN-local-part shapes. Anything
// else is rejected before it can rewrite the DN/UPN (bind injection).
var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// LDAP binds directly with the user's own credentials against a DN/UPN
// template (SPEC-020 mini-ADR 3): one round trip, and the portal holds
// zero directory secrets — there is no service account.
type LDAP struct {
	// URL is ldaps://host:636 (TLS) or ldap://host:389 (StartTLS enforced
	// unless Insecure).
	URL string
	// BindTemplate carries exactly one %s, e.g. "%s@corp.example.com".
	BindTemplate string
	// Insecure (dev only) skips certificate verification and allows
	// plaintext ldap:// without StartTLS. main logs a warning when set.
	Insecure bool

	// dial is swapped by tests; nil means the real go-ldap dialer.
	dial func(ctx context.Context) (ldapConn, error)
}

func (l LDAP) Bind(ctx context.Context, username, password string) (Identity, error) {
	if password == "" || !usernameRE.MatchString(username) {
		return Identity{}, ErrBadCredentials
	}
	dial := l.dial
	if dial == nil {
		dial = l.realDial
	}
	conn, err := dial(ctx)
	if err != nil {
		return Identity{}, fmt.Errorf("authn: ldap dial: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if strings.HasPrefix(l.URL, "ldap://") && !l.Insecure {
		// Plaintext port: credentials never travel unencrypted.
		if err := conn.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12}); err != nil {
			return Identity{}, fmt.Errorf("authn: ldap starttls: %w", err)
		}
	}
	if err := conn.Bind(fmt.Sprintf(l.BindTemplate, username), password); err != nil {
		return Identity{}, ErrBadCredentials
	}
	return Identity{Username: username, DisplayName: username}, nil
}
