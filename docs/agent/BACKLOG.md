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

### WU-011 · Instance API + cards/table UI — M · `done` (2026-07-07, commit bc04f2d)
**Evidence:** `npm run check` green (Go `-race` incl. new store tests on scratch DBs +
stub-based handler tests that run DB-free in CI; vitest 24/24 with 6 new MyDatabases
tests). Live binary on compose PG: `/api/instances` → 8 (ordered, JSON null for empty
size/window), `?env=test` → billing-test/crm-test/hr-test, `?env=prod2` → 400
`unknown env`, detail → full object, unknown name → 404. Release build re-proven:
single 18M binary served `/` (title DB Portal), `/databases` SPA fallback 200 HTML,
`/api/instances` → 8 — all same-origin. Health/last-backup/vacuum/bloat render "—"
(no data source until WU-012+). New dev dep: @testing-library/user-event.
List/detail endpoints with env filter; cards view per design brief Screen 1 (health dot,
badges, last-backup line) + `Cards ⇄ Table` toggle (Screen 2, minus bulk actions).
**Context brief:** design brief Screens 1–2; WU-010 schema; EnvBadge from WU-004.

### WU-011R · Last backup on fleet views — S · `done` (2026-07-10, user-requested s10)
WU-011 shipped "Last backup: —" as a placeholder; WU-012 created the data source
(runs) but nothing wired them — user hit it live after a successful backup. Fix:
`Instance.last_backup_at` = max(finished_at) of SUCCESSFUL dump runs (correlated
subquery in inventory.Store, shared by list+get; failed/canceled/non-dump excluded,
never-dumped = null); card + table render formatTimestamp or "—". Health/vacuum/
bloat stay placeholders (no probes until M3+).
**Evidence:** TestLastBackupAt (newest success wins over newer failed/canceled;
restore ≠ backup; no runs = nil); handler JSON contract (RFC3339 / explicit null);
golden flow asserts billing-test carries last_backup_at post-success, others null;
vitest card+table assertions both states; npm run check green; LIVE: analytics-dev
card shows the user's 03:45 backup, crm-test honestly "—" (only canceled runs).
**Context brief:** inventory/store.go, server/instances.go seam, MyDatabases.tsx,
lib/api.ts+format.ts; SPEC-012 run model.

### WU-012 · Catalog + run-now dump (hero) — M · `done` (2026-07-07, commits 5139a28+26d09ed+21d9e54)
**Evidence:** `npm run check` green both stacks (Go `-race`: 7 runs-service tests on
scratch DBs incl. failure injection, engine-refusal audit trail, orphan sweep, prod/nonprod
routing; audit immutability tests; 8 stub handler tests run DB-free in CI; vitest 29/29
with drawer + Activity coverage). SPEC-012 at `docs/specs/runs.md` (mini-ADRs: run
mutable / audit_event append-only per transition; catalog in code; watcher goroutine;
orphan sweep; actor='local-dev'). Live on the 18M release binary: POST dump on
billing-test → queued w/ job `mock-nonprod-1` → success in ~1s with artifact metadata
(name/1MiB/sha256); billing-prod run → job `mock-prod-1` (per-class adapters proven
live); audit_event: submitted (final_status NULL) + finished (success) per run, env +
playbook_tag stamped; psql UPDATE and DELETE on audit_event → trigger exception; unknown
instance → 404, unknown operation → 400; drawer verified in the embedded bundle.
Operation catalog (data-driven, `dump` only); launch drawer (Screen 3 pattern); POST run →
audit record (`submitted`) → MockEngine job → status polling; run list in Activity.
Audit schema per ARCHITECTURE.md §5 — append-only from the first migration
(no UPDATE/DELETE grants for the app role).
**Depends:** O-1 (artifact storage) decided — mock path acceptable: artifact metadata only.
**Context brief:** ARCHITECTURE.md §3 (hero workflow) + §5 (audit record); WU-005 adapter;
design brief Screen 3.

### WU-013 · Run detail + live logs — M · `done` (2026-07-07, commits 29f81ee+f256b9b+7d4422f)
**Evidence:** `npm run check` green both stacks (Go `-race`: 7 new stream/cancel service
tests on scratch DBs + SSE/cancel stub handler tests DB-free; vitest 38/38 with 6 RunDetail
tests incl. fake EventSource). SPEC-013 at `docs/specs/run-detail.md` (mini-ADRs: SSE over
WebSocket; explicit `end` event kills the EventSource replay loop; logs NOT persisted —
410 after restart; cancel ships here, no new audit action until WU-021). Live on the
release binary: SSE replay+follow+end on a real run; abort → 202 (idempotent) → run
`canceled` / "canceled by operator", audit = exactly submitted+finished(canceled), tamper
UPDATE → trigger exception; terminal cancel → 409; stale run logs after restart → 410;
SPA fallback /runs/3 → 200. Regression found live + fixed: MockEngine job ids aliased
across restarts (seq reset) so an orphaned run streamed a NEW run's logs — ids now carry a
per-instance nonce; JobID no-alias contract documented in engine.go + regression test.
Run page with single-stage state (chains → WU-032), dark log pane, Follow pill, live
elapsed, artifact strip, calm failed/canceled cards, Abort; linked from Activity ids +
drawer "View run".
**Context brief:** design brief Screen 5; WU-005 `stream_logs` contract.

### WU-014 · Audit UI + email notify — S · `done` (2026-07-07, commits 7a6c97f+c9f7661)
**Evidence:** `npm run check` green both stacks (Go `-race`: notify pkg vs in-test SMTP
server incl. leak-channel negative assertions; notifier fires on failed+canceled+refusal+
orphan-sweep, silent on success, log-only on SMTP error; ListFilter AND-composition tests;
vitest 44/44 with rewritten Activity tests + csv unit tests). SPEC-014 at
`docs/specs/activity-notify.md` (mini-ADRs: notify from finalize() = the single seam, on
any not-success terminal; best-effort post-commit send never blocks finalization; content
= who/what/where/status + link ONLY — no reason/error/params; filters server-side;
requester surfaced from the submitted audit event; export = client-side CSV of the view).
Live on the release binary: abort → mailpit `RUN-7 canceled — dump on billing-test (test)`
with run link, zero leak strings; SIGKILL mid-run + restart → orphan sweep mailed
`RUN-9 failed — dump on crm-test (test)` unattended; psql UPDATE and DELETE on
audit_event → "audit_event is append-only"; `?state=failed` → only the swept run,
`&env=prod` → [] (ANDed). Activity = Screen 6 minus approvals: filter chips in URL
params, Now-running section (indeterminate bar + live elapsed), Requester column,
Export CSV. Config: PORTAL_SMTP_FROM / PORTAL_NOTIFY_TO (empty=off) / PORTAL_BASE_URL.
Deferred: user filter → WU-021; date range + pagination + server export → icebox.
**Context brief:** design brief Screen 6; SPEC-012 §audit; internal/runs/service.go.

### WU-015 · Prod guardrails — S · `done` (2026-07-07, commit fc460d9)
Env-colored full-width banner on instance/run contexts; typed instance-name confirmation
for prod actions (paste disabled); non-prod = one click; env stamped in every audit row
(already in schema — assert it in tests). Guardrail layer 3 (separate engine credentials)
is asserted at the adapter-registry level: prod jobs MUST resolve a different engine
config object than nonprod, even while both are mocks.
**Context brief:** ARCHITECTURE.md §4 (guardrails); design brief Screen 4 (adapt: no
approval flow in MVP).

**M1 exit = the demo — CLOSED 2026-07-08 (s07, commit c0d1a5e).** `docs/demo-m1.md`
live-verified on the release binary: import ×2 (8 new → 8 unchanged) → fleet 8 (`?env=test`
→ 3) → dump billing-test success + artifact + SSE (7 log events, one `end`) → abort
crm-test → mailpit `RUN-2 canceled — dump on crm-test (test)` → billing-prod on
`mock-prod-…` → 6 audit rows env-stamped, UPDATE → "audit_event is append-only".
Command-level flow ~4 s; human pace fits < 5 min. Automated twin
`backend/e2e/golden_flow_test.go` in `go test -race ./...` = inside `npm run check`
(ADR-011; skips without compose PG like every DB test — CI gap iceboxed). Typed-name
ritual + EnvBanner stay vitest-pinned (UI-only beats). Multi-agent review gate =
DONE 2026-07-09 (s08) — see the M1-gate fix WUs below + docs/agent/reviews/m1-gate.md.

### M1-gate review fixes (2026-07-09, s08) — land BEFORE WU-020

> Gate outcome: 2 high / 6 medium / 9 low confirmed (each upheld by 2+ adversarial
> verifiers or inline-verified at the cited lines), 3 refuted. Full record with failure
> scenarios + fix sketches: `docs/agent/reviews/m1-gate.md`. Order 016 → 017 → 018 →
> 019; 019's CSV-injection item MUST precede WU-020 (real usernames).

### WU-016 · M1-gate fix: run lifecycle integrity — M · `done` (2026-07-09, commit 8531c68)
Backend run state machine holes (findings 1, 2, 3, 9 in the gate record):
(a) Start(): job_id-UPDATE failure after a successful StartJob strands the live engine
job (run stuck `queued`, no watcher, no `run.finished` audit event) — make the
post-StartJob path fail-safe (mini-ADR: cancel-the-job + finalize failed vs
retry-then-adopt; trail complete either way); (b) finalize(): guard the terminal
transition (UPDATE … WHERE state NOT IN terminal, audit INSERT only when a row
actually transitioned) so double-finalize can't duplicate `run.finished` or overwrite
a terminal state; (c) SweepOrphans: per-run best-effort instead of abort-on-first-error;
(d) SSE `end` event: emit the run's true terminal state (bounded wait on the row) or
amend SPEC-013's "final run state" wording — decide in-WU, one line either way.
**Verify:** new -race tests: double-finalize → exactly one `run.finished`; injected
job_id-UPDATE failure → no stranded live job + complete trail; e2e golden flow still
one `end`, now terminal-state-correct (or spec amended); `npm run check` green.
**Context brief:** docs/agent/reviews/m1-gate.md items 1-3, 9; internal/runs/service.go
(Start/finalize/watcher/SweepOrphans); internal/server/runs_http.go:165-186; SPEC-012/013.

### WU-017 · M1-gate fix: audit hardening (TRUNCATE + job_id) — S · `done` (2026-07-09, commit 07a12e5)
Migration 0004: `BEFORE TRUNCATE … FOR EACH STATEMENT` trigger reusing
audit_event_immutable() + `REVOKE TRUNCATE`; add `job_id text NULL` to audit_event,
stamped on `run.finished` (NULL at submit is honest — the id doesn't exist yet).
Reconcile SPEC-012 mini-ADR 1's "every §5 field" claim with reality.
**Verify:** DB test: `TRUNCATE audit_event` raises "append-only" (alongside the
existing UPDATE/DELETE tests); golden flow asserts job_id on the finished event;
goose down walks one migration; `npm run check` green.
**Context brief:** docs/agent/reviews/m1-gate.md items 4-5; 0003_runs_audit.sql;
internal/runs/service.go audit INSERTs; ARCHITECTURE §5; migrate_test down-walk.

### WU-018 · M1-gate fix: inventory size_gb canonicalization — S · `done (2026-07-10, commit 391e955)`
csv.go keeps the raw size_gb string; Postgres canonicalizes — two symptoms, one root
cause (gate items 8a/8b, one reproduced live): hex-float forms Go accepts but PG
rejects abort the WHOLE import instead of quarantining the row; PG-normalized forms
('1e2'→'100', '.5'→'0.5') re-import as "updated" forever, churning updated_at.
Fix at the source: canonicalize SizeGB at parse time (strconv.FormatFloat) so store
and compare see one form, and make PG-rejectable forms quarantine, not abort.
**Verify:** parse/import tests with '1e2', '.5', '0120', '0x1p4' → canonical store or
quarantine, never abort; same-file re-import → all unchanged (SPEC-010 behavior 2);
`npm run check` green.
**Context brief:** docs/agent/reviews/m1-gate.md item 8; internal/inventory/csv.go
(size_gb parse + finite check), import.go:140-165 (canonical compare); SPEC-010.

### WU-019 · M1-gate fix: frontend resilience — S · `done (2026-07-10, commit 3cfdf8f — Sonnet 5 delegation pilot)`
Gate items 6-7 + the low bundle 10-13: RunDetail/api.ts SSE — transient stream failure
(5xx) must retry/backoff keeping received lines, not morph into permanent "logs gone";
LaunchDrawer — overlay click must not dismiss mid-launch or swallow a just-fired prod
confirmation; getJSON/postJSON — wrap res.json() so a 2xx malformed body surfaces as
ApiError per the module contract; Activity — drop stale fetch responses under newer
filters (CSV export must match the view); exportCsv — defer revokeObjectURL past the
click (Safari); csv.ts field() — neutralize leading `=+-@\t` (OWASP CSV injection;
latent until WU-020's real usernames, so this WU precedes it).
**Verify:** vitest for each beat (fake EventSource transient error → lines kept +
retried; overlay click during launch keeps drawer; stale response ignored;
`=SUM(A1)` field neutralized; malformed 2xx → ApiError); `npm run check` green.
**Context brief:** docs/agent/reviews/m1-gate.md items 6-7, 10-13; frontend/src/lib/
{api,csv}.ts, components/LaunchDrawer.tsx, pages/{RunDetail,Activity}.tsx; SPEC-013/014/015.

---

## Phase 2 — Auth, scheduling, windows (M2)

> Groomed 2026-07-08 (s07, M1 close). Specs stay just-in-time: write `docs/specs/authn.md`
> etc. at WU start, not before. Order is fixed: 020 → 021 (guards need sessions) → 022 →
> 023 (windows warn on BOTH launch paths, so the scheduler must exist first).
> M2 exit criteria live-verified per WU (JOURNAL s10-s12). Multi-agent review gate =
> DONE 2026-07-10 (s12) — GATE PASSES with fix WUs: see M2-gate fixes below +
> docs/agent/reviews/m2-gate.md (11 confirmed findings, 0 refuted, no criticals).

### WU-020 · AuthN: LDAP bind against AD — M · `done (2026-07-10, commits 0e30914+5034dc8+d5c6731 — spec+backend by architect, UI slice via Sonnet 5 delegation)`
Login page + server sessions; AD LDAP bind (portal NEVER stores AD passwords —
ARCHITECTURE §2 Identity); dev mode = bypass flag + fake in-process directory (CI-safe,
same seam pattern as MockEngine); ONE break-glass local account whose every use writes
an alarmed audit event (new `auth.break_glass` action). AuthZ/roles are NOT here — any
authenticated user passes until WU-021. Size check: fits a session, but if it runs heavy
land (a) session store + LDAP/fake-directory bind + middleware first, checkpoint, then
(b) login UI + break-glass. go-ldap is the expected dep (mini-ADR it in the spec).
**Verify:** unauthenticated `/api/*` → 401 (healthz exempt); fake-directory login sets a
session and the SPA works end-to-end; break-glass login → audit row with alarmed action;
bypass flag off = no bypass; `npm run check` green (golden flow must pass authenticated
or via the dev bypass — decide the e2e wiring in the spec).
**Context brief:** ARCHITECTURE §2 (Identity) + §4; D2/D3; internal/server/middleware.go
(the middleware seam); server.Deps wiring in cmd/portal/main.go; backend/e2e (gate impact).

### WU-021 · AuthZ: DBA role, route guards, real actor — M · `done (2026-07-10, commits 5dbb24a spec + 4daf552 backend + f0a7d45 docs + 99bac19 UI via 3rd Sonnet delegation)`
Portal-DB role store (role table + user↦role, seeded DBA); route guards on every
mutating endpoint (DBA-only per D2 — read endpoints stay role-gated-lite, decide in
spec); audit actor = AD identity everywhere (replaces the `local-dev` constant in
internal/runs). Carries the deferred ledger accumulated across Phase 1 — this is why it
grew S → M: `run.canceled` audit action (SPEC-013 deferral), Activity requester/user
filter (SPEC-014), prod-ritual server-side enforcement + actor on the ritual
(SPEC-015 — the API stops being deliberately unguarded here). Also from the M1 gate
(m1-gate.md item 15): POST /api/runs body cap — http.MaxBytesReader (413 on overflow)
+ server-side `reason` length limit — lands with the route guards.
**Verify:** non-DBA user → 403 on POST /api/runs (audit records the denial — decide
shape in spec → `authz.denied` on auth_event, SPEC-021 mini-ADR 3); cancel writes
`run.cancel_requested` with the canceling actor (spec kept SPEC-013's name over this
entry's `run.canceled` shorthand — cancel can race a success finish); runs list
`?requested_by=` filter; prod run submitted by an authenticated DBA carries their AD
identity in both audit rows; `npm run check` green. ALL VERIFIED LIVE 2026-07-10.
**Context brief:** D2/D3; SPEC-012 §audit + SPEC-015 deferrals; internal/runs/service.go
(actor constant); server router; WU-020's session context.

### WU-022 · Portal-owned scheduler (ADR-003) — M · `done (2026-07-10, commits e316515 spec + 0579a58 backend + a5c2867 UI via 4th Sonnet delegation)`
Schedule CRUD (table + API + the stub /schedules screen) for scheduled dumps (D4);
executor = robfig/cron/v3 (SPEC-022 mini-ADR 1 narrowed this to parser-only — the
runner is a DB tick loop over a persisted next_fire_at, which makes misfire catch-up
and restart safety structural), jittered start, firing through runs.Service.Start —
the SAME guardrail/audit path as run-now, actor = `schedule:<owner>`. Misfire policy
(coalesced catch-up), overlap policy (skip visibly), enable/disable (freeze; re-enable
computes from now) each got their mini-ADR. Split as sized: backend slice first
(checkpoint 0579a58), UI second.
**Verify:** a schedule on a test instance fires within jitter bounds with full audit
attribution (`schedule:<owner>` in both rows — ROADMAP M2 exit); disabled schedule never
fires; portal restart neither double-fires nor silently drops a due schedule (per the
spec'd misfire policy); `npm run check` green. ALL VERIFIED LIVE 2026-07-10 (incl. a
real stop/start across two missed fire times → ONE coalesced catch-up).
**Context brief:** ADR-003; D4; internal/runs/service.go (Start seam); frontend
/schedules stub route (App.tsx); WU-021 actor conventions.

### WU-023 · Maintenance windows warn-only — S · `done (2026-07-10, commits 3de6187 spec + 97de1ab impl — architect-implemented, too small to delegate)`
Give `instance.maintenance_window` (raw text since WU-010, O-3) just enough semantics
to warn: parse the fixture's `Day HH:MM-HH:MM` shape (SPEC-023: wrap-capable — the
fixture ships `Sat 22:00-02:00`); launching OUTSIDE the window (drawer AND scheduler
path — one stamp point, runs.Start) shows a warn banner — never blocks (D6) — and
stamps `window_warned` on the run.submitted audit row. NO schema addition needed:
0003 pre-provisioned the column ("semantics arrive in WU-023") — the spec's first
draft invented a migration and the fresh-DB walk caught it. Unparseable/empty = no
warning, logged once per instance per process. Timezone: server-local (mini-ADR 6,
consistent with SPEC-022). Instance read model gains server-computed `window_state`
so the client never parses window text.
**Verify:** launch outside window → banner + `window_warned` true in audit; inside →
no flag; scheduled fire outside window carries the flag too; garbage window text never
blocks a launch; `npm run check` green. ALL VERIFIED LIVE 2026-07-10 (Fri-night runs
against the fixture's Sat windows; garbage window 2×201 + exactly 1 log line).
**Context brief:** D6; O-3 (DECISIONS §Open); WU-010 schema (instance.maintenance_window);
LaunchDrawer (frontend) + runs.Service.Start (stamp point); WU-022 executor path.

### M2-gate review fixes (2026-07-10, s12) — land BEFORE any Phase-3 WU

> Multi-agent gate review DONE 2026-07-10 (workflow wf_7ee53a3d-c48, 5 Sonnet
> reviewers, architect-verified inline): 11 findings, 11 confirmed, 0 refuted,
> no criticals — full scenarios + fix sketches: `docs/agent/reviews/m2-gate.md`.
> Order 024 → 025 (024 carries the one HIGH).

### WU-024 · M2-gate fix: scheduler hardening — M · `done (2026-07-11, commit 54db393 — architect-implemented; all verify criteria race-tested + promotion path live-verified)`
Gate items 1-3, 5, 8, 11 (docs/agent/reviews/m2-gate.md): (1) persist the
creation-time `confirm` string on the schedule row (migration 0008) and fire with
it verbatim — an instance promoted to prod after schedule creation then fails the
ritual visibly (status 'error') instead of auto-confirming; test the promotion
path. (2) stamp guards `next_fire_at = CASE WHEN enabled THEN … ELSE NULL END` +
re-check enabled at fire() entry. (3) overlap probe widens to ANY live run on the
instance (also stops piling onto a live button-press run) — amend SPEC-022
mini-ADR 4; cross-resource locking stays M4. (5) per-fire context.WithTimeout.
(8) SetEnabled short-circuits when state is unchanged (no jitter re-roll).
(11) PATCH oversized body → 413.
**Verify:** promote-instance-to-prod test → next fire stamps 'error', zero runs;
disable-during-fire leaves next_fire_at NULL; sibling schedules on one instance
never overlap (and a scheduled fire skips while a manual run is live); redundant
PATCH enabled=true leaves next_fire_at byte-identical; `npm run check` green.
**Context brief:** docs/agent/reviews/m2-gate.md items 1-3, 5, 8, 11;
internal/schedule/{schedule,executor}.go; SPEC-022 mini-ADRs 4+8;
inventory/import.go upsert (env update path).

### WU-025 · M2-gate fix: identity & session honesty — S · `done (2026-07-11, commit 90aae5a — architect-implemented; case-fold + boot warn live-verified)`
Gate items 4, 6, 7, 9, 10: (4) canonicalize (lowercase) the username ONCE at the
authn seam before session/audit/authz — AD binds are case-insensitive, the portal
is not; decide in-fix whether existing mixed-case session rows need care (TTL
makes them self-expire). (6) App bootstrap: 401 → login, anything else → an
"unavailable + retry" state (the idiom every page already uses). (7) sign-out:
proceed locally only on 401; other failures surface "could not confirm sign-out"
and stay signed in (httpOnly cookie cannot be cleared client-side). (9) gate the
New-schedule button on the list having loaded. (10) boot Warn when
AuthMode=="ldap" && !CookieSecure (mirror the PORTAL_LDAP_INSECURE warning).
**Verify:** login as "DBA1" in fake/ldap-shaped test → session + audit rows say
"dba1" and requireRole matches; bootstrap with a 500ing /me shows retry not
login; failed logout keeps the session UI-visible; `npm run check` green.
**Context brief:** docs/agent/reviews/m2-gate.md items 4, 6, 7, 9, 10;
internal/authn/{service,ldap}.go; frontend App.tsx + components/Shell.tsx +
pages/Schedules.tsx; cmd/portal/main.go (boot warnings).

## Phase 3 — Restore, chains, real engine (M3)

> Groomed 2026-07-11 (s14, M2 close). **Execution order is NOT numeric: 030 → 032 →
> 031 → 033 → 034 → 035 → 036.** Rationale: the restore workflow IS the first chain
> (verify → safety dump → restore — the same 3 steps the M3 exit criterion drills), so
> the chain engine (032) lands before restore (031) rides it; building restore ad hoc
> then re-platforming it one WU later is churn. **WU-036 is NEW at grooming:** the M3
> exit ("restore rehearsal on a compose target passes") needs a real restore playbook
> no skeleton WU covered — 034 is dump-only. Specs stay just-in-time (SPEC-030… at WU
> start). Storage story pre-O-1: mock artifacts are metadata-only forever; 034's real
> dumps land on a compose volume (location recorded); 035 moves bytes to minio — the
> registry schema (030) pre-provisions `location` so only semantics change later
> (the 0003 window_warned pattern). Engine story: MockEngine stays the default for dev
> and ALL tests (ADR-002); Semaphore is opt-in wiring, live-verified on the VM.
> Delegation: UI slices + playbook/compose scaffolds are Sonnet-brief material; chain
> engine core, adapter concurrency, ritual/authz seams stay architect-implemented.
> Carry-over lesson (WU-024): anything that fires later on a user's behalf (chains,
> like schedules) stores creation-time ritual EVIDENCE and fires with it verbatim.

### WU-030 · Artifact registry (metadata-first) — S · `done (2026-07-11, commit c7c6b73 — architect-implemented; SPEC-030 = docs/specs/artifacts.md)`
Dump artifacts become first-class queryable rows — the restore workflow's source
of truth — instead of three denormalized columns on `run`. Migration 0009:
`artifact` (id, run_id FK origin, name, size_bytes, checksum, retention_class
CHECK 'standard'|'safety' default 'standard', created_at, `location text NULL` —
dormant until 034/035, comment it) + backfill from historical successful runs.
finalize() inserts the registry row INSIDE the guarded terminal transition (same
tx as the run UPDATE — rides the WU-016 double-finalize guard, so exactly-once
is structural). GET /api/artifacts?instance=<name> (session-gated, newest first,
origin run id in the payload) — 031's drawer feeds from it. Run's own
artifact_* columns STAY (run read model + last_backup_at untouched); registry =
what can be restored, run columns = what this run produced — same values,
written atomically together (mini-ADR the dual write in SPEC-030). No retention
ENFORCEMENT — class is stored classification only (job is M4/icebox).
**AC:**
- [ ] 0009 up+down+up walks clean; backfill registers every historical successful
      dump exactly once (idempotent across the walk).
- [ ] Every successful dump (button AND scheduled) registers an artifact row
      atomically with finalize; failed/canceled/double-finalize paths never do.
- [ ] GET /api/artifacts?instance= → rows newest-first; unknown instance 404;
      session required; ?env-style validation consistent with /api/instances.
**Verify:** -race tests (exactly-once incl. double-finalize path; backfill walk on
scratch DB); golden flow asserts an artifact row w/ origin FK after its dump beat;
`npm run check` green.
**Context brief:** SPEC-012 (docs/specs/runs.md) §artifact; migrations
0003 (run artifact cols) + 0008 (current head); internal/runs/service.go
(finalize) + query.go (read model); internal/server/runs_http.go (handler
patterns); ARCHITECTURE §3 ("artifact registered (checksum, retention class)").

### WU-032 · Chain engine — M · `done` (2026-07-11, e465bab core + b10b960 UI — SPEC-032)
Portal-level sequential step execution over runs (D5): halt + notify on failure,
persisted state, resume from failed step — the mechanism restore (031) rides.
Migration 0010: `chain` (id, kind, actor, target instance FK, confirm — stored
creation-time ritual evidence per the WU-024 lesson, state, created/halted/
finished ts) + `chain_step` (chain FK, seq, name, params, state, run_id FK NULL
until fired — FK direction chosen so `run` is untouched). chain.Service: steps
fire strictly sequentially through runs.Service.Start (SAME guardrail/audit
path; step-run actor attribution — `chain:<initiator>` vs plain initiator — is
SPEC-032 mini-ADR 1); step failure → chain `halted` + ONE notify mail (existing
content rule: who/what/where/status + link only); POST /api/chains/{id}/resume
re-fires the FAILED step as a NEW run (history kept) then continues; boot sweep
mirrors SweepOrphans: chains left `running` with no live run → halted + notify
(runs after SweepOrphans in main.go so a swept step run is seen). Cancel of a
live step run halts the chain, resumable (confirm in spec). Env promotion of the
target mid-chain → stored confirm fails the ritual visibly, never auto-confirms
(the schedule.confirm behavior, ported). UI slice (checkpoint boundary): chain
strip on RunDetail (step N of M, sibling links) + Resume on halted chains;
Activity unchanged (steps ARE runs). Mutations DBA-gated + body-capped like
every other endpoint.
**AC:**
- [x] 3-step chain, injected failure at step 2: step 1 run+audit intact, chain
      halted, ONE mail; resume re-runs step 2 then 3 to success (the ROADMAP
      exit drill, as -race test at the service seam AND an HTTP-seam e2e).
      (chain_test.go behaviors 1–3 + golden flow Beat 9)
- [x] Halted chain survives restart; boot sweep halts orphaned running chains
      (crash mid-step) + notifies; no double-fire of a step across restart.
      (TestSweepOrphans + live boot-sweep drill s16: count=1, mail in mailpit)
- [x] Resume while running / double-resume → 409 (single-flight per chain);
      non-DBA resume → 403 with authz.denied trail. (TestResumeSingleFlight,
      resume route in TestMutationsRequireDBARole)
- [x] Step runs carry full audit attribution through runs.Start — zero Registry
      or audit bypass (grep-level: chain pkg never touches engine directly —
      imports are catalog/runs/pgx only).
**Verify:** the -race + e2e tests above; `npm run check` green.
**Context brief:** D5; ARCHITECTURE §3 (chains); docs/agent/reviews/m2-gate.md
item 1 (the stored-evidence pattern); internal/runs/service.go (Start seam,
finalize/SweepOrphans patterns to mirror); internal/schedule/executor.go
(fire-with-stored-confirm precedent); internal/notify/notify.go;
internal/server/schedules_http.go (guarded-CRUD handler pattern);
frontend/src/pages/RunDetail.tsx.

### WU-031 · Restore workflow (MockEngine) — M · `done`
D4's second catalog operation: restore a registered artifact to an EXPLICIT
target, with the incident-killing automatic pre-restore safety dump —
MockEngine first (real playbook = 036). Catalog gains `restore`; POST assembles
a kind=restore chain via 032: (1) checksum verify, (2) safety dump of the
TARGET (registers with retention_class 'safety'), (3) restore. SPEC-031
mini-ADR: verify as its own chain step vs a param the restore job re-checks
engine-side — pre-O-1 there are no portal-readable bytes, so MockEngine
"verifies" with injectable failure; real bytes are verified in 036's playbook
regardless (defense in depth). Target selection explicit, defaults non-prod;
prod target = full typed-name ritual (server-side, TARGET's name, stored on the
chain — 032 provides). Window warn stamps per step via runs.Start already —
zero new code, assert it. **The safety dump is unconditional — no skip
affordance anywhere, client or API** (the motivating incident,
institutionalized). Verify-fail halts BEFORE the safety dump: garbage artifact
= zero runs on the target. UI slice (checkpoint boundary): Restore drawer from
instance context — pick artifact (030 API), pick target, env banner + ritual,
launch → chain view.
**AC:** (all met — s17, commits f2dcf2d backend + 270e665 UI)
- [x] Golden flow gains Beat 10: restore on MockEngine end to end — safety-dump
      run + restore run both fully audited, 'safety' artifact registered,
      lineage tied (restore step params reference the artifact/checksum).
- [x] Prod-target restore without the exact typed TARGET name → 400, no chain
      row created; non-prod stays one click (D2 holds on the new path).
- [x] Injected verify failure → chain halts at step 1, zero runs on the target,
      notify mail sent, resumable after "fix".
- [x] No API shape permits restore-without-safety-dump (handler test + grep —
      Internal:true only in chain/driver.go; the recipe is the sole op source).
**Verify:** e2e Beat 9; -race chain-assembly tests; vitest drawer tests
(artifact pick, default-target rules, ritual); `npm run check` green.
**Context brief:** ARCHITECTURE §3 (restore); D4/D5; SPEC-030 + SPEC-032 (exist
by then); internal/catalog/catalog.go; internal/server/runs_http.go;
frontend/src/components/LaunchDrawer.tsx (ritual pattern) +
pages/MyDatabases.tsx (entry point); docs/specs/guardrails.md.

### WU-033 · SemaphoreAdapter — M · `done` (2026-07-11)
ADR-002's payoff: the same portal drives a REAL engine. Compose `semaphore`
service (BoltDB dialect for dev simplicity — mini-ADR; admin creds in .env only,
ADR-004); playbooks/ becomes a Semaphore-servable repo layout + `smoke.yml`
(echo/sleep — proves the adapter without 034); internal/engine/semaphore.go
implements Adapter: StartJob = create task on a template pinned to the playbook
tag (mapping via config), Status/StreamLogs = poll + incremental task output
(cadence config), webhook receiver for terminal-status acceleration with POLL
AS THE FALLBACK TRUTH (ADR-002) — webhook route authenticated by shared secret,
bogus/unauthenticated → 401 + zero state change; Cancel = task stop. Registry
wiring: `PORTAL_ENGINE_NONPROD=semaphore` opt-in; prod class stays mock in dev;
disjoint per-class config objects assert guardrail 3 structurally. ALL tests
keep MockEngine; ONE integration test drives real Semaphore and skips without
the compose service (the testutil.MigratedDB skip pattern). Split as sized:
(a) compose + adapter poll-only, checkpoint; (b) webhook + streaming + itest.
**AC:**
- [x] Live on VM: portal button-dump on the smoke template through real
      Semaphore — queued→running→success (run 1, task 2147483634, ~18s), logs
      stream into RunDetail (34 real ansible lines + `end`), cancel mid-run
      works (run 2 → canceled), audit trail shape identical to mock runs
      (submitted→finished / submitted→cancel_requested→finished). [s18]
- [x] Webhook: terminal status lands without waiting a poll interval (run 4,
      watcher parked 30s, webhook finalized on the spot); webhook DOWN → poll
      still finalizes (runs 1–3 fired no webhook; `TestPollFinalizesWithoutWebhook`);
      bad/missing secret → 401 + zero state change (live + `TestWebhookAuthFailsClosed`). [s18]
- [x] `npm run check` green with zero Semaphore dependence (itest skips clean
      when the token is unset). [s18]
**Verify:** itest output both modes (skip + live) in journal; the live drill
above; `npm run check` green. — DONE s18.
**Context brief:** ADR-002; ARCHITECTURE §2 (adapter, webhook+poll);
internal/engine/{engine.go,mock.go,registry.go}; internal/config/config.go;
infra/compose.yaml; internal/server/server.go (webhook route seam);
SPEC-012 (runs.Service ↔ adapter contract).

### WU-034 · Dump playbook (real) — M · `done` (2026-07-11, s19 — 36d08ab slice a + slice b; SPEC-034 = docs/specs/dump-playbook.md)
First real operation: `pg_dump -Fc` of a compose target Postgres via the
engine — proves playbook shape, machine-readable outcomes, artifact reality.
Compose `pgtarget` (postgres:16, seeded sample schema+rows via init script) +
inventory fixture row so it's a portal instance (env dev; runs need the FK).
`playbooks/dump.yml`: pg_dump -Fc → shared artifact volume, sha256 + size
computed, ONE machine-readable result line (JSON) the adapter parses into
engine.Artifact — which gains `Location` (mock leaves it empty). Target creds
engine-side ONLY (Semaphore key store / vault file outside git — ADR-004);
runner needs postgresql-client (image layer vs setup task — decide in spec).
O-4: fixed vetted flag set, zero user-facing options. Replica-first/Patroni
stays out (iceboxed).
**AC:** (all met — s19; live drill on an isolated portal, :8099 + portal_drill, torn down)
- [x] Live on VM: portal dump on pgtarget through Semaphore → real .dump on the
      volume (appdb-…​.dump, 5382 B); registry row carries REAL sha256/size/location
      (checksum == `sha256sum` of the file); `pg_restore --list` succeeds
      (ledger/widget/ledger_totals + TABLE DATA — genuinely restorable).
- [x] Playbook failure (bad pgtarget-env creds) → RUN-2 failed, honest error
      ("semaphore task failed"), ZERO artifact rows, notify mail "RUN-2 failed —
      dump on pgtarget (dev)" — the M1 failure path holds for real.
- [x] No secrets in repo, portal DB, params, or logs (pgtarget password + Semaphore
      token + webhook secret all absent from the portal_drill dump AND the log;
      params_digest is a hash; audit shape byte-identical to a mock run).
- [x] MockEngine tests untouched (location stays NULL — TestArtifactRegisteredOnSuccess);
      `npm run check` green engine-free (CHECK-EXIT:0, golangci 0 issues, vitest 116/116).
**Verify:** live drill outputs + pg_restore --list in journal; `npm run check`. — DONE s19.
**Context brief:** O-4 (DECISIONS §Open); SPEC-033 (adapter/template contract);
infra/compose.yaml (+pgtarget +volume); playbooks/smoke.yml (033 scaffold);
internal/engine/semaphore.go (result-parsing seam) + engine.go (Artifact);
infra/fixtures/instances.csv; ADR-004.

### WU-035 · O-1 storage: minio — S · `done`
Artifact bytes get a real home. Compose `minio` + bucket bootstrap; dump
playbook uploads AFTER checksum (engine-side creds, ADR-004) and records the
object URL in the result line → registry `location`; the volume becomes staging
only. Portal never proxies bytes — it stores/passes location strings (mini-ADR
in spec). An upload failure fails the RUN: a dump that isn't stored is not a
success and must not register an artifact claiming otherwise. Resolve O-1 in
DECISIONS.md (annotate the Open item, never delete).
**AC:**
- [x] Live: portal dump → object in minio; registry location = object URL;
      recorded checksum matches the object's actual hash (mc-side check).
- [x] Injected upload failure → run failed, zero artifact row, notify mail.
- [x] `npm run check` green engine-free; O-1 annotated resolved.
**Verify:** live drill + mc stat/hash output in journal; `npm run check` green.
**DONE s20 (2026-07-12, fe70fbc):** all 3 AC + Verify met. Live drill (isolated
portal :8099 + portal_drill035, semaphore engine, dump:3): dump on pgtarget →
success, registry `location=s3://dbportal-artifacts/appdb-…​.dump`, mc-side
`mc cat|sha256sum` == recorded sha256 (d1ad0cfe…​), staging file cleaned; minio
stopped → dump failed at the `mc cp` upload step (pg_dump ok), ZERO artifact,
mail "RUN-2 failed — dump on pgtarget (dev)"; all 4 secrets (PG/token/webhook/
minio) absent from DB dump + portal log + both task outputs. Recovered from an
interrupted twin (see JOURNAL s20).
**Context brief:** O-1 (DECISIONS §Open); infra/compose.yaml;
playbooks/dump.yml; internal/engine/semaphore.go (result parsing);
migration 0009 (location column: dormant → live).

### WU-036 · Restore playbook (real) + M3 rehearsal — M · `done` *(s21 slice a + s22 slice b)*
Close the loop the product exists for: a real restore of a real dump on the
compose target through the full portal chain — plus the scripted rehearsal that
IS the M3 exit evidence. `playbooks/restore.yml`: fetch artifact from location
(minio), sha256 verify BEFORE touching the target (mismatch = fail, ZERO target
writes), pg_restore with a vetted flag set (--clean --if-exists vs drop/create:
O-4-style mini-ADR), machine-readable result line; catalog restore template
pinned to it. `docs/demo-m3.md` (human twin, demo-m1.md pattern): seed → portal
dump → destroy a table → portal restore (verify → safety dump → restore chain)
→ data verified back + safety artifact registered; halt+resume beat with
injected failure; mock-vs-semaphore same-code beat. Patroni-aware sequencing
stays OUT (ARCHITECTURE §7; iceboxed).
**AC:**
- [x] Live rehearsal passes start-to-finish on the VM release binary, human
      pace ≤ 15 min; every M3 exit criterion checked off inside the doc.
      *(s22: docs/demo-m3.md, 8 beats budgeted 13.5 min; run live on the release
      binary via an isolated portal :8099 — dump pgtarget → `DROP TABLE widget`
      → POST /api/restore → chain 1 verify(2)/safety_dump(3)/restore(4) all
      success → widget back with its 4 rows + original timestamps, ledger_totals
      200/30150.00.)*
- [x] Checksum tamper (corrupt the object) → chain halts at verify, target
      untouched, notify mail; fix + resume → success.
      *(s22: chain 2 halted at verify(run 6) with steps 2+3 `run_id: null` — the
      safety dump and restore were never created; widget still absent; mailpit
      6→7 = exactly ONE mail "[db-portal] CHAIN-2 halted — restore on pgtarget
      (dev)". Good bytes re-uploaded → resume → verify(7)/safety_dump(8)/
      restore(9) success, widget back.)*
- [x] The rehearsal's safety-dump artifact is itself restorable
      (`pg_restore --list`).
      *(s22: artifact 2 fetched from minio hashes to 49506fd5… == its registry
      checksum; `pg_restore --list` shows ledger + sequence + ledger_totals view
      + TABLE DATA + pkey + index, and correctly no widget.)*
**Verify:** rehearsal transcript in journal; `npm run check` green.
*(s22: both — JOURNAL s22 carries the transcript; gate CHECK-EXIT:0.)*
**Also delivered (s22):** SPEC-036's open question RESOLVED — Semaphore DOES
forward the task `environment` as ansible `--extra-vars` (mini-ADR 1 holds; the
`lookup('env',…)` fallback dropped); O-4 annotated RESOLVED in DECISIONS.md
(dump half WU-034 + restore half WU-036).
**Context brief:** ARCHITECTURE §3 (restore) + §7 (do-not-discover-twice);
SPEC-031 (chain assembly + verify-step semantics); playbooks/dump.yml;
docs/demo-m1.md (rehearsal doc pattern); infra/compose.yaml.

**M3 exit (ROADMAP):** demo-m3.md rehearsal passes live; a 3-step chain halts on
injected failure, notifies, resumes; the SAME portal code runs Mock and
Semaphore behind the adapter. Then: multi-agent review gate (the M1/M2
pattern) + fix WUs before any Phase-4 WU. Organizational note when reached:
security vetting package (arch doc §8.2) becomes submittable.

### M3-gate review fixes (2026-07-16, s23) — land BEFORE any Phase-4 WU

> Multi-agent gate review DONE 2026-07-16 (workflow wf_49ca1969-37a, 5 Sonnet
> reviewers, architect-verified inline — recovered + checkpointed s24 after an ssh
> reset killed s23 pre-checkpoint): 8 findings, 8 confirmed, 0 refuted, 4 re-graded
> down, **NO criticals — GATE PASSES with fix WUs**. Full scenarios + fix sketches:
> `docs/agent/reviews/m3-gate.md`. Both HIGHs are the same root cause — a mock-to-real
> transient-error assumption that was true under MockEngine and silently stopped being
> true behind the real Semaphore seam; neither shows on the demo-m3 happy/deliberate
> paths, both need a *transient* fault (dropped conn, proxy 5xx, DB failover). Order
> 037 (both HIGHs) → 038 → 039 → 040. Finding 8 (staging-cleanup orphan) routed to M4.

### WU-037 · M3-gate fix: transient-error resilience (Status + chain driver) — M · `done (2026-07-16, s25)`
**Evidence:** `npm run check` CHECK-EXIT:0 (golangci 0 issues, format clean, go test
-race all pkgs incl. golden flow TestGoldenFlow PASS not-skipped, vitest 116/116).
Five new -race tests drive the real watch/drive goroutines with injected transient
errors: (runs) `TestWatchRetriesTransientStatusError` — a non-ErrUnknownJob Status
error retries then recovers to success (not a false FAILED); `TestWatchUnknownJobFinalizesFailed`
— ErrUnknownJob still finalizes failed, one poll, honest "engine lost the job";
`TestWatchGivesUpAfterSustainedOutage` — a sustained outage finalizes failed HONESTLY
("engine status unavailable after N attempts", NEVER "engine lost the job"), exactly at
the ceiling. (chain) `TestDriveRetriesTransientRunRead` — a transient step-run read
self-heals to success rather than wedging 'running'; `TestDriveHaltsOnSustainedReadOutage`
— a sustained read outage halts the chain honestly (one mail) and is resumable (leg 2/3
of finding 2 closed). Fix: `watch()` treats only ErrUnknownJob as fatal, every other
error transient (bounded retry + backoff via new `MaxStatusErrors`/`backoff`); `chain.drive`
retries all three DB reads (load/next/step-watch) up to `MaxReadErrors`, then halts+notifies
instead of silently exiting — so a wedged 'running' can't survive a blip, and Resume works.
NO periodic sweep added (chose bounded-retry-then-halt: self-halts promptly, symmetric with
the watcher, no ticker/lifecycle surface; boot sweep still covers process death). No UI/migration.

The two HIGHs (gate items 1+2), one root cause, fixed together.
(1) `runs.Service.watch` (service.go:267-272) finalizes a run **permanently FAILED**
on ANY `adapter.Status` error, not just `ErrUnknownJob` — under MockEngine "any error"
== "job lost", but `SemaphoreAdapter.Status` passes connection-refused / 5xx / timeout /
decode errors through verbatim (only 404/400 → ErrUnknownJob via `asUnknownJob`). The
result: a false, uncorrectable FAILED with a lying "engine lost the job" message on a
mainline flow, and — because a halted chain offers Resume — a path to re-fire `pg_restore`
while the first is genuinely still running. Fix: only `ErrUnknownJob` → finalize-failed;
other errors are transient → log + backoff + retry (the state-mirror UPDATE 15 lines below
already treats a DB error as retryable — mirror that), with a bounded ceiling before giving
up honestly. (2) `chain.drive` (driver.go:50-54, 108-112) exits the goroutine on ANY read
error and leaves the chain `state='running'` forever: `SweepOrphans` is boot-only (no
ticker; main.go:128), `Resume` guards `state='halted'` so a wedged running chain → 409
forever, and no mail fires. Fix: distinguish transient read errors (retry w/ backoff) from
fatal, AND/OR add a periodic orphan-chain sweep (not boot-only) so a wedged chain self-heals;
consider a force-halt path. Update driver.go's rationale comment (:31-33) accordingly.
**Verify:** a stubbed adapter returning a non-ErrUnknownJob error on Status does NOT
permanently fail the run (retries, then recovers when Status succeeds); ErrUnknownJob still
finalizes failed; a chain driver hitting a transient read error self-heals (or is swept)
rather than wedging `running`; a genuinely lost job still surfaces; `npm run check` green.
**Context brief:** docs/agent/reviews/m3-gate.md items 1+2; internal/runs/service.go
(watch/finalize + the mirror UPDATE asymmetry); internal/engine/semaphore.go (Status,
asUnknownJob, the four non-404 error surfaces); internal/chain/driver.go + chain.go
(Resume's halted guard, SweepOrphans); cmd/portal/main.go (sweep call site — no ticker).

### WU-038 · M3-gate fix: schedule.Create launchable gate — S · `done (2026-07-16, s26)`
**Evidence:** one-line fix — `schedule.Create` now mirrors runs.Start's gate
(`!ok || !op.Launchable` → `runs.ErrUnknownOperation`, the handler already maps to
400 "unknown operation"), so a `POST /api/schedules` for restore/verify/safety_dump is
refused at the door instead of writing a permanently-broken schedule. `TestCreateValidation`
extended: all three existing-but-non-launchable ids → ErrUnknownOperation (dump still 201,
"explode" still unknown, and the existing `count==0` assertion proves no failed create
leaves a row). `npm run check` CHECK-EXIT:0 (golangci 0, schedule pkg ran FRESH under
`-race` 10.3s, golden flow TestGoldenFlow PASS not-skipped, vitest 116/116). No migration,
no UI, no seam change; Go-test-shaped, no live stack needed.
Gate item 3 (MEDIUM). `schedule.Create` (schedule.go:123) tests catalog **existence
only** (`catalog.ByID`, which deliberately finds non-launchable ops), so a
`POST /api/schedules {operation:"restore"}` (or verify/safety_dump) returns **201** where
SPEC-031 behavior 5 (restore.md:172-175, "likewise POST /api/schedules") promises **400**.
The guardrail itself HOLDS (executor.fire never sets Internal, so runs.Start rejects the
fire) — but the schedule is **permanently, silently broken**: every tick hits fire()'s
`default:` branch → `last_fire_status='error'`, no run, no chain, **no mail**, `next_fire_at`
advancing forever. Fix: `schedule.Create` rejects a non-launchable operation up front (mirror
runs.Start's `!op.Launchable && !req.Internal` gate) → 400 unknown/again-non-launchable op.
**Verify:** `POST /api/schedules` with an EXISTING non-launchable id (restore/verify/
safety_dump) → 400, no row written; a launchable op (dump) still 201; extend
schedule_test.go's `TestCreateValidation` to cover the existing-but-non-launchable ids
(today it only exercises the absent id "explode"); `npm run check` green.
**Context brief:** docs/agent/reviews/m3-gate.md item 3; internal/schedule/schedule.go:123
+ executor.go:148/166-168; internal/catalog/catalog.go (ByID vs All / Launchable);
internal/runs/service.go:126 (the canonical gate); docs/specs/restore.md:172-175.

### WU-039 · M3-gate fix: restore.yml re-fetch footgun — S · `done (2026-07-16, s26)`
**Evidence:** playbook-only fix — `restore.yml`'s fetch dropped its `creates: {{ staging_path }}`
guard and gained a `clear any stale staging file` task (`file: state: absent`) BEFORE the
fetch, so a partial leftover on the deterministic shared `/artifacts` path can never
masquerade as the fetched artifact. `dump.yml`/`verify.yml` confirmed footgun-free (dump's
staging name embeds `now()`+`random` = unique per run, no `creates:`; verify streams via
`mc cat | sha256sum`, no staging file). All 3 playbooks `ansible-playbook --syntax-check`
EXIT:0 in `dbportal-semaphore:v2.17.39-pg16`. LIVE DRILL (isolated portal :8099 +
portal_drill039, semaphore engine; demo :8080 untouched): dump pgtarget → artifact 1 (sha
d71cc20e…) → DROP widget → **planted a 2000-byte partial at `/artifacts/restore-<name>`
(sha e6f64801… ≠ expected)** → POST /api/restore → **chain SUCCESS**, restore task
`ok=9 changed=4 failed=0 skipped=0` with `clear…→changed`, `fetch…→changed` (NOT skipped),
`assert checksum→ok`, widget back 4 rows — under the old `creates:` guard this partial would
have been hashed and reported "tampered". Regression: genuinely corrupt object → chain HALTED
at verify (run 5 failed), safety_dump+restore `run_id:null` never created, widget ABSENT
(target untouched), exactly ONE mail; fix bytes + resume → SUCCESS (verify 6/safety_dump 7/
restore 8, failed run 5 superseded). `npm run check` CHECK-EXIT:0 (regression-green, no Go/FE
change). Drill torn down, scratch DB dropped, demo :8080 healthz 200.
Gate item 4 (MEDIUM). `restore.yml`'s fetch uses `creates: {{ staging_path }}`
(restore.yml:65) on a **deterministic path on the persistent shared `/artifacts` volume**.
An interrupted fetch (task timeout, runner restart, killed container) leaves a partial file;
the next attempt sees the path exists, **skips the fetch**, hashes the partial file, and the
sha256 compare fails → the operator is told **their good backup is "tampered"** — worst on
the product's own advertised Resume-after-halt recovery path (teaching a DBA to distrust a
valid backup mid-incident). Fix: don't gate the fetch on `creates:` for a deterministic
shared path — remove any leftover first / fetch to a unique or per-run temp path / always
re-fetch, so a partial leftover can never masquerade as a checksum mismatch. Apply the same
scrutiny to dump.yml's staging if it shares the pattern.
**Verify:** simulate a leftover partial `restore-<name>` file on the staging volume, run the
restore path → it re-fetches the full object and verifies clean (NOT a false "tampered"
diagnosis); a genuinely corrupt object still halts at verify; `ansible-playbook
--syntax-check` clean; a live restore drill (demo-m3.md recipe) still passes end-to-end.
**Context brief:** docs/agent/reviews/m3-gate.md item 4; playbooks/restore.yml (fetch task
+ staging_path + the mismatch message); playbooks/dump.yml (staging); STATE note "the
artifacts volume is STAGING ONLY now"; docs/demo-m3.md (drill recipe).

### WU-040 · M3-gate fix: LOW bundle (parse/backfill/job_id) — S · `done (2026-07-16, s26)`
The three re-graded-down LOWs (gate items 5, 6, 7) — real, cheap, each needs a dev-only
trigger or has no consumer yet (M1 precedent: the low bundle rode one S WU). (5)
`parseResultLine` (semaphore.go:209-212) validates `Name` but not `SHA256`/`SizeBytes`, so a
result line with a name but no sha256 registers an **empty-checksum** artifact (harmless —
verify.yml fail-closes on it — but a dead un-restorable registry row); reject empty sha256
(and non-positive size) → nil, run still succeeds. (6) migration 0009's backfill
(0009_artifact_registry.sql:26-34) omits `retention_class`, so a `goose down`→`up` walk on a
DB holding safety artifacts silently reclassifies every `'safety'` row as `'standard'`; derive
the class in the backfill by joining `run.operation` (`'safety'` when `operation='safety_dump'`).
(7) `job_id` is a bare `text` (0003_runs_audit.sql:14) with no uniqueness — add a partial
`UNIQUE (job_id) WHERE job_id IS NOT NULL` (PG allows multiple NULLs, so queued runs are
unaffected) to harden ReconcileByJobID against a reused id after an engine BoltDB wipe.
**Verify:** a result line with name but empty sha256 registers NO artifact (run still
success); an up→down→up migration walk preserves `'safety'` classification; the job_id
constraint rejects a duplicate non-null id and permits multiple NULLs; new migration walks
clean both directions; `npm run check` green.
**Context brief:** docs/agent/reviews/m3-gate.md items 5, 6, 7; internal/engine/semaphore.go:
209-212; internal/db/migrations/0009_artifact_registry.sql + 0003_runs_audit.sql;
playbooks/verify.yml:37 (the fail-closed checksum-length assert that makes item 5 a LOW).

## Phase 4 — Hardening (M4)

> Groomed 2026-07-16 (s26, M3 close). **Execution order: 041 → 042 → 043 → 044 → 045 →
> 046 → 047.** Rationale: the estate seed (041) is a prerequisite for both the load test
> and a realistic cold-start/pilot, so it lands first; concurrency locks (042) are the
> headline correctness hardening and the load test (043) exists to PROVE the fix scales
> (the gap is already known analytically — the scheduler's overlap probe is instance-only
> and the button/chain paths are unguarded — so we build the lock, then load-test to
> validate, not the other way round). Retention/GC (044) is independent and can slot
> anywhere after 041; it's placed here so the load test's artifact churn gives it real
> data to reap. Reconciliation + packaging (045/046) describe and ship the FINISHED
> system, so they come after the hardening lands; the retrospective (047) needs the whole
> experiment done and is strictly last. **M4 exit gate:** after 047, run a milestone gate
> review (M3 precedent: a 5-reviewer pass with inline architect verification — author an
> `m4-gate-review` skill mirroring m3) before declaring M4 done; the two ROADMAP exit
> deliverables are the **pilot-deployable build (046)** and the **experiment retrospective
> (047)**. Specs stay just-in-time (written at WU start, not now). MockEngine stays the
> default for dev + ALL tests (ADR-002) — the load test drives mock, not Semaphore.
>
> **Organizational track (not a WU):** the security-vetting package (ARCHITECTURE §8.2)
> becomes submittable at M3 exit — surface to the user; it runs in parallel and gates the
> real-estate rollout, not the M4 code.

### WU-041 · Staging seed — realistic estate fixture — S · `done (2026-07-17, s27; SPEC-041 = docs/specs/staging-seed.md)`
The prerequisite tooling for the load test (043), cold-start (045), and a realistic pilot
(046): a deterministic generator that populates the ~500-instance estate the product
targets (CLAUDE.md), across all envs and multiple clusters, so guardrail/window/lock
behavior is exercised at scale. Rides the existing WU-010 inventory path (`portal import`)
or adds a thin `portal seed --instances N [--seed S]` subcommand that emits/loads a
synthetic CSV; idempotent (re-run leaves no duplicates — natural key is instance name);
env mix realistic (a prod slice so prod-ritual paths get load coverage, the rest non-prod).
No new schema — inventory tables already exist (WU-010). Keep the tiny 8-row test fixtures
(`instances.csv`/`dev-targets.csv`) untouched; the big estate is a separate, opt-in fixture.
**AC:**
- [ ] Seeding N≈500 instances yields rows across every env value + ≥5 clusters; the prod
      slice is non-empty (prod-ritual coverage) and the non-prod slice dominates.
- [ ] Deterministic for a given seed; idempotent (a second run adds zero rows, errors none).
- [ ] `portal seed`/`import` exits 0; a count query confirms the distribution; does NOT
      disturb the existing small test fixtures or the golden flow.
**Verify:** run the seed against a scratch DB, assert counts by env/cluster; `npm run
check` green (no regression); paste the distribution into the journal.
**Context brief:** WU-010 (internal/inventory, `portal import` idempotency), infra/fixtures/
instances.csv + dev-targets.csv, backend/cmd/portal (subcommand wiring), migrations 0002.

### WU-042 · Concurrency locks — instance TTL locks + self-target ban — M · `done (2026-07-17, s28, commit 2bb4b52; SPEC-042 = docs/specs/concurrency-locks.md)`
The core correctness hardening. Today only the scheduler refuses a fire when a run is live
on the instance (executor.go:118, instance-scoped, skip-visibly) — the button and chain
paths can still launch a second operation on the SAME instance concurrently, and two
concurrent restores (or a dump racing a restore) on one target is a real hazard on a
500-instance estate with multiple DBAs. Generalize the overlap notion into a real lock
enforced at `runs.Service.Start` across ALL launch paths (button, chain step, schedule),
with a TTL so a crashed/leaked holder self-heals (symmetric with SweepOrphans, not a wedge).
Fold two research gotchas: **#2 portal self-target ban** — refuse any op whose target is the
portal's own DB (enforce in the inventory/runs layer); **#1 Patroni-awareness** — at minimum
document + block a naive dump/restore against a replica (full leader/replica sequencing is
post-MVP, route via DECISIONS.md). Guardrails/audit/ritual paths UNCHANGED — the lock is a
new gate in front of them, not a rewrite. Decide advisory-lock vs lock-table in the SPEC
(pg advisory locks are cheap but process-scoped; a lock row survives restarts + carries the
TTL + is auditable — likely the lock table).
**AC:** (all met — s28; live HTTP drill on isolated portal :8098/portal_lock_drill, torn down)
- [x] Two concurrent Start on one instance → exactly one proceeds; the other gets a clear
      409/conflict on the button AND chain AND schedule paths (the scheduler's existing
      skip-visibly semantics preserved or subsumed). (button 409 + chain halt + scheduler
      skipped_overlap; live drill: two concurrent dumps → 1×201 + 1×409, only one run row.)
- [x] The lock releases on terminal finalize; a holder that died is reaped after TTL so the
      instance is never permanently wedged (test the reap). (release rides finalize's tx;
      TestInstanceLockReapsExpiredDeadHolder + never-steal-from-live-holder.)
- [x] A portal-self-target op is refused with a distinct error; audit records the denial.
      (ErrSelfTarget 403 + guardrail.denied on auth_event; live 403 on protected crm-test.)
- [x] Guardrails, ritual, audit attribution, and the golden flow are unchanged/green.
      (+ Patroni-restore block ErrPatroniRestore 403; VM restore still 201.)
**Verify:** -race contention tests (N goroutines Start same instance → 1 success + N-1
conflict; TTL reap; self-target refusal); golden flow green; `npm run check` green. — DONE.
**Context brief:** schedule/executor.go (overlap probe :80-132), runs/service.go (Start,
finalize, SweepOrphans), migrations head, research gotchas #1/#2 (STATE "Standing context"
+ icebox), ARCHITECTURE §concurrency; architect-implemented (concurrency-sensitive).
**How built:** migration 0012 `instance_lock` (PK instance_id) — the ARCHITECTURE
§concurrency TTL lock; acquire in Start's run-insert tx (conflict → tx rollback, clean 409,
no run), release in finalize's tx (atomic w/ terminal state → every path frees it, boot
sweep reclaims a crash). Steal predicate never takes a still-live holder (safe under any
TTL). `runs.acquireInstanceLock`/`lock.go`; `PORTAL_LOCK_TTL` (30m). Self-target =
declared `PORTAL_PROTECTED_INSTANCES` seeded w/ DBName (ErrSelfTarget); Patroni-block at
chain.Create for a restore step on k8s_patroni (ErrPatroniRestore); both audited
`guardrail.denied` on auth_event (0012 extends the CHECK). ADR-012 records the deferral of
full Patroni sequencing. Scheduler keeps its probe + maps ErrInstanceLocked → skipped.

### WU-043 · Load test — 25–50 concurrent mock dumps — M · `done (2026-07-17, s29; SPEC-043 = docs/specs/load-test.md)`
The ROADMAP M4 load-test exit item, and the proof WU-042's locks scale. A harness that
drives 25–50 concurrent dumps through MockEngine against the seeded estate (041) and
validates: exactly-once finalize per run (WU-016 holds under contention), per-instance lock
serialization correct (042), zero orphaned `state='running'` rows after settle, the runs
watcher + notify + DB pool don't melt, and the scheduler boot-stampede (coalesced catch-ups
after downtime — icebox) behaves. Findings the harness surfaces (pool sizing, stampede
spreading) get FILED and fixed IF they threaten the pilot; the WU's own deliverable is the
harness + a clean run + numbers, not a fix for every finding. MockEngine only (ADR-002) —
`MockConfig.StepDelay` throttles to force overlap.
**AC:** (all met — s29; skip-gated Go harness + a live 500-instance/50-concurrent drill)
- [x] 25–50 concurrent mock dumps across the seeded estate complete with exactly-once
      finalize each and consistent audit rows; no lost or double finalize. (B1: 50
      distinct-instance dumps → 50 success, each exactly one `run.submitted` + one
      `run.finished`; B3 mixed batch idem.)
- [x] Per-instance operations serialize under the 042 lock (concurrent same-instance →
      queued/conflict, never two live); cross-instance runs proceed in parallel. (B2: 40 at
      one instance → 1 win + 39 `ErrInstanceLocked`, 1 run row; B1 peak concurrency 50/50 —
      full cross-instance parallelism from committed timestamps.)
- [x] Zero orphaned `running` rows once the harness settles; documented throughput + a DB
      pool-size recommendation; any load-only finding filed (stampede, pool) with a verdict.
      (0 orphans + 0 leaked locks every scenario; drill ≈386–406 runs/s; F1 pool + F2
      stampede filed with verdicts in SPEC-043 + icebox.)
**Verify:** the harness run pasted into the journal (counts, timing, zero-orphan assertion);
`npm run check` green (the harness is skip-gated like the itest if it needs the dev stack). — DONE.
**Context brief:** WU-016 single-finalizer + runs watcher, config (pool size), WU-042 lock,
engine MockConfig (StepDelay), schedule tick loop (stampede), 041 seed.
**How built:** a skip-gated Go harness driving the REAL `runs.Service` over MockEngine on a
scratch DB seeded with `inventory.GenerateEstate` (WU-041), so it joins `go test -race ./...`
(runs on the VM, skips in CI) — no new binary/lifecycle (mini-ADR 1). `runs/load_test.go`:
B1 (50 distinct instances → all succeed, exactly-once, peak-concurrency proof from committed
started/finished timestamps, not a racy live sample), B2 (40 at one instance → 1+39 conflict,
lock frees on finalize), B3 (20×3 mixed → per-instance 1 win + K-1 conflict, global
exactly-once + zero-orphan). `schedule/stampede_test.go` B4: 25 due schedules, ONE sequential
`fireDue`, all fire, zero orphans. `PORTAL_LOADTEST_INSTANCES` env grows the estate for a
pilot-scale drill (500) with the same committed code. F1 (default pgxpool `max(4,NumCPU)`
adequate — conns held only per-tx/per-poll, 50 dumps overlapped fully without exhaustion; an
explicit `PORTAL_DB_MAX_CONNS` floor is a WU-046 nice-to-have) and F2 (the sequential
`fireDue` self-rate-limits — no stampede spreading needed for the pilot) both filed with
verdicts. No migration, no API/UI, no seam change; MockEngine + service code UNCHANGED (the
WU is the harness, not a fix).

### WU-044 · Maintenance & retention jobs — M · `todo`
The periodic-sweep subsystem a long-running deployment needs, on the existing scheduler/tick
lifecycle. Four cohesive sweeps: **(a) audit retention (1y)** — expire/archive `audit_event`
per a documented policy (default likely archive-not-hard-delete given append-only intent —
decide in SPEC + DECISIONS); **(b) artifact retention enforcement** — expire `standard`
artifacts past policy from BOTH the registry and the object store (engine-side delete),
PRESERVING `safety` (trustworthy after WU-040 item 6) — the icebox "retention ENFORCEMENT
job"; **(c) session GC sweep** — reap expired `session` rows (TTL is read-enforced only
today — icebox); **(d) M3-gate finding 8** — wrap dump.yml's post-`pg_dump` tasks in a
`block:` with `always: file state=absent` so an upload/stat failure never orphans real dump
bytes on `/artifacts` (playbook-only, cheap bundled slice). May checkpoint mid-WU between
the Go sweeps and the playbook fix.
**AC:**
- [ ] Audit rows past retention handled per the documented policy; the append-only trigger
      (0003) and audit integrity are respected (no silent mutation).
- [ ] `standard` artifacts past policy removed from the registry AND the object store;
      `safety` artifacts NEVER reaped; the deletion is audited.
- [ ] Expired sessions reaped on a schedule; a live session is never reaped.
- [ ] dump.yml orphans no bytes on a post-dump failure (block/always) — re-drilled or
      syntax-checked; each sweep has a -race test; golden flow green.
**Verify:** -race tests per sweep (retention boundaries, safety-preservation, session TTL);
dump.yml `--syntax-check` EXIT:0 (+ optional live orphan drill); `npm run check` green.
**Context brief:** SPEC-014 (notify/retention), WU-030 artifact registry + object-store
delete (`mc rm` engine-side, ADR-004), SPEC-020 sessions, schedule tick loop, playbooks/
dump.yml, icebox retention/GC items, docs/agent/reviews/m3-gate.md finding 8.

### WU-045 · Docs-vs-reality reconciliation + cold-start + CI hardening — M · `todo`
The ROADMAP M4 "docs-vs-reality reconciliation" exit item plus the accumulated CI/supply-
chain debt. **Cold-start:** from a clean clone on a fresh host/container, install → migrate
→ `npm run check` → `build:release` → run, pasted as evidence (the M0/M1 cold-start
precedent). **Reconcile:** walk every SPEC/DECISIONS/ARCHITECTURE/STATE claim against the
code and resolve drifts; refresh the stale demo-m1.md header ("live-verified 2026-07-08" —
carried housekeeping); reconcile WU-004 token hex vs `docs/specs/design-brief.md`; fix
`config.LocateDotenv`'s upward `.env` walk to stop at a repo marker + log the resolved file
(M1-gate item 16). **CI:** add a Postgres service to check.yml so DB tests + the golden flow
stop skipping (ADR-011 gap); pin the golangci-lint installer to v2.12.2 instead of `curl|sh`
from HEAD (M1-gate item 14 — supply-chain); bump the GH Actions versions + fix the setup-go
cache path. May checkpoint between the reconciliation report and the CI changes.
**AC:**
- [ ] Cold-start runbook passes on a fresh environment with pasted evidence (each step exit 0).
- [ ] A reconciliation report enumerates doc/code drifts and each is resolved or filed;
      demo-m1.md header refreshed; the `.env`-walk footgun closed.
- [ ] CI runs DB-backed tests + the golden flow (no longer skipped); golangci pinned to
      2.12.2; actions bumped; a CI run is green.
**Verify:** the cold-start transcript + a green CI run link in the journal; `npm run check`
green locally and in CI.
**Context brief:** docs/agent/SESSION-PROTOCOL.md (cold-start), .github/workflows/check.yml,
internal/config (LocateDotenv), docs/specs/design-brief.md + frontend/src/index.css tokens,
icebox CI items, M1-gate items 14/16.

### WU-046 · Packaging for a pilot deployment — M · `todo`
The ROADMAP M4 exit deliverable: turn the `build:release` binary (ADR-010) into something a
pilot operator can deploy on a fresh host and trust. A real (non-transient) **systemd unit**
generalizing the demo unit (survives reboot; env from a file, not inline); a **config/.env
template** documenting every required var (shape only, NO secrets — mirrors `.env.example`
discipline); a **deploy runbook** (fresh host → migrate → running portal serving SPA+API
with auth ON + guardrails ON + notify wired); **production-readiness hardening**: the
break-glass mail alarm (icebox — break-glass currently alarms audit + log only, not mail)
and explicit CookieSecure/TLS-in-front guidance (the SPEC-020 boot Warn made honest for
pilot). A versioned release artifact. Exit: pilot-deployable build.
**AC:**
- [ ] Following the runbook on a fresh host yields a working portal (auth on, prod ritual on,
      failure mail wired) from the release artifact; the systemd unit survives a reboot.
- [ ] The `.env` template lists every required var by shape with no secret values; a missing
      required var fails closed with a clear message.
- [ ] Break-glass use sends a mail alarm (not just the audit action + log line); CookieSecure/
      TLS guidance documented and the boot Warn behaves for the pilot config.
**Verify:** the deploy runbook executed on a fresh host/container, pasted; reboot-survival
shown; a break-glass login produces a mailpit alarm; `npm run check` green.
**Context brief:** ADR-010 (build:release), infra/bootstrap-vm.sh + the demo systemd recipe
(JOURNAL s16), internal/config (required-var handling), SPEC-020 (break-glass, CookieSecure
Warn) + notify, ARCHITECTURE §deployment, `.env.example`.

### WU-047 · Experiment retrospective (STRATEGY §8 metrics) — S · `todo`
The second ROADMAP M4 exit deliverable, and the true last WU. Docs-only: write the
AI-agent-driven-development retrospective against STRATEGY.md §8's metrics — what the harness
rules (repo-is-memory, one-WU-per-session, verify-don't-claim, the gate) actually bought;
the session-loss failure modes that recurred (ssh reset killing pre-checkpoint work, the
twin-session hazard) and the mitigations that worked (tmux persistence, ps/tty twin checks,
recover-don't-redo); the architect/implementer delegation outcomes + cost; and what to
change for the next experiment. Ground every claim in JOURNAL evidence and the memory files.
**AC:**
- [ ] The retrospective is written (STRATEGY.md §8 filled or a linked `docs/agent/
      RETROSPECTIVE.md`) and covers each §8 metric with JOURNAL-cited evidence.
- [ ] The recurring failure modes + their mitigations are named; concrete "next time" changes
      listed.
**Verify:** the doc exists and each §8 metric is addressed; links resolve; `npm run check`
green (docs-only).
**Context brief:** docs/agent/STRATEGY.md §8, JOURNAL.md, the memory files ([[twin-session-
hazard]], [[workflow-cost-sensitivity]], [[accidental-rejections]]), DECISIONS ADRs.

---

## Icebox (ideas & discovered debt — one line each, groom later)

- CI: add a Postgres service to check.yml so DB-backed tests + the golden-flow e2e stop skipping there (ADR-011 gap; VM gate covers them today) — **→ WU-045**
- Bump GH Actions action versions (checkout/setup-go/setup-node emit node20-deprecation warnings); same pass: fix setup-go cache miss (`cache-dependency-path: backend/go.sum`) — **→ WU-045**
- CI: pin the golangci-lint installer to the VM's v2.12.2 instead of `curl | sh` from HEAD (M1-gate item 14 — supply-chain + silent lint drift; check.yml:18) — **→ WU-045**
- config.LocateDotenv: stop the upward .env walk at a repo marker (.git/go.mod) or explicit path, and log the resolved file at startup (M1-gate item 16 — foreign-.env footgun) — **→ WU-045**
- frontend test hygiene (found by WU-019 delegation agent): pre-existing React act() warning on several RunDetail tests (likely IS_REACT_ACT_ENVIRONMENT missing in src/test/setup.ts); jsdom "navigation to another Document" noise when tests click the CSV export anchor — both cosmetic, tests green
- Reconcile WU-004 token hex values vs design brief §Design system — brief now ON the VM at `docs/specs/design-brief.md` (unblocked 2026-07-07) — **→ WU-045**

- Activity: date-range filter + pagination past 50 + server-side audit export (SPEC-014 deferred; client CSV caps at the view)
- Inventory: UI/API upload + import-history screen (MVP import is `portal import` CLI — SPEC-010)
- Inventory: Excel/.xlsx ingestion (MVP is CSV-only — SPEC-010)
- Patroni-aware dump/restore sequencing (research gotcha #1: cancel semantics too) — **partial DONE WU-042** (chain.Create blocks a restore step onto a k8s_patroni target, ADR-012; full leader/replica pause/detach→restore→reinit sequencing + per-instance role awareness stay post-MVP)
- PITR; Vacuum/Reindex buttons; approvals workflow (Screen 7); Jira linkage; SSO
- Portal self-target ban (research gotcha #2) — **DONE WU-042** (declared `PORTAL_PROTECTED_INSTANCES` set seeded w/ DBName, refused at Start + chain.Create, ErrSelfTarget 403, audited guardrail.denied; ADR-012). Auto-detection needs an inventory connection-tuple schema addition (post-MVP).
- Bulk/rolling operations (Screen 2 sticky bar); saved views
- 5-year audit shipping to object storage; SIEM export
- AuthN: session GC sweep — expired session rows accumulate forever (TTL enforced on read only); periodic delete (filed at s14 grooming) — **→ WU-044**
- AuthZ: denial-rate alarm — a spike of `authz.denied` should mail the DBA list, not sit silently in auth_event
- AuthN: break-glass use should ALSO send a mail alarm via notify (today: alarmed audit action + log line only) — **→ WU-046**
- Role admin CLI (`portal role grant|revoke|list`) — role grants happen only at boot per auth mode today
- Schedules UI: cron×window hint — flag when a schedule's upcoming fires fall outside the instance's maintenance window (needs window eval over future fire times)
- Run read model: expose `window_warned` on the run API (audit-only today) so the UI can show the flag post-hoc
- Scheduler: boot-stampede spreading — after long downtime, many coalesced catch-ups fire in one tick; spread them (M4 load-test territory) — **VALIDATED WU-043 (s29), NO FIX NEEDED for the pilot** (SPEC-043 F2 + `schedule/stampede_test.go` B4): `fireDue` already fires due schedules SEQUENTIALLY, one bounded `fire` at a time, so a stampede is self-rate-limited — not a thundering herd of parallel Starts; the WU-042 lock serializes any same-instance collision to `skipped_overlap`. Add per-tick jitter/spread only if a future estate makes even sequential catch-up too bursty. Kept here as a post-pilot nice-to-have.
- DB pool sizing: `db.NewPool` uses pgxpool defaults (`MaxConns = max(4, NumCPU)`) — **VALIDATED ADEQUATE WU-043 (s29)** (SPEC-043 F1): conns are held only per-tx/per-poll (never for a goroutine's lifetime), so 50 concurrent dumps + watchers overlapped fully (peak 50/50, ≈400 runs/s) without exhaustion or deadlock. An explicit `PORTAL_DB_MAX_CONNS` floor to decouple the pool from core count is a **→ WU-046** packaging nice-to-have, not a blocker.
- Schedule-change ledger — schedule rows are mutable with no edit history; consider audit_event actions or a ledger table
- Artifact retention ENFORCEMENT job (registry stores class only from WU-030; delete/expire is M4+ policy work) — **→ WU-044**
