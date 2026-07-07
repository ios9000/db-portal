# DB Portal

Internal self-service portal for PostgreSQL day-2 operations across a ~500-instance estate.
Custom web portal (the product) over an Ansible execution engine (internal, swappable).
MVP scope: **Backup (dump) + Restore, DBA-only role, prod/non-prod guardrails, audit trail.**

This project is also an experiment in AI-agent-driven development. The rules below exist to
prevent context loss between sessions. They are not optional.

## Session protocol — MANDATORY

1. **Start:** read `docs/agent/STATE.md` first. It names the active work unit (WU) and the next
   action. Trust it over any conversation summary. Then read the WU entry in
   `docs/agent/BACKLOG.md` and ONLY the files its context brief lists.
2. **Work:** one WU per session by default. Do not start a second WU without checkpointing.
   Do not refactor outside the WU's blast radius — file an idea in BACKLOG.md instead.
3. **Checkpoint** (at any natural boundary, and ALWAYS before context gets heavy):
   commit working code, update STATE.md, append one line to `docs/agent/JOURNAL.md`.
4. **End:** a WU is done only when every verification command in its BACKLOG entry passes.
   Then: mark it done in BACKLOG.md, update STATE.md to point at the next WU, journal, commit.

Full ritual: `docs/agent/SESSION-PROTOCOL.md`. Strategy rationale: `docs/agent/STRATEGY.md`.

## Ground rules

- **The repo is the memory.** Anything worth knowing next session must land in a file this
  session: code, tests, STATE.md, JOURNAL.md, or DECISIONS.md. Conversation context is a cache.
- **Decision precedence:** `docs/DECISIONS.md` (D1–D7 business, ADRs technical)
  → `docs/ARCHITECTURE.md` → `docs/VISION.md` → research folder
  (`P:/Projects/db-portal-research/`). When docs conflict, the leftmost wins.
- **Verify, don't claim.** Never report a WU complete without running its verification
  commands and pasting real output into the journal entry.
- **Tests are ground truth.** New behavior ships with tests in the same WU. A red suite blocks
  everything, including "unrelated" WUs.
- **Small commits, always shippable.** Commit at every green state. Never leave a session with
  uncommitted work — if forced to stop mid-WU, commit to a `wip/WU-xxx` branch and record the
  exact resume point in STATE.md.
- **Secrets never enter the repo, the portal DB, or logs.** Dev credentials live in
  `.env` (gitignored); `.env.example` documents shape only.

## Commands

Task runner = root `package.json` npm scripts (ADR-007). Scaffolded by WU-001.
Planned targets: `check` `fmt` `up` `down` `db-reset` `dev:be` `dev:fe`.

- Primary environment (ADR-009): a dedicated cloud Ubuntu 24.04 VM — native docker,
  node 22, uv. First VM session: follow STATE.md "Next action" (bootstrap + WU-000 re-audit),
  then replace this bullet with the audited versions.
- The Windows workstation clone (`P:\Projects\db-portal`) is secondary: docs work only,
  NO docker there (WSL track dead — ADR-008). Research corpus lives there:
  `P:/Projects/db-portal-research/`.

## Repo map

- `CLAUDE.md` — this file (keep short; it loads every session)
- `README.md` — human entry point
- `docs/VISION.md` — product north star, MVP scope, success metrics
- `docs/ARCHITECTURE.md` — MVP architecture (authoritative technical design)
- `docs/DECISIONS.md` — business decisions D1–D7 + technical ADRs
- `docs/ROADMAP.md` — phases M0–M4 with exit criteria
- `docs/specs/` — per-module specs, written just-in-time before their WU
- `docs/agent/STRATEGY.md` — the AI-harness development strategy
- `docs/agent/SESSION-PROTOCOL.md` — start/checkpoint/end rituals, recovery
- `docs/agent/STATE.md` — CURRENT STATE (overwritten each checkpoint; read first)
- `docs/agent/JOURNAL.md` — append-only session log
- `docs/agent/BACKLOG.md` — context-window-sized work units
- `backend/`, `frontend/`, `playbooks/`, `infra/` — created by WU-001
