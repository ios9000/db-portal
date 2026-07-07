# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-011 — Instance API + cards/table UI. **Not started.**
- **Status:** WU-010 DONE 2026-07-07 (s04, commit 5212481): migration 0002
  (cluster/instance/inventory_import/inventory_import_reject), `internal/inventory`
  importer (SPEC-010 behaviors 1–8 tested), `portal import <file>` CLI. Verified
  live: fixture ×2 idempotent (8 new → 8 unchanged), malformed.csv → 7 quarantined
  with reasons queryable. Gate green both stacks.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-011 from its BACKLOG entry: list/detail endpoints with env filter +
cards view (design brief Screen 1: health dot, badges, last-backup line) +
`Cards ⇄ Table` toggle (Screen 2, minus bulk actions). Read design brief
Screens 1–2 + BACKLOG entry FIRST. Backend: query the WU-010 tables (join
instance→cluster); health/last-backup render "—" (no data source until
WU-012+, per SPEC-010). Frontend: reuse EnvBadge (WU-004). Dev DB is already
migrated + fixture-loaded (8 instances / 6 clusters) — `npm run up` if compose
is down, re-import via `cd backend && go run ./cmd/portal import
../infra/fixtures/instances.csv` (idempotent).

## Blocked / needs user

- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.
- Nothing else.

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
- Test helper NEW: `testutil.MigratedDB(t)` = scratch DB + embedded migrations up,
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
  via Registry (starts in WU-012).
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults;
  server.NewRouter(log, Pinger) is what tests exercise (includes SPA fallback);
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

- 2026-07-07 — WU-010 done (5212481); dev DB fixture-loaded; active → WU-011.
- 2026-07-07 — Phase 1 groomed (SPEC-010 + fixture); active → WU-010 (fresh session).
- 2026-07-07 — M0 CLOSED (demo-m0.sh PASS, 9568239); gh authed, CI verified (7508614).
