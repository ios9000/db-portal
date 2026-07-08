# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-020 (AuthN) — **not started**; blocked only on the optional
  M-gate review below. M1/Phase 1 CLOSED 2026-07-08 (s07).
- **Status:** M1 close done (commit c0d1a5e + grooming commit): `docs/demo-m1.md`
  live-verified on the release binary (< 5 min; import ×2 → fleet → dump success +
  artifact + SSE → abort → mailpit mail → prod on mock-prod-… → audit append-only
  proof). Golden-flow e2e = `backend/e2e/golden_flow_test.go` (ADR-011) — runs inside
  `go test -race ./...`, i.e. inside `npm run check`; NO sibling target; skips without
  compose PG (icebox: PG service in CI). Phase 2 groomed: WU-020/021/022/023 have ACs +
  context briefs in BACKLOG; WU-021 grew S → M (carries the Phase 1 deferred ledger).
  Check green both stacks (vitest 52/52).
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **M-gate review (STRATEGY §6/§7, needs user):** multi-agent review pass over the
   Phase 1 code — Workflow-scale, user opts in per gate. Ask; if the user says
   "use a workflow for the M-gate review" (or equivalent), run it: independent
   correctness / security / spec-conformance reviewers + adversarial verification,
   findings become fix-WUs or icebox lines. If declined/deferred, note it here and move on.
2. **WU-020 (AuthN) in a FRESH session:** write `docs/specs/authn.md` FIRST
   (just-in-time; mini-ADRs: session store shape, go-ldap dep, fake-directory seam,
   break-glass alarm action, how the golden-flow e2e authenticates post-authn), then
   implement per the BACKLOG entry. Read: BACKLOG WU-020; ARCHITECTURE §2 (Identity);
   internal/server/middleware.go; cmd/portal/main.go wiring.

## Blocked / needs user

- M-gate review workflow opt-in (see Next action 1). Everything else is clear.

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

- 2026-07-08 — M1 CLOSED (s07): demo-m1.md live-verified, golden-flow e2e in the gate
  (ADR-011), Phase 2 groomed. Active → M-gate review (user opt-in) then WU-020
  (fresh session, spec first).
- 2026-07-07 — WU-015 done (guardrail layers 1–4 live-verified; Phase 1 complete);
  active → M1 close.
- 2026-07-07 — WU-014 done (failure/cancel mail + Screen 6 Activity, live-verified
  incl. unattended orphan-sweep mail); active → WU-015.
