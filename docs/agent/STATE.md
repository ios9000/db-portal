# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **WU-031 (restore workflow on MockEngine, M) IN
  PROGRESS**: backend landed + gate-green this session (s17); the **UI slice
  (Restore drawer) is the remaining half** and the next action. Execution
  order 030 → 032 → 031 → 033 → 034 → 035 → 036.
- **Status (s17, WU-031 backend):** restore backend complete, gate green both
  stacks. (Recovered an interrupted twin session's uncommitted backend after
  an ssh reset — verified-don't-redo: the on-disk code compiled and matched
  SPEC-031, so this session wrote the entire missing test layer + golden-flow
  beat rather than restarting. See JOURNAL s17.) SPEC-031 =
  docs/specs/restore.md (5 mini-ADRs): (1) **verify is its OWN chain step** —
  a bad artifact halts BEFORE the safety dump, target untouched; (2) retention
  class rides the catalog op, `finalize` stamps it — NO schema change (0009
  already carries retention_class); (3) verify/safety_dump/restore are
  INTERNAL (non-launchable) ops behind a SINGLE `StartRequest.Internal` choke
  point in `runs.Start` — the chain driver is the only caller that sets it, so
  a bare restore over POST /api/runs or the scheduler = 400 unknown op; (4)
  restore is a client POST assembling a chain via a PURE recipe (`no skip
  affordance` is structural, not a runtime check); (5) explicit target,
  default non-prod, prod = typed-name ritual enforced once in `chain.Create`.
  internal/restore.Steps = fixed `[verify, safety_dump, restore]` with
  artifact lineage (id+checksum+name) on verify+restore only. catalog gains
  `Launchable`+`RetentionClass` (All()=launchable only, ByID=all). runs.Start
  launchable gate; finalize stamps retention_class from the catalog
  ('safety' for safety_dump, else 'standard'). runs.GetArtifact +
  ErrArtifactNotFound. chain driver sets Internal:true on every step. mock
  gains `verify`; its restore script trimmed to restore-only (safety dump is
  its own step now). POST /api/restore (dba, body+reason capped) → 201 chain
  read model; 400 missing/prod-unconfirmed, 404 unknown artifact/instance.
  Golden flow **Beat 10** = restore end to end: happy path (3 steps success,
  ONE 'safety' artifact on the target, restore-step lineage tied,
  last_backup_at ignores the safety dump) THEN injected verify-fail halts at
  step 1 with ZERO safety_dump/restore runs on the target, one chain mail,
  API resume → success.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-031 UI slice — Restore drawer** (the remaining half; the pre-UI
   checkpoint boundary is already crossed — backend committed). Sonnet-brief
   candidate (delegation model). Reached from an instance's context
   (MyDatabases card / instance detail): pick a source artifact
   (`GET /api/artifacts?instance=` — origin registry, newest first, with
   checksum + size + class), pick an explicit TARGET (default a non-prod
   instance), EnvBanner for the TARGET's env + the typed-name prod ritual
   (reuse EnvBanner + the LaunchDrawer confirm pattern), a primary button that
   NAMES the consequence, submit → `POST /api/restore` → the chain view
   (RunDetail's chain strip already renders steps + Resume). The safety dump
   shows as an unconditional, NON-optional step in the drawer preview — NEVER
   a checkbox. `api.ts` gains `fetchArtifacts(instance)` +
   `startRestore({artifactId, target, confirm, reason})`. vitest: artifact
   pick, default-target rules, ritual. Then CLOSE WU-031 (all AC + Verify
   commands pass — incl. the grep half of AC-4), mark done in BACKLOG,
   journal, point STATE at WU-033.
2. After WU-031: WU-033 (SemaphoreAdapter, M — compose service, webhook +
   poll fallback; ritual/authz seams stay architect-side).
3. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080 (binary of b10b960, dev DB migrated to 0010) with
  `PORTAL_AUTH_MODE=fake` + demo break-glass hash (password "demo-glass",
  hash only in the unit env — recover via `systemctl show dbportal-demo -p
  Environment`; NOTE stopping the unit DELETES it and the env — s16
  regenerated the bcrypt hash from the known password and re-ran
  systemd-run, the recipe is in JOURNAL s16). Sign in dba1/dba1. Verify
  history in the DB now includes chain 1 (test-chain on hr-test, success)
  and its run 24 — RUN-24's page shows the chain strip live. Before any
  live check that runs its own portal: `systemctl stop dbportal-demo` —
  bind-in-use + two-portals-one-DB hazard. ALSO (bit s10 twice): a
  backgrounded `portal &` may report a wrapper PID in `$!` — always kill
  the PID that `ss -ltnp` shows holding :8080.

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
- **Delegation model (5 successes: WU-019, WU-020b, WU-021b, WU-022b,
  WU-032-UI):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. Workflow tool only
  for multi-agent pipelines (gate reviews). Phase 3 delegation candidates:
  UI slices of 031/032, playbook/compose scaffolds of 033/034; chain engine
  core, SemaphoreAdapter concurrency, ritual/authz seams = architect.
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

- 2026-07-11 — WU-031 backend done + gate-green (s17, CHECKPOINT — WU still in
  progress): restore workflow on MockEngine. internal/restore recipe, catalog
  Launchable/RetentionClass, runs.Start Internal launchable gate, finalize
  retention stamping, GetArtifact, chain driver Internal, mock `verify` op +
  trimmed restore script, POST /api/restore assembler, golden-flow Beat 10
  (happy + verify-fail halt/resume). RECOVERY session: an interrupted twin
  (ssh reset) had written the backend prod code + SPEC-031 uncommitted and
  never checkpointed (STATE.md still said "start fresh"). Reaped the idle twin
  (user-authorized), verified the on-disk code compiled + matched SPEC, then
  wrote the ENTIRE missing test layer (recipe, handler, launchable gate,
  retention, GetArtifact, catalog, Beat 10), fixed a stale engine mock_test
  assertion, `npm run check` green both stacks. Remaining: WU-031 UI slice.
  Active → WU-031 (UI).
- 2026-07-11 — WU-032 done (s16, e465bab + b10b960): chain engine —
  SPEC-032 (7 mini-ADRs), migration 0010, chain Service+driver+boot sweep,
  StepRunFilter halt-mail exactly-once, resume API, golden flow Beat 9,
  RunDetail chain strip (5th Sonnet delegation success). FakeSMTP
  multi-session fix. Live drill: boot sweep + mailpit mail + HTTP resume
  → run 24 as chain:dba1. Session recovered mid-WU from an ssh reset —
  tree survey found the core complete; this session added Beat 9, authz
  route pins, 0010 walk pins, ChainHalted mail test, UI slice.
  Active → WU-031 (restore workflow).
- 2026-07-11 — WU-030 done (s15, c7c6b73): artifact registry metadata-first —
  SPEC-030 (5 mini-ADRs), migration 0009 (artifact table, UNIQUE run_id,
  dormant location, idempotent backfill), finalize() registers inside the
  guarded terminal tx, GET /api/artifacts?instance=, golden flow asserts
  both launch paths. Architect-implemented; gate green both stacks.
  Active → WU-032 (chain engine).
