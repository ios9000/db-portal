# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-017 (M1-gate fix: audit hardening — TRUNCATE trigger + job_id) —
  **not started**. WU-016 DONE 2026-07-09 (s08, commit 8531c68).
- **Status:** WU-016 closed all four run-lifecycle gate findings (items 1-3, 9 in
  docs/agent/reviews/m1-gate.md): stranded-job repair in Start (cancel + finalize
  failed, SPEC-012 mini-ADR 7), terminal guard on finalize AND on the watcher's status
  mirror (mini-ADR 8 — the unguarded mirror could resurrect a finalized run; found by
  the new concurrent test, worse than the gate finding), SweepOrphans per-run
  best-effort, SSE `end` waits (bounded 2 s) for the terminal row state. Evidence:
  new -race tests (stranded-repair trail-complete; exactly one run.finished under 3
  racing finalizers; SSE end waits/bounded via server.SetEndStateWait); npm run check
  green both stacks (vitest 52/52); LIVE check — streamed a live dump run, `end`
  carried {"state":"success"} (the gate's live repro emitted "running"). Test seams:
  export_test.go in runs (SetFailRecordJobID, Finalize) + server (SetEndStateWait).
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-017 in a fresh session:** audit hardening migration 0004 — BEFORE TRUNCATE
   statement trigger reusing audit_event_immutable() + REVOKE TRUNCATE; add
   `job_id text NULL` to audit_event, stamped on run.finished; reconcile SPEC-012
   mini-ADR 1's "every §5 field" claim. Read: BACKLOG WU-017; m1-gate.md items 4-5;
   0003_runs_audit.sql; runs/service.go audit INSERTs; migrate_test down-walk.
2. Then WU-018 → 019 (order fixed; 019's CSV-injection beat MUST precede WU-020).
3. **WU-020 (AuthN) after the gate fixes, FRESH session:** write `docs/specs/authn.md`
   FIRST (mini-ADRs: session store shape, go-ldap dep, fake-directory seam, break-glass
   alarm action, golden-flow e2e authentication), then implement per BACKLOG.

## Blocked / needs user

- Nothing. (Paused workflow run wf_3ba143f2-9c0 can be ignored/discarded — its results
  are harvested into docs/agent/reviews/m1-gate.md.)

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
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
  engine.ClassForEnv (single authority).
- Instance API (WU-011): GET /api/instances[?env=] ordered/never-null/snake_case,
  detail 404 JSON. server.InstanceReader = handler seam. Frontend MyDatabases
  cards/table, view+env in URL params.
- Runs (WU-012, SPEC-012 = docs/specs/runs.md): run mutable; audit_event append-only
  (trigger raises on UPDATE/DELETE), one event per transition, env + playbook_tag
  stamped. internal/runs.Service = ONLY Registry caller (Start → watcher → finalize;
  SweepOrphans on boot; actor='local-dev' until WU-021). POST /api/runs: 400/404/502,
  trail complete either way. engineParams seam is tests-only (API passes nil).
- Run detail + logs (WU-013, SPEC-013): GET /api/runs/{id}/logs = SSE replay→follow,
  ONE `end` event, 404/410; logs NOT persisted. Cancel → 202 async → `canceled`.
  MockEngine job ids nonce'd; stale ids MUST fail (JobID contract in engine.go).
- Notify + Activity (WU-014, SPEC-014): Notifier fired from finalize() post-commit for
  failed|canceled, tracked goroutine, log-only errors; mail = who/what/where/status +
  link ONLY (tests assert absence of reason/error). PORTAL_SMTP_*/PORTAL_NOTIFY_TO
  (empty = off)/PORTAL_BASE_URL. GET /api/runs?instance=&state=&env=&operation=
  (ANDed, unknown → empty). Activity: chips, Now-running, Requester, client CSV.
- Test helpers: `testutil.MigratedDB(t)` scratch DB + migrations (skips w/o compose
  PG); `testutil.DB(t)` dev DB; `testutil.FakeSMTP(t)` capture-only SMTP (one session
  per call). goose down reverts ONE migration.
- Single-binary (WU-006): build:release embeds frontend/dist; binary starts with no DB
  (healthz 503 degraded by design). Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail closed;
  params["mock_fail_at"]="N" injects failure. Only tests reference MockEngine concretely.
- Chassis (WU-003)/frontend (WU-004): config.Load env>file>defaults; server.NewRouter =
  test seam incl. SPA fallback; react-router v7 (`react-router`, NOT react-router-dom);
  tokens ONLY in src/index.css; lib/api.ts typed client. tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4, golangci-lint
  2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO Postgres service — DB tests +
  golden flow skip there (icebox); VM gate is the real gate.
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it over.
- Verify-procedure gotcha (bit s05 AND s07): `pkill -f <pattern>` matches the tool
  shell itself → exit 144. Use exact PIDs from pgrep/ss.

## Checkpoint log (last 3, newest first)

- 2026-07-09 — WU-016 done (s08, 8531c68): run lifecycle integrity — stranded-job
  repair, single-finalizer guards (incl. watcher mirror), sweep best-effort, SSE end
  terminal state (live-verified). Active → WU-017 (fresh session).
- 2026-07-09 — M-gate review DONE (s08): 17 confirmed findings (2 high) → WU-016..019
  + icebox + WU-021 ledger; 3 refuted; record in docs/agent/reviews/m1-gate.md.
  Active → WU-016 (fresh session).
- 2026-07-08 — M1 CLOSED (s07): demo-m1.md live-verified, golden-flow e2e in the gate
  (ADR-011), Phase 2 groomed. Active → M-gate review (user opt-in) then WU-020
  (fresh session, spec first).
- 2026-07-07 — WU-014 done (failure/cancel mail + Screen 6 Activity, live-verified
  incl. unattended orphan-sweep mail); active → WU-015.
