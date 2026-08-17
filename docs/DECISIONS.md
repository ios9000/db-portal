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

### ADR-001 · Stack: FastAPI + React/TypeScript — `superseded by ADR-010` (user veto 2026-07-06)
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

### ADR-010 · Backend: Go, shipped as a single binary — `accepted` (user directive 2026-07-06)
User vetoed Python (ADR-001) requiring Go for **single-binary deployment**: one static
artifact, no runtime on the host, `scp && run` operability — a real fit for a DBA-owned
tool. Frontend stays React/TS/Vite (unchanged); in production its `dist/` is embedded
into the Go binary via `go:embed` and served with an SPA fallback; in dev, Vite proxies
`/api` to the Go server. Stack (researched 2026-07-06; latest stable Go, pinned via
`go.mod` toolchain directive):

| concern | choice | rejected & why |
|---|---|---|
| HTTP router | chi v5 (stdlib-compatible, middleware for auth/RBAC later) | gin (non-stdlib idioms), pure stdlib (kept as fallback — chi is removable) |
| Postgres | pgx/v5 pool | database/sql+lib/pq (dated), GORM (runtime magic) |
| Queries | sqlc (compile-time typed SQL) | GORM/ent (codegen weight, ORM drift vs DBA-first SQL culture) |
| Migrations | goose v3, SQL files embedded in the binary | golang-migrate (fine, goose simpler to embed) |
| Config | env vars via caarlos0/env; godotenv in dev | viper/koanf (overkill for 12-factor env) |
| Logging | log/slog JSON (stdlib) | zap/zerolog (perf not needed; stdlib wins) |
| Testing | stdlib testing + testify; `-race` in the gate | — |
| Scheduler (WU-022) | robfig/cron/v3 | — |
| Lint/format | golangci-lint v2 (standard linters + gofumpt/goimports formatters) | separate tools (aggregator is the ecosystem norm) |

**Consequences:** ADR-007 unchanged (npm scripts stay; node exists for the frontend
anyway) but `check:be` = `golangci-lint run` + format-diff + `go test -race ./...`;
uv/Python leave the toolchain; CI gains setup-go + golangci-lint; pre-commit hook's
backend branch checks Go formatting; new WU-006 (single-binary embed build) added;
WU-003/WU-005 re-architected for Go idioms (interfaces + goroutines/channels for the
MockEngine log streaming). The Python scaffold from WU-001 is removed by WU-001R.

### ADR-011 · Golden-flow e2e lives inside `go test`, not a new gate target — `accepted` (M1 close, 2026-07-08)
The M1 golden-flow test (STRATEGY §7: dump run-now → status → audit, green every session)
is a Go test at the HTTP seam — `backend/e2e/golden_flow_test.go` — driving the production
wiring (router + runs.Service + per-class MockEngines + real SMTP mailer against an
in-test server) on a migrated scratch DB via `testutil.MigratedDB`. It therefore runs
inside `go test -race ./...`, i.e. inside the existing `npm run check` — the gate stays
ONE command, no sibling target. Like every DB test it **skips when the compose Postgres
is absent**, so today it guards the VM gate but silently skips in CI (no PG service —
icebox item filed to add one). **Rejected:** browser automation (Playwright) — a whole
toolchain for the two UI-only beats (typed-name prod ritual, EnvBanner) already pinned
by vitest component tests; shell-script e2e à la `demo-m0.sh` — not `-race`'d, string
assertions against psql output, and a second thing to keep green; separate `npm run e2e`
target — gates that aren't THE gate rot. `docs/demo-m1.md` stays the human twin.

### ADR-012 · Concurrency: per-instance TTL lock row; self-target ban; naive-Patroni-restore blocked — `accepted` (WU-042, 2026-07-17)
The M4 correctness hardening (SPEC-042 = docs/specs/concurrency-locks.md; ARCHITECTURE
§concurrency planned "hierarchical TTL locks (M4)"). Three decisions:

- **Instance lock = a `instance_lock` ROW (migration 0012), not a pg advisory lock.** At most
  one live operation per instance, enforced at the single choke point `runs.Service.Start`, so
  every launch path (button, scheduler, chain step) inherits it. A row survives restarts, names
  its holder run + actor (auditable), and carries `expires_at`; a `pg_try_advisory_lock` is
  session-scoped, TTL-less, and invisible. Acquire rides Start's existing run-insert tx (a
  conflict rolls the whole tx back — no run, clean 409); release rides finalize's own tx (atomic
  with the terminal state), so every terminal path frees the lock and a crashed holder is
  reclaimed at boot by `SweepOrphans` → finalize. The TTL is only a backstop and the acquire
  **never steals from a still-live holder** (steal predicate requires expired AND holder not in
  `queued`/`running`), so a misconfigured-short TTL can never cause two concurrent ops.
  **Rejected:** advisory locks (above); a lock held for a whole chain's duration (per-step is
  enough — steps are sequential; a chain-wide lock complicates resume/sweep). Lock renewal and
  cross-*resource* (cluster) locking are post-MVP.
- **Self-target ban = a declared protected set**, not auto-detection. The portal's own DB must
  never be a target (self-upgrade deadlock, research gotcha #2; ARCHITECTURE §concurrency). The
  inventory schema carries no connection tuple, so the portal cannot *detect* that an instance
  resolves to its own DB — `PORTAL_PROTECTED_INSTANCES` declares the names, seeded with
  `PORTAL_DB_NAME` so the portal DB is protected out of the box. Refused at Start and
  chain.Create with `ErrSelfTarget` (403); the denial is recorded `guardrail.denied` on
  auth_event (the security ledger — a refused target never becomes a run).
- **Naive-Patroni-restore blocked; full sequencing post-MVP.** A `pg_restore` into a
  `k8s_patroni` cluster behind Patroni's back diverges the cluster (ARCHITECTURE §7). The
  portal has no primary/replica visibility (inventory has only `cluster.platform`), so
  chain.Create refuses any chain with a `restore` step onto a Patroni target
  (`ErrPatroniRestore`, 403, audited). Dumps stay allowed (safe, even desirable from a replica).
  **Amended (WU-048, m4-gate finding 1):** the Create-time check alone was bypassable — a
  re-import re-platforming the target `vm → k8s_patroni` between create and resume let the
  restore step fire. `runs.Start` now re-validates the block with a fresh platform read at
  EVERY fire (the same per-fire treatment as the self-target ban and the prod ritual);
  chain.Create's check remains as the front-door 403. SPEC-042 mini-ADR 6 carries the detail.
  **Deferred to post-MVP (research gotcha #1 remainder):** the real pause/detach → restore →
  reinit-replicas leader/replica sequencing — the "hard engineering item" ARCHITECTURE §7 names;
  and per-instance role awareness (needs an inventory schema addition).

### ADR-013 · Maintenance & retention: reap sessions + standard artifacts; the audit trail is retained in-DB — `accepted` (WU-044, 2026-07-17)
The periodic-sweep subsystem a long-running deployment needs (SPEC-044 = docs/specs/
maintenance.md). A new `internal/maintenance.Service` runs on the same tick lifecycle as the
scheduler (`go maint.Run(ctx)`), sweeping once at boot then every `PORTAL_MAINTENANCE_INTERVAL`
(default 1h). Three passes, each best-effort and independent:

- **Session GC.** `DELETE FROM session WHERE expires_at < now()` — a plain bulk delete
  (session is not append-only); the predicate can never match a live session, so a live session
  is never reaped. Complements SPEC-020's lazy per-token expiry (which only reaps a token when
  it is next presented). No config; not audited (a session row is not a security event).
- **Artifact retention enforcement.** Deletes `standard` registry artifacts older than
  `PORTAL_ARTIFACT_RETENTION` (default 90d = `2160h`), **structurally preserving `safety`**
  (the WHERE matches `retention_class='standard'`, so safety is unselectable). The delete +
  an `artifact.reaped` `audit_event` (actor `maintenance`, linked to the artifact's origin run)
  ride ONE modifying-CTE statement — INSERT into the append-only audit_event is allowed, and
  readers filter on specific actions (the `run.cancel_requested` precedent), so a new action
  breaks nothing. `run.artifact_*` columns STAY (history, distinct from the restorable
  registry — SPEC-030). A non-positive age DISABLES the pass (a zero age would reap everything
  — fail-safe). **Object bytes are NOT deleted by the portal** (ADR-004: the portal holds no
  object-store credentials, never issues `mc rm`): byte-level TTL is the object store's own
  lifecycle-expiry, configured engine-side (the deploy runbook, WU-046), with `safety` in a
  lifecycle-exempt key space; the portal enforces the registry, the store enforces the bytes,
  kept consistent by matching the age. Under the pilot's MockEngine `location` is NULL (no
  object store); a reaped artifact that still carries a non-NULL location is logged at WARN so
  it is never silently orphaned.
- **Audit retention = retain in-DB; cold-storage archival is post-MVP.** `audit_event` and
  `auth_event` are append-only **by database trigger** (0003–0005) — a DELETE cannot succeed
  — so a retention job can only mean archive-then-purge, and shipping to cold storage is an
  explicit icebox item ("5-year audit shipping to object storage") beyond MVP. The policy: the
  ledgers are retained in-database for the pilot's lifetime and the maintenance loop **never
  mutates them** (respecting the trigger + audit integrity — no silent mutation); the audit
  pass is *observational*, logging the oldest event's age when it exceeds
  `PORTAL_AUDIT_RETENTION` (default 365d = `8760h`) so an operator knows when archival becomes
  necessary. `run` rows are pinned by the audit FK and equally immortal — deliberately: the
  audit trail is the long-term operational record.

**Rejected:** folding sweeps into `schedule.Service` (muddies its single responsibility);
hard-deleting audit rows (impossible without dropping the append-only trigger — that IS the
integrity property); a portal-driven per-object `mc rm` reap (would require object-store
credentials in the portal, violating ADR-004, or a bespoke engine "reap" job type coupling the
maintenance loop to the engine seam and minting spurious run/audit rows). Also in this WU
(m3-gate finding 8): dump.yml's post-`pg_dump` tasks are wrapped in a `block:`/`always:` so a
failed upload never orphans real dump bytes on the runner volume.

### ADR-014 · Post-MVP pivot: in-process Local Ansible engine replaces Semaphore; playbooks become platform content — `accepted` (user directive 2026-08-12, M5)

**Context (the product pivot).** Post-MVP, the target user broadens: from "a DBA pressing
buttons on a fixed two-operation catalog" to **Ansible playbook developers** — the portal
becomes a self-service **execution and delivery platform** for their playbooks (backups,
upgrades, maintenance). Hard product constraints set by the owner: **zero dependency on an
external execution engine** (no Semaphore); the portal must stay **self-contained, compact,
lightweight** — the ADR-010 single-binary ideal, extended to the whole runtime. VISION's own
formulation ("buttons are data, execution is Ansible behind an adapter") already points here;
what changes is who authors the buttons and where Ansible runs.

**Decision.**
1. **New `engine/local` adapter** implements the UNCHANGED `engine.Adapter` seam
   (StartJob/Status/StreamLogs/Cancel) by supervising locally-executed `ansible-playbook`
   processes via `os/exec` — streaming stdout as LogLines, mapping exit codes to job states,
   parsing the existing `DBPORTAL_RESULT` artifact line, killing the process group on
   Cancel/timeout. SPEC-050 (`docs/specs/local-engine.md`) is the module spec.
2. **The engine moves in-process**: same binary, same host, no engine API/webhook/bootstrap.
   "Engine-side" (ADR-004, ADR-013, SPEC-035) now reads "portal-host-side, service-user-side".
3. **Playbooks become first-class platform content**: a playbook library on disk with
   per-playbook manifests (id, params schema, guardrail flags — SPEC-050 mini-ADR 7)
   replaces the fixed template-id mapping; the operation catalog becomes manifest-driven,
   which is the "delivery platform" half of the pivot. Delivery v1 is git/filesystem —
   playbooks ship with the deploy package; an upload/signing API is deliberately deferred.
4. **SemaphoreAdapter is removed** (code, webhook route, bootstrap script, compose service,
   config) — but only AFTER the local engine proves parity on the WU-034/036 rehearsal
   (M5 decommission WU). Git history is the archive.
5. **MockEngine remains the default for dev and ALL tests, forever** — the ADR-002 seam +
   mock halves are REAFFIRMED; only the "Semaphore in M3" half is superseded.

**Why the ADR-002 cost verdict flips.** ADR-002 priced a custom executor at "6–12 dev-months
of commodity machinery" — against the FULL research product (distributed runners, template
store, engine RBAC, engine UI). Post-MVP the portal already owns the entire value layer:
catalog, guardrails, locks, chains, scheduler, audit, artifact registry, notifications, log
streaming UI. What Semaphore actually provides the shipped product today is process
supervision of `ansible-playbook`, a template-id lookup, and a key store. Behind the proven
seam that is weeks of Go, not months — and it deletes an entire external system (API client,
webhook + shared secret, bootstrap, compose service, poll fallback) that cost real M3
sessions to integrate and would cost every operator a second system to run, secure, and
upgrade. The research verdict was right for the product it priced; it is not the product
being built now.

**Secrets posture (ADR-004 restated, invariants intact).** The load-bearing invariant
SURVIVES UNCHANGED: the portal DB stores no target credentials, ever; logs/mails carry no
secrets or params. What changes is where engine-side credentials live: not a separate
engine's key store but the **portal host, readable only by the service user** — Ansible
Vault files (password file 0600, outside git), `~dbportal/.pgpass`, or host env; referenced
by playbooks, never stored in or passed through the portal DB/API/extra_vars. The OpenBao
commitment ("first post-MVP infra item") stands and now has a single obvious integration
point. Consequence to state honestly: portal compromise and credential compromise are no
longer separated by a network boundary — the mitigation is the service-user file boundary,
the systemd sandbox (WU-046), and the M5 gate's exec-security review dimension.

**Guardrail layer 3 (disjoint prod/nonprod) restated.** Was "disjoint engine credentials";
becomes: separate Registry entries per class remain mandatory, each `local` instance gets
its own library root and credential namespace (e.g. distinct vault password files), and a
mis-routed job still fails closed (no adapter, or a credential namespace it cannot read).
This is honestly WEAKER than two network-separated engines; the hardening path (separate
prod portal instance, or a distinct runner user per class) is an M5-gate/icebox concern.

**Rejected.** Keeping Semaphore (contradicts the zero-dependency directive; a second system
to operate; the integration surface cost real sessions and its webhook/poll duality is the
kind of complexity the pivot deletes). AWX/Tower (heavier in every axis). Embedding Ansible
in-process via CPython bindings (absurd coupling). A Go-native SSH executor without Ansible
(abandons the playbook-developer target user — the playbook IS the product's content).
`ansible-runner` as the required runtime (structured events are attractive, but it adds a
second runtime contract; raw `ansible-playbook` matches the existing LogLine seam and the
proven `DBPORTAL_RESULT` contract — runner events stay a recorded option, SPEC-050).

**Amends:** ADR-002 (Semaphore half), ADR-004 (location of engine-side secrets),
SPEC-035/O-1 (object store becomes optional; artifact-bytes destination is an M5 open
question), ARCHITECTURE §2/§6 (engine component + deployment), VISION (target user) —
doc reconciliation is an M5 WU; until it lands, THIS ADR wins where they conflict
(ADR-006 precedence).

## Open (inherited from architecture doc §10)

- **O-1** dump artifact storage (rec: S3-compatible; minio in dev) — needed by WU-012 (mock ok) / WU-035 (real).
  **RESOLVED 2026-07-12 (WU-035, SPEC-035 = docs/specs/artifact-storage.md):** S3-compatible object
  store, minio in dev (compose `minio` + `createbuckets`). The dump playbook uploads the `.dump`
  engine-side via `mc` after the checksum and records `location = s3://<bucket>/<name>`; upload
  failure fails the run (no artifact). The portal never touches object bytes — it stores/passes
  `location` strings (mini-ADR 1); object-store creds live engine-side only (ADR-004). Prod swaps
  the `mc` alias + endpoint for a real S3 target — no portal code changes.
- **O-3** maintenance-window source (rec: per-instance inventory field) — WU-010 adds the
  field; WU-023 gives it warn-only semantics (SPEC-023: `Day HH:MM-HH:MM`, server-local).
  Still open post-MVP: multi-window/holiday grammar, per-instance timezones, editing UI.
- **O-4** dump options matrix (rec: `pg_dump -Fc`, small vetted option set) — WU-034.
  **RESOLVED 2026-07-15 (dump half WU-034/SPEC-034, restore half WU-036/SPEC-036 mini-ADR 2):**
  ZERO user-facing options — both flag sets are fixed in the playbooks, not exposed in the UI or
  the API. Dump = `pg_dump --format=custom --no-owner --no-privileges` (custom format so
  `pg_restore` can read it directly and order drops reverse-dependency). Restore =
  `pg_restore --clean --if-exists --no-owner --no-privileges --single-transaction` into the LIVE
  target database: `--clean --if-exists` drops-then-recreates each dumped object in place (no DROP
  DATABASE — you cannot drop the one you are connected to, and that path needs a maintenance DB +
  terminating every other connection); `--no-owner --no-privileges` mirror the dump, since the
  runner role does not own the objects; `--single-transaction` makes the restore atomic, so a
  mid-restore failure rolls back and leaves the target exactly as it was. Rejected for MVP:
  DROP/CREATE DATABASE restore (post-MVP), `--jobs` parallel restore (incompatible with
  `--single-transaction`; an M4 load-test concern). Live-rehearsed 2026-07-15 (docs/demo-m3.md,
  JOURNAL s22): a real dropped table restored onto the compose target with its rows intact.
- **O-5** workshop "Wrap" section never received — confirm no extra decisions outstanding
