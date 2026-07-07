# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-010 — Inventory schema + CSV import (first Phase 1 WU).
  **Not started**; fully groomed and ready.
- **Status:** Phase 1 GROOMED 2026-07-07 (s03): SPEC-010 written
  (`docs/specs/inventory.md` — schema, CSV contract, 8 numbered behaviors,
  `portal import` CLI decision), fixture created (`infra/fixtures/instances.csv`,
  the 8 design-brief instances), BACKLOG § refs fixed to the real ARCHITECTURE
  outline, WU-012 split-if-heavy note added. Before that: M0 CLOSED
  (`infra/demo-m0.sh` PASS, 9568239), WU-006 done (0922576), gh authed + ALL
  CI runs verified green from the VM.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-010 from `docs/specs/inventory.md` (read it FIRST — it is the contract;
BACKLOG entry is the summary). Order inside the WU: goose migration (cluster,
instance, inventory_import, inventory_import_reject + CHECKs) → importer package
(`internal/inventory`) with the 8 spec behaviors as table-driven tests
(testutil.DB(t) pattern — skips without compose PG, so keep pure-parse tests
DB-free) → `portal import <file>` subcommand → run `npm run up` + import fixture
twice → verify BACKLOG line (same counts, quarantine reasons visible). Malformed
fixture ships WITH the tests. Gate + checkpoint per protocol.

## Blocked / needs user

- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.
- Nothing else. (gh auth RESOLVED 2026-07-07 — authed as ios9000, CI verified green,
  run 28864460359. Design brief RESOLVED — at `docs/specs/design-brief.md`, 88a66a6.)

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Inventory (SPEC-010 decisions): env enum dev|test|prod, instance.name = natural
  key, clusters auto-created, first-row-wins on in-file dups, import via CLI
  subcommand (no auth exists yet), maintenance_window stored RAW (O-3 field;
  WU-022 owns semantics), NO last_backup/health columns (derived later, WU-011
  renders "—").
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
  check.yml (no Postgres service — DB tests skip; icebox: action bump + go cache path).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key.
- Research corpus stays on the workstation (ADR-006); design brief extracted to
  docs/specs/. Need another research file? Ask the user to copy it over.
- Windows-era forensics: JOURNAL 2026-07-06 + ADR-008. Do not resurrect WSL.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — Phase 1 groomed (SPEC-010 + fixture); active → WU-010 (fresh session).
- 2026-07-07 — M0 CLOSED (demo-m0.sh PASS, 9568239); gh authed, CI verified (7508614).
- 2026-07-07 — WU-006 done (0922576); design brief chore (88a66a6).
