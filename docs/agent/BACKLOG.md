# Backlog — context-window-sized work units

Statuses: `todo` · `active` · `done` · `dropped`. One WU `active` at a time.
Sizing: S = comfortable single session · M = single session with a planned mid-checkpoint.
L is not a valid size — split before starting. Phase 2+ WUs are deliberately coarse;
each gets groomed (ACs + context brief written) by the session that closes the prior phase.

Every WU entry must have: Goal · Deliverables · Acceptance criteria · Verify (executable
commands) · Context brief (exact files to read — keep it minimal and current).

---

## Phase 0 — Walking skeleton (Milestone M0)

### WU-000 · Toolchain audit & task-runner decision — S · `done` (2026-07-06)

**VM audit — PRIMARY environment (`wdsvc55@80.85.254.99` "compute-ins-0018", Ubuntu 24.04
LTS, 4 vCPU / 15 GB RAM / 96 GB disk):** git 2.43.0 · Docker 29.6.1 + Compose v5.3.0
(hello-world verified) · node v22.23.1 / npm 10.9.8 · Python 3.12.3 (3.14 wheel risk
GONE) · uv 0.11.27. ADR-007 confirmed (npm scripts; single shell now). Repo cloned at
`~/db-portal` via write-enabled deploy key. The docker-absent AC branch was resolved by
ADR-009 (native docker on the VM), not by a Windows fallback.

**Workstation audit (secondary, docs-only — kept for history):**
| tool | status |
|---|---|
| git | 2.52.0.windows.1 |
| gh | 2.91.0 |
| node / npm | v22.14.0 / 10.9.2 |
| python | 3.14.2 (`py`/`python`/`python3` all resolve); pip 25.3 via `python -m pip`; venv OK |
| uv, pipx | absent (uv to be installed user-space in WU-001) |
| just, make | absent |
| docker | ABSENT — Docker Desktop uninstalled 2026-05-13 (ProgramData installer log) |
| WSL2 | enabled (default version 2), NO distro installed |

Python 3.14 is bleeding-edge → risk of missing wheels; mitigation: uv manages interpreter,
pin 3.12 if WU-003 hits wheel gaps (record outcome there).
Container decision: **blocked on user** — see STATE.md. Task runner: ADR-007 (npm scripts).
**Goal:** know exactly what this Windows machine can run before scaffolding anything.
**Deliverables:** audit results recorded here; ADR-007 (task runner) in DECISIONS.md;
CLAUDE.md "Commands" section updated with reality.
**AC:**
- [ ] Presence+version recorded for: git, node, npm, python, uv/pip, docker, docker compose.
- [ ] Task runner chosen that works in BOTH Git Bash and PowerShell (candidates: just,
      make-if-present, npm scripts + a thin `dev.ps1`/`dev.sh` pair). ADR-007 written.
- [ ] If docker is absent: STOP, flag to user — Phase 0 shape depends on it (dev Postgres,
      mailpit). Record the fallback chosen (native Postgres? SQLite-for-dev is NOT allowed
      — the portal's own store is Postgres by design).
**Verify:** each tool's `--version` output pasted into the journal `done` line.
**Context brief:** this entry; CLAUDE.md.

### WU-001 · Repo scaffold + quality gates — M · `todo`
**Goal:** monorepo skeleton with lint/typecheck/test gates wired, so every later WU
inherits the global gate for free.
**Deliverables:** `backend/` (Python project, empty package + 1 placeholder test),
`frontend/` (Vite React-TS app, default build), `playbooks/`, `infra/`; root `.gitignore`,
`.editorconfig`; pre-commit config (backend: ruff+format, frontend: eslint+prettier);
task-runner targets `check` (lint+typecheck+test, both stacks) and `fmt`; CI stub
(GitHub Actions yaml running `check`) — inert until a remote exists.
**AC:**
- [ ] `check` target green from a clean clone on this machine.
- [ ] Pre-commit blocks a deliberately mis-formatted file (prove it, then revert).
- [ ] CLAUDE.md Commands section documents `check`, `fmt`, and how to run each stack's dev server (stubbed OK).
**Verify:** `<runner> check` exit 0; `git commit` on a bad file rejected.
**Context brief:** WU-000 results; ADR-001, ADR-005, ADR-007 in DECISIONS.md.

### WU-002 · Dev environment (docker-compose) — S · `todo`
**Goal:** one command brings up portal Postgres 16 + mailpit (SMTP catcher); reset is cheap.
**Deliverables:** `infra/compose.yaml`, `.env.example`, runner targets `up`/`down`/`db-reset`.
**AC:**
- [ ] `up` → `docker compose ps` shows postgres healthy + mailpit UI reachable (localhost).
- [ ] `db-reset` drops and recreates the portal DB idempotently.
- [ ] `.env` gitignored; `.env.example` has every var with safe placeholder values.
**Verify:** `<runner> up && docker compose -f infra/compose.yaml ps` (both healthy);
`psql` connect with `.env.example`-shaped creds against dev password.
**Context brief:** WU-001 layout; ARCHITECTURE.md §Deployment.

### WU-003 · Backend skeleton — M · `todo`
**Goal:** FastAPI app factory with settings, structured logging, migrations, test harness —
the chassis every feature WU bolts onto.
**Deliverables:** app factory + pydantic-settings (env-driven); `/healthz` (checks DB
round-trip); alembic wired with migration 0001 (empty baseline); pytest + async test
client + a DB-backed test fixture (transaction-rollback pattern); JSON structured logging.
**AC:**
- [ ] `uvicorn` serves `/healthz` → `{"status":"ok","db":"ok"}` against compose Postgres.
- [ ] `alembic upgrade head` / `downgrade base` both clean.
- [ ] ≥3 tests: healthz ok, healthz with DB down (503), settings precedence. Suite < 30s.
**Verify:** `<runner> check`; `curl localhost:8000/healthz`.
**Context brief:** ADR-001; ARCHITECTURE.md §Components→Portal application; WU-002 env vars.

### WU-004 · Frontend shell — M · `todo`
**Goal:** navigable app shell speaking to the backend; the visual grammar (env badges,
status colors) established once, reused everywhere.
**Deliverables:** router + top nav (`My Databases · Activity · Schedules` — no Approvals in
MVP); API client with typed error handling; `EnvBadge` (PROD red / TEST amber / DEV gray —
redundant encoding, never color alone) and `RunStatus` chip components; a status footer
probing `/healthz`.
**AC:**
- [ ] `build` and dev server both work; shell renders with nav + healthz status.
- [ ] EnvBadge/RunStatus have component tests (vitest) covering all variants.
- [ ] Design tokens follow the design brief (`db-portal-research/05-claude-design-brief.md`
      §Design system) — Inter, 8px grid, defined status colors.
**Verify:** `<runner> check`; dev server renders against running backend.
**Context brief:** ADR-001; design brief §Design system + Screen 1 (skim); WU-003 API shape.

### WU-005 · ExecutionAdapter + MockEngine — M · `todo`
**Goal:** the engine seam (THE architectural bet, ADR-002) proven with a fake engine good
enough to build the whole UI against.
**Deliverables:** `ExecutionAdapter` protocol — `start_job(template, params) -> job_id`,
`get_status(job_id)`, `stream_logs(job_id) -> async iter`, `cancel(job_id)`; `MockEngine`
simulating a dump job (realistic timed log lines, artifact metadata on success) with
failure injection (`params={"mock_fail_at": "step"}`); engine registry keyed by env class
(prod/nonprod separation exists from day one, per guardrail layer 3).
**AC:**
- [ ] Unit tests: happy path, injected failure, cancel mid-run, two concurrent jobs isolated.
- [ ] No portal code imports MockEngine directly — only via the adapter interface + registry.
**Verify:** `<runner> check` (adapter tests visible in output).
**Context brief:** ARCHITECTURE.md §4.1 + §Components→Execution engine; ADR-002.

**M0 exit:** all Phase 0 WUs done + golden thread: backend up, frontend shell up, a
MockEngine job runnable from a pytest — committed demo script proving it.

---

## Phase 1 — Hero flow: dump run-now, end to end (Milestone M1)

### WU-010 · Inventory schema + CSV import — M · `todo`
Instance/cluster tables (env enum, platform, patroni ref, owner, maintenance window field —
O-3); CSV import with per-row validation + quarantine for failures (no connectivity probe
yet — that needs real targets; validation is structural). Import is idempotent (natural key).
**Verify:** import fixture CSV twice → same row count; malformed rows quarantined with reasons.
**Context brief:** ARCHITECTURE.md §Inventory; sample CSV to be created in `infra/fixtures/`.

### WU-011 · Instance API + cards/table UI — M · `todo`
List/detail endpoints with env filter; cards view per design brief Screen 1 (health dot,
badges, last-backup line) + `Cards ⇄ Table` toggle (Screen 2, minus bulk actions).
**Context brief:** design brief Screens 1–2; WU-010 schema; EnvBadge from WU-004.

### WU-012 · Catalog + run-now dump (hero) — M · `todo`
Operation catalog (data-driven, `dump` only); launch drawer (Screen 3 pattern); POST run →
audit record (`submitted`) → MockEngine job → status polling; run list in Activity.
Audit schema per ARCHITECTURE.md §8.1 — append-only from the first migration
(no UPDATE/DELETE grants for the app role).
**Depends:** O-1 (artifact storage) decided — mock path acceptable: artifact metadata only.
**Context brief:** ARCHITECTURE.md §6.1 + §8.1; WU-005 adapter; design brief Screen 3.

### WU-013 · Run detail + live logs — M · `todo`
Run page with stage state + log pane streaming from `stream_logs` (SSE or WebSocket —
decide via mini-ADR in the WU); follow mode; final status + artifact strip.
**Context brief:** design brief Screen 5; WU-005 `stream_logs` contract.

### WU-014 · Audit UI + email notify — S · `todo`
Activity/history view (Screen 6, minus approval rows); failure email via mailpit with
run link. Email content: who/what/where/status — no params, no log excerpts (leak risk).
**Verify:** kill a mock run → mailpit shows the mail; audit row immutable (UPDATE attempt fails).

### WU-015 · Prod guardrails — S · `todo`
Env-colored full-width banner on instance/run contexts; typed instance-name confirmation
for prod actions (paste disabled); non-prod = one click; env stamped in every audit row
(already in schema — assert it in tests). Guardrail layer 3 (separate engine credentials)
is asserted at the adapter-registry level: prod jobs MUST resolve a different engine
config object than nonprod, even while both are mocks.
**Context brief:** ARCHITECTURE.md §7; design brief Screen 4 (adapt: no approval flow in MVP).

**M1 exit = the demo:** import CSV → see fleet → run dump on a TEST instance (1 click) →
watch live logs → succeed with artifact → audit row + email on a failure case → typed-name
ritual on a PROD instance. Scripted in `docs/demo-m1.md`, runs start-to-finish < 5 min.
Golden-flow e2e test (this script, automated) enters the global gate here.

---

## Phase 2 — Auth, scheduling, windows (M2) — groom at M1 close

- WU-020 · AuthN: LDAP bind against AD (dev: bypass flag + fake directory), sessions,
  break-glass local account (usage alarmed in audit) — M
- WU-021 · AuthZ: portal-DB role store, DBA role, route guards; audit actor = AD identity — S
- WU-022 · Portal-owned scheduler (ADR-003): schedule CRUD + jittered execution through the
  SAME guardrail/audit path as run-now; `schedule:<owner>` actor — M
- WU-023 · Maintenance windows warn-only: window field → warn banner + `window_warned`
  audit flag — S

## Phase 3 — Restore, chains, real engine (M3) — groom at M2 close

- WU-030 · Artifact registry: dump artifacts as first-class rows (checksum, retention class,
  origin run) — S
- WU-031 · Restore workflow: explicit target (defaults non-prod), checksum verify,
  auto pre-restore safety dump, prod ritual; MockEngine first — M
- WU-032 · Chain engine: sequential steps, halt+notify on failure, persisted chain state,
  resume-from-failed-step — M
- WU-033 · SemaphoreAdapter: real Semaphore in compose; task templates pinned to playbook
  tags; webhook status callback + poll fallback; MockEngine remains the test default — M
- WU-034 · Dump playbook (real): pg_dump -Fc via engine against a compose "target" Postgres;
  replica-first logic deferred to a Patroni WU — M
- WU-035 · O-1 storage: artifact upload to S3-compatible (minio in compose) — S

## Phase 4 — Hardening (M4) — groom at M3 close

Concurrency/locking (cluster/instance TTL locks), load test (25–50 concurrent mock dumps),
staging seed, retention job (1y audit), cold-start + docs reconciliation audit, packaging.

---

## Icebox (ideas & discovered debt — one line each, groom later)

- Patroni-aware dump/restore sequencing (research gotcha #1: cancel semantics too)
- PITR; Vacuum/Reindex buttons; approvals workflow (Screen 7); Jira linkage; SSO
- Portal self-target ban (research gotcha #2) — enforce in inventory layer when real targets exist
- Bulk/rolling operations (Screen 2 sticky bar); saved views
- 5-year audit shipping to object storage; SIEM export
