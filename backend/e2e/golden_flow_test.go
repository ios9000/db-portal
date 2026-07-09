// Package e2e holds the golden-flow test (STRATEGY §7): the M1 hero flow,
// end to end at the HTTP seam, on a migrated scratch DB with the real
// service/engine/notify wiring. It is the canary that layers beneath a WU
// didn't silently break — it must stay green in every session. Skips (like
// every DB test) when the compose Postgres is absent; see ADR-011.
//
// The automated twin of docs/demo-m1.md. UI-only beats (typed-name prod
// ritual, EnvBanner, drawer) are pinned by the vitest component suite —
// the API beneath them is deliberately unguarded until authz (SPEC-015).
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/notify"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/server"
	"github.com/ios9000/db-portal/backend/internal/testutil"
)

const fixtureCSV = "../../infra/fixtures/instances.csv"

func TestGoldenFlow(t *testing.T) {
	ctx := context.Background()
	pool := testutil.MigratedDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Production wiring in miniature (cmd/portal/main.go): one adapter per
	// env class (guardrail layer 3), real service, real SMTP mailer.
	registry := engine.NewRegistry()
	registry.Register(engine.ClassProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-prod", StepDelay: 5 * time.Millisecond}))
	registry.Register(engine.ClassNonProd,
		engine.NewMockEngine(engine.MockConfig{Name: "mock-nonprod", StepDelay: 5 * time.Millisecond}))

	svc := runs.NewService(pool, registry, log)
	svc.PollInterval = 2 * time.Millisecond
	t.Cleanup(svc.Wait) // watchers + notify goroutines drain before the pool closes

	smtpAddr, mail := testutil.FakeSMTP(t)
	svc.Notifier = &notify.Mailer{
		Addr: smtpAddr, From: "portal@db-portal.local",
		To: []string{"dba@example.test"}, BaseURL: "http://portal.local:8080",
	}

	ts := httptest.NewServer(server.NewRouter(log, server.Deps{
		DB: pool, Instances: inventory.NewStore(pool), Runs: svc,
	}))
	t.Cleanup(ts.Close)

	// Beat 1 — import the estate, twice: the second pass must be a no-op
	// (idempotent natural-key import, SPEC-010).
	rep := importFixture(t, ctx, pool)
	require.Equal(t, 8, rep.New)
	require.Zero(t, rep.Quarantined)
	rep = importFixture(t, ctx, pool)
	require.Zero(t, rep.New)
	require.Equal(t, 8, rep.Unchanged)

	// Beat 2 — the fleet is visible, env filter is server-side.
	require.Len(t, listInstances(t, ts, ""), 8)
	require.Len(t, listInstances(t, ts, "?env=test"), 3)

	// Beat 3+4 — dump on a TEST instance through the API, watch it succeed
	// with artifact metadata, routed to the nonprod adapter.
	run := startRun(t, ts, "billing-test")
	run = waitTerminal(t, ts, run.ID)
	require.Equal(t, "success", run.State)
	require.NotNil(t, run.JobID)
	require.True(t, strings.HasPrefix(*run.JobID, "mock-nonprod-"), "job %q not on nonprod engine", *run.JobID)
	require.NotNil(t, run.Artifact)
	require.NotEmpty(t, run.Artifact.Checksum)
	requireAudit(t, ctx, pool, run.ID, "test", "success")

	// Beat 4 — live logs over SSE: replay of the finished job, then exactly
	// one `end` event (SPEC-013).
	sse := readLogStream(t, ts, run.ID)
	require.Contains(t, sse, "event: log")
	require.Contains(t, sse, "PLAY RECAP")
	require.Equal(t, 1, strings.Count(sse, "event: end"))
	require.Contains(t, sse, `"state":"success"`)

	// Beat 5 — a failing run leaves a complete audit trail and mails the
	// DBA list. Failure injection rides the engineParams seam (tests only;
	// the API passes nil — SPEC-012), everything downstream is production
	// code: watcher, finalize, audit append, post-commit mail.
	failRun, err := svc.Start(ctx, "crm-test", "dump", "", map[string]string{"mock_fail_at": "1"})
	require.NoError(t, err)
	failRun = waitTerminal(t, ts, failRun.ID)
	require.Equal(t, "failed", failRun.State)
	requireAudit(t, ctx, pool, failRun.ID, "test", "failed")

	var msg testutil.SMTPCapture
	select {
	case msg = <-mail:
	case <-time.After(5 * time.Second):
		t.Fatal("no failure mail arrived")
	}
	require.Contains(t, msg.Data,
		fmt.Sprintf("Subject: [db-portal] RUN-%d failed — dump on crm-test (test)", failRun.ID))
	require.Contains(t, msg.Data, fmt.Sprintf("http://portal.local:8080/runs/%d", failRun.ID))
	require.NotContains(t, msg.Data, "injected failure") // error text never leaves the portal

	// Beat 6 — PROD: same API, different engine credentials (layer 3) and
	// env stamped 'prod' in every audit row (layer 4).
	prodRun := startRun(t, ts, "billing-prod")
	prodRun = waitTerminal(t, ts, prodRun.ID)
	require.Equal(t, "success", prodRun.State)
	require.NotNil(t, prodRun.JobID)
	require.True(t, strings.HasPrefix(*prodRun.JobID, "mock-prod-"), "job %q not on prod engine", *prodRun.JobID)
	requireAudit(t, ctx, pool, prodRun.ID, "prod", "success")
}

func importFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) inventory.Report {
	t.Helper()
	f, err := os.Open(fixtureCSV)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	rep, err := inventory.Import(ctx, pool, "instances.csv", f)
	require.NoError(t, err)
	return rep
}

func listInstances(t *testing.T, ts *httptest.Server, query string) []json.RawMessage {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/instances" + query)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Instances []json.RawMessage `json:"instances"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body.Instances
}

func startRun(t *testing.T, ts *httptest.Server, instance string) runs.Run {
	t.Helper()
	payload := fmt.Sprintf(`{"instance": %q, "operation": "dump"}`, instance)
	resp, err := http.Post(ts.URL+"/api/runs", "application/json", strings.NewReader(payload))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var run runs.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&run))
	return run
}

func waitTerminal(t *testing.T, ts *httptest.Server, id int64) runs.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("%s/api/runs/%d", ts.URL, id))
		require.NoError(t, err)
		var run runs.Run
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&run))
		_ = resp.Body.Close()
		switch run.State {
		case "success", "failed", "canceled":
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("run %d never reached a terminal state", id)
	return runs.Run{}
}

func readLogStream(t *testing.T, ts *httptest.Server, id int64) string {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/api/runs/%d/logs", ts.URL, id))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body) // stream closes itself after the `end` event
	require.NoError(t, err)
	return string(body)
}

// requireAudit pins guardrail layer 4 (SPEC-015): exactly one submitted +
// one finished audit event per run, environment stamped on both.
func requireAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID int64, env, finalStatus string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT action, environment, COALESCE(final_status, ''), COALESCE(job_id, '')
		FROM audit_event WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()

	type event struct{ action, env, final string }
	var events []event
	var jobIDs []string
	for rows.Next() {
		var e event
		var jobID string
		require.NoError(t, rows.Scan(&e.action, &e.env, &e.final, &jobID))
		events = append(events, e)
		jobIDs = append(jobIDs, jobID)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []event{
		{"run.submitted", env, ""},
		{"run.finished", env, finalStatus},
	}, events)
	// job_id (0004): NULL at submit — the engine id doesn't exist yet — and
	// stamped on finished, anchoring the run<->engine-job linkage immutably.
	require.Empty(t, jobIDs[0])
	require.NotEmpty(t, jobIDs[1], "run.finished must carry the engine job_id")
}
