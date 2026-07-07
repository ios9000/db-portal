# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-000 — Toolchain audit & task-runner decision (`docs/agent/BACKLOG.md`)
- **Status:** audit done (results in the WU entry); ADR-007 (npm-scripts runner) accepted;
  **blocked on ADR-008** (container runtime) — question is with the user.
- **Branch:** main (docs only, no code yet)

## Next action (be exact)

When the user answers the container question: write the answer into ADR-008
(status → accepted), update CLAUDE.md "Commands" section (runner = npm scripts; note
container runtime), flip WU-000 → `done` in BACKLOG.md, journal with audit evidence,
set this file's Active WU → WU-001, commit `chore: checkpoint WU-000 (done) → WU-001`.

## Blocked / needs user

- **ADR-008 — container runtime for dev.** Docker Desktop uninstalled 2026-05-13; WSL2
  enabled, no distro. Options: (a) reinstall Docker Desktop — full compose fidelity,
  needs admin, license OK on Education; (b) WSL2 Ubuntu + docker-ce inside — free,
  no Desktop, slight plumbing (recommended if (a) was uninstalled deliberately);
  (c) Podman — compose-compat friction risk at M3 (Semaphore/minio); (d) no containers,
  native Postgres — badly hurts M3, not recommended.
- **ADR-001 (stack: FastAPI + React/TS)** still `proposed` — confirm/veto before WU-003/004.

## Standing context (stable facts worth re-stating)

- Toolchain: node 22/npm 10, python 3.14.2 (pip via `python -m pip`), git 2.52, gh 2.91;
  no docker/just/make/uv. Python 3.14 wheel risk → uv + possible 3.12 pin (WU-003).
- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Research corpus: `~/Projects/db-portal-research/` — reference, not authority.

## Checkpoint log (last 3, newest first)

- 2026-07-06 — WU-000 audit complete; ADR-007 accepted; blocked on ADR-008 (user).
- 2026-07-06 — repo + agent-docs package created; no code yet.
