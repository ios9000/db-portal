package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/server"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func get(t *testing.T, db server.Pinger, path string) (*http.Response, map[string]string) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(server.NewRouter(log, server.Deps{DB: db, Instances: stubReader{}, Runs: stubRuns{}, Auth: allowAllAuth{}}))
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })

	var body map[string]string
	if resp.Header.Get("Content-Type") == "application/json" {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	}
	return resp, body
}

func TestHealthzOK(t *testing.T) {
	resp, body := get(t, fakePinger{}, "/healthz")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, map[string]string{"status": "ok", "db": "ok"}, body)
}

func TestHealthzDBDown(t *testing.T) {
	resp, body := get(t, fakePinger{err: errors.New("connection refused")}, "/healthz")
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.Equal(t, map[string]string{"status": "degraded", "db": "down"}, body)
}

func TestUnknownAPIRouteIs404(t *testing.T) {
	resp, _ := get(t, fakePinger{}, "/api/nope")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// Non-API paths land on the embedded SPA (WU-006). A fresh checkout
// embeds only the placeholder page, a release build the real dist —
// either way the router must answer HTML with 200.
func TestNonAPIRoutesServeSPA(t *testing.T) {
	for _, path := range []string{"/", "/instances/42"} {
		resp, _ := get(t, fakePinger{}, path)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		require.Contains(t, resp.Header.Get("Content-Type"), "text/html", path)
	}
}
