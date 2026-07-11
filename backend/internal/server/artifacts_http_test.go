package server_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubArtifacts fakes the registry read for handler tests (the real query
// is covered by DB-backed tests in internal/runs).
type stubArtifacts struct {
	list     []runs.RegisteredArtifact
	err      error
	instance *string

	// GetArtifact side (the restore assembler's source lookup, SPEC-031).
	one    runs.RegisteredArtifact
	getErr error
	gotID  *int64
}

func (s stubArtifacts) ListArtifacts(_ context.Context, instance string) ([]runs.RegisteredArtifact, error) {
	if s.instance != nil {
		*s.instance = instance
	}
	return s.list, s.err
}

func (s stubArtifacts) GetArtifact(_ context.Context, id int64) (runs.RegisteredArtifact, error) {
	if s.gotID != nil {
		*s.gotID = id
	}
	return s.one, s.getErr
}

func artifactsServer(t *testing.T, ar server.ArtifactReader) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{},
		Artifacts: ar, Auth: allowAllAuth{}, Roles: allowAllRoles{},
	})
}

func TestListArtifactsServesRegistry(t *testing.T) {
	var asked string
	ts := artifactsServer(t, stubArtifacts{
		instance: &asked,
		list: []runs.RegisteredArtifact{{
			ID: 3, RunID: 41, Name: "billing-test-20260711.dump.tgz",
			SizeBytes: 123456, Checksum: "deadbeef", RetentionClass: "standard",
			CreatedAt: time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC),
		}},
	})

	var body struct {
		Artifacts []map[string]any `json:"artifacts"`
	}
	resp := apiGet(t, ts, "/api/artifacts?instance=billing-test", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "billing-test", asked)
	require.Len(t, body.Artifacts, 1)
	a := body.Artifacts[0]
	require.EqualValues(t, 41, a["run_id"], "origin run id rides the payload")
	require.Equal(t, "standard", a["retention_class"])
	require.NotContains(t, a, "location",
		"dormant column stays server-side until WU-034 gives it meaning")
}

// SPEC-030 mini-ADR 4: the registry read is a per-instance feed — no param,
// no list.
func TestListArtifactsRequiresInstance(t *testing.T) {
	ts := artifactsServer(t, stubArtifacts{})

	var body map[string]string
	resp := apiGet(t, ts, "/api/artifacts", &body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body["error"], "instance")
}

func TestListArtifactsUnknownInstance(t *testing.T) {
	ts := artifactsServer(t, stubArtifacts{err: runs.ErrUnknownInstance})

	var body map[string]string
	resp := apiGet(t, ts, "/api/artifacts?instance=nope", &body)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Equal(t, "no such instance", body["error"])
}

func TestListArtifactsEmptyNeverNull(t *testing.T) {
	ts := artifactsServer(t, stubArtifacts{})

	var body struct {
		Artifacts []runs.RegisteredArtifact `json:"artifacts"`
	}
	resp := apiGet(t, ts, "/api/artifacts?instance=billing-test", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, body.Artifacts, `"artifacts": [], never null`)
	require.Empty(t, body.Artifacts)
}

func TestListArtifactsServiceError(t *testing.T) {
	ts := artifactsServer(t, stubArtifacts{err: errors.New("boom")})

	var body map[string]string
	resp := apiGet(t, ts, "/api/artifacts?instance=billing-test", &body)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Equal(t, "artifacts unavailable", body["error"])
}
