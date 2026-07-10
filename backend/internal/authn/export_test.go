package authn

import (
	"context"
	"time"
)

// SetFailDelay shrinks the login-failure delay so tests don't sleep 300ms
// per negative case.
func (s *Service) SetFailDelay(d time.Duration) { s.failDelay = d }

// LDAPConn exposes the connection surface for test doubles.
type LDAPConn = ldapConn

// SetDialer swaps the LDAP dialer so unit tests never touch the network.
func (l *LDAP) SetDialer(dial func(ctx context.Context) (LDAPConn, error)) {
	l.dial = dial
}
