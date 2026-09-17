# DB Portal

Internal self-service portal for PostgreSQL day-2 operations across a ~500-instance estate.
Custom web portal (the product) over an Ansible execution engine (internal, swappable).
MVP scope: **Backup (dump) + Restore, DBA-only role, prod/non-prod guardrails, audit trail.**

This project is also an experiment in AI-agent-driven development. The rules below exist to
prevent context loss between sessions. They are not optional.

## Session protocol — MANDATORY

1. **Start:** the state card at the top of `docs/agent/STATE.md` comes first. A SessionStart
   hook injects it on startup, resume and compaction; if it is not in your context, read the
   `STATE-CARD` block yourself (STATE.md is large — read the card plus the sections you need,
   not the whole file). It names the active work unit (WU) and the next action. Trust it over
   any conversation summary, but never over the user: **a new user request beats the saved
   task.** Then read the WU entry in `docs/agent/BACKLOG.md` and ONLY the files its context
   brief lists.
2. **Work:** one WU per session by default. Do not start a second WU without checkpointing.
   Do not refactor outside the WU's blast radius — file an idea in BACKLOG.md instead.
3. **Checkpoint** (at any natural boundary, and ALWAYS before context gets heavy):
   commit working code, update STATE.md, append one line to `docs/agent/JOURNAL.md`.
4. **End:** a WU is done only when every verification command in its BACKLOG entry passes.
   Then: mark it done in BACKLOG.md, update STATE.md to point at the next WU, journal, commit.

Full ritual: `docs/agent/SESSION-PROTOCOL.md`. Strategy rationale: `docs/agent/STRATEGY.md`.

### State card — MANDATORY

- **When:** rewrite the card (the whole `STATE-CARD` block, fresh UTC timestamp) after every
  significant milestone or decision, and ALWAYS before handing off — session end, delegating
  to a subagent, or stopping to ask the user. Do not wait for the full checkpoint ritual.
- **What:** goal · completion criteria · active task + owner · branch · decisions with their
  why · done · verified · blockers · next step · last updated. Budget 5000 bytes (the hook
  cuts the rest). Write `UNKNOWN` rather than guessing or leaving a field out.
- **Done ≠ verified.** "Done" is a claim. "Verified" needs the command, its result and a link
  to the evidence (commit, CI run, JOURNAL line, review doc). Keep those links when you
  rewrite the card; never promote an item to Verified without having run the check.
- **Parallel work:** every concurrent task gets its own record `docs/agent/tasks/<id>.md`
  with exactly ONE owner, who writes only that file (template in its README). The card has a
  single owner — the main (architect) session — and only it folds task records into the card.

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

Task runner = root `package.json` npm scripts (ADR-007). Run from repo root on the VM.

- `npm run check` — THE gate: backend (golangci-lint run, format-diff, `go test -race`)
  then frontend (oxlint, tsc -b, vitest). Must be green before any WU closes. Also runs in CI.
- `npm run fmt` — auto-format both stacks (golangci-lint fmt · prettier).
- `npm run build:release` — the ADR-010 artifact: fe build → embed copy →
  static `backend/bin/portal` (serves SPA + API alone).
- `npm run dev:fe` — Vite dev server. Backend: `cd backend && go run ./cmd/portal`
  (serves :8080; `migrate up|down|status` subcommand runs embedded goose migrations).
- `up` / `down` / `db-reset` targets arrive with WU-002 (compose env).
- Once per fresh clone: root `npm install` (wires `.githooks/` via prepare),
  `cd frontend && npm ci`. Go deps resolve on first build (`go build ./...`).
- Backend is **Go** (ADR-010; Python vetoed) — single-binary deployment, SPA embedded in
  prod (WU-006). Frontend linter is **oxlint** (2026 Vite template) — NO eslint here.

- Primary environment (ADR-009): cloud VM `dbportal-vm` (root@80.209.240.36, host
  "206610", 8 vCPU / 31 GB / 387 GB), repo at `/root/db-portal`. Bootstrapped via
  `infra/bootstrap-vm.sh`, audited 2026-07-06: Docker 29.6.1 + Compose v5.3.0 ·
  node v22.23.1 / npm 10.9.8 · **Go 1.26.4** · golangci-lint 2.12.2 · git 2.43 ·
  Claude Code 2.1.202. (Python/uv left the stack with ADR-010.)
  (VM #1 at 80.85.254.99 is decommissioned; its deploy key is revoked.)
- The Windows workstation clone (`P:\Projects\db-portal`) is secondary: docs work only,
  NO docker there (WSL track dead — ADR-008). It reaches the VM via `ssh dbportal-vm`
  (alias in workstation `~/.ssh/config`). Research corpus lives on the workstation:
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
- `docs/agent/STATE.md` — CURRENT STATE; opens with the state card (overwritten each
  checkpoint; read first)
- `docs/agent/tasks/` — one record per PARALLEL task, one owner each (README = template)
- `docs/agent/JOURNAL.md` — append-only session log
- `docs/agent/BACKLOG.md` — context-window-sized work units
- `.claude/hooks/session-state.sh` — SessionStart hook that injects the state card
  (wired in `.claude/settings.json`; startup | resume | compact)
- `backend/`, `frontend/`, `playbooks/`, `infra/` — created by WU-001
