# Decisions

Highest authority in the repo (see CLAUDE.md precedence). Two registers: business
decisions (D — from the 2026-07-04 discovery workshop, immutable without the customer)
and technical ADRs (A — ours, supersedable). Never delete; supersede with a new entry.

## Business decisions (workshop 2026-07-04)

- **D1 — Hero operation:** Dump. (Motivating incident: dump skipped before a critical op.)
- **D2 — Permission matrix v0:** DBA role only; single pane of glass for prod + non-prod
  WITH engineered mix-up safeguards.
- **D3 — App admins in MVP:** no prod access; role deferred entirely.
- **D4 — MVP catalog:** Backup + Restore only. Dump: scheduled AND run-now.
- **D5 — Chain failure behavior:** halt + notify DBA + resume from failed step.
- **D6 — Guardrails v0:** audit = who/when/final-status, 1-year retention (policy target
  5y post-MVP); no ticket linkage; maintenance windows warn-only; security vetting 2 weeks.
- **D7 — Appetite & ownership:** fully custom build; owner = Head of DBA; acceptance =
  customer management approval; 12-month metric = button clicks.

## ADRs

### ADR-001 · Stack: FastAPI + React/TypeScript — `proposed` (needs user confirm/veto)
Backend Python 3.12 + FastAPI + SQLAlchemy 2 + Alembic + pydantic-settings; portal store
PostgreSQL 16. Frontend Vite + React + TS. **Why:** Python is the DBA-adjacent language
(long-term maintainability, D7 owner is the DBA team); async-native fits log streaming;
the design brief's UI (drawers, live pipelines, toggles) wants a real SPA; both stacks are
high-competence territory for the agent, which matters for the harness experiment.
**Rejected:** Django (admin unused, async awkward for SSE), HTMX-only (Screen 5 live-run
UX would fight it), Go backend (weakest fit to DBA-team maintenance), Node full-stack
(single language, but Python wins on operator familiarity).

### ADR-002 · Engine seam: ExecutionAdapter; MockEngine first, Semaphore in M3 — `accepted`
All engine access behind one interface (start/status/stream_logs/cancel) with a per-env
registry. MockEngine (realistic logs, failure injection) is the default for dev and ALL
tests forever; SemaphoreAdapter lands in WU-033. **Why:** research verdict stands
(fully-custom executor = 6–12 dev-months of commodity machinery; bare Semaphore/AWX
fails the brief) — but no external dependency belongs in the walking skeleton, and the
mock keeps the whole UI buildable and testable engine-free. The registry split by env
class implements guardrail layer 3 (disjoint prod/nonprod credentials) structurally.

### ADR-003 · Scheduler is portal-owned, not engine cron — `accepted`
Scheduled runs enqueue through the identical guardrail + audit path as button presses,
attributed `schedule:<owner>`. Engine cron would bypass the portal's whole value layer.

### ADR-004 · Secrets posture — `accepted`
No approved org secrets store exists. Target credentials live ONLY engine-side (Ansible
Vault files / Semaphore key store; key outside git). The portal DB stores no target
credentials, ever; logs and emails carry no secrets or params. Dev: `.env` gitignored.
Roadmap commitment: dedicated vault (OpenBao) is the first post-MVP infra item.

### ADR-005 · Monorepo — `accepted`
`backend/ frontend/ playbooks/ infra/ docs/` in one repo. One agent, one working tree,
atomic cross-stack WUs, one quality gate. Split later only if team shape demands it.

### ADR-006 · Docs precedence & research corpus — `accepted`
DECISIONS.md → ARCHITECTURE.md → VISION.md → `P:/Projects/db-portal-research/*` (reference
only). The research assumes the FULL product (3 personas, approvals, SSO, Semaphore Pro);
MVP deliberately narrows it — on any conflict, D1–D7 win. The design brief (research 05)
remains the UI's visual authority where it doesn't conflict (e.g., Approvals nav: out).

### ADR-007 · Task runner: root `package.json` npm scripts — `accepted` (WU-000, 2026-07-06)
Audit found node 22 + npm 10 present and identical in Git Bash and PowerShell; `just` and
`make` absent. npm scripts need zero new installs and behave the same in both shells, so
the root `package.json` is the task runner: `npm run check | fmt | up | down | db-reset |
dev:be | dev:fe`, delegating into `backend/` and `frontend/`. Backend Python environments
are managed by `uv` (user-space install, no admin; also our escape hatch to pin
Python 3.12 if 3.14 wheel gaps bite — decided in WU-003). **Rejected:** `just` (extra
install, low added value here), Make (absent on Windows), PowerShell-only scripts
(break Git Bash usage).

### ADR-008 · Dev containers: WSL2 + Docker Engine — `superseded by ADR-009`
Was: dedicated WSL2 distro `dbportal-dev` with docker-ce. Provisioning succeeded only
partially: disk exhaustion caused WSL2 E_FAIL (fixed by relocating to P:), after which
the WSL NAT layer proved dead (gateway unreachable; stale HNS/WinNAT suspected; repair
needs admin elevation this account lacks). Kept for the forensics: `wsl --install`
first-boot is broken on this Win10 build — the `wsl --import` path works.

### ADR-009 · Dev environment: dedicated cloud Linux VM — `accepted` (user, 2026-07-06)
Supersedes ADR-008. Development — agent sessions included — moves to a single dedicated
cloud VM: Ubuntu 24.04 LTS, 4 vCPU, 16 GB RAM (8 min), 100 GB SSD, SSH-only ingress.
Claude Code runs ON the VM; repo is cloned from GitHub. **Why:** the workstation blocked
on three independent walls — disk (60 GB @ 95%), privileges (no admin; WinNAT repair
needs elevation), RAM (8 GB shared with host). Native Linux deletes the WSL/NAT/UAC
problem class. One VM hosts everything through M3 (portal, Postgres, mailpit, Semaphore,
minio as containers). Extra VMs (1 real playbook target; 3 Patroni rehearsal) remain an
M3+ decision. **Consequences:** ADR-007 stands and simplifies (single shell); WU-000
audit re-runs on the VM; local leftovers `P:\wsl\`, `C:\Users\Archer\.wslconfig` are
disposable; `P:\Projects\db-portal` stays as a secondary clone.

## Open (inherited from architecture doc §10)

- **O-1** dump artifact storage (rec: S3-compatible; minio in dev) — needed by WU-012 (mock ok) / WU-035 (real)
- **O-3** maintenance-window source (rec: per-instance inventory field) — WU-010 adds the field
- **O-4** dump options matrix (rec: `pg_dump -Fc`, small vetted option set) — WU-034
- **O-5** workshop "Wrap" section never received — confirm no extra decisions outstanding
