# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-020 (AuthN) — **spec DONE (0e30914, SPEC-020 = docs/specs/authn.md), implementation next**. WU-019 DONE
  2026-07-10 (s10, commit 3cfdf8f). ALL m1-gate fix WUs (016-019) now closed.
- **Status:** WU-019 closed gate items 6-7 + 10-13 (docs/agent/reviews/m1-gate.md):
  SSE streams that die before `end` retry with backoff keeping displayed lines
  (only an exhausted budget shows "logs no longer available"); LaunchDrawer overlay
  click can't dismiss mid-launch or on the started state; malformed 2xx bodies
  surface as ApiError; Activity drops the on-screen list on filter change (stale
  rows + stale CSV export were reachable); revokeObjectURL deferred (Safari);
  csv field() neutralizes leading =+-@/TAB (OWASP — landed BEFORE WU-020's real
  usernames, as the gate required). SPEC-013/014 reconciled. Evidence: vitest
  65/65, npm run check green both stacks.
  **Delegation pilot (s10): WU-019 was implemented by a Sonnet 5 subagent** from
  an architect-written brief (Fable wrote the brief, reviewed the diff, ran the
  gate, reconciled specs, committed). First pass came back all-green; one
  brief-vs-reality deviation was correctly caught and empirically justified by
  the agent (Activity's cancelled-flag was already sound; the real bug was the
  uncleared stale view). Implementer spend: 166,242 tokens / 78 tool calls /
  ~15 min — billed at Sonnet rates (3.3× cheaper than Fable, 5× on intro
  pricing through 2026-08-31). Policy recorded in agent memory: well-specified
  implementation WUs → Sonnet subagent; specs/mini-ADRs, concurrency/security
  WUs, review + verification stay with the architect.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-020 (AuthN) in a FRESH session, spec FIRST:** write `docs/specs/authn.md`
   before any code (mini-ADRs: session store shape, go-ldap dep, fake-directory
   seam, break-glass alarm action, golden-flow e2e authentication), then implement
   per BACKLOG. This is design work — architect (Fable) writes the spec; consider
   delegating the post-spec implementation slices per the delegation policy.
2. Then WU-021 (authz + deferred ledger: run.cancel_requested audit action,
   requester filter, server-side prod ritual enforcement, POST body cap).

## Blocked / needs user

- Nothing.
- HEADS-UP: a demo portal may be running as transient systemd unit `dbportal-demo`
  on :8080 (started s10 for the user, survives SSH drops; binary of commit 3cfdf8f,
  publicly reachable — no authn until WU-020). Before any live check that runs its
  own portal: `systemctl stop dbportal-demo` — else bind-in-use + the s05
  two-portals-one-DB sweep hazard.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- **Delegation model (piloted s10, WU-019):** implementation WUs with a tight
  BACKLOG brief can run on a Sonnet 5 general-purpose subagent (Agent tool,
  `model: sonnet`, no git access — architect commits after review + full gate).
  Fable stays on: specs/mini-ADRs, concurrency/security-sensitive WUs, diff
  review, verification, checkpointing. Workflow tool only for multi-agent
  pipelines (gate reviews), not single-implementer WUs.
- **Golden flow (M1 close, ADR-011):** `backend/e2e/golden_flow_test.go` must stay
  green EVERY session — it is the canary for the whole hero flow (import → fleet →
  run → SSE → audit → mail → prod routing). It's a normal Go test: real router +
  runs.Service + per-class mocks + notify.Mailer against `testutil.FakeSMTP`, on
  `testutil.MigratedDB`. Human twin = docs/demo-m1.md (reset recipe at the bottom).
- Guardrails (WU-015, SPEC-015 = docs/specs/guardrails.md): EnvBanner on RunDetail +
  LaunchDrawer; prod ritual = typed exact instance name (client-side friction ONLY —
  API deliberately unguarded until WU-021); Registry.Register panics on cross-class
  adapter sharing; env stamping pinned by tests (23502 schema test + prod-rows
  assertions + e2e requireAudit).
- Inventory (WU-010): tables cluster/instance (name natural key, env CHECK
  dev|test|prod, maintenance_window raw text — WU-023 gives it warn-only semantics),
  quarantine tables. `portal import <csv>` idempotent; report line
  `imported N new, updated M, unchanged U, quarantined Q`. Env validity =
  engine.ClassForEnv (single authority). size_gb canonicalized at parse (WU-018) —
  plain-decimal FormatFloat text is what stores and compares.
- Instance API (WU-011): GET /api/instances[?env=] ordered/never-null/snake_case,
  detail 404 JSON. server.InstanceReader = handler seam. Frontend MyDatabases
  cards/table, view+env in URL params. last_backup_at (WU-011R) = newest
  SUCCESSFUL dump run's finished_at, null if never dumped; health/vacuum/bloat
  still placeholder "—" until probes (M3+).
- Runs (WU-012, SPEC-012 = docs/specs/runs.md): run mutable; audit_event append-only
  (triggers raise on UPDATE/DELETE/TRUNCATE — 0004), one event per transition, env +
  playbook_tag stamped; job_id stamped on run.finished only (0004, WU-017). internal/runs.Service = ONLY Registry caller (Start → watcher → finalize;
  SweepOrphans on boot; actor='local-dev' until WU-021). POST /api/runs: 400/404/502,
  trail complete either way. engineParams seam is tests-only (API passes nil).
- Run detail + logs (WU-013, SPEC-013): GET /api/runs/{id}/logs = SSE replay→follow,
  ONE `end` event (bounded 2s wait for terminal state — WU-016), 404/410; logs NOT
  persisted. Cancel → 202 async → `canceled`. MockEngine job ids nonce'd; stale ids
  MUST fail (JobID contract in engine.go). Client (WU-019): stream death before
  `end` retries w/ backoff (500ms×2^n cap 8s, 5 attempts), lines kept; placeholder
  only after budget exhausted.
- Notify + Activity (WU-014, SPEC-014): Notifier fired from finalize() post-commit for
  failed|canceled, tracked goroutine, log-only errors; mail = who/what/where/status +
  link ONLY (tests assert absence of reason/error). PORTAL_SMTP_*/PORTAL_NOTIFY_TO
  (empty = off)/PORTAL_BASE_URL. GET /api/runs?instance=&state=&env=&operation=
  (ANDed, unknown → empty). Activity: chips, Now-running, Requester, client CSV
  (WU-019: filter change drops stale view; export fields formula-neutralized;
  revoke deferred).
- Test helpers: `testutil.MigratedDB(t)` scratch DB + migrations (skips w/o compose
  PG); `testutil.DB(t)` dev DB; `testutil.FakeSMTP(t)` capture-only SMTP (one session
  per call). goose down reverts ONE migration.
- Single-binary (WU-006): build:release embeds frontend/dist; binary starts with no DB
  (healthz 503 degraded by design). Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail closed;
  params["mock_fail_at"]="N" injects failure. Only tests reference MockEngine concretely.
- Chassis (WU-003)/frontend (WU-004): config.Load env>file>defaults; server.NewRouter =
  test seam incl. SPA fallback; react-router v7 (`react-router`, NOT react-router-dom);
  tokens ONLY in src/index.css; lib/api.ts typed client (ALL failures = ApiError,
  incl. malformed 2xx — WU-019). tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4, golangci-lint
  2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO Postgres service — DB tests +
  golden flow skip there (icebox); VM gate is the real gate.
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it over.
- Verify-procedure gotcha (bit s05 AND s07): `pkill -f <pattern>` matches the tool
  shell itself → exit 144. Use exact PIDs from pgrep/ss.

## Checkpoint log (last 3, newest first)

- 2026-07-10 — WU-019 done (s10, 3cfdf8f): frontend resilience — SSE retry w/
  backoff, overlay guard, ApiError contract, stale-filter drop, deferred revoke,
  CSV injection neutralized. Implemented by Sonnet 5 subagent (delegation pilot,
  first pass green); architect reviewed/gated/committed. M1-GATE FIXES COMPLETE
  (WU-016..019). Active → WU-020 (fresh session, spec first).
- 2026-07-10 — WU-011R done (s10, df5fadf): last_backup_at wired API→UI
  (user-requested; newest successful dump per instance, live-verified on the
  demo portal). WU-018 done same day (391e955): size_gb canonicalization.
- 2026-07-09 — WU-017 done (s09, 07a12e5): audit hardening — 0004 TRUNCATE trigger +
  REVOKE, audit_event.job_id stamped on run.finished, SPEC-012 §5 claim reconciled
  (live-verified: psql TRUNCATE → append-only). Active → WU-018 (fresh session).
