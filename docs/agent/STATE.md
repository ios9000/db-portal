# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-000 — Toolchain audit & task-runner decision (`docs/agent/BACKLOG.md`)
- **Status:** audit done; ADR-007 accepted; ADR-008 accepted (WSL2 + docker-ce).
  Provisioning hit a **disk-space incident** — C: at 99% (861MB free), which is the
  likely cause of the WSL2 `Wsl/Service/CreateInstance/E_FAIL` (VM can't allocate
  swap/VHDX). WSL1 boot of the distro works → image and registration are good.
- **Branch:** main (docs only, no code yet)

## Next action (be exact)

Execute the disk remediation the user approves (see Blocked), then: re-import distro
`dbportal-dev` to `P:\wsl\dbportal-dev --version 2` (re-download image from
cdimage.ubuntu.com/ubuntu-wsl/noble/daily-live/current/noble-wsl-amd64.wsl — local copy
was deleted to free space), write `~/.wslconfig` with swap+swapfile pointed at P: and a
memory cap, boot test, install docker-ce (get.docker.com), `docker run hello-world`,
`docker compose version`. Then close WU-000: BACKLOG → done, CLAUDE.md already updated,
journal with evidence, STATE → WU-001, commit.

## Blocked / needs user (disk remediation choices)

- Delete orphaned Docker Desktop data `C:\Users\Archer\AppData\Local\Docker` (1.6 GB,
  product uninstalled 2026-05-13, uninstaller left it behind)? → frees C: to ~2.9 GB.
- Relocate dev sandbox (WSL distro, docker data, WSL swap) to P: (local NTFS "PG",
  17 GB free)? Current 642 MB distro at `C:\wsl\dbportal-dev` would be unregistered.
- **ADR-001 (stack: FastAPI + React/TS)** still `proposed` — confirm/veto before WU-003/004.

## Provisioning facts (for the resuming session)

- WSL 2.7.10 store version; kernel 6.18.33.2-2; `wsl --install -d Ubuntu` first-boot is
  broken on this box (E_FAIL both fresh-install and clean import while C: was full) —
  use the `wsl --import` path.
- Distro `dbportal-dev` currently registered as **WSL1** at `C:\wsl\dbportal-dev`
  (converted during diagnosis; WSL1 cannot run docker — do not build on it).
- Deleted: `~/wsl-images/noble-wsl-amd64.wsl` (my download, re-fetchable).

## Standing context (stable facts worth re-stating)

- Toolchain: node 22/npm 10, python 3.14.2 (pip via `python -m pip`), git 2.52, gh 2.91;
  no docker/just/make/uv. Python 3.14 wheel risk → uv + possible 3.12 pin (WU-003).
- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Research corpus: `P:/Projects/db-portal-research/` — reference, not authority.

## Checkpoint log (last 3, newest first)

- 2026-07-06 — WU-000 audit complete; ADR-007 accepted; blocked on ADR-008 (user).
- 2026-07-06 — repo + agent-docs package created; no code yet.
