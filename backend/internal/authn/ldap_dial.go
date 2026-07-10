package authn

import (
	"context"
	"crypto/tls"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// realDial is the production dialer: go-ldap over ldaps:// (TLS at dial
// time) or ldap:// (Bind enforces StartTLS unless Insecure).
func (l LDAP) realDial(_ context.Context) (ldapConn, error) {
	var opts []ldap.DialOpt
	if l.Insecure && strings.HasPrefix(l.URL, "ldaps://") {
		opts = append(opts, ldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec // dev-only flag, main warns
	}
	conn, err := ldap.DialURL(l.URL, opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
