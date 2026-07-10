# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-019 (M1-gate fix: frontend resilience) — **not started**.
  WU-018 DONE 2026-07-10 (s10, commit 391e955).
- **Status:** WU-018 closed gate item 8 (docs/agent/reviews/m1-gate.md): size_gb is
  canonicalized at parse time (strconv.FormatFloat plain decimal, -0 folded) so the
  store and the unchanged-row compare see one form — hex floats never reach the
  ::numeric cast (no whole-import abort), PG-normalized forms ('1e2'→'100',
  '.5'→'0.5') no longer re-import as "updated" forever; unparseable forms still
  quarantine. SPEC-010 CSV contract updated. Evidence: parse tests (1e2/.5/0120/
  0x1p4/+7/-0 → canonical; inf + truncated hex → quarantine reasons);
  TestImportSizeGBFormsIdempotent (4 exotic forms import, stored text = canonical,
  re-import all-unchanged); npm run check green both stacks (vitest 52/52); LIVE
  CLI import x2 on dev DB: "3 new, 1 quarantined (0x1p)" then "3 unchanged, 1
  quarantined" — stored 100/0.5/16; dev DB restored (test rows deleted).
  NOTE: s10 resumed uncommitted WU-018 work left by an SSH-reset-killed session —
  the tree-wins rule worked; code was reviewed against the brief, then verified.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **WU-019 in a fresh session:** frontend resilience — gate items 6-7 + lows 10-13:
   RunDetail/api.ts SSE transient failure must retry/backoff keeping lines (not
   permanent "logs gone"); LaunchDrawer overlay click must not dismiss mid-launch;
   getJSON/postJSON wrap res.json() → ApiError on malformed 2xx; Activity drops stale
   fetch responses; exportCsv defers revokeObjectURL; csv.ts field() neutralizes
   leading `=+-@\t` (OWASP CSV injection — MUST precede WU-020's real usernames).
   Read: BACKLOG WU-019; m1-gate.md items 6-7, 10-13; frontend/src/lib/{api,csv}.ts,
   components/LaunchDrawer.tsx, pages/{RunDetail,Activity}.tsx; SPEC-013/014/015.
2. **WU-020 (AuthN) after, FRESH session:** write `docs/specs/authn.md` FIRST
   (mini-ADRs: session store shape, go-ldap dep, fake-directory seam, break-glass
   alarm action, golden-flow e2e authentication), then implement per BACKLOG.

## Blocked / needs user

- Nothing. (Paused workflow run wf_3ba143f2-9c0 can be ignored/discarded — its results
  are harvested into docs/agent/reviews/m1-gate.md.)
- HEADS-UP: a demo portal may be running as transient systemd unit `dbportal-demo`
  on :8080 (started s10 for the user, survives SSH drops; binary of commit 391e955,
  publicly reachable — no authn until WU-020). Before any live check that runs its
  own portal: `systemctl stop dbportal-demo` — else bind-in-use + the s05
  two-portals-one-DB sweep hazard.

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
  engine.ClassForEnv (single authority). size_gb canonicalized at parse (WU-018) —
  plain-decimal FormatFloat text is what stores and compares.
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

- 2026-07-10 — WU-018 done (s10, 391e955): size_gb canonicalized at parse time —
  hex floats can't abort imports, normalized forms can't churn updated_at
  (live-verified: CLI import x2 → 3 new/1 quarantined then 3 unchanged).
  Active → WU-019 (fresh session).
- 2026-07-09 — WU-017 done (s09, 07a12e5): audit hardening — 0004 TRUNCATE trigger +
  REVOKE, audit_event.job_id stamped on run.finished, SPEC-012 §5 claim reconciled
  (live-verified: psql TRUNCATE → append-only). Active → WU-018 (fresh session).
- 2026-07-09 — WU-016 done (s08, 8531c68): run lifecycle integrity — stranded-job
  repair, single-finalizer guards (incl. watcher mirror), sweep best-effort, SSE end
  terminal state (live-verified). Active → WU-017 (fresh session).
