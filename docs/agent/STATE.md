# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-006 — Single-binary production build (go:embed of frontend/dist)
- **Status:** not started. WU-005 closed 2026-07-07 (6124732): engine seam live —
  engine.Adapter (ExecutionAdapter, ADR-002), MockEngine (goroutines/channels, failure
  injection, log replay), Registry keyed prod|nonprod failing closed. 12 tests -race.
  Earlier today: WU-003 (aee80cc, Go chassis), WU-004 (2304b9c, frontend shell).
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-006 per its BACKLOG entry, ON THE VM: `go:embed` of `frontend/dist` (build
copies into `backend/internal/webui/dist/`), SPA fallback handler (non-/api 404s →
index.html), root script `build:release` = fe build → copy → `go build -trimpath` →
`backend/bin/portal`; binary must compile with a placeholder dist when frontend wasn't
built. Verify: run binary alone, curl `/` (HTML) + `/healthz` (JSON). WU-006 closes
Phase 0 → then M0 exit check (golden thread demo script) before Phase 1 grooming.

## Blocked / needs user

- **`gh auth login`** (user, one-time, device flow): gh 2.96.0 is installed on the VM
  + codified in bootstrap-vm.sh, but unauthenticated — CI runs for today's 5 pushes
  (WU-003/004/005 + 2 chores) are still UNVERIFIED from the VM. Local gate green.
- **Design brief extract** (nice-to-have): copy §Design system + Screen 1 from
  `P:/Projects/db-portal-research/05-claude-design-brief.md` to the VM (docs/specs/)
  so WU-004's placeholder hex tokens can be reconciled (icebox item).
- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Engine seam (WU-005): `internal/engine` — engine.Adapter iface; Registry.For(class)
  + ClassForEnv(env) both fail closed; MockEngine via NewMockEngine(MockConfig{Name,
  StepDelay}); StreamLogs replays history then follows live, channel closes on job end
  or stream-ctx cancel (job keeps running); Cancel async + idempotent;
  params["mock_fail_at"]="N" injects failure at zero-based step N. Only tests may
  reference MockEngine concretely (AC) — wiring into the portal happens in feature WUs
  via Registry.
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults without
  mutating process env; server.NewRouter(log, Pinger) is what tests exercise;
  migrations in backend/internal/db/migrations/ (goose, embed.FS);
  `portal migrate up|down|status`; testutil.DB(t) skips when compose PG absent (CI-safe).
- Frontend (WU-004): react-router v7 (`react-router` package, NOT react-router-dom);
  tokens live ONLY in src/index.css; components/{EnvBadge,RunStatus,StatusFooter};
  lib/api.ts = typed client (ApiError, status 0 = unreachable; healthz 503 = degraded,
  not an error). Vite proxies /api + /healthz → :8080. tsc strict is ON.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (unauthenticated), git 2.43. CI = check.yml
  (no Postgres service — DB tests skip).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); key extracts already in docs/. Need a research file on the
  VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — WU-005 done (6124732, evidence in BACKLOG/JOURNAL); active → WU-006.
- 2026-07-07 — WU-004 done (2304b9c); gh CLI installed on VM (297538a).
- 2026-07-07 — WU-003 done (aee80cc).
