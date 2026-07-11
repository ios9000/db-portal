# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-023 (maintenance windows warn-only, S) — **not started,
  fresh session, spec first** (parse `Day HH:MM-HH:MM`, warn-never-block per
  D6, `window_warned` audit stamp shape + timezone are the spec decisions).
  WU-022 DONE 2026-07-10 (s12, commits e316515 spec + 0579a58 backend +
  a5c2867 UI). ROADMAP M2 scheduled-dump exit criterion met + live-verified.
- **Status:** WU-022 closed per SPEC-022 (docs/specs/scheduler.md, 10
  mini-ADRs). Backend (architect-implemented): migration 0007 (schedule
  table, instance FK CASCADE, partial due-index), internal/schedule =
  store CRUD (prod ritual at CREATION — confirm == instance name; executor
  then confirms programmatically every fire) + executor (DB tick loop over
  persisted pre-jittered next_fire_at; robfig/cron/v3 is PARSER ONLY;
  misfire = coalesced catch-up for free — due-in-past is just due;
  fire-then-stamp: crash mid-fire retries via sweep+catch-up composition;
  overlap = skip visibly; disabled = frozen, re-enable computes from now;
  engine refusal counts as 'fired' → SPEC-014 mail; every outcome stamps +
  advances, never wedges). Actor = `schedule:<created_by>` through the
  explicit StartRequest door. API GET/POST/PATCH/DELETE /api/schedules,
  mutations behind requireRole, 64KiB/500-char caps. UI (4th Sonnet
  delegation, first pass green): Schedules page — table (env badge, cron
  chip, next/last fire, RUN-n links, toggle, two-step delete) + create
  drawer w/ prod typed-name ritual; api.ts Schedule type + 4 fns +
  patchJSON/deleteVoid helpers. Golden flow Beat 8 = live executor loop
  fires a due schedule e2e. Evidence: npm run check green (vitest 94/94,
  Go all pkgs -race). LIVE on release binary: jitter-bounded fire w/ both
  audit rows schedule:dba1; disabled never fires; stop across 2 fire
  times → ONE coalesced catch-up; restart w/ nothing due → no re-fire.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-023 (windows warn-only) in a FRESH session, spec first**
   (docs/specs/windows.md): parse the fixture's `Day HH:MM-HH:MM` shape from
   instance.maintenance_window (raw text since WU-010, O-3); launching
   OUTSIDE the window — drawer AND scheduler path — warns, NEVER blocks
   (D6); `window_warned` stamp on the audit trail (spec decides column vs
   action — note audit_event has hardened NOT NULLs and append-only
   triggers); unparseable/empty = no warning, logged once; timezone
   mini-ADR (SPEC-022 mini-ADR 7 pinned server-local — stay consistent).
   Read: BACKLOG WU-023; D6; O-3 (DECISIONS §Open); WU-010 schema;
   LaunchDrawer + runs.Service.Start (stamp point); internal/schedule
   executor fire() (the second launch path).
2. WU-023 closes Phase 2 → M2 milestone gate: groom Phase 3 (WU-030…035)
   ACs + context briefs at gate close, per ROADMAP sequencing rules.

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080 (binary of a5c2867, migrations at 0007) with `PORTAL_AUTH_MODE=fake`
  + demo break-glass hash (password "demo-glass", hash only in the unit
  env — recover via `systemctl show dbportal-demo -p Environment`). Sign in
  dba1/dba1. Schedules list is empty (live-verify schedules were deleted;
  runs 14/15 remain as schedule:dba1 history). Before any live check that
  runs its own portal: `systemctl stop dbportal-demo` — bind-in-use +
  two-portals-one-DB hazard. ALSO (bit s10 twice): a backgrounded
  `portal &` may report a wrapper PID in `$!` — always kill the PID that
  `ss -ltnp` shows holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Delegation model (4 successes: WU-019, WU-020b, WU-021b, WU-022b):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. Workflow tool only
  for multi-agent pipelines (gate reviews).
- **Scheduler (WU-022, SPEC-022 = docs/specs/scheduler.md):** internal/
  schedule.Service = store + executor; tick loop (Tick 10s, Jitter 60s
  defaults) fires `enabled AND next_fire_at <= now()` through
  runs.Service.Start as `schedule:<created_by>` with Confirm set
  programmatically (human ritual happened at creation). next_fire_at
  persisted PRE-JITTERED, NULL iff disabled. Misfire = coalesced catch-up;
  overlap = skip (status skipped_overlap); re-enable computes from now.
  Fire-then-stamp + orphan sweep = crash mid-fire retries the dump.
  main.go: `go sched.Run(ctx)` after SweepOrphans.
- **AuthZ (WU-021, SPEC-021):** roles in portal DB (role/user_role).
  Mutations need dba (requireRole); reads session-only. Denials =
  auth_event `authz.denied` (store failure = 500 never 403). Audit actor
  is an EXPLICIT param (StartRequest.Actor, Cancel actor arg) — never ctx
  magic; run.finished inherits submitter; cancel writes
  run.cancel_requested intent-first (3 rows on canceled runs). Prod ritual
  server-side: Confirm must equal instance name on prod. Grants at boot
  per auth mode; break-glass seeded dba in 0006.
- **AuthN (WU-020, SPEC-020):** every /api route needs a session except
  POST /api/auth/login; healthz + SPA public. Sessions in portal DB
  (sha256, 12h TTL); cookie portal_session httpOnly SameSite=Lax.
  PORTAL_AUTH_MODE ldap (default, fails closed) | fake (dba1/dba1,
  dba2/dba2) | off (local-dev). Break-glass via PORTAL_BREAKGLASS_HASH,
  every use alarmed. auth_event append-only. authn.From(ctx) carries
  identity. Frontend: bootstrap gate, /login outside Shell, api.ts
  onUnauthorized (401 only).
- **Golden flow (ADR-011):** `backend/e2e/golden_flow_test.go` must stay
  green EVERY session. Beat 0 login; WU-021 beats (ritual 400, actor
  pinned, requested_by); Beat 8 (WU-022): live executor fires a due
  schedule, schedule:dba1 attribution. Human twin = docs/demo-m1.md
  (beats 1–7 + §8 schedules preview).
- Guardrails (WU-015, SPEC-015): EnvBanner on RunDetail + LaunchDrawer +
  schedule drawer; prod ritual = typed exact instance name, client AND
  server, launch AND schedule-create; Registry.Register panics on
  cross-class adapter sharing; env stamping pinned by tests.
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

- 2026-07-10 — WU-022 done (s12, e316515+0579a58+a5c2867): scheduler —
  SPEC-022, tick-loop executor over persisted jittered next_fire_at,
  schedule CRUD + Schedules UI, golden-flow Beat 8; live-verified incl.
  restart battery (coalesced catch-up, no double-fire). Demo portal
  rebuilt on a5c2867, DB at migration 0007. Active → WU-023 (fresh
  session, spec first; closes Phase 2 → M2 gate).
- 2026-07-10 — WU-021 done (s11, 5dbb24a+4daf552+f0a7d45+99bac19): AuthZ —
  SPEC-021, role store + guards + real actor + run.cancel_requested +
  server-side prod ritual + requested_by filter + body caps; live-verified
  end to end (403 denial trail, ritual 400s, 3-row cancel trail).
- 2026-07-10 — WU-020 done (s10, 0e30914+5034dc8+d5c6731): AuthN — SPEC-020,
  sessions + directory seam + break-glass + login UI; golden flow
  authenticates for real; live-verified end to end.
