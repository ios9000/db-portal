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

### WU-017 · M1-gate fix: audit hardening (TRUNCATE + job_id) — S · `pending`
Migration 0004: `BEFORE TRUNCATE … FOR EACH STATEMENT` trigger reusing
audit_event_immutable() + `REVOKE TRUNCATE`; add `job_id text NULL` to audit_event,
stamped on `run.finished` (NULL at submit is honest — the id doesn't exist yet).
Reconcile SPEC-012 mini-ADR 1's "every §5 field" claim with reality.
**Verify:** DB test: `TRUNCATE audit_event` raises "append-only" (alongside the
existing UPDATE/DELETE tests); golden flow asserts job_id on the finished event;
goose down walks one migration; `npm run check` green.
**Context brief:** docs/agent/reviews/m1-gate.md items 4-5; 0003_runs_audit.sql;
internal/runs/service.go audit INSERTs; ARCHITECTURE §5; migrate_test down-walk.

### WU-018 · M1-gate fix: inventory size_gb canonicalization — S · `pending`
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

### WU-019 · M1-gate fix: frontend resilience — S · `pending`
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

### WU-020 · AuthN: LDAP bind against AD — M · `pending`
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

### WU-021 · AuthZ: DBA role, route guards, real actor — M · `pending`
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
shape in spec); cancel writes `run.canceled` with the canceling actor; runs list
`?requested_by=` filter; prod run submitted by an authenticated DBA carries their AD
identity in both audit rows; `npm run check` green.
**Context brief:** D2/D3; SPEC-012 §audit + SPEC-015 deferrals; internal/runs/service.go
(actor constant); server router; WU-020's session context.

### WU-022 · Portal-owned scheduler (ADR-003) — M · `pending`
Schedule CRUD (table + API + the stub /schedules screen) for scheduled dumps (D4);
executor = robfig/cron/v3 (ADR-010 table) in-process, jittered start, firing through
runs.Service.Start — the SAME guardrail/audit path as run-now, actor =
`schedule:<owner>`. Misfire policy (portal down at fire time), overlap policy (previous
run still live), and enable/disable are spec decisions — mini-ADR each. Size check: if
heavy, land schedule table + executor + audit attribution first, checkpoint, UI second.
**Verify:** a schedule on a test instance fires within jitter bounds with full audit
attribution (`schedule:<owner>` in both rows — ROADMAP M2 exit); disabled schedule never
fires; portal restart neither double-fires nor silently drops a due schedule (per the
spec'd misfire policy); `npm run check` green.
**Context brief:** ADR-003; D4; internal/runs/service.go (Start seam); frontend
/schedules stub route (App.tsx); WU-021 actor conventions.

### WU-023 · Maintenance windows warn-only — S · `pending`
Give `instance.maintenance_window` (raw text since WU-010, O-3) just enough semantics
to warn: parse the fixture's `Day HH:MM-HH:MM` shape; launching OUTSIDE the window
(drawer AND scheduler path) shows a warn banner — never blocks (D6) — and stamps a
`window_warned` flag on the audit trail (schema addition, spec decides column vs
action). Unparseable/empty window = no warning, logged once. Timezone: assume portal
server TZ, mini-ADR it.
**Verify:** launch outside window → banner + `window_warned` true in audit; inside →
no flag; scheduled fire outside window carries the flag too; garbage window text never
blocks a launch; `npm run check` green.
**Context brief:** D6; O-3 (DECISIONS §Open); WU-010 schema (instance.maintenance_window);
LaunchDrawer (frontend) + runs.Service.Start (stamp point); WU-022 executor path.

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

- CI: add a Postgres service to check.yml so DB-backed tests + the golden-flow e2e stop skipping there (ADR-011 gap; VM gate covers them today)
- Bump GH Actions action versions (checkout/setup-go/setup-node emit node20-deprecation warnings); same pass: fix setup-go cache miss (`cache-dependency-path: backend/go.sum`)
- CI: pin the golangci-lint installer to the VM's v2.12.2 instead of `curl | sh` from HEAD (M1-gate item 14 — supply-chain + silent lint drift; check.yml:18)
- config.LocateDotenv: stop the upward .env walk at a repo marker (.git/go.mod) or explicit path, and log the resolved file at startup (M1-gate item 16 — foreign-.env footgun)
- Reconcile WU-004 token hex values vs design brief §Design system — brief now ON the VM at `docs/specs/design-brief.md` (unblocked 2026-07-07)

- Activity: date-range filter + pagination past 50 + server-side audit export (SPEC-014 deferred; client CSV caps at the view)
- Inventory: UI/API upload + import-history screen (MVP import is `portal import` CLI — SPEC-010)
- Inventory: Excel/.xlsx ingestion (MVP is CSV-only — SPEC-010)
- Patroni-aware dump/restore sequencing (research gotcha #1: cancel semantics too)
- PITR; Vacuum/Reindex buttons; approvals workflow (Screen 7); Jira linkage; SSO
- Portal self-target ban (research gotcha #2) — enforce in inventory layer when real targets exist
- Bulk/rolling operations (Screen 2 sticky bar); saved views
- 5-year audit shipping to object storage; SIEM export
