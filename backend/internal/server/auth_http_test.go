package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/authn"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// strictAuth accepts exactly one credential pair and one token, so these
// tests exercise the guard for real (unlike allowAllAuth).
type strictAuth struct {
	loggedOut []string
}

const goodToken = "tok-424242"

func (a *strictAuth) Login(_ context.Context, username, password, _ string) (authn.Session, error) {
	if username != "dba1" || password != "dba1" {
		return authn.Session{}, authn.ErrBadCredentials
	}
	return authn.Session{
		Token:     goodToken,
		Identity:  authn.Identity{Username: "dba1", DisplayName: "Dana Baker"},
		ExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (a *strictAuth) Validate(_ context.Context, token string) (authn.Identity, error) {
	if token != goodToken {
		return authn.Identity{}, authn.ErrNoSession
	}
	return authn.Identity{Username: "dba1", DisplayName: "Dana Baker"}, nil
}

func (a *strictAuth) Logout(_ context.Context, token string) error {
	a.loggedOut = append(a.loggedOut, token)
	return nil
}

func authServer(t *testing.T) (*httptest.Server, *strictAuth) {
	t.Helper()
	auth := &strictAuth{}
	ts := depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{instances: sampleInstances()},
		Runs: stubRuns{}, Auth: auth, Roles: allowAllRoles{},
	})
	return ts, auth
}

func postLogin(t *testing.T, ts *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(ts.URL+"/api/auth/login", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	return resp
}

func withSession(t *testing.T, req *http.Request) *http.Request {
	t.Helper()
	req.AddCookie(&http.Cookie{Name: "portal_session", Value: goodToken})
	return req
}

// SPEC-020 behavior 1: every API route is 401 without a session; healthz
// and the SPA stay public.
func TestAPIRequiresSession(t *testing.T) {
	ts, _ := authServer(t)

	for _, path := range []string{"/api/instances", "/api/runs", "/api/operations", "/api/artifacts", "/api/auth/me"} {
		resp, err := http.Get(ts.URL + path)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, path)
	}

	resp, err := http.Get(ts.URL + "/healthz")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "healthz stays public")

	resp, err = http.Get(ts.URL + "/login")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "SPA (login page) stays public")
}

func TestLoginSetsHardenedCookie(t *testing.T) {
	ts, _ := authServer(t)

	resp := postLogin(t, ts, `{"username":"dba1","password":"dba1"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "portal_session" {
			cookie = c
		}
	}
	require.NotNil(t, cookie, "login must set the session cookie")
	require.Equal(t, goodToken, cookie.Value)
	require.True(t, cookie.HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Equal(t, "/", cookie.Path)

	// The authenticated cookie unlocks the API.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/instances", nil)
	apiResp, err := http.DefaultClient.Do(withSession(t, req))
	require.NoError(t, err)
	require.NoError(t, apiResp.Body.Close())
	require.Equal(t, http.StatusOK, apiResp.StatusCode)
}

func TestLoginFailuresAreUniform(t *testing.T) {
	ts, _ := authServer(t)

	for name, body := range map[string]string{
		"wrong password": `{"username":"dba1","password":"nope"}`,
		"unknown user":   `{"username":"ghost","password":"nope"}`,
	} {
		resp := postLogin(t, ts, body)
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode, name)
		require.Empty(t, resp.Cookies(), name)
	}

	resp := postLogin(t, ts, `{not json`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestMeReturnsIdentity(t *testing.T) {
	ts, _ := authServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/me", nil)
	var id authn.Identity
	resp := apiDo(t, withSession(t, req), &id)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "dba1", id.Username)
	require.Equal(t, "Dana Baker", id.DisplayName)
}

func TestLogoutRevokesAndClearsCookie(t *testing.T) {
	ts, auth := authServer(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/logout", nil)
	resp, err := http.DefaultClient.Do(withSession(t, req))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, []string{goodToken}, auth.loggedOut)

	var cleared *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "portal_session" {
			cleared = c
		}
	}
	require.NotNil(t, cleared)
	require.Empty(t, cleared.Value)
	require.Negative(t, cleared.MaxAge, "cookie must be expired")
}

// apiDo mirrors apiGet for a prepared request (cookies attached).
func apiDo(t *testing.T, req *http.Request, out any) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	return resp
}
