# Vision

## Problem

~500 PostgreSQL instances operated by hand; 1,000+ manual-operation incidents in the
last 12 months. The archetype: an engineer forgets the safety dump before a critical
change. Day-2 operations are tribal CLI knowledge with no audit trail.

## Product

A self-service portal that turns dangerous, forgettable manual procedures into audited,
guard-railed, one-click operations — starting with the DBA team, eventually safe enough
for application admins. Buttons are data (catalog), execution is Ansible behind an
adapter, every action is attributable and replayable.

**North star (research corpus):** 14 modules — catalog, engine integration, inventory,
scheduling, workflows with resume, approvals, notifications, history/live logs, RBAC,
audit, surveys, maintenance windows, concurrency locks, dry-run. Three personas
(app admin / DBA / team lead). See `P:/Projects/db-portal-research/02-system-analysis.md`.

**MVP (decisions D1–D7):** Backup + Restore, DBA-only, prod guardrails, append-only
audit (1y), email notifications, chains that halt-notify-resume. Acceptance: customer
management approval of the demo. Metric: button clicks — each click is an avoided
manual procedure, which is the incident-reduction pitch that buys the budget.

## This repo's second mission

The build doubles as a controlled test of AI-agent-driven development (Fable 5 +
Claude Code): can an agent ship a complex multi-stack product across many sessions
without context loss? Strategy and instrumentation: `docs/agent/STRATEGY.md`.
