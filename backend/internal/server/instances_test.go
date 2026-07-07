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

	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubReader fakes the inventory so handler tests run without a database
// (the real Store is covered by DB-backed tests in internal/inventory).
type stubReader struct {
	instances []inventory.Instance
	err       error
}

func (s stubReader) ListInstances(_ context.Context, env string) ([]inventory.Instance, error) {
	if s.err != nil {
		return nil, s.err
	}
	if env == "" {
		return s.instances, nil
	}
	out := []inventory.Instance{}
	for _, in := range s.instances {
		if in.Env == env {
			out = append(out, in)
		}
	}
	return out, nil
}

func (s stubReader) GetInstance(_ context.Context, name string) (inventory.Instance, error) {
	if s.err != nil {
		return inventory.Instance{}, s.err
	}
	for _, in := range s.instances {
		if in.Name == name {
			return in, nil
		}
	}
	return inventory.Instance{}, inventory.ErrNotFound
}

func apiServer(t *testing.T, inv server.InstanceReader) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{DB: fakePinger{}, Instances: inv, Runs: stubRuns{}})
}

func depsServer(t *testing.T, d server.Deps) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ts := httptest.NewServer(server.NewRouter(log, d))
	t.Cleanup(ts.Close)
	return ts
}

func apiGet(t *testing.T, ts *httptest.Server, path string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
	return resp
}

func sampleInstances() []inventory.Instance {
	size := func(v float64) *float64 { return &v }
	window := "Sat 02:00-06:00"
	return []inventory.Instance{
		{
			Name: "billing-prod", Cluster: "billing", Env: "prod", Platform: "k8s_patroni",
			PGVersion: "16.3", SizeGB: size(412), Owner: "billing-team", MaintenanceWindow: &window,
		},
		{
			Name: "billing-test", Cluster: "billing", Env: "test", Platform: "k8s_patroni",
			PGVersion: "16.3", SizeGB: size(38), Owner: "billing-team",
		},
	}
}

func TestListInstances(t *testing.T) {
	ts := apiServer(t, stubReader{instances: sampleInstances()})

	var body struct {
		Instances []map[string]any `json:"instances"`
	}
	resp := apiGet(t, ts, "/api/instances", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body.Instances, 2)

	first := body.Instances[0]
	require.Equal(t, "billing-prod", first["name"])
	require.Equal(t, "billing", first["cluster"])
	require.Equal(t, "prod", first["env"])
	require.Equal(t, "k8s_patroni", first["platform"])
	require.Equal(t, "16.3", first["pg_version"])
	require.InDelta(t, 412, first["size_gb"], 0.001)
	require.Equal(t, "billing-team", first["owner"])
	require.Equal(t, "Sat 02:00-06:00", first["maintenance_window"])

	// Nullable fields serialize as JSON null, not omitted (the UI renders "—").
	second := body.Instances[1]
	require.Contains(t, second, "maintenance_window")
	require.Nil(t, second["maintenance_window"])
}

func TestListInstancesEnvFilter(t *testing.T) {
	ts := apiServer(t, stubReader{instances: sampleInstances()})

	var body struct {
		Instances []map[string]any `json:"instances"`
	}
	resp := apiGet(t, ts, "/api/instances?env=test", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, body.Instances, 1)
	require.Equal(t, "billing-test", body.Instances[0]["name"])
}

func TestListInstancesUnknownEnvIs400(t *testing.T) {
	ts := apiServer(t, stubReader{instances: sampleInstances()})

	var body map[string]string
	resp := apiGet(t, ts, "/api/instances?env=prod2", &body)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.Contains(t, body["error"], "unknown env")
}

func TestListInstancesEmptyIsJSONArray(t *testing.T) {
	ts := apiServer(t, stubReader{})

	var body struct {
		Instances []map[string]any `json:"instances"`
	}
	resp := apiGet(t, ts, "/api/instances", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotNil(t, body.Instances, `"instances" must be [], not null`)
	require.Empty(t, body.Instances)
}

func TestGetInstance(t *testing.T) {
	ts := apiServer(t, stubReader{instances: sampleInstances()})

	var body map[string]any
	resp := apiGet(t, ts, "/api/instances/billing-test", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "billing-test", body["name"])
	require.Equal(t, "test", body["env"])
}

func TestGetInstanceUnknownIs404(t *testing.T) {
	ts := apiServer(t, stubReader{instances: sampleInstances()})

	var body map[string]string
	resp := apiGet(t, ts, "/api/instances/nope", &body)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, body["error"], "no such instance")
}

func TestInstancesStoreErrorIs500(t *testing.T) {
	ts := apiServer(t, stubReader{err: errors.New("pool is on fire")})

	var body map[string]string
	resp := apiGet(t, ts, "/api/instances", &body)
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Equal(t, "inventory unavailable", body["error"])
}
