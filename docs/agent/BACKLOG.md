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

### WU-001 · Repo scaffold + quality gates — M · `done` (2026-07-06, commit bbae15c)
**Evidence:** `npm run check` exit 0 on VM; GitHub Actions run **success** on clean
runner (the strongest clean-clone proof); pre-commit blocked a mis-formatted .py
("1 file would be reformatted"), then reverted. Reality delta vs plan: 2026 Vite
template ships **oxlint**, not eslint — hook and docs adapted; prettier still used
for formatting. Backend: uv + ruff + mypy(strict) + pytest, hatchling src layout.
**Goal:** monorepo skeleton with lint/typecheck/test gates wired, so every later WU
inherits the global gate for free.
**Deliverables:** `backend/` (Python project, empty package + 1 placeholder test),
`frontend/` (Vite React-TS app, default build), `playbooks/`, `infra/`; root `.gitignore`,
`.editorconfig`; pre-commit hooks (backend: ruff+format, frontend: oxlint+prettier);
task-runner targets `check` (lint+typecheck+test, both stacks) and `fmt`; CI running
`check` on GitHub Actions.
**AC:**
- [x] `check` target green from a clean clone on this machine.
- [x] Pre-commit blocks a deliberately mis-formatted file (prove it, then revert).
- [x] CLAUDE.md Commands section documents `check`, `fmt`, and how to run each stack's dev server (stubbed OK).
**Verify:** `<runner> check` exit 0; `git commit` on a bad file rejected.
**Context brief:** WU-000 results; ADR-001, ADR-005, ADR-007 in DECISIONS.md.

### WU-001R · Backend rework: Python → Go (ADR-010) — M · `done` (2026-07-06, commit 4122ac7)
**Evidence:** `npm run check` CHECK-EXIT:0 on VM (golangci-lint 0 issues, fmt-diff clean,
`go test -race` pass — needed `gcc` for cgo, added to bootstrap); CI **success** on clean
runner; hook blocked a bad .go (`unused` linter caught it); `find` shows 0 `*.py`, 0
`uv.lock`; backend/ = cmd + go.mod + internal only. Go 1.26.4, golangci-lint 2.12.2.
**Goal:** replace the Python backend scaffold with a Go one, same gate rigor, zero
Python remnants.
**Deliverables:** `backend/go.mod` (module `github.com/ios9000/db-portal/backend`,
toolchain-pinned); `cmd/portal/main.go` (prints version — real server is WU-003);
`internal/version/` + test; `.golangci.yml` (v2 config: standard linters, gofumpt+goimports
formatters); root scripts `check:be`/`fmt:be` rewired; pre-commit hook Go branch; CI
swaps uv→Go toolchain + golangci-lint; `infra/bootstrap-vm.sh` installs Go + golangci-lint;
Python files (pyproject, uv.lock, src/, tests/) removed.
**AC:**
- [ ] `npm run check` green locally AND in CI (clean runner).
- [ ] Pre-commit blocks an unformatted `.go` file (prove, revert).
- [ ] `go build ./...` clean; no `*.py`/uv artifacts remain anywhere.
- [ ] CLAUDE.md Commands + env bullets reflect Go toolchain.
**Verify:** `npm run check` exit 0; CI run success; hook rejection demonstrated.
**Context brief:** ADR-010; current backend/ layout; .githooks/pre-commit; .github/workflows/check.yml.

### WU-002 · Dev environment (docker-compose) — S · `done` (2026-07-06, commit 44325db)
**Evidence:** `npm run up` → postgres + mailpit both `(healthy)` via --wait;
`db-reset` ran twice cleanly (idempotent); TCP psql with .env.example creds →
`creds-ok`; mailpit UI HTTP 200. Ports bound to 127.0.0.1 only.
**Goal:** one command brings up portal Postgres 16 + mailpit (SMTP catcher); reset is cheap.
**Deliverables:** `infra/compose.yaml`, `.env.example`, runner targets `up`/`down`/`db-reset`.
**AC:**
- [ ] `up` → `docker compose ps` shows postgres healthy + mailpit UI reachable (localhost).
- [ ] `db-reset` drops and recreates the portal DB idempotently.
- [ ] `.env` gitignored; `.env.example` has every var with safe placeholder values.
**Verify:** `<runner> up && docker compose -f infra/compose.yaml ps` (both healthy);
`psql` connect with `.env.example`-shaped creds against dev password.
**Context brief:** WU-001 layout; ARCHITECTURE.md §Deployment.

### WU-003 · Backend skeleton (Go chassis) — M · `done` (2026-07-07, commit aee80cc)
**Evidence:** `npm run check` green on VM (lint 0 issues, fmt-diff clean, `go test -race`
pass — DB-backed migrate round-trip ran for real against compose PG, 0.08s not skipped);
live `curl /healthz` → 200 `{"db":"ok","status":"ok"}` with slog JSON request line;
`portal migrate status/up/down/up` all clean via binary; SIGTERM → "shutting down",
exit 0. Deps: chi 5.3.1, pgx 5.10.0, goose 3.27.2, env 11.4.1, testify 1.11.1.
**Goal:** the Go server chassis every feature WU bolts onto.
**Deliverables:** chi server with graceful shutdown; env config (caarlos0/env +
godotenv in dev); `/healthz` (pgx pool ping → `{"status":"ok","db":"ok"}`); goose wired
with migration 0001 (baseline) embedded via `embed.FS`; slog JSON logging + request-log
middleware; httptest-based handler tests + a DB-backed test helper.
**AC:**
- [x] `go run ./cmd/portal` serves `/healthz` ok against compose Postgres.
- [x] `goose up` / `goose down` both clean (via a `migrate` subcommand on the binary).
- [x] ≥3 tests: healthz ok, healthz with DB down (503), config precedence. `-race` clean.
**Verify:** `npm run check`; `curl localhost:8080/healthz`.
**Context brief:** ADR-010; ARCHITECTURE.md §Components→Portal application; WU-002 env vars.

### WU-004 · Frontend shell — M · `done` (2026-07-07, commit 2304b9c)
**Evidence:** `npm run check` green (oxlint · tsc strict · vitest 18/18); `npm run build`
clean; live dev-server check: Vite :5173 served shell (`<title>DB Portal</title>`) and
proxied `/healthz` → Go :8080 → 200 `{"db":"ok","status":"ok"}`. EnvBadge 3/3 +
RunStatus 5/5 variants tested; App test covers nav + index redirect + footer status.
Tokens: Inter self-hosted (@fontsource-variable), 8px grid, PROD/TEST/DEV per docs —
exact hexes are placeholders; reconcile vs brief §Design system when extract lands (icebox).
**Goal:** navigable app shell speaking to the backend; the visual grammar (env badges,
status colors) established once, reused everywhere.
**Deliverables:** router + top nav (`My Databases · Activity · Schedules` — no Approvals in
MVP); API client with typed error handling; `EnvBadge` (PROD red / TEST amber / DEV gray —
redundant encoding, never color alone) and `RunStatus` chip components; a status footer
probing `/healthz`.
**AC:**
- [x] `build` and dev server both work; shell renders with nav + healthz status.
- [x] EnvBadge/RunStatus have component tests (vitest) covering all variants.
- [x] Design tokens follow the design brief (`db-portal-research/05-claude-design-brief.md`
      §Design system) — Inter, 8px grid, defined status colors. *(structure/rules yes;
      exact hex values pending brief extract on VM — see icebox item.)*
**Verify:** `<runner> check`; dev server renders against running backend.
**Context brief:** ADR-001; design brief §Design system + Screen 1 (skim); WU-003 API shape.

### WU-005 · ExecutionAdapter + MockEngine — M · `done` (2026-07-07, commit 6124732)
**Evidence:** `npm run check` green with engine tests visible (`ok internal/engine`);
12 tests pass `-race` at `-count=5` (AC quartet + replay-after-finish, stream-ctx-drop
≠ job cancel, unknown-job errors, registry fail-closed ×2, ClassForEnv). grep
`MockEngine` outside internal/engine → 0 hits. Interface lives as `engine.Adapter`
(Go idiom per ADR-010; doc comment names it the ExecutionAdapter seam).
**Goal:** the engine seam (THE architectural bet, ADR-002) proven with a fake engine good
enough to build the whole UI against.
**Deliverables:** `ExecutionAdapter` Go interface — `StartJob(ctx, template, params)
(JobID, error)`, `Status(ctx, JobID)`, `StreamLogs(ctx, JobID) (<-chan LogLine, error)`,
`Cancel(ctx, JobID)`; `MockEngine` (goroutine-driven, realistic timed log lines, artifact
metadata on success) with failure injection (`params["mock_fail_at"]`); engine registry
keyed by env class (prod/nonprod separation from day one, per guardrail layer 3).
**AC:**
- [x] Tests: happy path, injected failure, cancel mid-run, two concurrent jobs isolated —
      all `-race` clean (channels + goroutines are exactly where races hide).
- [x] No portal code references MockEngine concretely — only the interface + registry.
**Verify:** `npm run check` (adapter tests visible in output).
**Context brief:** ARCHITECTURE.md §4.1 + §Components→Execution engine; ADR-002; ADR-010.

### WU-006 · Single-binary production build — S · `done` (2026-07-07, commit 0922576)
**Evidence:** `npm run check` green (webui tests visible, `-race`); `go build ./...`
passed BEFORE dist copy (placeholder-only compile, AC2); `npm run build:release` →
`backend/bin/portal` 17M, `file`/`ldd`: statically linked; binary run alone from an
empty dir (no .env, no PG): `/` → 200 real index.html, `/healthz` → 503
`{"db":"down","status":"degraded"}` (designed DB-less behavior), `/instances/42` →
200 HTML (SPA fallback), `/api/nope` → 404 text/plain. Tree stays clean after
build (embed dir gitignored except .gitkeep anchor).
**Goal:** the ADR-010 payoff: one static binary serving API + embedded SPA.
**Deliverables:** `go:embed` of `frontend/dist` (build step copies it into
`backend/internal/webui/dist/`); SPA fallback handler (non-`/api` 404s → index.html);
root script `build:release` = frontend build → copy → `go build -trimpath` →
`backend/bin/portal`; dev mode unchanged (Vite proxy).
**AC:**
- [x] `npm run build:release` emits ONE binary; `./backend/bin/portal` alone serves the
      UI shell and `/healthz` on a machine with nothing else installed.
- [x] Binary runs with a placeholder dist when frontend wasn't built (no compile break).
**Verify:** run binary, curl `/` (HTML) and `/healthz` (JSON); `file` shows static-ish binary.
**Context brief:** ADR-010; WU-003 server layout; WU-004 dist output.

**M0 exit:** all Phase 0 WUs done + golden thread: backend up, frontend shell up, a
MockEngine job runnable from a Go test — committed demo script proving it.
*(“pytest” predates ADR-010.)*
**CLOSED 2026-07-07:** `infra/demo-m0.sh` PASS — check green, TestHappyPathDump -v
visible, release binary alone served `/` (HTML shell) + `/healthz` (JSON, 200 with
compose PG up / 503 degraded without). Evidence in JOURNAL s03.

---

## Phase 1 — Hero flow: dump run-now, end to end (Milestone M1)

> Groomed 2026-07-07 (s03, M0 close): § refs below corrected to the real
> ARCHITECTURE.md outline (§2 components · §3 workflows · §4 guardrails · §5 audit);
> design brief = `docs/specs/design-brief.md`; WU-010 spec = `docs/specs/inventory.md`.
> Sizes rechecked: all fit a session; WU-012 is the widest — if it runs heavy, land
> the audit migration + append-only grant tests first, checkpoint, then the flow.

### WU-010 · Inventory schema + CSV import — M · `done` (2026-07-07, commit 5212481)
**Evidence:** `npm run check` green (inventory tests `-race`: SPEC-010 behaviors 1–8 as
table-driven parse tests + 6 DB tests on migrated scratch DBs via new `testutil.MigratedDB`).
Real CLI on compose PG: fixture import #1 → `imported 8 new, updated 0, unchanged 0,
quarantined 0`, #2 → `imported 0 new, updated 0, unchanged 8, quarantined 0` (8 instances /
6 clusters both times); `malformed.csv` → `imported 2 new ... quarantined 7`, all 7 rejects
queryable in `inventory_import_reject` with reasons (unknown env, blank + regex-violating
names, non-numeric size_gb, wrong column count, duplicate-in-file, cluster platform
conflict); bad header and missing file → exit 1, nothing written. Update path (test):
changed pg_version → `updated 1, unchanged 7`, updated_at bumped, no duplicate row.
Instance/cluster tables (env enum, platform, patroni ref, owner, maintenance window field —
O-3); CSV import with per-row validation + quarantine for failures (no connectivity probe
yet — that needs real targets; validation is structural). Import is idempotent (natural key).
**Verify:** import fixture CSV twice → same row count; malformed rows quarantined with reasons.
**Context brief:** `docs/specs/inventory.md` (SPEC-010 — schema, CSV contract, 8 numbered
behaviors, import = `portal import` CLI); fixture EXISTS at `infra/fixtures/instances.csv`;
ARCHITECTURE.md §2 (Inventory, Portal DB).

### WU-011 · Instance API + cards/table UI — M · `todo`
List/detail endpoints with env filter; cards view per design brief Screen 1 (health dot,
badges, last-backup line) + `Cards ⇄ Table` toggle (Screen 2, minus bulk actions).
**Context brief:** design brief Screens 1–2; WU-010 schema; EnvBadge from WU-004.

### WU-012 · Catalog + run-now dump (hero) — M · `todo`
Operation catalog (data-driven, `dump` only); launch drawer (Screen 3 pattern); POST run →
audit record (`submitted`) → MockEngine job → status polling; run list in Activity.
Audit schema per ARCHITECTURE.md §5 — append-only from the first migration
(no UPDATE/DELETE grants for the app role).
**Depends:** O-1 (artifact storage) decided — mock path acceptable: artifact metadata only.
**Context brief:** ARCHITECTURE.md §3 (hero workflow) + §5 (audit record); WU-005 adapter;
design brief Screen 3.

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
**Context brief:** ARCHITECTURE.md §4 (guardrails); design brief Screen 4 (adapt: no
approval flow in MVP).

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

- Bump GH Actions action versions (checkout/setup-go/setup-node emit node20-deprecation warnings); same pass: fix setup-go cache miss (`cache-dependency-path: backend/go.sum`)
- Reconcile WU-004 token hex values vs design brief §Design system — brief now ON the VM at `docs/specs/design-brief.md` (unblocked 2026-07-07)

- Inventory: UI/API upload + import-history screen (MVP import is `portal import` CLI — SPEC-010)
- Inventory: Excel/.xlsx ingestion (MVP is CSV-only — SPEC-010)
- Patroni-aware dump/restore sequencing (research gotcha #1: cancel semantics too)
- PITR; Vacuum/Reindex buttons; approvals workflow (Screen 7); Jira linkage; SSO
- Portal self-target ban (research gotcha #2) — enforce in inventory layer when real targets exist
- Bulk/rolling operations (Screen 2 sticky bar); saved views
- 5-year audit shipping to object storage; SIEM export
