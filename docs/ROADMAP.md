# Roadmap

Software-build roadmap for the MVP. Organizational tracks from the architecture doc
(security vetting, real-estate rollout) run in parallel and are noted where they gate us.
WU detail lives in `docs/agent/BACKLOG.md`; this file is the altitude view.

## M0 — Walking skeleton (WU-000…005)

Toolchain audited; monorepo with lint/typecheck/test gates; compose dev env (Postgres,
mailpit); Go server chassis with migrations + `/healthz`; frontend shell with the visual
grammar (env badges, run-state chips); ExecutionAdapter seam + MockEngine.

**Exit:** `check` green from clean clone; a MockEngine dump job runs from a test;
committed proof script. *Everything after M0 is feature work on a stable chassis.*

## M1 — Hero flow, end to end (WU-010…015)

Inventory (CSV import, quarantine), fleet UI (cards ⇄ table), the Dump button (launch
drawer → run → live logs → artifact), append-only audit + Activity view, failure email,
prod guardrails (banner, typed confirmation, per-env engine separation).

**Exit:** `docs/demo-m1.md` runs start-to-finish in < 5 min; golden-flow e2e test enters
the permanent gate; multi-agent review gate; cold-start test passes.
*M1 is the management-demo milestone — the D7 acceptance gate in real-project terms.*

## M2 — Auth, scheduling, windows (WU-020…023)

AD/LDAP authentication (dev bypass + fake directory), portal-DB authorization (DBA role,
break-glass), portal-owned scheduler through the same guardrail/audit path, maintenance
windows warn-only.

**Exit:** no anonymous access; a scheduled dump fires with full audit attribution;
window warning visible in UI and audit flag.

## M3 — Restore, chains, real engine (WU-030…036)

Artifact registry, restore workflow (checksum verify, auto safety-dump, prod ritual),
chain engine (halt + notify + resume-from-failed-step), SemaphoreAdapter against a real
Semaphore in compose, first real playbook (pg_dump against a compose target), minio
artifact storage, real restore playbook + scripted rehearsal (WU-036, added at s14
grooming — the exit criterion had no covering WU).

**Exit:** restore rehearsal on a compose target passes; a 3-step chain halts on injected
failure, notifies, resumes; the SAME portal code runs Mock and Semaphore engines behind
the adapter. *Real-project gate: security vetting package (arch doc §8.2) submittable.*

## M4 — Hardening

Concurrency locks, load test (25–50 concurrent dumps), audit retention job, staging seed,
docs-vs-reality reconciliation, packaging for a pilot deployment.

**Exit:** pilot-deployable build; experiment retrospective written (STRATEGY.md §8 metrics).

## Sequencing rules

- Grooming is a deliverable: each phase's WUs get ACs + context briefs at the prior
  milestone close, not sooner (specs rot) and not later (blocks the next session).
- Milestone gates are hard: no next-phase WU starts until exit criteria + review gate pass.
- Scope changes route through DECISIONS.md, never directly into the backlog.
