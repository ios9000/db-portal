# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** M1 close — demo script + golden-flow e2e + Phase 2 grooming. **Not started.**
- **Status:** WU-015 DONE 2026-07-07 (s06, commit fc460d9): all four ARCHITECTURE
  §4 guardrail layers live. SPEC-015 = docs/specs/guardrails.md. EnvBanner
  (text+color, never color alone) on RunDetail + LaunchDrawer — single-env
  contexts only, fleet screens keep per-row badges. Prod launch = typed exact
  instance-name confirm (case-sensitive, paste/drop disabled, placeholder shows
  the name — friction, not memory); non-prod one click unchanged. NOTE: the
  ritual is client-side friction only — the API stays deliberately unguarded
  until authz (WU-021). Registry.Register now PANICS if one adapter instance is
  wired for two env classes (fail closed at boot). Tests pin env stamping: prod
  audit rows both 'prod', audit_event.environment NULL → 23502. Verified live
  on the release binary (run 10 → mock-prod job, audit stamped). Closes
  WU-012's interim 1-click-prod risk. ALL Phase 1 WUs done; check green.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Close M1 in a FRESH session. (1) Script `docs/demo-m1.md` per the BACKLOG
WU-015 M1-exit paragraph: import CSV → see fleet → 1-click dump on a TEST
instance → live logs → success with artifact → failure case with audit row +
email → typed-name ritual on PROD; must run start-to-finish < 5 min. (2)
Automate that script as the golden-flow e2e test and add it to the global
gate (`npm run check` or a sibling target it calls — decide there, mini-ADR).
(3) At M1 close, groom Phase 2 (WU-020 authn, WU-021 authz, WU-022 scheduler,
WU-023 windows): specs just-in-time, size check, icebox sweep. Read: BACKLOG
Phase 2 list; the M1-exit paragraph under WU-015; infra/demo-m0.sh as the
prior demo-script shape.

## Blocked / needs user

- Nothing.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Guardrails (WU-015 landed, SPEC-015 = docs/specs/guardrails.md): EnvBanner
  component (components/EnvBanner.tsx) on RunDetail + LaunchDrawer; prod
  ritual in LaunchDrawer (`confirmed` gate on the primary button); Registry
  panics on cross-class adapter sharing; env stamping pinned by tests
  (23502 schema test + prod-rows assertions in runs service tests).
  Required-reason-on-prod deferred with approvals (icebox); window warning
  → WU-023; ritual actor/authz → WU-020/021.
- Inventory (WU-010 landed): tables cluster (name UNIQUE, platform CHECK
  k8s_patroni|vm), instance (name = natural key UNIQUE, env CHECK dev|test|prod,
  size_gb numeric NULL, maintenance_window raw text — WU-022 owns semantics),
  inventory_import + inventory_import_reject (quarantine: row_number, raw verbatim,
  reasons text[]). `portal import <file.csv>` = idempotent CLI import; exit 1 =
  file-level failure, nothing written; report line
  `imported N new, updated M, unchanged U, quarantined Q`, reject detail → stderr.
  Env validity delegated to engine.ClassForEnv (single authority — never bypass).
  NO last_backup/health columns (derived later; WU-011 renders "—").
- Instance API (WU-011 landed): inventory.Store (reads; writes only via Import);
  GET /api/instances[?env=] → {"instances":[...]} (ordered by name, never null,
  snake_case fields, nullable → JSON null), /api/instances/{name} → 404 JSON
  {"error":...}. server.InstanceReader iface = handler seam (stub in CI, Store
  satisfies). Frontend: MyDatabases cards/table, view+env in URL search params,
  search client-side, env filter server-side; "—" placeholders for
  health/backup/vacuum/bloat.
- Runs (WU-012 landed, SPEC-012 = docs/specs/runs.md): `run` = mutable
  operational row; `audit_event` = append-only (trigger raises on
  UPDATE/DELETE — works even for the dev table owner), one event per
  transition (run.submitted / run.finished), env + playbook_tag stamped.
  internal/runs.Service = ONLY place touching engine Registry: Start (run +
  submitted audit in one tx → Registry.For fails closed → StartJob → watcher
  goroutine polls Status → finalize), SweepOrphans on boot ("engine job lost
  (portal restart)"), actor='local-dev' until WU-020/021. internal/catalog =
  static op data (dump only), engine fields never serialized. API: GET
  /api/operations, POST /api/runs (400 unknown op / 404 unknown instance /
  502 engine refused — trail complete either way). Router deps =
  server.Deps{DB, Instances, Runs}. main wires mock-prod + mock-nonprod.
- Run detail + logs (WU-013 landed, SPEC-013 = docs/specs/run-detail.md):
  GET /api/runs/{id}/logs = SSE (`log` events replay→follow, ONE `end`
  event, 15s keepalive; 404 unknown, 410 logs-gone; logs NOT persisted by
  decision). POST /api/runs/{id}/cancel → 202 async (409/502); watcher
  finalizes `canceled`; NO cancel audit action until WU-021. MockEngine job
  ids nonce'd (`mock-nonprod-<hex>-N`) — stale ids MUST fail (JobID contract
  in engine.go). Frontend: /runs/:id (RunDetail), SSE via openRunLogStream
  (buffer resets on reconnect replay), Follow pill, Abort locks until poll
  shows terminal; lib/format.ts shared.
- Notify + Activity (WU-014 landed, SPEC-014 = docs/specs/activity-notify.md):
  runs.Service.Notifier iface field (nil = off) fired from finalize() for
  failed|canceled AFTER commit, tracked goroutine, log-only errors —
  internal/notify.Mailer = SMTP impl (plain, no auth = mailpit-shaped).
  Mail content contract: who/what/where/status + PORTAL_BASE_URL/runs/{id}
  ONLY — never reason/error/params (tests assert absence). Config:
  PORTAL_SMTP_HOST/PORT/FROM, PORTAL_NOTIFY_TO (comma list, EMPTY = off —
  main logs which), PORTAL_BASE_URL. GET /api/runs filters:
  ?instance=&state=&env=&operation= (runs.ListFilter, ANDed, unknown value
  → empty list never error); run JSON has requested_by (actor of the
  run.submitted audit event, via scalar subquery — not a run column).
  Frontend: Activity = chips (state/env/op in URL params) + Now-running
  section (indeterminate bar, 1s elapsed tick) + history table w/ Requester
  + Export CSV (lib/csv.ts, table columns only, no error/reason). Deferred:
  user filter → WU-021; date range/pagination/server export → icebox.
- Test helper: `testutil.MigratedDB(t)` = scratch DB + embedded migrations up,
  dropped on cleanup; use for schema-touching DB tests. `testutil.DB(t)` = dev DB,
  skips without compose PG. `goose down` reverts ONE migration (migrate_test walks).
  internal/notify tests run an in-test SMTP server (no compose dependency).
- Single-binary (WU-006): `internal/webui.Handler()` = embedded SPA; build:release
  copies `frontend/dist` → `backend/internal/webui/dist/` (gitignored except
  .gitkeep). Dev unchanged: Vite :5173 proxies /api + /healthz → Go :8080. Binary
  starts with no DB (lazy pool; /healthz → 503 degraded) — designed, not a bug.
  `sh infra/demo-m0.sh` = golden-thread smoke test.
- Engine seam (WU-005): `internal/engine` — engine.Adapter iface; Registry.For(class)
  + ClassForEnv(env) both fail closed; Register panics on cross-class adapter
  sharing (WU-015); MockEngine via NewMockEngine; StreamLogs replays then
  follows; Cancel async + idempotent; params["mock_fail_at"]="N" injects
  failure. Only tests reference MockEngine concretely — portal wiring goes
  via Registry.
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults;
  server.NewRouter(log, Deps) is what tests exercise (includes SPA fallback);
  goose migrations embedded; `portal migrate up|down|status`; testutil.DB(t) skips
  when compose PG absent (CI-safe).
- Frontend (WU-004): react-router v7 API (`react-router` package, NOT react-router-dom);
  tokens ONLY in src/index.css (hex reconcile vs docs/specs/design-brief.md =
  icebox); lib/api.ts typed client (status 0 = unreachable; healthz 503 = degraded,
  not an error). tsc strict ON.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (authed as ios9000), git 2.43, `file`. CI =
  check.yml (no Postgres service — DB tests + scratch-DB tests skip; icebox:
  action bump + go cache path).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key.
- Research corpus stays on the workstation (ADR-006); design brief extracted to
  docs/specs/. Need another research file? Ask the user to copy it over.
- Windows-era forensics: JOURNAL 2026-07-06 + ADR-008. Do not resurrect WSL.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — WU-015 done (guardrail layers 1–4 live-verified; Phase 1
  complete); active → M1 close: demo-m1.md + golden-flow e2e + Phase 2
  grooming (fresh session).
- 2026-07-07 — WU-014 done (failure/cancel mail + Screen 6 Activity,
  live-verified incl. unattended orphan-sweep mail); active → WU-015 (fresh
  session).
- 2026-07-07 — WU-013 done (SSE logs + cancel + RunDetail, live-verified; mock
  job-id aliasing bug fixed); active → WU-014 (fresh session).
