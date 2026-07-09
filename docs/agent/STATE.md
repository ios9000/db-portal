# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-018 (M1-gate fix: inventory size_gb canonicalization) —
  **not started**. WU-017 DONE 2026-07-09 (s09, commit 07a12e5).
- **Status:** WU-017 closed gate items 4-5 (docs/agent/reviews/m1-gate.md): migration
  0004 adds a statement-level BEFORE TRUNCATE trigger reusing audit_event_immutable()
  + REVOKE TRUNCATE (same trigger-enforces/REVOKE-documents split as 0003), and
  `audit_event.job_id text NULL` stamped from run.job_id on `run.finished` via the
  finalize INSERT..SELECT JOIN (NULL at submit is honest — the id doesn't exist yet).
  SPEC-012 reconciled: mini-ADR 1 now says §5 fields appear on the event PAIR
  (final_status + job_id finished-only); data section covers 0003+0004. Evidence:
  TestAuditEventIsAppendOnly extended with TRUNCATE → "append-only"; TestMigrateUpDown
  walks 0004 down (job_id gone, table intact) then 0003→0001; golden flow requireAudit
  asserts job_id '' on submitted / non-empty on finished for all three runs; npm run
  check green both stacks (vitest 52/52); LIVE dev DB: migrate up applied 0004, psql
  `TRUNCATE audit_event` → ERROR "audit_event is append-only".
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-018 in a fresh session:** size_gb canonicalization — canonicalize SizeGB at
   parse time (strconv.FormatFloat) so store and compare see one form; PG-rejectable
   forms (hex floats) must quarantine the row, never abort the import. Read: BACKLOG
   WU-018; m1-gate.md item 8; internal/inventory/csv.go (size_gb parse + finite
   check); import.go:140-165 (canonical compare); SPEC-010.
2. Then WU-019 (frontend resilience; its CSV-injection beat MUST precede WU-020).
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
  (triggers raise on UPDATE/DELETE/TRUNCATE — 0004), one event per transition, env +
  playbook_tag stamped; job_id stamped on run.finished only (0004, WU-017). internal/runs.Service = ONLY Registry caller (Start → watcher → finalize;
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

- 2026-07-09 — WU-017 done (s09, 07a12e5): audit hardening — 0004 TRUNCATE trigger +
  REVOKE, audit_event.job_id stamped on run.finished, SPEC-012 §5 claim reconciled
  (live-verified: psql TRUNCATE → append-only). Active → WU-018 (fresh session).
- 2026-07-09 — WU-016 done (s08, 8531c68): run lifecycle integrity — stranded-job
  repair, single-finalizer guards (incl. watcher mirror), sweep best-effort, SSE end
  terminal state (live-verified). Active → WU-017 (fresh session).
- 2026-07-09 — M-gate review DONE (s08): 17 confirmed findings (2 high) → WU-016..019
  + icebox + WU-021 ledger; 3 refuted; record in docs/agent/reviews/m1-gate.md.
  Active → WU-016 (fresh session).
