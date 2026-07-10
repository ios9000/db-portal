# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-022 (portal-owned scheduler, ADR-003) — **not started,
  spec first** (misfire + overlap + enable/disable mini-ADRs per BACKLOG).
  WU-021 DONE 2026-07-10 (s11, commits 5dbb24a spec + 4daf552 backend +
  f0a7d45 doc reconciliation + 99bac19 UI).
- **Status:** WU-021 closed per SPEC-021 (docs/specs/authz.md, 8 mini-ADRs).
  Backend (architect-implemented): migration 0006 (role + user_role, seeds
  dba role + standing break-glass grant; auth_event CHECK += authz.denied),
  internal/authz (HasRole / idempotent Grant / Require → denial on auth
  trail, store failure = 500 never 403), requireRole middleware on POST
  /api/runs + cancel (reads stay session-gated; logout role-free),
  runs.StartRequest w/ explicit Actor ('local-dev' constant DELETED,
  run.finished inherits the submitted row's actor — sweep/watcher act on
  the requester's behalf), run.cancel_requested written intent-first
  (canceled run = 3 audit rows), server-side prod ritual (Confirm ==
  instance name → else ErrProdUnconfirmed/400), requested_by list filter,
  body caps (64KiB runs POST → 413, 4KiB login, reason ≤ 500 → 400).
  Dev role grants follow auth mode at boot (fake: dba1/dba2, off:
  local-dev; ldap: manual INSERT documented in spec). UI (3rd Sonnet 5
  delegation, first pass green): Activity `by` URL param + clickable
  Requester cell + clearable chip; drawer sends typed prod name as
  confirm + renders server 400/403 via err.detail; api.ts errorDetail now
  unwraps the {"error": ...} envelope. Evidence: npm run check green (Go
  all pkgs incl. amended golden flow — unconfirmed prod POST refused beat,
  dba1 actor pinned on both audit rows; vitest 80/80). LIVE on release
  binary: prod ritual 400/400/201, cancel trail 3 rows w/ actors,
  requested_by filter, revoked role → 403 + authz.denied row + reads still
  200 + re-grant works, denial rows immutable, break-glass grant seeded.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-022 (scheduler) in a FRESH session, spec first** (docs/specs/
   scheduler.md): mini-ADR misfire policy (portal down at fire time),
   overlap policy (previous run still live), enable/disable, jitter,
   timezone. Executor = robfig/cron/v3 in-process, firing through
   runs.Service.Start with `StartRequest{Actor: "schedule:<owner>",
   Confirm: <instance>}` — the explicit-actor door and programmatic
   prod-confirm were built for this (SPEC-021 mini-ADRs 4+6; human ritual
   for prod schedules happens at schedule creation). Read: BACKLOG WU-022;
   ADR-003; D4; internal/runs/service.go (StartRequest); SPEC-021;
   frontend /schedules stub route.
2. Then WU-023 (windows warn-only — needs the scheduler's second launch path).

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080 (binary of 99bac19, migrations at 0006) with `PORTAL_AUTH_MODE=fake`
  + demo break-glass hash (password "demo-glass", hash only in the unit
  env — recover via `systemctl show dbportal-demo -p Environment`). Sign in
  dba1/dba1. Before any live check that runs its own portal:
  `systemctl stop dbportal-demo` — bind-in-use + two-portals-one-DB hazard.
  ALSO (bit s10 twice): a backgrounded `portal &` may report a wrapper PID
  in `$!` — always kill the PID that `ss -ltnp` shows holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Delegation model (3 successes: WU-019, WU-020b, WU-021b):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. Workflow tool only
  for multi-agent pipelines (gate reviews).
- **AuthZ (WU-021, SPEC-021 = docs/specs/authz.md):** roles in portal DB
  (role/user_role; directory = who you are, portal = what you may do).
  Mutations need dba (requireRole); reads session-only. Denials =
  auth_event `authz.denied` (actor-centric ledger; store failure ≠ denial).
  Audit actor is an EXPLICIT param (runs.StartRequest.Actor, Cancel actor
  arg) — never ctx magic; run.finished inherits submitter; cancel writes
  run.cancel_requested intent-first (3 rows on canceled runs). Prod ritual
  server-side: StartRequest.Confirm must equal instance name on prod.
  Grants at boot per auth mode; break-glass seeded dba in 0006.
- **AuthN (WU-020, SPEC-020 = docs/specs/authn.md):** every /api route needs a
  session except POST /api/auth/login; healthz + SPA public. Sessions in
  portal DB (sha256 of opaque token, 12h TTL, lazy expiry delete); cookie
  portal_session httpOnly SameSite=Lax. PORTAL_AUTH_MODE ldap (default, fails
  closed) | fake (dba1/dba1, dba2/dba2) | off (bypass, local-dev). Break-glass
  local account via PORTAL_BREAKGLASS_HASH (bcrypt, empty=disabled), every use
  alarmed (auth.break_glass + slog Error). auth_event append-only (triggers).
  authn.From(ctx) carries identity. Frontend: bootstrap gate (App), /login
  outside Shell, api.ts onUnauthorized (401 only; 403 = plain ApiError).
- **Golden flow (M1 close, ADR-011):** `backend/e2e/golden_flow_test.go` must
  stay green EVERY session. Real router + runs.Service + per-class mocks +
  notify.Mailer vs testutil.FakeSMTP on testutil.MigratedDB. Beat 0 (WU-020):
  401 until cookie-jar login via Fake directory. WU-021 beats: unconfirmed
  prod POST → 400; dba1 actor on both audit rows; requested_by filter.
  Human twin = docs/demo-m1.md (sign in dba1/dba1).
- Guardrails (WU-015, SPEC-015): EnvBanner on RunDetail + LaunchDrawer; prod
  ritual = typed exact instance name, enforced client AND server (WU-021);
  Registry.Register panics on cross-class adapter sharing; env stamping pinned
  by tests.
- Inventory (WU-010): cluster/instance tables (name natural key, env CHECK,
  window raw text), quarantine tables; `portal import <csv>` idempotent;
  engine.ClassForEnv = env authority. size_gb canonicalized at parse (WU-018).
- Instance API (WU-011): GET /api/instances[?env=], detail 404 JSON;
  server.InstanceReader seam; MyDatabases cards/table. last_backup_at
  (WU-011R) = newest SUCCESSFUL dump's finished_at, null if never dumped.
- Runs (WU-012, SPEC-012): run mutable; audit_event append-only (UPDATE/
  DELETE/TRUNCATE triggers), env + playbook_tag stamped, job_id on
  run.finished only. runs.Service = ONLY Registry caller; single-finalizer
  guards incl. watcher mirror (WU-016); SweepOrphans best-effort on boot.
  POST /api/runs: 400/403/404/413/502, trail complete either way.
- Run detail + logs (WU-013, SPEC-013): SSE replay→follow, ONE `end` (bounded
  2s terminal wait), 404/410; logs NOT persisted; cancel 202 async. Client
  (WU-019): stream death before `end` retries w/ backoff, lines kept.
- Notify + Activity (WU-014, SPEC-014): finalize() post-commit mail for
  failed|canceled (who/what/where/status + link ONLY). GET /api/runs filters
  ANDed (state/env/operation/instance/requested_by). Activity: chips + `by`
  requester filter, CSV export (WU-019: stale-filter drop, formula
  neutralization, deferred revoke).
- Test helpers: testutil.MigratedDB(t) scratch DB (skips w/o compose PG);
  testutil.DB(t) dev DB; testutil.FakeSMTP(t). goose down reverts ONE
  migration (0006 down caveat: fails if authz.denied rows exist — spec'd).
- Single-binary (WU-006): build:release embeds frontend/dist; degraded-mode
  healthz 503 without DB. Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail
  closed; params["mock_fail_at"] injects failure (tests only).
- Chassis/frontend (WU-003/004): config.Load env>file>defaults;
  server.NewRouter = test seam; react-router v7 ('react-router'); tokens ONLY
  in src/index.css; lib/api.ts = ALL failures are ApiError (errorDetail
  unwraps the {"error": ...} envelope since WU-021). tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO
  Postgres service (DB tests + golden flow skip there; VM gate is the real
  gate). Deps: go-ldap/v3, x/crypto (both direct since WU-021 tidy).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it.
- Verify-procedure gotcha (s05, s07, s10×2): pkill -f matches the tool shell;
  `$!` can be a wrapper PID. Always kill the exact PID from `ss -ltnp`.

## Checkpoint log (last 3, newest first)

- 2026-07-10 — WU-021 done (s11, 5dbb24a+4daf552+f0a7d45+99bac19): AuthZ —
  SPEC-021, role store + guards + real actor + run.cancel_requested +
  server-side prod ritual + requested_by filter + body caps; live-verified
  end to end (403 denial trail, ritual 400s, 3-row cancel trail). Demo
  portal rebuilt on 99bac19, DB at migration 0006. Active → WU-022 (fresh
  session, spec first).
- 2026-07-10 — WU-020 done (s10, 0e30914+5034dc8+d5c6731): AuthN — SPEC-020,
  sessions + directory seam + break-glass + login UI; golden flow
  authenticates for real; live-verified end to end. Demo portal in fake
  mode (dba1/dba1).
- 2026-07-10 — WU-019 done (s10, 3cfdf8f): frontend resilience via 1st Sonnet
  delegation; M1-GATE FIXES COMPLETE (016-019). WU-020 spec checkpoint
  (0e30914) same session.
