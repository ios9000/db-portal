# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-012 — Catalog + run-now dump (hero). **Not started.**
  O-1 note: mock artifact path is acceptable (metadata only) — not blocked.
- **Status:** WU-011 DONE 2026-07-07 (s04, commit bc04f2d): inventory.Store +
  GET /api/instances[?env=] + /{name}; MyDatabases cards/table UI (Screens 1–2
  minus bulk actions). WU-010 DONE same session (5212481): migration 0002,
  importer + quarantine, `portal import` CLI. Both CI-green.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-012 from its BACKLOG entry (consider a just-in-time SPEC-012 first,
like SPEC-010 — the WU is the widest in Phase 1; split note in BACKLOG: if it
runs heavy, land audit migration + append-only grant tests, checkpoint, then
the flow). Scope: operation catalog (data-driven, `dump` only), launch drawer
(Screen 3 pattern), POST run → audit record (`submitted`) → MockEngine job via
Registry (first portal wiring of the engine seam!) → status polling → run list
in Activity. Audit schema per ARCHITECTURE.md §5: append-only from the first
migration — app role gets NO UPDATE/DELETE grants; artifact = metadata only
(O-1 mock path). Read ARCHITECTURE §3 (hero workflow) + §5 (audit), design
brief Screen 3, WU-005 adapter contract. Dev DB is migrated + fixture-loaded;
`npm run up` if compose is down.

## Blocked / needs user

- Nothing. (O-1 artifact storage: WU-012 proceeds with mock/metadata-only path
  per BACKLOG; real storage = minio, WU-035.)

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
  satisfies). NewRouter(log, Pinger, InstanceReader). Frontend: MyDatabases
  cards/table, view+env in URL search params, search client-side, env filter
  server-side; placeholders "—" for health/backup/vacuum/bloat.
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

- 2026-07-07 — WU-011 done (bc04f2d); active → WU-012 (hero flow).
- 2026-07-07 — WU-010 done (5212481); dev DB fixture-loaded; active → WU-011.
- 2026-07-07 — Phase 1 groomed (SPEC-010 + fixture); active → WU-010 (fresh session).
