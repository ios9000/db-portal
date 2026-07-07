# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-000 — Toolchain audit (RE-RUN on the new machine; see Next action)
- **Status:** development is MIGRATING to a dedicated cloud VM (ADR-009). The GitHub
  repo is the source of truth; the Windows workstation (`P:\Projects\db-portal`) is a
  secondary clone. If you are reading this ON the VM: you are the primary environment.
- **Branch:** main (docs only, no code yet)

## Next action (be exact) — first session on the VM

1. Bootstrap (once): docker via `curl -fsSL https://get.docker.com | sh`, node 22 LTS,
   `uv` (astral.sh installer), git identity, `docker run hello-world`.
2. Re-run WU-000 on this machine: record tool versions in the BACKLOG WU-000 entry
   (append a "VM audit" row set), confirm ADR-007 (npm scripts — now single-shell),
   update CLAUDE.md "Commands" (drop the WSL invocation note — plain `docker` works).
3. Flip WU-000 → `done` with version evidence in JOURNAL. STATE → WU-001. Commit, push.
4. Proceed to WU-001 (repo scaffold) per BACKLOG.

## Blocked / needs user

- **ADR-001 (stack: FastAPI + React/TS)** still `proposed` — confirm/veto before WU-003/004.
- O-1 (dump artifact storage): mock path OK until WU-012; minio planned WU-035.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). Engine is MockEngine until WU-033.
- Research corpus stays on the workstation (`P:/Projects/db-portal-research/`) — it is
  reference-only (ADR-006). Key extracts already live in docs/ (ARCHITECTURE, VISION);
  if a WU needs a research file, ask the user to paste/copy it to the VM.
- Windows-era forensics (WSL E_FAIL, NAT death, disk incident): JOURNAL 2026-07-06
  entries + ADR-008. Do not resurrect the WSL track.

## Checkpoint log (last 3, newest first)

- 2026-07-06 — Pivot to cloud VM (ADR-009); repo pushed to GitHub; WU-000 to re-run on VM.
- 2026-07-06 — WSL2 boots on P: after disk remediation, but NAT dead; repair needs admin.
- 2026-07-06 — WU-000 audit complete; ADR-007 accepted; disk incident discovered.
