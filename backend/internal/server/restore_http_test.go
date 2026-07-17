package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/restore"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
)

// restoreServer wires both the registry read and the chain assembler — the
// two collaborators POST /api/restore drives (SPEC-031 mini-ADR 4).
func restoreServer(t *testing.T, ar server.ArtifactReader, cs server.ChainService) *httptest.Server {
	t.Helper()
	return depsServer(t, server.Deps{
		DB: fakePinger{}, Instances: stubReader{}, Runs: stubRuns{},
		Artifacts: ar, Chains: cs, Auth: allowAllAuth{}, Roles: allowAllRoles{},
	})
}

func sampleArtifact() runs.RegisteredArtifact {
	return runs.RegisteredArtifact{
		ID: 3, RunID: 41, Name: "billing-test-20260711.dump.tgz",
		SizeBytes: 123456, Checksum: "deadbeef", RetentionClass: "standard",
		CreatedAt: time.Date(2026, 7, 11, 4, 0, 0, 0, time.UTC),
	}
}

// The happy path: the handler loads the source artifact, hands chain.Create
// the fixed verify → safety_dump → restore recipe with the artifact lineage
// pinned into the verify and restore steps, and answers 201 with the chain
// read model so the UI can navigate to the strip.
func TestStartRestoreAssemblesRecipe(t *testing.T) {
	art := sampleArtifact()
	var askedID int64
	captured := &chain.CreateRequest{}
	ts := restoreServer(t,
		stubArtifacts{one: art, gotID: &askedID},
		stubChains{chain: sampleChain(), created: captured})

	body := fmt.Sprintf(`{"artifact_id": %d, "target": "crm-test", "reason": "rollback"}`, art.ID)
	var got chain.Chain
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/restore", body, &got)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.EqualValues(t, art.ID, askedID, "the posted artifact_id is what we load")
	require.EqualValues(t, 7, got.ID, "the response is the chain read model")

	require.Equal(t, restore.Kind, captured.Kind)
	require.Equal(t, "crm-test", captured.Instance, "the chain runs on the TARGET")
	require.Equal(t, "local-dev", captured.Actor, "actor is the session identity, explicit")
	require.Equal(t, "rollback", captured.Reason)

	require.Len(t, captured.Steps, 3)
	require.Equal(t, restore.OpVerify, captured.Steps[0].Operation)
	require.Equal(t, restore.OpSafetyDump, captured.Steps[1].Operation)
	require.Equal(t, restore.OpRestore, captured.Steps[2].Operation)
	// Lineage pinned to the source artifact on verify + restore, never the
	// safety dump (which dumps the target as-is).
	require.Equal(t, strconv.FormatInt(art.ID, 10), captured.Steps[0].Params["artifact_id"])
	require.Equal(t, art.Checksum, captured.Steps[2].Params["checksum"])
	require.Empty(t, captured.Steps[1].Params, "the safety dump carries no source lineage")
}

// Field validation is the handler's own (before it touches either collaborator).
func TestStartRestoreValidation(t *testing.T) {
	cases := []struct {
		name, body, detail string
	}{
		{"missing artifact_id", `{"target": "crm-test"}`, "artifact_id"},
		{"zero artifact_id", `{"artifact_id": 0, "target": "crm-test"}`, "artifact_id"},
		{"missing target", `{"artifact_id": 3}`, "target"},
		{"reason too long", fmt.Sprintf(`{"artifact_id": 3, "target": "crm-test", "reason": %q}`,
			longReason()), "reason too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := restoreServer(t, stubArtifacts{one: sampleArtifact()}, stubChains{chain: sampleChain()})
			var body map[string]string
			resp := doJSON(t, http.MethodPost, ts.URL+"/api/restore", tc.body, &body)
			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			require.Contains(t, body["error"], tc.detail)
		})
	}
}

func TestStartRestoreUnknownArtifact(t *testing.T) {
	ts := restoreServer(t,
		stubArtifacts{getErr: runs.ErrArtifactNotFound},
		stubChains{chain: sampleChain()})

	body := `{"artifact_id": 999, "target": "crm-test"}`
	var errBody map[string]string
	resp := doJSON(t, http.MethodPost, ts.URL+"/api/restore", body, &errBody)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.Contains(t, errBody["error"], "no such artifact")
}

// The assembler's refusals map to their status (SPEC-031 mini-ADR 4): an
// unknown target is 404, a prod target without the typed name is 400 — and
// because chain.Create checks the ritual BEFORE writing any row, "400 = no
// chain" is structural (asserted end-to-end in the golden flow).
func TestStartRestoreCreateErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		detail string
	}{
		{"unknown target", runs.ErrUnknownInstance, http.StatusNotFound, "no such instance"},
		{"prod unconfirmed", runs.ErrProdUnconfirmed, http.StatusBadRequest, "typing the target"},
		{"self-target", runs.ErrSelfTarget, http.StatusForbidden, "portal's own database"},
		{"patroni restore", runs.ErrPatroniRestore, http.StatusForbidden, "Patroni-managed cluster"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := restoreServer(t,
				stubArtifacts{one: sampleArtifact()},
				stubChains{err: tc.err})
			var body map[string]string
			resp := doJSON(t, http.MethodPost, ts.URL+"/api/restore",
				`{"artifact_id": 3, "target": "crm-prod"}`, &body)
			require.Equal(t, tc.status, resp.StatusCode)
			require.Contains(t, body["error"], tc.detail)
		})
	}
}

func longReason() string {
	b := make([]byte, 4096)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
