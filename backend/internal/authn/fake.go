package authn

import (
	"context"
	"crypto/subtle"
)

// FakeUser is one seeded principal in the in-process directory.
type FakeUser struct {
	Password    string
	DisplayName string
}

// Fake is the dev/CI directory (PORTAL_AUTH_MODE=fake): an in-process user
// map, CI-safe, same standing as MockEngine. The credentials are fixtures,
// not secrets.
type Fake map[string]FakeUser

// DevDirectory seeds the fixed dev users the demo and tests log in as.
func DevDirectory() Fake {
	return Fake{
		"dba1": {Password: "dba1", DisplayName: "Dana Baker"},
		"dba2": {Password: "dba2", DisplayName: "Devon Blake"},
	}
}

func (f Fake) Bind(_ context.Context, username, password string) (Identity, error) {
	u, ok := f[username]
	// Constant-time compare and no early return on unknown users: the fake
	// mirrors the "no user-exists oracle" property of the real path.
	match := subtle.ConstantTimeCompare([]byte(u.Password), []byte(password)) == 1
	if !ok || password == "" || !match {
		return Identity{}, ErrBadCredentials
	}
	return Identity{Username: username, DisplayName: u.DisplayName}, nil
}
