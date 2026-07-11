# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **WU-034 (Dump playbook — real, M): slice (a) DONE
  (s19, uncommitted→committing), slice (b) NEXT.** SPEC-034 written =
  `docs/specs/dump-playbook.md`. Execution order 030 → 032 → 031 → 033 →
  **034** → 035 → 036.
- **Status (s19, WU-034 slice a):** the real pg_dump path is PROVEN at the
  Semaphore level (no portal yet). Delivered + live-verified: compose
  `pgtarget` (postgres:16, seeded widget/ledger/ledger_totals via
  `infra/fixtures/pgtarget-init.sql`, host 127.0.0.1:5433); a CUSTOM Semaphore
  runner image (`infra/semaphore.Dockerfile` = v2.17.39 + `postgresql16-client`
  + writable `/artifacts` owned 1001:0) — compose `semaphore` now `build:`s it,
  tag `dbportal-semaphore:v2.17.39-pg16`, BoltDB state survived the swap;
  shared `artifacts` named volume at `/artifacts`; `playbooks/dump.yml`
  (`pg_dump --format=custom --no-owner --no-privileges`, creds via engine-side
  libpq env — NO secret in the playbook, ADR-004; emits ONE line
  `DBPORTAL_RESULT=<base64 json {name,size_bytes,sha256,location}>`, base64 to
  survive ansible's debug-callback escaping); bootstrap extended with
  `pgtarget-env` Environment (id 3, creds from `.env`) + `dump` template
  (**id 3**). Direct Semaphore run → success, result line decodes clean,
  sha256 == `sha256sum` of the file, `pg_restore --list` OK, password absent
  from task output + repo. KEY BUG FIXED mid-slice: `now()`/`random` in
  ansible `vars:` re-evaluate lazily → pg_dump's `--file` and the later `stat`
  computed different names; `set_fact` freezes the name once.
  **Deviation from the brief:** pgtarget went in a NEW
  `infra/fixtures/dev-targets.csv` (env=dev), NOT `instances.csv` — that
  fixture is pinned to exactly 8 rows by 4 test pkgs + the golden flow
  (`require.Len(…,8)`); a 9th row reddens the suite. Import is additive.
- **Status (s18, WU-033 CLOSED):** slice (b) landed the webhook accelerator +
  `runs.ReconcileByJobID` + the fallback/auth tests + the live drill. The SAME
  portal now drives REAL Semaphore for the non-prod class, opt-in via
  `PORTAL_ENGINE_NONPROD=semaphore`, behind the UNCHANGED engine.Adapter seam.
  Webhook = `POST /api/engine/semaphore/webhook`, session-less, shared-secret
  constant-time (`hmac.Equal`, header `X-Portal-Webhook-Secret`), empty secret
  ⇒ 401-only (poll-only mode); it reads ONLY the task id and re-polls the REAL
  task — POLL is the finalization truth (ADR-002), so webhook-down still
  finalizes and a lying payload can't force an outcome. `ReconcileByJobID`
  (find run by job_id → one guarded Status→finalize) is the webhook's only
  entry into runs; no-ops on unknown/terminal/non-terminal/ErrUnknownJob. Also
  wired: when `EngineNonProd=semaphore`, the runs watcher polls at
  `PORTAL_SEMAPHORE_POLL_INTERVAL` (not the mock's tight 500ms) so a real REST
  engine isn't hammered and the webhook meaningfully accelerates.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-034 slice (b) — adapter parse + Location + finalize + tests + drill.**
   Read SPEC-034 (`docs/specs/dump-playbook.md`) mini-ADRs 5+6.
   - `engine.go`: add `Location string` to `Artifact` (mock leaves it empty —
     mock.go needs NO change, "" is the zero value).
   - `internal/engine/semaphore.go`: on `Status` mapping to `StateSuccess`,
     fetch `/output`, `parseResultLine` = regex `DBPORTAL_RESULT=([A-Za-z0-9+/=]+)`
     → base64 decode → json → `Artifact{Name,SizeBytes,Checksum,Location}`.
     No sentinel / bad b64 / bad json → nil Artifact + Warn (run still success).
     StartJob keeps ignoring params (no extra-vars in WU-034).
   - `internal/runs/service.go` finalize: the `INSERT INTO artifact` gains
     `location` = `NULLIF($N,'')` from `artifact.Location`.
   - Tests (engine-free, gate stays green): stub-HTTP result-line parse cases;
     a finalize test asserting `artifact.location` set when the (fake) adapter
     returns Location, NULL for mock. Golden flow unaffected (mock, location NULL).
   - LIVE DRILL (isolated portal, per [[live-drill-isolation]]): scratch DB +
     spare port + auth off, `PORTAL_ENGINE_NONPROD=semaphore`
     `PORTAL_SEMAPHORE_TEMPLATES=dump:3,smoke:1`, token from `.env`. Import
     `infra/fixtures/dev-targets.csv` (+instances.csv). POST dump on `pgtarget`
     → success, registry row w/ REAL sha256/size/location, `pg_restore --list`
     on the volume file OK. Failure path: temporarily point `pgtarget-env` at
     bad creds (or stop pgtarget) → run failed, NO artifact row, notify mail.
     grep no-secret. Tear down; leave demo :8080 untouched.
   - Dev facts: semaphore project 1, dump template **3**, pgtarget-env 3,
     smoke template 1; pgtarget host-port 5433; `.env` has the token +
     `PGTARGET_*`. Persistent `.env` STAYS `PORTAL_ENGINE_NONPROD=mock`.
2. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.

## Blocked / needs user

- Nothing.
- HEADS-UP (WU-034 slice a, s19): compose now also runs **`pgtarget`**
  (dbportal-dev-pgtarget-1, postgres:16, 127.0.0.1:5433, seeded appdb) and the
  `semaphore` service is now the **BUILT** image `dbportal-semaphore:v2.17.39-pg16`
  (postgresql16-client + `/artifacts`). New named volumes `artifacts`,
  `pgtargetdata`. To bring the whole dev stack up from a fresh clone:
  `docker compose -f infra/compose.yaml --env-file .env up -d --build --wait`
  then `set -a; . ./.env; set +a; sh infra/semaphore-bootstrap.sh` (now also
  creates `pgtarget-env` + the `dump` template; prints
  `SEMAPHORE_DUMP_TEMPLATE_ID`). `pgtarget` creds live in `.env` as `PGTARGET_*`
  (engine-side only, ADR-004). A leftover `.dump` from the slice-(a) proof sits
  in the `artifacts` volume — harmless.
- HEADS-UP (Semaphore, WU-033 DONE): the compose `semaphore` service is LEFT
  RUNNING on the VM at 127.0.0.1:3000 (`docker ps` → dbportal-dev-semaphore-1).
  The skip-gated itest depends on it PLUS the API token in gitignored `.env`
  (`PORTAL_SEMAPHORE_API_TOKEN`, minted by infra/semaphore-bootstrap.sh);
  `.env` also now carries `PORTAL_SEMAPHORE_WEBHOOK_SECRET` (used by slice-b's
  drill). If the service was restarted/reset (BoltDB in the `semaphore_data`
  volume — a `docker compose down -v` wipes it), re-run `docker compose -f
  infra/compose.yaml --env-file .env up -d --wait semaphore` then `set -a;
  . ./.env; set +a; sh infra/semaphore-bootstrap.sh` and update
  `PORTAL_SEMAPHORE_API_TOKEN` in `.env` with the freshly-printed token (tokens
  are shown ONCE, never re-listable). Verify: `curl 127.0.0.1:3000/api/ping`
  → 200, and `go test ./internal/engine/ -run TestSemaphoreIntegration` passes
  (not skips). NOTE the persistent `.env` stays `PORTAL_ENGINE_NONPROD=mock`
  with `PORTAL_SEMAPHORE_TEMPLATES=smoke:1` — the s18 full-portal drill ran an
  EPHEMERAL isolated portal (`portal_drill` DB on the dev PG, port :8099, env
  `PORTAL_ENGINE_NONPROD=semaphore` + `dump:1,smoke:1`), now torn down (portal
  stopped, `portal_drill` dropped). The `dump→smoke` template map was
  drill-only; WU-034 makes `dump` a REAL playbook template.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080. **NOT touched at s18** (the WU-033 drill used an isolated portal, not
  the demo). Still the 270e665 WU-031 binary with MockEngine; dev DB at 0010
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

- MVP scope = D1–D7 (DECISIONS.md). MockEngine is the forever default for dev
  + ALL tests (ADR-002); real Semaphore is opt-in per WU-033 (below).
- **SemaphoreAdapter (WU-033, SPEC-033 = docs/specs/semaphore.md):** real
  engine for the non-prod class, opt-in via `PORTAL_ENGINE_NONPROD=semaphore`,
  behind the UNCHANGED engine.Adapter seam. `internal/engine/semaphore.go` =
  StartJob/Status/StreamLogs(poll replay-then-follow)/Cancel over Semaphore's
  REST API; JobID = the durable Semaphore task id (no in-process job state;
  ErrUnknownJob falls out for a task Semaphore forgot). Tag→template-id is
  config (`PORTAL_SEMAPHORE_TEMPLATES=tag:id,…`), fail-closed on an unmapped
  tag. Webhook `POST /api/engine/semaphore/webhook` (server/webhook_http.go) is
  SESSION-LESS, shared-secret constant-time (`hmac.Equal`, header
  `X-Portal-Webhook-Secret`, empty secret ⇒ 401-only), reads ONLY the task id,
  re-polls the REAL task → `runs.ReconcileByJobID` (find run by job_id → ONE
  guarded Status→finalize). POLL is the finalization truth (ADR-002): the
  webhook only accelerates; webhook-down still finalizes; a lying payload can't
  force an outcome. Disjoint per-class config = guardrail 3 structural (Registry
  panics on a shared instance). When semaphore is on, the runs watcher polls at
  `PORTAL_SEMAPHORE_POLL_INTERVAL` (gentle, webhook-accelerated), not the mock's
  500ms. Compose Semaphore v2.17.39 BoltDB, creds `.env`-only; ONE skip-gated
  itest. NEXT (WU-034): `dump` becomes a REAL pg_dump playbook + `Artifact.Location`.
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

- 2026-07-11 — WU-034 slice (a) done (s19): the real pg_dump path proven at the
  Semaphore level. compose `pgtarget` (seeded) + custom runner image
  (`infra/semaphore.Dockerfile`, postgresql16-client + writable `/artifacts`) +
  `artifacts` volume; `playbooks/dump.yml` (pg_dump -Fc, engine-side libpq
  creds, `DBPORTAL_RESULT=<base64 json>` result line); bootstrap `pgtarget-env`
  + `dump` template (id 3); `dev-targets.csv` (NOT instances.csv — 8-row test
  coupling); SPEC-034 = docs/specs/dump-playbook.md. Direct Semaphore run →
  success, result decodes clean, sha256 matches file, `pg_restore --list` OK,
  no password in output/repo. Next: slice (b) — adapter parse + Artifact.Location
  + finalize + tests + full-portal live drill. No Go changes yet.
- 2026-07-11 — WU-033 DONE (s18, slice b): SemaphoreAdapter complete — the
  SAME portal drives REAL Semaphore for nonprod, opt-in, behind the unchanged
  engine.Adapter seam. Slice (b) shipped: `runs.ReconcileByJobID` (find run by
  job_id → ONE guarded Status→finalize; no-ops on unknown/terminal/
  non-terminal/ErrUnknownJob) + the session-less webhook route
  `POST /api/engine/semaphore/webhook` (server/webhook_http.go: constant-time
  `hmac.Equal` shared secret via header, empty secret ⇒ 401-only, body-capped,
  reads ONLY the task id, re-polls the REAL task — mini-ADR 2+5) + main wiring
  (Engine reconciler + secret; watcher polls at PORTAL_SEMAPHORE_POLL_INTERVAL
  when semaphore, not 500ms). Tests: reconcile_test (fake-adapter accelerate /
  unknown / already-terminal / ErrUnknownJob-defer / non-terminal + the
  no-webhook poll fallback) + webhook_http_test (auth-fails-closed / disabled /
  task-id shapes / bad-JSON / no-id / 413 / error-swallowed). Gate GREEN both
  stacks (golangci 0 issues, race pass, vitest 116/116); itest ran LIVE
  (smoke→success 17s streamed, cancel→canceled) AND skips clean w/ token unset.
  FULL-PORTAL LIVE DRILL on an ISOLATED portal (portal_drill DB + :8099, real
  Semaphore, `dump→smoke` map): run1 dump billing-test→success (task 2147483634,
  ~18s) w/ 34 SSE ansible lines + `end`; run2 cancel-mid-run→canceled; audit
  parity vs mock (submitted→finished / submitted→cancel_requested→finished,
  job_id = task id); run4 webhook-accelerated finalize (watcher parked 30s,
  webhook→success on the spot) + bad/no secret→401 zero-change. Drill torn down
  (portal stopped, portal_drill dropped); demo :8080 untouched. Active → WU-034.
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
