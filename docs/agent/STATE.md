# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** none — **M0 CLOSED 2026-07-07** (`infra/demo-m0.sh` PASS: gate
  green, TestHappyPathDump visible, binary served shell + healthz standalone).
  Next up: Phase 1 grooming, then WU-010.
- **Status:** WU-006 closed 2026-07-07 (0922576): `internal/webui` embeds the SPA
  (`all:dist`, .gitkeep anchor → compiles without a frontend build, runtime
  placeholder page in that state); SPA fallback mounted as router NotFound (client
  routes → index.html, /api misses stay plain 404, hashed assets immutable-cached);
  `npm run build:release` → static 17M `backend/bin/portal` (CGO_ENABLED=0,
  -trimpath). Verified running alone from an empty dir.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Groom Phase 1 (fresh session): re-read BACKLOG WU-010…015 against ARCHITECTURE.md
§Inventory + §6.1/§8.1 and the design brief (now at docs/specs/design-brief.md);
write `docs/specs/inventory.md` just-in-time for WU-010 (template:
docs/specs/SPEC-TEMPLATE.md); create the sample fixture CSV in `infra/fixtures/`;
size-check each WU still fits a session. Then start WU-010 per its entry.
Re-run `sh infra/demo-m0.sh` any time the chassis feels doubtful — it's the
golden-thread smoke test now.

## Blocked / needs user

- **`gh auth login`** (user, one-time, device flow): gh 2.96.0 installed but
  unauthenticated — CI runs for today's pushes (now 8+) still UNVERIFIED from the VM.
  Local gate green throughout.
- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.
- ~~Design brief extract~~ RESOLVED 2026-07-07: user copied full brief to VM; now at
  `docs/specs/design-brief.md` (88a66a6). Token-reconcile icebox item is unblocked.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Single-binary (WU-006): `internal/webui.Handler()` = embedded SPA; release builds
  copy `frontend/dist` → `backend/internal/webui/dist/` (gitignored except .gitkeep).
  Dev unchanged: Vite :5173 proxies /api + /healthz → Go :8080. Binary starts with
  no DB (pool is lazy; /healthz → 503 degraded) — that's designed, not a bug.
- Engine seam (WU-005): `internal/engine` — engine.Adapter iface; Registry.For(class)
  + ClassForEnv(env) both fail closed; MockEngine via NewMockEngine(MockConfig{Name,
  StepDelay}); StreamLogs replays history then follows live; Cancel async + idempotent;
  params["mock_fail_at"]="N" injects failure at step N. Only tests may reference
  MockEngine concretely (AC) — wiring into the portal happens in feature WUs via Registry.
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults;
  server.NewRouter(log, Pinger) is what tests exercise (now includes SPA fallback);
  goose migrations embedded; `portal migrate up|down|status`; testutil.DB(t) skips
  when compose PG absent (CI-safe).
- Frontend (WU-004): react-router v7 API (`react-router` package, NOT react-router-dom);
  tokens ONLY in src/index.css (hexes still placeholder — reconcile vs
  docs/specs/design-brief.md is an icebox item); lib/api.ts typed client (status 0 =
  unreachable; healthz 503 = degraded, not an error). tsc strict ON.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (unauthenticated), git 2.43, `file` added 2026-07-07.
  CI = check.yml (no Postgres service — DB tests skip).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); design brief now extracted to docs/specs/. Need another
  research file on the VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — M0 CLOSED (demo-m0.sh PASS); active → Phase 1 grooming (fresh session).
- 2026-07-07 — WU-006 done (0922576); design brief chore (88a66a6); active → M0 exit check.
- 2026-07-07 — WU-005 done (6124732, evidence in BACKLOG/JOURNAL); active → WU-006.
