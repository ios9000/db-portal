# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-001 — Repo scaffold + quality gates (`docs/agent/BACKLOG.md`)
- **Status:** not started. WU-000 closed 2026-07-06 with full VM audit evidence.
- **Where:** PRIMARY = cloud VM `dbportal-vm` (wdsvc55@80.85.254.99), repo `~/db-portal`.
  Workstation `P:\Projects\db-portal` is a docs-only secondary clone.
- **Branch:** main (docs only, no code yet)

## Next action (be exact)

Start WU-001 per its BACKLOG entry, ON THE VM: monorepo skeleton (`backend/` uv-managed
Python 3.12 + FastAPI placeholder test; `frontend/` Vite React-TS; `playbooks/`; `infra/`),
root `package.json` npm-script targets `check`/`fmt`, pre-commit hooks, CI stub.
ADR-001 (FastAPI+React/TS) and ADR-007 (npm scripts) are both accepted — no open
decisions block WU-001 or WU-002.

## Blocked / needs user

- (nothing) — O-1 (dump artifact storage) remains open but only matters at WU-012;
  mock path is acceptable there, minio lands WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- VM toolchain (audited): Docker 29.6.1, Compose v5.3.0, node 22.23.1, Python 3.12.3,
  uv 0.11.27, git 2.43. Passwordless sudo; docker group active after next login.
- GitHub: private repo `ios9000/db-portal`; VM pushes via write-enabled deploy key
  (`~/.ssh/dbportal_deploy`, Host github.com stanza in VM ssh config).
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`),
  reference-only (ADR-006); key extracts already in docs/. Need a research file on the
  VM? Ask the user to copy it over.
- Windows-era forensics (WSL E_FAIL, dead NAT, disk incident): JOURNAL 2026-07-06 +
  ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-06 — WU-000 done (VM audit evidence in BACKLOG/JOURNAL); active → WU-001.
- 2026-07-06 — VM bootstrapped (docker/node/uv), deploy key added, repo cloned on VM.
- 2026-07-06 — Pivot to cloud VM (ADR-009); repo pushed to GitHub.
