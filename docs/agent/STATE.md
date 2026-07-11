# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **grooming DONE 2026-07-11 (s14)**; next WU is
  **WU-030 (artifact registry, S)** in a fresh session. M2 was formally
  closed s13 (gate record docs/agent/reviews/m2-gate.md, fix WUs 024/025
  done). Phase 3 is fully groomed in BACKLOG.md: WU-030…036 each carry
  Goal/Deliverables/AC/Verify/Context brief.
- **Status (s14, grooming):** docs-only session, per ROADMAP "grooming is a
  deliverable". Key grooming decisions: (1) **execution order is 030 → 032 →
  031 → 033 → 034 → 035 → 036**, NOT numeric — restore IS the first chain
  (verify → safety dump → restore = the 3-step chain the M3 exit drills), so
  the chain engine (032) lands before restore (031) rides it. (2) **WU-036
  added** (real restore playbook + docs/demo-m3.md rehearsal) — the M3 exit
  criterion "restore rehearsal on a compose target passes" had no covering
  WU; ROADMAP updated to WU-030…036. (3) Storage pre-O-1 pinned: registry
  (migration 0009, WU-030) is metadata-first with a dormant `location`
  column (the 0003 window_warned pattern); 034 fills it with a compose
  volume path; 035 moves bytes to minio. (4) WU-024's stored-ritual-evidence
  lesson ported to chains: chain rows store creation-time confirm, fire
  verbatim. (5) Safety dump is UNCONDITIONAL in the restore chain — no skip
  affordance, client or API (the motivating incident). Icebox gained the 8
  candidates STATE carried (session GC, denial-rate alarm, break-glass mail,
  role CLI, cron×window hint, window_warned read model, boot-stampede,
  schedule-change ledger) + artifact-retention enforcement.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **Start WU-030 in a fresh session** (artifact registry, S — first WU of
   Phase 3). Ritual: read its BACKLOG entry + ONLY its context brief; write
   SPEC-030 (docs/specs/ — use SPEC-TEMPLATE.md) with the mini-ADRs the entry
   names (dual write run-columns + registry; retention classes); then
   migration 0009 + finalize() insert + GET /api/artifacts + tests. S-sized,
   architect-implementable in one session; no UI slice.
2. After WU-030: WU-032 (chain engine, M) — NOT WU-031; see the Phase 3
   header note in BACKLOG.md for the order rationale.
3. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080 (binary of 90aae5a, migrations at 0008)
  with `PORTAL_AUTH_MODE=fake` + demo break-glass hash (password
  "demo-glass", hash only in the unit env — recover via `systemctl show
  dbportal-demo -p Environment`). Sign in dba1/dba1. Schedules list is
  empty (live-verify schedules deleted; runs 14–21 remain as verify
  history incl. schedule:dba1 rows). Fixture windows restored (only the
  4 prods have windows). Before any live check that runs its own portal:
  `systemctl stop dbportal-demo` — bind-in-use + two-portals-one-DB
  hazard. ALSO (bit s10 twice): a backgrounded `portal &` may report a
  wrapper PID in `$!` — always kill the PID that `ss -ltnp` shows
  holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Delegation model (4 successes: WU-019, WU-020b, WU-021b, WU-022b):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. Workflow tool only
  for multi-agent pipelines (gate reviews). Phase 3 delegation candidates:
  UI slices of 031/032, playbook/compose scaffolds of 033/034; chain engine
  core, SemaphoreAdapter concurrency, ritual/authz seams = architect.
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
  schedule, schedule:dba1 attribution. WU-031 adds Beat 9 (restore chain).
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
  Artifact metadata currently = 3 columns on run (registry table = WU-030).
- Run detail + logs (WU-013, SPEC-013): SSE replay→follow, ONE `end`,
  404/410; logs NOT persisted; cancel 202 async; client retry w/ backoff
  (WU-019).
- Notify + Activity (WU-014, SPEC-014): post-commit mail for
  failed|canceled (who/what/where/status + link ONLY). GET /api/runs
  filters ANDed (state/env/operation/instance/requested_by). Activity:
  chips + `by` filter, CSV export (formula neutralization).
- Test helpers: testutil.MigratedDB(t) scratch DB (skips w/o compose PG);
  testutil.DB(t) dev DB; testutil.FakeSMTP(t). goose down reverts ONE
  migration (0006 down caveat: fails if authz.denied rows exist — spec'd).
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

- 2026-07-11 — Phase 3 groomed (s14, docs-only): WU-030…036 full BACKLOG
  entries; execution order 030→032→031→033→034→035→036; WU-036 (restore
  playbook + demo-m3.md rehearsal) added — exit criterion had no covering
  WU; ROADMAP range updated; 9 icebox items filed. Active → WU-030.
- 2026-07-11 — WU-024 + WU-025 done (s13, 54db393 + 90aae5a): both M2-gate
  fix WUs — scheduler hardening (stored ritual evidence, race guards,
  instance-wide overlap, bounded fires) + identity/session honesty
  (username case-fold, honest bootstrap/sign-out, list gating, boot warn).
  M2 FORMALLY CLOSED.
- 2026-07-10 — WU-023 done (s12, 3de6187+97de1ab): windows warn-only —
  SPEC-023, internal/window parser + audit stamp (0003's dormant column)
  + window_state read model + drawer warn line; live-verified Fri-night
  vs Sat fixture windows. PHASE 2 COMPLETE. Demo portal on 97de1ab.
