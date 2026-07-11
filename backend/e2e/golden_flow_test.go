// Package e2e holds the golden-flow test (STRATEGY §7): the M1 hero flow,
// end to end at the HTTP seam, on a migrated scratch DB with the real
// service/engine/notify wiring. It is the canary that layers beneath a WU
// didn't silently break — it must stay green in every session. Skips (like
// every DB test) when the compose Postgres is absent; see ADR-011.
//
// The automated twin of docs/demo-m1.md. UI-only beats (EnvBanner, drawer)
// are pinned by the vitest component suite; since WU-021 the API beneath
// them is guarded for real — dba role on mutations, prod ritual server-side.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/ios9000/db-portal/backend/internal/authn"
	"github.com/ios9000/db-portal/backend/internal/authz"
	"github.com/ios9000/db-portal/backend/internal/chain"
	"github.com/ios9000/db-portal/backend/internal/engine"
	"github.com/ios9000/db-portal/backend/internal/inventory"
	"github.com/ios9000/db-portal/backend/internal/notify"
	"github.com/ios9000/db-portal/backend/internal/restore"
	"github.com/ios9000/db-portal/backend/internal/runs"
	"github.com/ios9000/db-portal/backend/internal/schedule"
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

	// Notifier wiring in the production shape (SPEC-032 mini-ADR 5): run
	// mail behind the chain step filter, chain halt mail direct.
	smtpAddr, mail := testutil.FakeSMTP(t)
	mailer := &notify.Mailer{
		Addr: smtpAddr, From: "portal@db-portal.local",
		To: []string{"dba@example.test"}, BaseURL: "http://portal.local:8080",
	}
	svc.Notifier = chain.StepRunFilter{Next: mailer}

	chainSvc := chain.New(pool, svc, log)
	chainSvc.PollInterval = 2 * time.Millisecond
	chainSvc.Notifier = mailer
	t.Cleanup(chainSvc.Wait)

	// Same boot-time grants as main.go's fake mode (SPEC-021 mini-ADR 8):
	// the dev directory's DBAs get the dba role; the guard is otherwise live.
	roles := authz.NewStore(pool, log)
	require.NoError(t, roles.Grant(ctx, authz.RoleDBA, "dba1", "dba2"))

	// The scheduler executor, live like main.go runs it (SPEC-022) — a fast
	// tick so the schedule beat observes a fire without waiting a minute.
	sched := schedule.New(pool, svc, log)
	sched.Tick = 5 * time.Millisecond
	sched.Jitter = 0
	schedCtx, stopSched := context.WithCancel(ctx)
	t.Cleanup(stopSched)
	go sched.Run(schedCtx)

	ts := httptest.NewServer(server.NewRouter(log, server.Deps{
		DB: pool, Instances: inventory.NewStore(pool), Runs: svc,
		Artifacts: svc,
		Schedules: sched,
		Chains:    chainSvc,
		Auth:      authn.NewService(pool, authn.DevDirectory(), log, time.Hour, ""),
		Roles:     roles,
	}))
	t.Cleanup(ts.Close)

	// Beat 0 — the door is locked (SPEC-020): the API is 401 until a real
	// login against the fake directory mints a session; the cookie jar then
	// carries it through every later beat, exactly like the browser. No
	// test-only bypass here — the canary covers authn end to end.
	c := newAPIClient(t, ts)
	locked, err := c.get("/api/instances")
	require.NoError(t, err)
	require.NoError(t, locked.Body.Close())
	require.Equal(t, http.StatusUnauthorized, locked.StatusCode, "API must be locked before login")

	loginResp, err := c.post("/api/auth/login", `{"username":"dba1","password":"dba1"}`)
	require.NoError(t, err)
	require.NoError(t, loginResp.Body.Close())
	require.Equal(t, http.StatusOK, loginResp.StatusCode)

	// Beat 1 — import the estate, twice: the second pass must be a no-op
	// (idempotent natural-key import, SPEC-010).
	rep := importFixture(t, ctx, pool)
	require.Equal(t, 8, rep.New)
	require.Zero(t, rep.Quarantined)
	rep = importFixture(t, ctx, pool)
	require.Zero(t, rep.New)
	require.Equal(t, 8, rep.Unchanged)

	// Beat 2 — the fleet is visible, env filter is server-side.
	require.Len(t, listInstances(t, c, ""), 8)
	require.Len(t, listInstances(t, c, "?env=test"), 3)

	// Beat 3+4 — dump on a TEST instance through the API, watch it succeed
	// with artifact metadata, routed to the nonprod adapter.
	run := startRun(t, c, "billing-test")
	run = waitTerminal(t, c, run.ID)
	require.Equal(t, "success", run.State)
	require.NotNil(t, run.JobID)
	require.True(t, strings.HasPrefix(*run.JobID, "mock-nonprod-"), "job %q not on nonprod engine", *run.JobID)
	require.NotNil(t, run.Artifact)
	require.NotEmpty(t, run.Artifact.Checksum)
	requireAudit(t, ctx, pool, run.ID, "dba1", "test", "success")

	// The fleet view reflects the backup (WU-011R): billing-test now carries
	// last_backup_at; untouched instances stay null.
	for _, raw := range listInstances(t, c, "") {
		var in struct {
			Name         string  `json:"name"`
			LastBackupAt *string `json:"last_backup_at"`
		}
		require.NoError(t, json.Unmarshal(raw, &in))
		if in.Name == "billing-test" {
			require.NotNil(t, in.LastBackupAt, "successful dump must surface as last_backup_at")
		} else {
			require.Nil(t, in.LastBackupAt, "%s was never dumped", in.Name)
		}
	}

	// The dump is registered (SPEC-030, WU-030): a first-class registry row
	// keyed to its origin run — what WU-031's restore drawer will feed from.
	arts := listArtifacts(t, c, "billing-test")
	require.Len(t, arts, 1)
	require.Equal(t, run.ID, arts[0].RunID, "registry row carries the origin run FK")
	require.Equal(t, run.Artifact.Checksum, arts[0].Checksum)
	require.Equal(t, "standard", arts[0].RetentionClass)

	// Beat 4 — live logs over SSE: replay of the finished job, then exactly
	// one `end` event (SPEC-013).
	sse := readLogStream(t, c, run.ID)
	require.Contains(t, sse, "event: log")
	require.Contains(t, sse, "PLAY RECAP")
	require.Equal(t, 1, strings.Count(sse, "event: end"))
	require.Contains(t, sse, `"state":"success"`)

	// Beat 5 — a failing run leaves a complete audit trail and mails the
	// DBA list. Failure injection rides the engineParams seam (tests only;
	// the API passes nil — SPEC-012), everything downstream is production
	// code: watcher, finalize, audit append, post-commit mail.
	failRun, err := svc.Start(ctx, runs.StartRequest{
		Actor: "dba1", Instance: "crm-test", Operation: "dump",
		EngineParams: map[string]string{"mock_fail_at": "1"},
	})
	require.NoError(t, err)
	failRun = waitTerminal(t, c, failRun.ID)
	require.Equal(t, "failed", failRun.State)
	requireAudit(t, ctx, pool, failRun.ID, "dba1", "test", "failed")

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

	// Beat 6 — PROD: the ritual is server-side since WU-021 (SPEC-021
	// mini-ADR 6) — no confirm means no run, no rows; then the confirmed
	// launch rides different engine credentials (layer 3) with env stamped
	// 'prod' in every audit row (layer 4).
	refused, err := c.post("/api/runs", `{"instance": "billing-prod", "operation": "dump"}`)
	require.NoError(t, err)
	require.NoError(t, refused.Body.Close())
	require.Equal(t, http.StatusBadRequest, refused.StatusCode,
		"prod launch without the typed-name confirm must be refused")

	prodRun := startRun(t, c, "billing-prod")
	prodRun = waitTerminal(t, c, prodRun.ID)
	require.Equal(t, "success", prodRun.State)
	require.NotNil(t, prodRun.JobID)
	require.True(t, strings.HasPrefix(*prodRun.JobID, "mock-prod-"), "job %q not on prod engine", *prodRun.JobID)
	require.Equal(t, "dba1", prodRun.RequestedBy, "the run carries the AD identity that launched it")
	requireAudit(t, ctx, pool, prodRun.ID, "dba1", "prod", "success")

	// Beat 7 — the trail answers "who": the requester filter finds dba1's
	// runs and an unknown username matches nothing (SPEC-021).
	require.NotEmpty(t, listRuns(t, c, "?requested_by=dba1"))
	require.Empty(t, listRuns(t, c, "?requested_by=nobody"))

	// Beat 8 — the scheduler (SPEC-022, M2 exit): a prod schedule demands
	// the same typed-name ritual at creation; a created schedule, once due,
	// is fired by the live executor loop through the identical audit path,
	// attributed schedule:dba1.
	refusedSched, err := c.post("/api/schedules",
		`{"instance": "billing-prod", "operation": "dump", "cron_spec": "@daily"}`)
	require.NoError(t, err)
	require.NoError(t, refusedSched.Body.Close())
	require.Equal(t, http.StatusBadRequest, refusedSched.StatusCode,
		"prod schedule without the typed-name confirm must be refused")

	createdResp, err := c.post("/api/schedules",
		`{"instance": "billing-test", "operation": "dump", "cron_spec": "@daily", "reason": "nightly"}`)
	require.NoError(t, err)
	var created schedule.Schedule
	require.NoError(t, json.NewDecoder(createdResp.Body).Decode(&created))
	require.NoError(t, createdResp.Body.Close())
	require.Equal(t, http.StatusCreated, createdResp.StatusCode)
	require.Equal(t, "dba1", created.CreatedBy, "the creator owns the schedule")
	require.NotNil(t, created.NextFireAt)

	// Make it due (the misfire shape) and let the loop catch up.
	_, err = pool.Exec(ctx,
		`UPDATE schedule SET next_fire_at = now() - interval '1 hour' WHERE id = $1`, created.ID)
	require.NoError(t, err)
	var schedRun runs.Run
	require.Eventually(t, func() bool {
		fired := listRuns(t, c, "?requested_by=schedule:dba1")
		if len(fired) != 1 {
			return false
		}
		schedRun = fired[0]
		return true
	}, 10*time.Second, 10*time.Millisecond, "the executor must fire the due schedule")
	schedRun = waitTerminal(t, c, schedRun.ID)
	require.Equal(t, "success", schedRun.State)
	require.Equal(t, "schedule:dba1", schedRun.RequestedBy)
	requireAudit(t, ctx, pool, schedRun.ID, "schedule:dba1", "test", "success")

	// Scheduled dumps register too (SPEC-030 behavior 1) — both launch
	// paths share finalize, so billing-test now lists two, newest first.
	schedArts := listArtifacts(t, c, "billing-test")
	require.Len(t, schedArts, 2)
	require.Equal(t, schedRun.ID, schedArts[0].RunID, "the scheduled dump's artifact leads")

	// Beat 9 — the chain engine (SPEC-032, the ROADMAP M3 drill at the HTTP
	// seam): a portal-assembled 3-step chain halts on an injected step-2
	// failure with exactly ONE mail at chain granularity, then a DBA resume
	// over the API re-fires the failed step as a NEW run and carries the
	// chain to success. Chains have no client-facing create (031's restore
	// POST is the first assembler), so creation rides the service like Beat
	// 5's failure injection; everything after is production HTTP.
	drill, err := chainSvc.Create(ctx, chain.CreateRequest{
		Kind: "test-chain", Instance: "hr-test", Actor: "dba1", Reason: "drill",
		Steps: []chain.StepSpec{
			{Operation: "dump"},
			{Operation: "dump", Params: map[string]string{"mock_fail_at": "1"}},
			{Operation: "dump"},
		},
	})
	require.NoError(t, err)

	var halted chain.Chain
	require.Eventually(t, func() bool {
		halted, err = chainSvc.Get(ctx, drill.ID)
		require.NoError(t, err)
		return halted.State == "halted"
	}, 10*time.Second, 5*time.Millisecond, "the injected failure must halt the chain")
	chainSvc.Wait() // halt-mail goroutine settles before asserting on it

	require.Equal(t, "success", halted.Steps[0].Status, "step 1 stands")
	require.Equal(t, "failed", halted.Steps[1].Status)
	require.Equal(t, "pending", halted.Steps[2].Status, "step 3 never fired")
	failedStepRun := *halted.Steps[1].RunID

	// The strip lookup answers over HTTP from any step run.
	stripResp, err := c.get(fmt.Sprintf("/api/runs/%d/chain", failedStepRun))
	require.NoError(t, err)
	var strip chain.Chain
	require.NoError(t, json.NewDecoder(stripResp.Body).Decode(&strip))
	require.NoError(t, stripResp.Body.Close())
	require.Equal(t, http.StatusOK, stripResp.StatusCode)
	require.Equal(t, drill.ID, strip.ID)
	require.Equal(t, "halted", strip.State)

	// Exactly ONE mail, the chain's — the step run's own failure mail is
	// suppressed by the filter (mini-ADR 5).
	select {
	case msg = <-mail:
	case <-time.After(5 * time.Second):
		t.Fatal("no chain halt mail arrived")
	}
	require.Contains(t, msg.Data,
		fmt.Sprintf("Subject: [db-portal] CHAIN-%d halted — test-chain on hr-test (test)", drill.ID))
	require.Contains(t, msg.Data, fmt.Sprintf("http://portal.local:8080/runs/%d", failedStepRun))
	select {
	case extra := <-mail:
		t.Fatalf("halt must mail exactly once, got a second: %.120s", extra.Data)
	default:
	}

	// The human fixes the cause, then resumes over the API as themselves.
	_, err = pool.Exec(ctx,
		`UPDATE chain_step SET params = '{}' WHERE chain_id = $1 AND seq = 2`, drill.ID)
	require.NoError(t, err)
	resumeResp, err := c.post(fmt.Sprintf("/api/chains/%d/resume", drill.ID), "")
	require.NoError(t, err)
	require.NoError(t, resumeResp.Body.Close())
	require.Equal(t, http.StatusOK, resumeResp.StatusCode)

	var done chain.Chain
	require.Eventually(t, func() bool {
		done, err = chainSvc.Get(ctx, drill.ID)
		require.NoError(t, err)
		return done.State == "success"
	}, 10*time.Second, 5*time.Millisecond, "the resumed chain must finish")

	// The failed step re-fired as a NEW run (history kept: the superseded
	// run survives, still failed); every step run is fully audited through
	// Start, attributed chain:dba1 — and steps ARE runs, so Activity's
	// requester filter finds the whole drill.
	require.NotEqual(t, failedStepRun, *done.Steps[1].RunID)
	requireAudit(t, ctx, pool, failedStepRun, "chain:dba1", "test", "failed")
	for _, st := range done.Steps {
		requireAudit(t, ctx, pool, *st.RunID, "chain:dba1", "test", "success")
	}
	require.Len(t, listRuns(t, c, "?requested_by=chain:dba1"), 4,
		"three steps + the superseded attempt, all visible as runs")

	// Beat 10 — restore (SPEC-031, D4's second catalog operation): the first
	// production chain assembler. POST /api/restore of a registered artifact
	// onto an explicit target assembles the fixed verify → safety_dump →
	// restore chain (the safety dump unconditional — the incident D1 kills),
	// drives it to success over MockEngine, and ties the artifact lineage
	// through. A non-prod target stays one click (D2). Then the incident
	// itself: an injected verify failure halts BEFORE the safety dump, so the
	// target is never touched — resumable after the "fix".
	src := listArtifacts(t, c, "billing-test")[0] // newest registered dump
	restoreResp, err := c.post("/api/restore", fmt.Sprintf(
		`{"artifact_id": %d, "target": "crm-test", "reason": "golden restore"}`, src.ID))
	require.NoError(t, err)
	var restoreChain chain.Chain
	require.NoError(t, json.NewDecoder(restoreResp.Body).Decode(&restoreChain))
	require.NoError(t, restoreResp.Body.Close())
	require.Equal(t, http.StatusCreated, restoreResp.StatusCode, "non-prod restore is one click")
	require.Equal(t, restore.Kind, restoreChain.Kind)
	require.Equal(t, "crm-test", restoreChain.Instance)

	var restored chain.Chain
	require.Eventually(t, func() bool {
		restored, err = chainSvc.Get(ctx, restoreChain.ID)
		require.NoError(t, err)
		return restored.State == "success"
	}, 10*time.Second, 5*time.Millisecond, "the restore chain must finish")

	require.Len(t, restored.Steps, 3)
	require.Equal(t, "verify", restored.Steps[0].Operation)
	require.Equal(t, "safety_dump", restored.Steps[1].Operation)
	require.Equal(t, "restore", restored.Steps[2].Operation)
	for _, st := range restored.Steps {
		require.Equal(t, "success", st.Status)
		requireAudit(t, ctx, pool, *st.RunID, "chain:dba1", "test", "success")
	}

	// The safety dump registered exactly ONE artifact for the target, class
	// 'safety' (mini-ADR 2) — while verify and restore registered nothing.
	crmArts := listArtifacts(t, c, "crm-test")
	require.Len(t, crmArts, 1, "only the safety dump registers on the target")
	require.Equal(t, "safety", crmArts[0].RetentionClass)
	require.Equal(t, *restored.Steps[1].RunID, crmArts[0].RunID, "the safety_dump step's run owns it")

	// The restore step's params reference the source artifact id + checksum —
	// lineage tied, no secrets (SPEC-032).
	var restoreParams []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT params FROM chain_step WHERE chain_id = $1 AND operation = 'restore'`,
		restoreChain.ID).Scan(&restoreParams))
	var lineage map[string]string
	require.NoError(t, json.Unmarshal(restoreParams, &lineage))
	require.Equal(t, strconv.FormatInt(src.ID, 10), lineage["artifact_id"])
	require.Equal(t, src.Checksum, lineage["checksum"])

	// A safety dump is NOT the target's backup: last_backup_at (dump only)
	// still ignores it (mini-ADR 2 consequence, behavior 6).
	var lastBackup *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT (SELECT max(r.finished_at) FROM run r
		        WHERE r.instance_id = i.id AND r.operation = 'dump' AND r.state = 'success')
		FROM instance i WHERE i.name = 'crm-test'`).Scan(&lastBackup))
	require.Nil(t, lastBackup, "the safety dump must not count as crm-test's backup")

	// The incident, institutionalized: a bad artifact fails verify (step 1),
	// which halts the chain BEFORE the safety dump or restore run — the target
	// is never dumped or restored. mock_fail_at stands in for the checksum
	// mismatch until WU-036 verifies real bytes.
	badSteps := restore.Steps(src.ID, src.Checksum, src.Name)
	badSteps[0].Params["mock_fail_at"] = "1" // verify step fails
	badChain, err := chainSvc.Create(ctx, chain.CreateRequest{
		Kind: restore.Kind, Instance: "hr-test", Actor: "dba1", Reason: "bad restore",
		Steps: badSteps,
	})
	require.NoError(t, err)

	var haltedRestore chain.Chain
	require.Eventually(t, func() bool {
		haltedRestore, err = chainSvc.Get(ctx, badChain.ID)
		require.NoError(t, err)
		return haltedRestore.State == "halted"
	}, 10*time.Second, 5*time.Millisecond, "the failed verify must halt the chain")
	chainSvc.Wait() // halt-mail goroutine settles

	require.Equal(t, "failed", haltedRestore.Steps[0].Status, "verify failed")
	require.Equal(t, "pending", haltedRestore.Steps[1].Status, "the safety dump never fired")
	require.Equal(t, "pending", haltedRestore.Steps[2].Status, "the restore never fired")
	require.Nil(t, haltedRestore.Steps[1].RunID, "no safety_dump run exists")
	require.Nil(t, haltedRestore.Steps[2].RunID, "no restore run exists")

	// Zero runs on the target for the target-mutating steps — the whole point.
	var targetRuns int
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT count(*) FROM run r JOIN instance i ON i.id = r.instance_id
		WHERE i.name = 'hr-test' AND r.operation IN ('safety_dump', 'restore')`).Scan(&targetRuns))
	require.Zero(t, targetRuns, "verify-fail leaves the target untouched: no safety_dump, no restore")

	// Exactly one halt mail — the chain's (the failed verify's run mail is
	// suppressed by the StepRunFilter).
	select {
	case msg = <-mail:
	case <-time.After(5 * time.Second):
		t.Fatal("no restore halt mail arrived")
	}
	require.Contains(t, msg.Data,
		fmt.Sprintf("Subject: [db-portal] CHAIN-%d halted — restore on hr-test (test)", badChain.ID))
	select {
	case extra := <-mail:
		t.Fatalf("restore halt must mail exactly once, got a second: %.120s", extra.Data)
	default:
	}

	// The operator fixes the artifact (drop the injected mismatch) and resumes
	// over the API: verify re-fires and passes, and the chain completes — the
	// safety dump and restore now run.
	_, err = pool.Exec(ctx,
		`UPDATE chain_step SET params = params - 'mock_fail_at' WHERE chain_id = $1 AND seq = 1`, badChain.ID)
	require.NoError(t, err)
	fixedResp, err := c.post(fmt.Sprintf("/api/chains/%d/resume", badChain.ID), "")
	require.NoError(t, err)
	require.NoError(t, fixedResp.Body.Close())
	require.Equal(t, http.StatusOK, fixedResp.StatusCode)

	var fixed chain.Chain
	require.Eventually(t, func() bool {
		fixed, err = chainSvc.Get(ctx, badChain.ID)
		require.NoError(t, err)
		return fixed.State == "success"
	}, 10*time.Second, 5*time.Millisecond, "the resumed restore must finish")
	for _, st := range fixed.Steps {
		require.Equal(t, "success", st.Status)
	}
	// Only after the fix does the target get its safety dump — registered on
	// the safety_dump step's own run, class 'safety'.
	var safetyClass string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT retention_class FROM artifact WHERE run_id = $1`, *fixed.Steps[1].RunID).Scan(&safetyClass))
	require.Equal(t, "safety", safetyClass)
}

// apiClient is the authenticated client every HTTP beat runs through: a
// cookie jar carries the session minted by the login beat.
type apiClient struct {
	base string
	http *http.Client
}

func newAPIClient(t *testing.T, ts *httptest.Server) *apiClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &apiClient{base: ts.URL, http: &http.Client{Jar: jar}}
}

func (c *apiClient) get(path string) (*http.Response, error) {
	return c.http.Get(c.base + path)
}

func (c *apiClient) post(path, body string) (*http.Response, error) {
	return c.http.Post(c.base+path, "application/json", strings.NewReader(body))
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

func listInstances(t *testing.T, c *apiClient, query string) []json.RawMessage {
	t.Helper()
	resp, err := c.get("/api/instances" + query)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Instances []json.RawMessage `json:"instances"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body.Instances
}

func startRun(t *testing.T, c *apiClient, instance string) runs.Run {
	t.Helper()
	// confirm mirrors the drawer's typed-name ritual; non-prod ignores it.
	payload := fmt.Sprintf(`{"instance": %q, "operation": "dump", "confirm": %q}`, instance, instance)
	resp, err := c.post("/api/runs", payload)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var run runs.Run
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&run))
	return run
}

func listArtifacts(t *testing.T, c *apiClient, instance string) []runs.RegisteredArtifact {
	t.Helper()
	resp, err := c.get("/api/artifacts?instance=" + instance)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Artifacts []runs.RegisteredArtifact `json:"artifacts"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body.Artifacts
}

func listRuns(t *testing.T, c *apiClient, query string) []runs.Run {
	t.Helper()
	resp, err := c.get("/api/runs" + query)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body struct {
		Runs []runs.Run `json:"runs"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body.Runs
}

func waitTerminal(t *testing.T, c *apiClient, id int64) runs.Run {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := c.get(fmt.Sprintf("/api/runs/%d", id))
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

func readLogStream(t *testing.T, c *apiClient, id int64) string {
	t.Helper()
	resp, err := c.get(fmt.Sprintf("/api/runs/%d/logs", id))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(resp.Body) // stream closes itself after the `end` event
	require.NoError(t, err)
	return string(body)
}

// requireAudit pins guardrail layer 4 (SPEC-015): exactly one submitted +
// one finished audit event per run, environment stamped on both — and,
// since WU-021, the requesting identity as actor on both (SPEC-021).
func requireAudit(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID int64, actor, env, finalStatus string) {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT actor, action, environment, COALESCE(final_status, ''), COALESCE(job_id, '')
		FROM audit_event WHERE run_id = $1 ORDER BY id`, runID)
	require.NoError(t, err)
	defer rows.Close()

	type event struct{ actor, action, env, final string }
	var events []event
	var jobIDs []string
	for rows.Next() {
		var e event
		var jobID string
		require.NoError(t, rows.Scan(&e.actor, &e.action, &e.env, &e.final, &jobID))
		events = append(events, e)
		jobIDs = append(jobIDs, jobID)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []event{
		{actor, "run.submitted", env, ""},
		{actor, "run.finished", env, finalStatus},
	}, events)
	// job_id (0004): NULL at submit — the engine id doesn't exist yet — and
	// stamped on finished, anchoring the run<->engine-job linkage immutably.
	require.Empty(t, jobIDs[0])
	require.NotEmpty(t, jobIDs[1], "run.finished must carry the engine job_id")
}
