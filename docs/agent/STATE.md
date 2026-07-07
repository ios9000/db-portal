# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-004 — Frontend shell (router, nav, EnvBadge/RunStatus, healthz footer)
- **Status:** not started. WU-003 closed 2026-07-07 (aee80cc): Go chassis live —
  chi + graceful shutdown, config (env > .env > defaults), /healthz (200/503),
  goose migrate subcommand w/ embedded 0001, slog JSON request logging, tests -race clean.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

Start WU-004 per its BACKLOG entry, ON THE VM: router + top nav (My Databases ·
Activity · Schedules), typed API client, EnvBadge (PROD red / TEST amber / DEV gray,
never color alone) + RunStatus components with vitest coverage, status footer probing
`/healthz`. Design tokens per design brief §Design system (Inter, 8px grid) — brief is
in the research corpus on the workstation; ask user to copy the §Design system extract
if not already in docs/. Backend for dev: `cd backend && go run ./cmd/portal`
(serves :8080; compose PG must be up: `npm run up`).

## Blocked / needs user

- (nothing) — WU-004 wants design-brief §Design system + Screen 1; if missing on VM,
  request a copy from `P:/Projects/db-portal-research/05-claude-design-brief.md`.
- O-1 (dump artifact storage): only matters at WU-012; mock OK there, minio WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Backend chassis (WU-003): config.Load(dotenv) merges env>file>defaults without
  mutating process env; server.NewRouter(log, Pinger) is what tests exercise;
  migrations live in backend/internal/db/migrations/ (goose, embed.FS);
  `portal migrate up|down|status`; testutil.DB(t) skips when compose PG absent (CI-safe).
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, git 2.43. CI = check.yml (no Postgres service — DB tests skip).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); key extracts already in docs/. Need a research file on the
  VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-07 — WU-003 done (aee80cc, evidence in BACKLOG/JOURNAL); active → WU-004.
- 2026-07-06 — WU-002 done (44325db: compose postgres+mailpit, up/down/db-reset).
- 2026-07-06 — WU-001R done (4122ac7: backend Python → Go per ADR-010).
