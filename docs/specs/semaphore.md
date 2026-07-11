# SPEC-033 · SemaphoreAdapter (real engine, opt-in)

> Groomed 2026-07-11 for WU-033 (Phase 3). Authority chain: ADR-002 (engine
> seam: one Adapter, per-env registry; MockEngine is the forever default for
> dev + ALL tests; Semaphore lands here) → ARCHITECTURE §2 (ExecutionAdapter:
> start/status/stream/cancel; status via webhook with POLLING FALLBACK; prod
> and nonprod NEVER share credentials = guardrail layer 3) → this file. Rides
> SPEC-012 (runs.Service ↔ Adapter contract: Start → recordJobID → watcher
> polls Status to terminal → finalize; single-finalizer guard WU-016;
> SweepOrphans on boot).

## Scope

Make the SAME portal drive a REAL execution engine (Semaphore) for the
non-prod env class in dev, behind the existing `engine.Adapter` seam — no
portal code above the seam changes. This WU delivers:

1. A compose `semaphore` service (BoltDB dialect — dev simplicity) + a
   Semaphore-servable `playbooks/` repo layout with a dependency-free
   `smoke.yml` (echo/sleep) that proves the adapter end to end WITHOUT a real
   dump playbook (that is WU-034).
2. `internal/engine/semaphore.go` — the `engine.Adapter` over Semaphore's
   REST API: `StartJob` (create a task on the template mapped to the playbook
   tag), `Status` (GET the task), `StreamLogs` (poll task output, replay then
   follow), `Cancel` (stop the task).
3. A webhook receiver that ACCELERATES finalization (terminal status without
   waiting a poll interval), with POLL AS THE FALLBACK TRUTH (ADR-002):
   secret-authenticated, and it never asserts state — it triggers a re-poll.
4. Registry wiring: `PORTAL_ENGINE_NONPROD=mock|semaphore` opt-in; prod stays
   mock in dev; disjoint per-class config makes guardrail 3 structural.
5. Tests: ALL keep MockEngine; ONE skip-gated integration test drives real
   Semaphore and skips clean without the compose service.

**Explicitly NOT here:** the real dump playbook / real pg_dump (WU-034); object
storage / artifact bytes (WU-035); prod-class Semaphore or any deploy wiring
(post-MVP — prod stays mock in dev); Semaphore inventory/SSH/vault management
beyond what `smoke.yml` needs; multi-runner or HA Semaphore.

## Mini-ADRs (agent decisions, revisitable)

1. **JobID = the Semaphore task id; the adapter holds NO job state.** Semaphore
   persists tasks server-side, so a task id is durable across a portal restart
   — every `Status`/`StreamLogs`/`Cancel` resolves it by querying Semaphore,
   never in-process memory. This satisfies `engine.go`'s "a JobID must never
   alias across adapter instances" for free (the id namespace is Semaphore's,
   not this process's), and makes orphan recovery fall out: a task id Semaphore
   no longer knows → `ErrUnknownJob`, exactly the mock contract SweepOrphans
   already handles. (The mock is in-memory and loses jobs on restart; the real
   adapter is BETTER here, not worse — no new runs.Service code for it.)

2. **Poll is the only finalizer; the webhook merely triggers a re-poll — it
   never asserts state.** runs.Service's watcher already polls `Status` to
   terminal then `finalize`s under the WU-016 single-finalizer guard (a losing
   finalizer affects 0 rows). So the webhook receiver does NOT write terminal
   state from its payload: it authenticates, maps the payload's task id → run,
   and runs ONE ordinary `Status`→`finalize` for that run — concurrently with
   the periodic watcher, made safe by the same guard (whoever wins finalizes;
   the other is a no-op). Consequences: (a) "webhook down → poll still
   finalizes" is the DEFAULT, not a special path — the fallback test just omits
   the webhook and asserts the periodic poll finalizes; (b) a request that
   clears the secret bar still can't force a wrong outcome — it only causes a
   re-poll of the REAL task, whose Semaphore status is authoritative (defense
   in depth); (c) exactly-once and the artifact-registration tx (SPEC-030) are
   untouched. Seam: runs.Service gains one method, `ReconcileByJobID(ctx,
   jobID)` (find the run by its `job_id`, do one Status→finalize) — finalize-
   adjacent, so ARCHITECT-implemented; SweepOrphans is the precedent.

3. **Playbook tag → Semaphore template id is CONFIG, fail-closed.**
   `StartJob(template, …)` receives the playbook tag (`"dump"`, `"smoke"`) —
   the portal's identifier. Semaphore runs numeric template ids. A config map
   (`PORTAL_SEMAPHORE_TEMPLATES`, e.g. `dump:3,smoke:1`) resolves tag → id; an
   UNMAPPED tag fails `StartJob` (fail closed — a mis-mapped op must not run
   the wrong template). Playbook identity stays portal-side; the number is a
   deployment fact, not code.

4. **StreamLogs = poll Semaphore task output incrementally, replay then
   follow.** Fetch all output so far (replay), then poll at
   `PORTAL_SEMAPHORE_POLL_INTERVAL` cadence tracking the last-seen line offset,
   emitting only NEW `LogLine`s; close the channel when `Status` is terminal or
   ctx is cancelled. This is the mock's StreamLogs contract verbatim (replay →
   follow → one close), so RunDetail's SSE endpoint (WU-013) and its
   replay-on-reconnect logic need zero changes.

5. **The webhook route is secret-authenticated, fail-closed, and the only
   session-less mutating-ish endpoint.** `POST /api/engine/semaphore/webhook`,
   registered OUTSIDE `requireSession` (Semaphore is not a browser), guarded by
   a shared secret (`PORTAL_SEMAPHORE_WEBHOOK_SECRET`) compared in CONSTANT time
   (`hmac.Equal`); a missing/wrong secret → 401 + ZERO state change + an
   auth_event? No — it's a machine endpoint; log at Warn, no audit row (audit
   is run/authz-centric). Body-capped like every POST. Because the handler only
   ever triggers a re-poll of a run it can name (mini-ADR 2), it writes nothing
   from the body — no injection surface. Disabled (empty secret) → the route
   still exists but 401s everything (poll-only operation, the slice-(a) mode).

6. **Opt-in per class; disjoint config objects = guardrail 3, structural.**
   `PORTAL_ENGINE_NONPROD=mock` (default) `|semaphore`. `semaphore` → main
   builds a `SemaphoreAdapter` from the NON-PROD semaphore config (URL, api
   token, webhook secret, template map, poll interval) and registers it for
   `ClassNonProd`; `ClassProd` stays a MockEngine in dev. The two adapters are
   distinct instances with disjoint config; `Registry.Register` panics if one
   instance is shared across classes — so a wiring bug that crosses prod/nonprod
   fails at BOOT, before serving, not as a mis-routed prod job. An unknown
   `PORTAL_ENGINE_NONPROD` value → boot error (fail closed, mirrors AuthMode).

7. **Compose Semaphore uses the BoltDB dialect; admin creds + api token in
   `.env` only (ADR-004).** No external DB for Semaphore in dev — embedded
   BoltDB, simplest to stand up and reset. Admin user/password, the api token,
   and the webhook secret live in `.env` (gitignored); `.env.example` documents
   SHAPE only. Bound to `127.0.0.1` like postgres/mailpit. `docker compose up`
   gains the service; a fresh clone needs the Semaphore admin bootstrap
   documented in the compose comments / a make target.

8. **ALL tests keep MockEngine; ONE skip-gated integration test drives real
   Semaphore (ADR-002).** `internal/engine/semaphore_itest_test.go` (or a
   `//go:build` tag) drives a live compose Semaphore: `StartJob(smoke)` →
   `Status` to `success`, `StreamLogs` replay+follow, `Cancel` mid-run, and (in
   slice b) a webhook round-trip + a webhook-absent fallback. It SKIPS cleanly
   when the Semaphore endpoint is unreachable — the `testutil.MigratedDB`
   skip-without-compose pattern. `npm run check` stays green with ZERO
   Semaphore dependence.

## Interfaces

- `config.Config` gains a disjoint non-prod engine block:
  `EngineNonProd string` (`PORTAL_ENGINE_NONPROD`, default `mock`),
  `SemaphoreURL`, `SemaphoreAPIToken`, `SemaphoreWebhookSecret`,
  `SemaphoreTemplates string` (parsed `tag:id,…` → `map[string]int`),
  `SemaphorePollInterval time.Duration` (default e.g. 3s). A helper
  `SemaphoreConfig()` builds the typed adapter config; unknown `EngineNonProd`
  or unparsable templates → error at Load/build (fail closed).
- `engine.NewSemaphoreAdapter(cfg SemaphoreConfig, log) *SemaphoreAdapter`
  implementing `engine.Adapter`. Pointer-shaped (Registry compares by identity).
  `Status` maps Semaphore task status → `JobState`; unknown task → `ErrUnknownJob`.
- `runs.Service.ReconcileByJobID(ctx, jobID string) error` — find the run by
  `job_id`, run one `Status`→`finalize`; no-op (no error) if the run is already
  terminal or the job is unknown to the adapter. The webhook handler’s only
  entry into runs.
- `main.go` (`backend/cmd/portal`): the `ClassNonProd` registration (line 86)
  becomes conditional on `cfg.EngineNonProd`; `ClassProd` unchanged (mock).
- `server.go`: new route `POST /api/engine/semaphore/webhook` OUTSIDE the
  session-gated group, wired to a `webhookSemaphore(secret, runs)` handler.
- `infra/compose.yaml`: `semaphore` service (BoltDB, 127.0.0.1 bind, env creds).
- `playbooks/`: Semaphore repo layout + `smoke.yml` (echo/sleep, no deps).

## Behavior (testable)

1. **Poll-only lifecycle (slice a):** `StartJob("smoke")` → a task id; `Status`
   transitions queued→running→success; `StreamLogs` replays the smoke output
   then closes on terminal; `Cancel` moves a running task to `canceled`. Driven
   live in the skip-gated itest; unit-tested against a stub Semaphore HTTP
   server for status/output/cancel mapping + `ErrUnknownJob`.
2. **Template mapping fail-closed:** `StartJob("no-such-tag")` → error, no task
   created; a mapped tag hits the configured template id (asserted on the stub).
3. **Webhook accelerates, poll is the truth (slice b):** a valid-secret webhook
   naming a finished task finalizes the run WITHOUT waiting the poll interval;
   with the webhook receiver disabled/omitted, the periodic poll still
   finalizes (the ADR-002 fallback) — same run, same terminal state, same
   single audit shape. A webhook whose payload LIES about status can’t change
   the outcome (the handler re-polls the real task).
4. **Webhook auth:** missing/empty/wrong secret → 401, zero state change, no
   run touched; oversized body → 413.
5. **Guardrail 3 structural:** registering the semaphore adapter under both
   classes panics (existing Registry test extended); `PORTAL_ENGINE_NONPROD`
   unknown → boot error.
6. **Audit parity:** a Semaphore run’s audit trail (submitted/finished, actor,
   env, job_id) is byte-shape-identical to a mock run’s — the seam guarantees
   it, asserted by reusing the runs audit assertions against a stubbed adapter.

## Guardrails & audit

Everything above the seam is unchanged, so all four guardrail layers, the prod
ritual, authz, windows, and the audit trail apply to Semaphore runs exactly as
to mock runs (that is the point of ADR-002). Layer 3 (disjoint prod/nonprod
engine credentials) is enforced structurally by the Registry + disjoint config
(mini-ADR 6). The webhook endpoint holds no session and writes nothing from its
payload; the shared secret is the only guard and it fails closed (mini-ADR 5).
No secrets (api token, admin password, webhook secret) enter the repo, the
portal DB, logs, or step params — `.env` only (ADR-004); `.env.example` shape
only. Poll remains the finalization authority, so exactly-once (WU-016) and the
artifact tx (SPEC-030) are untouched.

## Split (as sized in BACKLOG)

- **Slice (a) — compose + adapter, poll-only. CHECKPOINT.** compose semaphore
  service; `playbooks/` + `smoke.yml`; `semaphore.go` StartJob/Status/Cancel +
  poll-based StreamLogs; config + main opt-in wiring; unit tests over a stub
  HTTP server + the skip-gated itest (poll-only). No webhook yet.
- **Slice (b) — webhook + streaming polish + live drill.** webhook route +
  secret auth + `ReconcileByJobID`; the webhook-down fallback test; extend the
  itest with a webhook round-trip; the live VM drill (button-dump on `smoke`
  through real Semaphore: queued→running→success, logs stream into RunDetail,
  cancel mid-run, audit parity).

Delegation: compose + `playbooks/`/`smoke.yml` scaffold = Sonnet-brief
candidate; the adapter HTTP client, webhook auth, and `ReconcileByJobID` =
architect (external integration + a session-less endpoint + finalize-adjacent).

## Out of scope / deferred

- Real dump playbook + pg_dump + real artifact bytes → WU-034.
- Object storage / `location` → WU-035.
- Prod-class Semaphore, deploy/runner topology, Semaphore HA → post-MVP.
- Semaphore-side inventory/SSH-key/vault automation beyond `smoke.yml` needs.
- Webhook payload signing beyond a shared secret (HMAC-of-body) → icebox.

## Open questions

- **Semaphore REST specifics** (task-create payload, status enum, output
  pagination, cancel verb) are pinned during slice (a) against the running
  BoltDB service — the interface above is stable regardless. Owner: agent.
- **Fresh-clone Semaphore bootstrap** (admin user + api token + template +
  repo/inventory rows): scripted vs documented-manual is decided in slice (a)
  when the compose service is live. None blocking. Owner: agent.
