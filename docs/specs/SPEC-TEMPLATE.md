# SPEC-XXX · <module name>

> Written just-in-time, by the session closing the prior milestone (grooming), for
> modules where a BACKLOG entry isn't enough. Keep under ~120 lines; link, don't repeat.

## Scope
One paragraph: what this module does and — as important — what it explicitly does not.

## Interfaces
API endpoints / adapter methods / UI surfaces. Signatures and shapes, not prose.

## Data
Tables/columns touched, migrations required, invariants (e.g. "append-only", "FK to inventory").

## Behavior
Numbered scenarios, each testable: happy path, failure modes, edge cases.
Steal from research corpus FRs where they apply (cite FR numbers).

## Guardrails & audit
What gets stamped in the audit row; which env-safeguard layers apply; secrets exposure check.

## Out of scope / deferred
Explicit list, with the Icebox line or future WU where each deferred item lives.

## Open questions
Each with an owner: `user` (blocks — surface in STATE.md) or `agent` (decide in-WU, mini-ADR).
