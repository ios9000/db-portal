# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-005 — ExecutionAdapter + MockEngine (the engine seam, ADR-002)
- **Status:** not started. WU-004 closed 2026-07-07 (2304b9c): frontend shell live —
  react-router nav, EnvBadge/RunStatus (all variants tested), typed API client,
  StatusFooter probing /healthz, tokens in index.css, tsc strict, vitest 18/18.
  WU-003 closed same day (aee80cc): Go chassis (chi, config, /healthz, goose, slog).
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-005 per its BACKLOG entry, ON THE VM: `ExecutionAdapter` Go interface
(StartJob/Status/StreamLogs/Cancel), goroutine-driven MockEngine with realistic timed
log lines + failure injection (`params["mock_fail_at"]`), registry keyed by env class
(prod/nonprod never share config). Tests: happy path, injected failure, cancel mid-run,
two concurrent jobs isolated — all `-race` clean. No portal code may reference
MockEngine concretely (interface + registry only).

## Blocked / needs user

- **Design brief extract** (nice-to-have, not blocking): copy §Design system + Screen 1
  from `P:/Projects/db-portal-research/05-claude-design-brief.md` to the VM (docs/specs/)
  so WU-004's placeholder hex tokens can be reconciled (icebox item).
- CI status can't be verified FROM THE VM (`gh` not installed — icebox item); check
  Actions on GitHub directly. Local `npm run check` (the same gate) is green.
- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults without
  mutating process env; server.NewRouter(log, Pinger) is what tests exercise;
  migrations in backend/internal/db/migrations/ (goose, embed.FS);
  `portal migrate up|down|status`; testutil.DB(t) skips when compose PG absent (CI-safe).
- Frontend (WU-004): react-router v7 (`react-router` package, NOT react-router-dom);
  tokens live ONLY in src/index.css; components/{EnvBadge,RunStatus,StatusFooter};
  lib/api.ts = typed client (ApiError, status 0 = unreachable; healthz 503 = degraded,
  not an error). Vite proxies /api + /healthz → :8080. tsc strict is ON.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, git 2.43. CI = check.yml (no Postgres service — DB tests skip;
  no `gh` on VM).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); key extracts already in docs/. Need a research file on the
  VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — WU-004 done (2304b9c, evidence in BACKLOG/JOURNAL); active → WU-005.
- 2026-07-07 — WU-003 done (aee80cc, evidence in BACKLOG/JOURNAL).
- 2026-07-06 — WU-002 done (44325db: compose postgres+mailpit, up/down/db-reset).
