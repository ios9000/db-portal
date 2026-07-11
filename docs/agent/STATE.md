# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **WU-033 (SemaphoreAdapter, M) IN PROGRESS
  (s18)**: SPEC-033 written (docs/specs/semaphore.md); **slice (a) — compose
  Semaphore + poll-only adapter — is the next action.** WU-031 done s17
  (f2dcf2d + 270e665, live-drilled 98fc2a8). Execution order 030 → 032 → 031 →
  033 → 034 → 035 → 036.
- **Status (s18, WU-033 spec):** SPEC-033 = docs/specs/semaphore.md (8
  mini-ADRs). ADR-002's payoff — the SAME portal drives REAL Semaphore for the
  NON-PROD class, opt-in, behind the UNCHANGED engine.Adapter seam. Key
  decisions: (1) JobID = the Semaphore task id, adapter holds NO job state
  (durable across restart, ErrUnknownJob falls out — no runs.Service change);
  (2) **POLL is the only finalizer; the webhook merely triggers a re-poll and
  NEVER asserts state** — so "webhook down → poll finalizes" is the DEFAULT
  (not a special path) and a spoofed payload can't force an outcome; seam =
  new `runs.ReconcileByJobID` (find run by job_id, one Status→finalize, safe
  under the WU-016 guard); (3) playbook tag → Semaphore template id is config,
  fail-closed; (4) StreamLogs = poll output replay-then-follow, the mock
  contract verbatim (RunDetail SSE unchanged); (5) webhook route is
  session-less + shared-secret constant-time + writes NOTHING from the body,
  fails closed; (6) `PORTAL_ENGINE_NONPROD=mock|semaphore` opt-in, prod stays
  mock in dev, disjoint config = guardrail 3 structural (Registry panics on a
  shared instance); (7) compose Semaphore BoltDB dialect, creds `.env`-only
  (ADR-004); (8) ALL tests keep MockEngine + ONE skip-gated itest. Split: (a)
  compose + playbooks/smoke.yml + adapter poll-only + stub-server units +
  skip-gated itest → CHECKPOINT; (b) webhook + `ReconcileByJobID` + fallback
  test + live VM drill. Delegation: compose/playbook scaffold = Sonnet-brief
  candidate; adapter HTTP client + webhook auth + ReconcileByJobID = architect.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-033 slice (a)** — SPEC-033 is written; build the compose Semaphore
   service (BoltDB dialect, 127.0.0.1, creds `.env`-only) + `playbooks/` repo
   layout + `smoke.yml` (echo/sleep), then `internal/engine/semaphore.go`
   (StartJob/Status/Cancel + poll-based StreamLogs, JobID = task id, no job
   state), config fields (`PORTAL_ENGINE_NONPROD` + semaphore block) + main
   opt-in wiring, unit tests over a stub HTTP server + the skip-gated itest
   (poll-only). Then CHECKPOINT (commit, STATE, journal) BEFORE slice (b)
   (webhook + `ReconcileByJobID` + fallback test + live VM drill). Compose +
   playbook scaffold = Sonnet-brief candidate; adapter/webhook = architect.
   First live task: stand up the compose Semaphore, pin its REST specifics
   (task-create payload, status enum, output pagination, cancel verb) against
   the running BoltDB service — open question 1 in SPEC-033.
2. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.
3. (DONE s17) Live drive of the restore flow completed — demo rebuilt to the
   270e665 binary and driven end-to-end over HTTP (no headless browser on the
   VM, so the SPA render/click wasn't automated; every API call the drawer
   makes WAS): POST /api/restore billing-test→hr-test 201 → chain 2 drove
   verify(run25)/safety_dump(run26)/restore(run27) all to success; hr-test
   registered a 'safety' artifact (run 26); GET /api/runs/27/chain returns the
   restore chain success; prod target (billing-prod) no/wrong confirm → 400
   with zero chain rows; bare POST /api/runs{operation:restore} → 400; served
   bundle contains /api/restore + "Restore onto". A human can click the drawer
   visually at the demo.

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080. **Rebuilt s17 to the 270e665 binary (WU-031); dev DB still at 0010**
  (WU-031 adds no migration). Env `PORTAL_AUTH_MODE=fake` + demo break-glass
  hash (password "demo-glass", hash only in the unit env — recover via
  `systemctl show dbportal-demo -p Environment`; NOTE stopping the unit
  DELETES it and the env — the recipe is in JOURNAL s16; s17 reused the same
  captured hash `$2a$10$BQnBaouoM6y…olo5.`). Sign in dba1/dba1. Dev DB history
  now also includes the s17 live restore: chain 2 (restore on hr-test,
  success) with runs 25/26/27 and a 'safety' artifact on hr-test (run 26).
  Before any live check that runs its own portal: `systemctl stop
  dbportal-demo` — bind-in-use + two-portals-one-DB hazard. ALSO (bit s10
  twice): a backgrounded `portal &` may report a wrapper PID in `$!` — always
  kill the PID that `ss -ltnp` shows holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Chain engine (WU-032, SPEC-032 = docs/specs/chains.md):** chains are
  portal-assembled ONLY (no client create API; 031's restore POST is the
  first assembler). internal/chain imports catalog/runs, NEVER engine —
  steps ARE runs through runs.Service.Start (full guardrail/audit path),
  attributed `chain:<mover>` (creator, then resumer). Halt = ONE chain
  mail; run mail suppressed via StepRunFilter wired in main. Stored
  confirm replays verbatim per step (env promotion mid-chain fails the
  ritual visibly). Boot sweep AFTER runs.SweepOrphans. Step status derived
  from linked run; superseded runs keep history, leave the strip. UI
  step-status vocabulary = `pending` + run states, fixed; 031 consumes
  as-is.
- **Restore (WU-031, SPEC-031 = docs/specs/restore.md):** the FIRST chain
  assembler. `POST /api/restore {artifact_id, target, confirm, reason}` (dba)
  builds a `kind=restore` chain via the pure recipe `internal/restore.Steps` =
  `[verify, safety_dump, restore]` — the safety dump is a LITERAL in the slice,
  so "no skip affordance" is structural, not a runtime check (D1 incident). The
  ops verify/safety_dump/restore are catalog entries with `Launchable=false`;
  `runs.Start` refuses a non-launchable op unless `StartRequest.Internal` is
  set, which ONLY chain/driver.go does → bare restore over POST /api/runs or
  the scheduler = 400. `finalize` stamps `artifact.retention_class` from the
  catalog op ('safety' for safety_dump, else 'standard'); last_backup_at (dump
  only) ignores safety dumps. Prod target ritual enforced once in
  `chain.Create` before any row. 034/036 add the real verify/restore playbooks
  behind this same recipe. UI = RestoreDrawer from MyDatabases.
- **Delegation model (6 successes: WU-019, WU-020b, WU-021b, WU-022b,
  WU-032-UI, WU-031-UI):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable/Opus) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. WU-031-UI (s17,
  124k tok): reviewed line-by-line, only issue was a prettier gap the fe gate
  doesn't check but the pre-commit hook does (`npm run fmt` before committing
  delegated FE work). Workflow tool only for multi-agent pipelines (gate
  reviews). Phase 3 delegation candidates: playbook/compose scaffolds of
  033/034; chain engine core, SemaphoreAdapter concurrency, ritual/authz
  seams = architect.
  (WU-023 + WU-030 were architect-implemented end to end: S-sized, no/thin
  UI — delegation overhead exceeds the diff.)
- **Artifact registry (WU-030, SPEC-030 = docs/specs/artifacts.md):**
  `artifact` table (0009) = what can be restored; run's artifact_* columns =
  what this run produced — same values, same finalize tx (dual write,
  mini-ADR 1). Registration rides the WU-016 guarded terminal transition +
  UNIQUE(run_id); only state='success' with a non-nil engine artifact
  registers. retention_class 'standard'|'safety' stamped at registration —
  'safety' writer + Start→finalize plumbing = WU-031. `location` dormant
  until 034/035. GET /api/artifacts?instance= required-param read,
  unknown 404, newest 50, `location` not exposed.
- **Scheduler (WU-022 + WU-024 hardening, SPEC-022 = docs/specs/scheduler.md):**
  internal/schedule.Service = store + executor; tick loop (Tick 10s, Jitter
  60s defaults) fires `enabled AND next_fire_at <= now()` through
  runs.Service.Start as `schedule:<created_by>` with the STORED creation-time
  confirm (migration 0008) verbatim. next_fire_at persisted PRE-JITTERED,
  NULL iff disabled; stamp CASE-guards a concurrent disable (::timestamptz
  cast — bare param breaks pgx inference SILENTLY). Misfire = coalesced
  catch-up; overlap probe = ANY live run on the instance (manual runs block
  scheduled fires too); FireTimeout 30s; SetEnabled idempotent.
- **AuthZ (WU-021, SPEC-021):** roles in portal DB (role/user_role).
  Mutations need dba (requireRole); reads session-only. Denials =
  auth_event `authz.denied` (store failure = 500 never 403). Audit actor
  is an EXPLICIT param (StartRequest.Actor, Cancel actor arg) — never ctx
  magic; run.finished inherits submitter; cancel writes
  run.cancel_requested intent-first (3 rows on canceled runs). Prod ritual
  server-side: Confirm must equal instance name on prod. Grants at boot
  per auth mode; break-glass seeded dba in 0006.
- **AuthN (WU-020 + WU-025 honesty, SPEC-020):** every /api route needs a
  session except POST /api/auth/login; healthz + SPA public. Sessions in
  portal DB (sha256, 12h TTL); cookie portal_session httpOnly SameSite=Lax.
  PORTAL_AUTH_MODE ldap (default, fails closed) | fake (dba1/dba1,
  dba2/dba2) | off (local-dev). Login lowercases the username at the seam
  (one human = one actor; passwords NOT folded). Break-glass via
  PORTAL_BREAKGLASS_HASH, every use alarmed. auth_event append-only.
  authn.From(ctx) carries identity. Frontend: bootstrap gate (401=login,
  else retry screen), honest sign-out, /login outside Shell, api.ts
  onUnauthorized (401 only). ldap+CookieSecure=false → boot Warn.
- **Golden flow (ADR-011):** `backend/e2e/golden_flow_test.go` must stay
  green EVERY session. Beat 0 login; WU-021 beats (ritual 400, actor
  pinned, requested_by); Beat 8 (WU-022): live executor fires a due
  schedule, schedule:dba1 attribution; WU-030 asserts artifact registration
  w/ origin FK on both dump beats; Beat 9 (WU-032): 3-step chain halts on
  injected step-2 failure with ONE chain mail, HTTP resume re-fires as
  chain:dba1 to success. WU-031 extends with the restore chain.
  Human twin = docs/demo-m1.md (beats 1–7 + §8 schedules preview).
- Guardrails (WU-015, SPEC-015): EnvBanner on RunDetail + LaunchDrawer +
  schedule drawer; prod ritual = typed exact instance name, client AND
  server, launch AND schedule-create; Registry.Register panics on
  cross-class adapter sharing; env stamping pinned by tests.
- **Windows (WU-023, SPEC-023 = docs/specs/windows.md):** internal/window
  pure parser (`Day HH:MM-HH:MM`, weekly, wrap-capable, server-local —
  same clock as cron). runs.Start stamps audit_event.window_warned
  (column pre-provisioned in 0003) on run.submitted only; both launch
  paths share it. instance JSON window_state inside|outside|null is
  server-computed — the client never parses window text. D6: every
  parse/eval failure = no warning, never a block; garbage logs once per
  instance per process.
- Inventory (WU-010): cluster/instance tables (name natural key, env CHECK,
  window raw text), quarantine tables; `portal import <csv>` idempotent;
  engine.ClassForEnv = env authority. size_gb canonicalized (WU-018).
- Instance API (WU-011): GET /api/instances[?env=], detail 404 JSON;
  MyDatabases cards/table. last_backup_at = newest SUCCESSFUL dump.
- Runs (WU-012, SPEC-012): run mutable; audit_event append-only, env +
  playbook_tag stamped, job_id on run.finished only. runs.Service = ONLY
  Registry caller; single-finalizer guards incl. watcher mirror (WU-016);
  SweepOrphans best-effort on boot. POST /api/runs: 400/403/404/413/502.
- Run detail + logs (WU-013, SPEC-013): SSE replay→follow, ONE `end`,
  404/410; logs NOT persisted; cancel 202 async; client retry w/ backoff
  (WU-019).
- Notify + Activity (WU-014, SPEC-014): post-commit mail for
  failed|canceled (who/what/where/status + link ONLY). GET /api/runs
  filters ANDed (state/env/operation/instance/requested_by). Activity:
  chips + `by` filter, CSV export (formula neutralization).
- Test helpers: testutil.MigratedDB(t) scratch DB (skips w/o compose PG);
  testutil.DB(t) dev DB; testutil.FakeSMTP(t); scratchDSN(t) in db pkg
  tests (migration walks). goose down reverts ONE migration (0006 down
  caveat: fails if authz.denied rows exist — spec'd).
- Single-binary (WU-006): build:release embeds frontend/dist; degraded-mode
  healthz 503 without DB. Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail
  closed; params["mock_fail_at"] injects failure (tests only); MockConfig
  StepDelay throttles job speed (tests).
- Chassis/frontend (WU-003/004): config.Load env>file>defaults;
  server.NewRouter = test seam; react-router v7 ('react-router'); tokens
  ONLY in src/index.css; lib/api.ts = ALL failures are ApiError
  (errorDetail unwraps the {"error": ...} envelope). tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO
  Postgres service (DB tests + golden flow skip there; VM gate is the real
  gate). Deps: go-ldap/v3, x/crypto, robfig/cron/v3 (direct).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it.
- Verify-procedure gotcha (s05, s07, s10×2): pkill -f matches the tool shell;
  `$!` can be a wrapper PID. Always kill the exact PID from `ss -ltnp`.

## Checkpoint log (last 3, newest first)

- 2026-07-11 — WU-033 spec written (s18, SPEC-CHECKPOINT — implementation not
  started): SPEC-033 = docs/specs/semaphore.md (8 mini-ADRs) for the
  SemaphoreAdapter. Core design: same portal drives REAL Semaphore for nonprod,
  opt-in via PORTAL_ENGINE_NONPROD, behind the unchanged engine.Adapter seam;
  JobID = durable Semaphore task id (no in-process job state); POLL is the only
  finalizer and the webhook merely triggers a re-poll (never asserts state → an
  absent webhook still finalizes by default; seam = new runs.ReconcileByJobID);
  tag→template-id is fail-closed config; webhook route session-less +
  shared-secret constant-time; disjoint per-class config = guardrail 3
  structural; BoltDB compose dialect, creds .env-only; MockEngine stays the
  test default + ONE skip-gated itest. Split: (a) compose + poll-only adapter +
  itest → checkpoint; (b) webhook + fallback test + live drill. Docs-only, no
  gate run needed. Active → WU-033 slice (a).
- 2026-07-11 — WU-031 DONE (s17, f2dcf2d backend + 270e665 UI): restore
  workflow on MockEngine, all AC + Verify met. Backend (recovered from an
  interrupted twin — see below): internal/restore recipe, catalog
  Launchable/RetentionClass, runs.Start Internal launchable gate, finalize
  retention stamping, GetArtifact, chain driver Internal, mock `verify` +
  trimmed restore, POST /api/restore assembler, golden-flow Beat 10 (happy +
  verify-fail halt/resume). UI (6th Sonnet delegation success): RestoreDrawer
  from MyDatabases (source = entry instance; explicit target defaulting to
  source only when non-prod; TARGET EnvBanner + typed-name ritual;
  unconditional safety-dump plan preview, never a checkbox; success links to
  the first step run or /activity), api.ts fetchArtifacts/startRestore, 10
  vitest. AC-4 grep confirmed: Internal:true only in chain/driver.go, the
  recipe is the sole op source. Gate green both stacks (vitest 116/116).
  LIVE-DRILLED s17 (98fc2a8): demo rebuilt to 270e665, POST /api/restore drove
  verify→safety_dump→restore to success over real HTTP + 'safety' artifact on
  the target + prod-ritual 400s (no headless browser on VM, so SPA render not
  automated). Active → WU-033.
  RECOVERY DETAIL: the twin (ssh reset) left the backend prod code + SPEC-031
  uncommitted and never checkpointed (STATE.md said "start fresh" — stale;
  trust the tree). Reaped the idle twin (user-authorized), verified on-disk
  code matched SPEC, then wrote the ENTIRE missing test layer + Beat 10. KEY
  GOTCHA: `go build ./...` was green but the TEST TREE was red — the twin added
  interface methods (GetArtifact, Create) without stubbing them; always
  `go test -run NONE ./...` when recovering. See [[twin-session-hazard]].
- 2026-07-11 — WU-032 done (s16, e465bab + b10b960): chain engine —
  SPEC-032 (7 mini-ADRs), migration 0010, chain Service+driver+boot sweep,
  StepRunFilter halt-mail exactly-once, resume API, golden flow Beat 9,
  RunDetail chain strip (5th Sonnet delegation success). FakeSMTP
  multi-session fix. Live drill: boot sweep + mailpit mail + HTTP resume
  → run 24 as chain:dba1. Session recovered mid-WU from an ssh reset —
  tree survey found the core complete; this session added Beat 9, authz
  route pins, 0010 walk pins, ChainHalted mail test, UI slice.
  Active → WU-031 (restore workflow).
