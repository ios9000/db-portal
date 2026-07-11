package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// stubChains fakes the chain service so handler tests run without a
// database; the driver's real semantics live in internal/chain tests.
type stubChains struct {
	chain   chain.Chain
	err     error
	forRun  *int64
	resumed *struct {
		id    int64
		actor string
	}
	created *chain.CreateRequest // captures the assembler's ask (SPEC-031)
}

func (s stubChains) ForRun(_ context.Context, runID int64) (chain.Chain, error) {
	if s.forRun != nil {
		*s.forRun = runID
	}
	return s.chain, s.err
}

func (s stubChains) Resume(_ context.Context, id int64, actor string) (chain.Chain, error) {
	if s.resumed != nil {
		s.resumed.id, s.resumed.actor = id, actor
	}
	return s.chain, s.err
}

func (s stubChains) Create(_ context.Context, req chain.CreateRequest) (chain.Chain, error) {
	if s.created != nil {
		*s.created = req
	}
	return s.chain, s.err
}

func sampleChain() chain.Chain {
	run1 := int64(41)
	return chain.Chain{
		ID: 7, Kind: "test-chain", Instance: "billing-test", Env: "test",
		State: "halted", CreatedBy: "dba1",
		CreatedAt: time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
		Steps: []chain.Step{
			{Seq: 1, Operation: "dump", RunID: &run1, Status: "failed"},
			{Seq: 2, Operation: "dump", Status: "pending"},
		},
	}
}

func chainsServer(t *testing.T, cs server.ChainService) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{},
		Chains: cs, Auth: allowAllAuth{}, Roles: allowAllRoles{},
	})
}

func TestGetRunChain(t *testing.T) {
	var askedRun int64
	ts := chainsServer(t, stubChains{chain: sampleChain(), forRun: &askedRun})

	var body chain.Chain
	resp := doJSON(t, http.MethodGet, ts.URL+"/api/runs/41/chain", "", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 41, askedRun)
	require.EqualValues(t, 7, body.ID)
	require.Len(t, body.Steps, 2)

	// Unknown run and not-a-step answer the same 404 — no strip either way.
	ts404 := chainsServer(t, stubChains{err: chain.ErrNotFound})
	var errBody map[string]string
	resp = doJSON(t, http.MethodGet, ts404.URL+"/api/runs/41/chain", "", &errBody)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodGet, ts.URL+"/api/runs/not-a-number/chain", "", &errBody)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// The resumer's session identity rides the call explicitly (SPEC-032
// mini-ADR 1): it becomes the chain:<resumer> attribution downstream.
func TestResumeChainPassesActor(t *testing.T) {
	resumed := &struct {
		id    int64
		actor string
	}{}
	ts := chainsServer(t, stubChains{chain: sampleChain(), resumed: resumed})

	var body chain.Chain
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/chains/7/resume", "", &body)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 7, resumed.id)
	require.Equal(t, "local-dev", resumed.actor)
	require.EqualValues(t, 7, body.ID)
}

// SPEC-032 API contract: every service refusal maps to its status.
func TestResumeChainErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{"unknown chain", chain.ErrNotFound, http.StatusNotFound, "no such chain"},
		{"not halted", chain.ErrNotResumable, http.StatusConflict, "not halted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := chainsServer(t, stubChains{err: tc.err})
			var body map[string]string
			resp := doJSON(t, http.MethodPost, ts.URL+"/api/chains/7/resume", "", &body)
			require.Equal(t, tc.status, resp.StatusCode)
			require.Contains(t, body["error"], tc.detail)
		})
	}

	ts := chainsServer(t, stubChains{chain: sampleChain()})
	var body map[string]string
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/chains/not-a-number/resume", "", &body)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
