# Architecture (MVP)

Agent-readable distillation of the v0.1 architecture document
(`P:/Projects/db-portal-research/pg-ops-portal-architecture.html`). This file is the
technical authority (below DECISIONS.md); the HTML is the presentation form.

## 1. Shape

Custom portal (the product) over an execution engine (internal machinery, swappable).
The portal owns: identity, authorization, guardrails, inventory, catalog, scheduling,
audit, notifications, UX. The engine owns: job execution, log capture, runners.

```
DBA (browser)          Active Directory
     │  HTTPS                │ LDAP bind
     ▼                       ▼
┌────────────────────────────────────┐      ┌──────────────────┐
│ PG Ops Portal (SPA + API)          │◄────►│ Portal PostgreSQL │
│ RBAC · guardrails · scheduler ·    │      │ inventory · authz │
│ audit writer · inventory · notify  │      │ runs · audit(A-O) │
└────────────┬───────────────────────┘      └──────────────────┘
             │ ExecutionAdapter (per-env registry: prod ≠ nonprod creds)
             ▼
┌────────────────────────────────────┐      ┌──────────────────┐
│ Engine: MockEngine (dev/test) /    │◄────►│ Git playbook repo │
│ Semaphore (M3+, not user-visible)  │      │ + Ansible Vault   │
└────────────┬───────────────────────┘      └──────────────────┘
             │ SSH / kubectl / psql
             ▼
   Targets: K8s+Patroni · legacy VMs        Artifacts: S3-compatible (O-1)
```

## 2. Components (build-relevant facts)

**Portal app** — SPA + REST API; the only user-facing surface. Engine UIs are
network-restricted. Server pushes run logs (SSE/WebSocket, WU-013 decides).
Backend is a **single Go binary** (ADR-010): production embeds the built SPA via
`go:embed` (WU-006) so one static artifact serves UI + API; dev runs Vite separately
with an `/api` proxy.

**ExecutionAdapter** (ADR-002) — `start_job(template, params) → job_id`,
`get_status(job_id)`, `stream_logs(job_id)`, `cancel(job_id)`. Registry resolves engine
config by environment class; prod and nonprod NEVER share credentials (guardrail layer 3).
Status arrives via webhook with polling fallback (real engine); mock pushes directly.

**Portal DB (PostgreSQL 16)** — inventory (`cluster`, `instance`: env enum, platform,
patroni ref, owner, maintenance window), authz (roles/grants — the DBA team's "local
database", but passwords only for break-glass), runs, schedules, artifacts registry,
audit (append-only: app role has no UPDATE/DELETE).

**Identity** — AuthN: AD LDAP bind (dev: bypass flag + fake directory); portal never
stores AD passwords. One alarmed break-glass local account. AuthZ: portal DB. SSO: post-MVP.

**Inventory** — Excel/CSV imported with per-row validation + quarantine; portal DB is
the working copy from day one. Every run/audit row FKs an inventory row. CMDB later = export.

**Scheduler** (ADR-003) — portal-owned, jittered, same guardrail/audit path as run-now.

**Notifications** — email only (dev: mailpit). Failure/halt/window-warn to DBA list.
Content: who/what/where/status + run link. Never params or log excerpts.

## 3. Key workflows

**Run-now dump (hero):** select instance → env context + window check (warn-only) →
audit `submitted` → adapter `start_job` (pinned playbook tag as param) → live logs →
artifact registered (checksum, retention class) → audit finalized → email on failure.
Patroni estates: dump from replica by default, primary fallback (real-playbook concern, M3+).

**Restore:** registered artifact → explicit target (default non-prod; prod = full ritual)
→ checksum verify → **automatic pre-restore safety dump of the target** (institutionalizes
the motivating incident's missing step) → restore. Patroni-aware sequencing (never behind
Patroni's back: pause/detach → restore → reinit replicas) is the hard engineering item.
PITR: out of scope.

**Chains (D5):** portal-level step sequences over adapter jobs. Failure → halt, persist
chain state + step artifacts, email → DBA resumes from failed step. Mechanism ships with
the backup chain; more steps post-MVP.

## 4. Guardrails (prod/non-prod, D2)

1. Full-width env-colored banner on every contextual screen (redundant encoding, not color-only).
2. Prod actions: typed instance-name confirmation, paste disabled. Non-prod: one click.
3. Disjoint per-env engine credentials — a mis-routed job fails closed. Exists from
   MockEngine day one as separate registry configs.
4. Environment stamped in every audit row (detection, not just prevention).

## 5. Audit record

`id, ts_submitted, ts_finished, actor (AD id | schedule:<owner>), action, params_digest,
instance_id (FK), environment, playbook_tag, job_id, final_status, window_warned`.
Append-only; retention 1y (MVP) → 5y via object storage (post-MVP).

## 6. Deployment (target; dev shape mirrors it in compose)

K8s namespace: portal ×2, engine + runners, portal Postgres (team's own Patroni tooling).
Portal's own DB is NEVER a portal target (self-upgrade deadlock — research gotcha #2).
Concurrency planning: ~25–50 parallel dumps at peak (500 instances, nightly windows,
jittered). A staging portal pointed only at non-prod rehearses releases.

## 7. Known hard problems (do not discover twice)

- Cancel semantics: killing Ansible doesn't stop a running server-side operation;
  per-operation cancel policy needed (Icebox, pre-M3 grooming).
- Patroni-aware restore sequencing (M3, prototype first). *WU-042/ADR-012: a restore step
  onto a `k8s_patroni` target is refused (ErrPatroniRestore) — dumps stay allowed; the full
  pause/detach → restore → reinit sequencing remains post-MVP.*
- Locking: same op, same instance, two clickers — hierarchical TTL locks (M4). *Delivered
  WU-042/ADR-012: the `instance_lock` row (PK instance_id, TTL backstop) is acquired in
  `runs.Service.Start`'s tx on every launch path and released on finalize; a still-live
  holder is never stolen. Proven under 25–50 concurrent load in WU-043.*
- Playbook hardening (idempotency, machine-readable outcomes) is project scope, not a given.
- Timezones/DST for schedules and windows (WU-022 must decide storage in UTC + display rules).
