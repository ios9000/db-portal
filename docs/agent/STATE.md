# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-021 (AuthZ: DBA role, route guards, real actor) — **not
  started**. WU-020 DONE 2026-07-10 (s10, commits 0e30914 spec + 5034dc8
  backend + d5c6731 UI).
- **Status:** WU-020 closed per SPEC-020 (docs/specs/authn.md, 8 mini-ADRs).
  Backend (architect-implemented — security-sensitive per delegation policy):
  migration 0005 (session w/ sha256 token hashes; append-only auth_event w/
  0003/0004 trigger pattern), internal/authn (Directory seam: LDAP template
  bind w/ injection allowlist + StartTLS enforcement + empty-password
  rejection | Fake dev directory | Bypass), Service (uniform delayed
  failures, break-glass alarmed via auth.break_glass + slog Error),
  requireSession on /api (login exempt; healthz + SPA public), auth
  endpoints, PORTAL_AUTH_MODE ldap|fake|off failing closed (pinned by config
  test). UI (2nd Sonnet 5 delegation, first pass green): bootstrap gate in
  App.tsx (fetchMe → loading|login|shell), chrome-less /login card, Shell
  extracted w/ identity badge + sign-out, api.ts onUnauthorized single-slot
  401 signal (login/me opt out), postVoid for 204s. Golden flow now
  authenticates for real (beat 0: 401 assert → cookie-jar login).
  Evidence: npm run check green (Go all pkgs, vitest 73/73); LIVE on release
  binary: unauth API 401, login page 200, dba1 login → cookie → API 200,
  break-glass login → auth_event row + BREAK-GLASS error log, TRUNCATE
  auth_event → "append-only". Runs actor stays 'local-dev' until WU-021.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-021 (AuthZ) in a FRESH session:** role store (role table + user↦role,
   seeded DBA), route guards on mutating endpoints (D2), audit actor = real
   identity everywhere (replaces 'local-dev' in internal/runs — authn.From(ctx)
   is already plumbed by WU-020's middleware). Carries the deferred ledger:
   run.canceled audit action, Activity requester filter, server-side prod
   ritual enforcement, POST /api/runs body cap (m1-gate item 15). Spec
   decisions in-WU per BACKLOG. Read: BACKLOG WU-021; D2/D3; SPEC-012 §audit +
   SPEC-015 deferrals; internal/runs/service.go (actor constant); SPEC-020
   (session context seam).
2. Then WU-022 (scheduler, spec first: misfire/overlap mini-ADRs), WU-023
   (windows warn-only).

## Blocked / needs user

- Nothing.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080 (binary of d5c6731) with `PORTAL_AUTH_MODE=fake` + a demo break-glass
  hash (password "demo-glass", hash lives only in the unit env). Sign in:
  dba1/dba1. Before any live check that runs its own portal:
  `systemctl stop dbportal-demo` — bind-in-use + two-portals-one-DB hazard.
  ALSO (bit s10 twice): a backgrounded `portal &` may report a wrapper PID in
  `$!` — always kill the PID that `ss -ltnp` shows holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Delegation model (piloted s10 WU-019, 2nd success WU-020 slice b):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. Workflow tool only
  for multi-agent pipelines (gate reviews).
- **AuthN (WU-020, SPEC-020 = docs/specs/authn.md):** every /api route needs a
  session except POST /api/auth/login; healthz + SPA public. Sessions in
  portal DB (sha256 of opaque token, 12h TTL, lazy expiry delete); cookie
  portal_session httpOnly SameSite=Lax. PORTAL_AUTH_MODE ldap (default, fails
  closed) | fake (dba1/dba1, dba2/dba2) | off (bypass, local-dev). Break-glass
  local account via PORTAL_BREAKGLASS_HASH (bcrypt, empty=disabled), every use
  alarmed (auth.break_glass + slog Error). auth_event append-only (triggers).
  authn.From(ctx) carries identity; runs actor STILL 'local-dev' (WU-021).
  Frontend: bootstrap gate (App), /login outside Shell, api.ts onUnauthorized.
- **Golden flow (M1 close, ADR-011):** `backend/e2e/golden_flow_test.go` must
  stay green EVERY session. Real router + runs.Service + per-class mocks +
  notify.Mailer vs testutil.FakeSMTP on testutil.MigratedDB. Beat 0 (WU-020):
  401 until cookie-jar login via Fake directory. Human twin = docs/demo-m1.md
  (sign in dba1/dba1 since WU-020).
- Guardrails (WU-015, SPEC-015): EnvBanner on RunDetail + LaunchDrawer; prod
  ritual = typed exact instance name (client-side ONLY until WU-021);
  Registry.Register panics on cross-class adapter sharing; env stamping pinned
  by tests.
- Inventory (WU-010): cluster/instance tables (name natural key, env CHECK,
  window raw text), quarantine tables; `portal import <csv>` idempotent;
  engine.ClassForEnv = env authority. size_gb canonicalized at parse (WU-018).
- Instance API (WU-011): GET /api/instances[?env=], detail 404 JSON;
  server.InstanceReader seam; MyDatabases cards/table. last_backup_at
  (WU-011R) = newest SUCCESSFUL dump's finished_at, null if never dumped.
- Runs (WU-012, SPEC-012): run mutable; audit_event append-only (UPDATE/
  DELETE/TRUNCATE triggers), one event per transition, env + playbook_tag
  stamped, job_id on run.finished only. runs.Service = ONLY Registry caller;
  single-finalizer guards incl. watcher mirror (WU-016); SweepOrphans
  best-effort on boot. POST /api/runs: 400/404/502, trail complete either way.
- Run detail + logs (WU-013, SPEC-013): SSE replay→follow, ONE `end` (bounded
  2s terminal wait), 404/410; logs NOT persisted; cancel 202 async. Client
  (WU-019): stream death before `end` retries w/ backoff, lines kept.
- Notify + Activity (WU-014, SPEC-014): finalize() post-commit mail for
  failed|canceled (who/what/where/status + link ONLY). GET /api/runs filters
  ANDed. Activity: chips, CSV export (WU-019: stale-filter drop, formula
  neutralization, deferred revoke).
- Test helpers: testutil.MigratedDB(t) scratch DB (skips w/o compose PG);
  testutil.DB(t) dev DB; testutil.FakeSMTP(t). goose down reverts ONE
  migration.
- Single-binary (WU-006): build:release embeds frontend/dist; degraded-mode
  healthz 503 without DB. Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail
  closed; params["mock_fail_at"] injects failure (tests only).
- Chassis/frontend (WU-003/004): config.Load env>file>defaults;
  server.NewRouter = test seam; react-router v7 ('react-router'); tokens ONLY
  in src/index.css; lib/api.ts = ALL failures are ApiError. tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO
  Postgres service (DB tests + golden flow skip there; VM gate is the real
  gate). New deps (WU-020): go-ldap/v3, x/crypto (bcrypt).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it.
- Verify-procedure gotcha (s05, s07, s10×2): pkill -f matches the tool shell;
  `$!` can be a wrapper PID. Always kill the exact PID from `ss -ltnp`.

## Checkpoint log (last 3, newest first)

- 2026-07-10 — WU-020 done (s10, 0e30914+5034dc8+d5c6731): AuthN — SPEC-020,
  sessions + directory seam + break-glass + login UI; golden flow
  authenticates for real; live-verified end to end (401 → login → API;
  break-glass alarmed; auth trail immutable). Demo portal now runs in fake
  mode (dba1/dba1). Active → WU-021 (fresh session).
- 2026-07-10 — WU-019 done (s10, 3cfdf8f): frontend resilience via 1st Sonnet
  delegation; M1-GATE FIXES COMPLETE (016-019). WU-020 spec checkpoint
  (0e30914) same session.
- 2026-07-10 — WU-011R done (s10, df5fadf): last_backup_at wired API→UI.
  WU-018 done (391e955): size_gb canonicalization.
