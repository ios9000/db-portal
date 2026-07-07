# DB Portal

Self-service portal for PostgreSQL day-2 operations (backup & restore first) across a
~500-instance estate. Custom web portal over a swappable Ansible execution engine.
Also: a live experiment in AI-agent-driven development with Claude Code + Fable 5.

## Status

Documentation package complete; code not yet scaffolded. Next: `WU-000` (toolchain audit).

## Orientation

| You want | Read |
|---|---|
| The product idea and MVP scope | `docs/VISION.md` |
| The technical design | `docs/ARCHITECTURE.md` |
| Why things are the way they are | `docs/DECISIONS.md` |
| What's planned, at altitude | `docs/ROADMAP.md` |
| What's planned, in detail | `docs/agent/BACKLOG.md` |
| Where work stands right now | `docs/agent/STATE.md` |
| How the AI-driven development works | `docs/agent/STRATEGY.md` |

Research corpus (business/system/UX analysis, clickable prototype, architecture doc):
`~/Projects/db-portal-research/` — reference material; `docs/DECISIONS.md` is authoritative.

## Working with the agent

Open a Claude Code session in this directory. `CLAUDE.md` auto-loads the session
protocol; the agent reads `docs/agent/STATE.md` and continues from the active work unit.
Useful phrases: "continue" (picks up the active WU) · "checkpoint" (forces the save
ritual) · "groom phase N" (writes ACs/context briefs for the next phase) ·
"use a workflow for the M-gate review" (opts into the multi-agent milestone review).
