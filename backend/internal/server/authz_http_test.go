package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/authz"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// strictRoles denies (or errors) and records what the guard was asked —
// the store's real semantics live in internal/authz tests.
type strictRoles struct {
	err  error
	last *roleAsk
}

type roleAsk struct{ username, role, detail string }

func (s strictRoles) Require(_ context.Context, username, role, _, detail string) error {
	if s.last != nil {
		*s.last = roleAsk{username, role, detail}
	}
	return s.err
}

func guardedServer(t *testing.T, guard server.RoleGuard) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{run: sampleRun()},
		Schedules: stubSchedules{}, Chains: stubChains{chain: sampleChain()},
		Auth: allowAllAuth{}, Roles: guard,
	})
}

// SPEC-021 behavior 1 (handler half): mutations answer 403 for a session
// without the dba role; the guard sees the session identity and the route.
// SPEC-022 behavior 8 adds the schedule mutations, SPEC-032 behavior 4 the
// chain resume, to the same group.
func TestMutationsRequireDBARole(t *testing.T) {
	asked := &roleAsk{}
	ts := guardedServer(t, strictRoles{err: authz.ErrDenied, last: asked})

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/runs"},
		{http.MethodPost, "/api/runs/7/cancel"},
		{http.MethodPost, "/api/schedules"},
		{http.MethodPatch, "/api/schedules/3"},
		{http.MethodDelete, "/api/schedules/3"},
		{http.MethodPost, "/api/chains/7/resume"},
	} {
		req, err := http.NewRequest(route.method, ts.URL+route.path,
			strings.NewReader(`{"instance":"billing-test","operation":"dump"}`))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusForbidden, resp.StatusCode, route.path)
		require.Equal(t, "local-dev", asked.username, "guard must see the session identity")
		require.Equal(t, authz.RoleDBA, asked.role)
		require.Equal(t, route.method+" "+route.path, asked.detail)
	}
}

// Reads stay session-gated only (SPEC-021 mini-ADR 2): a denying guard
// must never be consulted for them.
func TestReadsSkipTheRoleGuard(t *testing.T) {
	ts := guardedServer(t, strictRoles{err: authz.ErrDenied})

	for _, path := range []string{"/api/instances", "/api/runs", "/api/runs/7", "/api/runs/7/chain", "/api/operations", "/api/schedules", "/api/auth/me"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
}

// A broken role store is a 500, never a 403 — "denied" must mean denied
// (SPEC-021 behavior 7).
func TestRoleStoreFailureIsNot403(t *testing.T) {
	ts := guardedServer(t, strictRoles{err: errors.New("db down")})

	resp, err := http.Post(ts.URL+"/api/runs", "application/json",
		strings.NewReader(`{"instance":"billing-test","operation":"dump"}`))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

// SPEC-021 mini-ADR 6 (handler half): the service's ritual refusal maps to
// a 400 with an actionable message.
func TestProdUnconfirmedIs400(t *testing.T) {
	ts := runsServer(t, stubRuns{err: runs.ErrProdUnconfirmed})

	var body map[string]string
	resp := postRuns(t, ts, `{"instance":"billing-prod","operation":"dump"}`, &body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body["error"], "typing the instance name")
}

// m1-gate item 15 / SPEC-021 mini-ADR 7: oversized bodies stop at the cap,
// oversized reasons at the policy limit.
func TestStartRunBodyCaps(t *testing.T) {
	ts := runsServer(t, stubRuns{run: sampleRun()})

	var body map[string]string
	huge := `{"instance":"billing-test","operation":"dump","reason":"` +
		strings.Repeat("x", 65<<10) + `"}`
	resp := postRuns(t, ts, huge, &body)
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)

	longReason := `{"instance":"billing-test","operation":"dump","reason":"` +
		strings.Repeat("x", 501) + `"}`
	resp = postRuns(t, ts, longReason, &body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body["error"], "reason too long")
}

// The canceling actor is the session identity, passed explicitly to the
// service (SPEC-021 mini-ADR 4).
func TestCancelPassesActor(t *testing.T) {
	var actor string
	ts := runsServer(t, stubRuns{cancelActor: &actor})

	var body map[string]string
	resp := postCancel(t, ts, "/api/runs/7/cancel", &body)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, "local-dev", actor)
}
