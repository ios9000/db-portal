# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-014 — Audit UI + email notify. **Not started.**
- **Status:** WU-013 DONE 2026-07-07 (s05, commits 29f81ee + f256b9b + 7d4422f):
  SPEC-013, GET /api/runs/{id}/logs (SSE bridge over engine.StreamLogs:
  replay → follow → one `end` event, 410 when logs are gone), POST
  /api/runs/{id}/cancel (202 async, watcher finalizes `canceled`), RunDetail
  page (/runs/:id — stage panel, dark log pane, Follow pill, live elapsed,
  artifact strip, failed/canceled cards, Abort), links from Activity ids +
  drawer "View run". Bug fixed en route: MockEngine job ids now nonce'd so
  stale job_ids never alias to new jobs. All verified live on the release
  binary; check green both stacks.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-014 from its BACKLOG entry in a FRESH session: Activity/history view
(design brief Screen 6, minus approval rows — filters/export land here) +
failure email via mailpit with a run link. Email content: who/what/where/
status ONLY — no params, no log excerpts (leak risk). Compose already runs
mailpit (dbportal-dev-mailpit-1, SMTP + HTTP UI). Hook the send into the
finalize path in internal/runs (service is the single finalization point —
watcher, engine-refusal and orphan sweep all pass through finalize(), so one
seam covers all failure shapes). Needs SMTP config in config.Load (.env
shape documented in .env.example, no secrets committed). Verify per BACKLOG:
kill a mock run (mock_fail_at injection or abort) → mailpit shows the mail
with the run link; audit row immutable (UPDATE attempt fails). Read: BACKLOG
WU-014 entry; design brief Screen 6; SPEC-012 §audit; internal/runs/service.go
finalize().

## Blocked / needs user

- Nothing.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
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
  502 engine refused — trail complete either way), GET /api/runs[?instance=]
  + /{id}. Router deps = server.Deps{DB, Instances, Runs}. main wires
  mock-prod + mock-nonprod MockEngines. Frontend: LaunchDrawer (consequence-
  labeled button), Activity polls 3s live / 10s idle; RunStatus has canceled.
- Run detail + logs (WU-013 landed, SPEC-013 = docs/specs/run-detail.md):
  GET /api/runs/{id}/logs = SSE (`log` events {ts,line} — replay then follow
  per the WU-005 adapter contract — then ONE `end` event {state}, 15s
  keepalive comments; 404 unknown, 410 logs-gone: no job / engine lost it;
  logs are NOT persisted, by decision). POST /api/runs/{id}/cancel → 202
  async (409 not-cancelable / 502 engine refused); watcher finalizes
  `canceled`; NO cancel audit action until WU-021. runs.Service.StreamLogs +
  Cancel = still the only Registry callers. Known spec'd lag: end-event state
  can trail the watcher one poll; the UI closes the stream and re-polls.
  MockEngine job ids are nonce'd (`mock-nonprod-<hex>-N`) — stale ids MUST
  fail (JobID contract in engine.go). Frontend: /runs/:id (RunDetail), SSE
  via lib/api.ts openRunLogStream (buffer resets on reconnect replay), Follow
  pill, Abort locks until poll shows terminal; lib/format.ts shared by
  Activity + RunDetail.
- Test helper: `testutil.MigratedDB(t)` = scratch DB + embedded migrations up,
  dropped on cleanup; use for schema-touching DB tests. `testutil.DB(t)` = dev DB,
  skips without compose PG. `goose down` reverts ONE migration (migrate_test walks).
- Single-binary (WU-006): `internal/webui.Handler()` = embedded SPA; build:release
  copies `frontend/dist` → `backend/internal/webui/dist/` (gitignored except
  .gitkeep). Dev unchanged: Vite :5173 proxies /api + /healthz → Go :8080. Binary
  starts with no DB (lazy pool; /healthz → 503 degraded) — designed, not a bug.
  `sh infra/demo-m0.sh` = golden-thread smoke test.
- Engine seam (WU-005): `internal/engine` — engine.Adapter iface; Registry.For(class)
  + ClassForEnv(env) both fail closed; MockEngine via NewMockEngine; StreamLogs
  replays then follows; Cancel async + idempotent; params["mock_fail_at"]="N"
  injects failure. Only tests reference MockEngine concretely — portal wiring goes
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

- 2026-07-07 — WU-013 done (SSE logs + cancel + RunDetail, live-verified; mock
  job-id aliasing bug fixed); active → WU-014 (fresh session).
- 2026-07-07 — WU-012 done (hero flow live-verified); active → WU-013.
- 2026-07-07 — WU-011 done (bc04f2d); active → WU-012 (hero flow).
