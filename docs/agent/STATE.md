# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active WU:** WU-000 — Toolchain audit & task-runner decision (`docs/agent/BACKLOG.md`)
- **Status:** not started
- **Branch:** main (clean; docs package only, no code yet)

## Next action (be exact)

Run the WU-000 audit commands (node, python/uv, docker, compose — versions and presence),
record results in the WU entry, decide the task runner (ADR-007 in DECISIONS.md), update
the Commands section of CLAUDE.md with whatever is real, mark WU-000 done, move to WU-001.

## Blocked / needs user

- **ADR-001 (stack: FastAPI + React/TS) is `proposed`** — user should confirm or veto
  before WU-003/WU-004 start. WU-000..002 are stack-agnostic enough to proceed.
- **O-1 (dump artifact storage)** still open — does not block until Phase 1 (WU-012).

## Standing context (stable facts worth re-stating)

- MVP scope is Decisions D1–D7 (DECISIONS.md): dump+restore, DBA-only, guardrails, audit.
- Execution engine is mocked (MockEngine) until WU-033; nothing before Phase 3 talks to
  a real Semaphore or a real database target.
- Research corpus lives at `~/Projects/db-portal-research/` — reference, not authority;
  DECISIONS.md wins on conflict.

## Checkpoint log (last 3, newest first — older history is in JOURNAL.md)

- 2026-07-06 — repo + agent-docs package created; no code yet.
