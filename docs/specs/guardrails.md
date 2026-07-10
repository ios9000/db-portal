# SPEC-015 · Prod guardrails

> WU-015. Implements ARCHITECTURE §4 (D2 mix-up safeguards) for the MVP: the portal is a
> single pane over prod AND non-prod, so every layer below exists to make "wrong database"
> structurally hard, visually loud, and forensically visible. No approval flow in MVP —
> design brief Screen 4 is adapted, not copied.

## Scope

Four independent safeguard layers (ARCHITECTURE §4), each cheap alone, strong stacked:
env-colored context banner (layer 1), typed-name prod ritual (layer 2), structurally
disjoint per-class engine adapters (layer 3), env stamped in every audit row (layer 4 —
already in schema, this WU pins it with tests). Explicitly NOT: approvals/risk tiers
(Screen 7 — icebox), maintenance-window warnings (WU-023), required-reason-for-prod
(Screen 4 shows it; deferred with approvals — reason stays optional in MVP).

## Interfaces

- `EnvBanner({ env })` (new component) — full-width strip naming the environment in text
  AND color (never color alone). Rendered on every single-env context:
  - `RunDetail` — env of the run, above the header.
  - `LaunchDrawer` — env of the target instance, above the header.
  - Fleet screens (My Databases, Activity) stay banner-free: mixed envs, badges per row.
- `LaunchDrawer` prod ritual — when `instance.env === 'prod'`: a "To confirm, type the
  instance name" field, helper "(paste disabled)"; primary button disabled until the typed
  value equals `instance.name` exactly. Non-prod: field absent, one click (unchanged).
- `engine.Registry.Register(class, adapter)` — panics if `adapter` is already registered
  under a different class. Same-class re-register stays allowed (idempotent wiring).

## Data

No schema change. `audit_event.environment` and `run.environment` are `NOT NULL` since
migration 0003 — this WU adds the tests that make that a pinned contract, not an accident.

## Behavior

1. Run detail shows a full-width banner naming the run's environment ("PRODUCTION" /
   "TEST" / "DEV" wording), colored with the env tokens; prod carries the ⚠ mark.
2. Launch drawer shows the same banner for the target instance's env.
3. Prod launch: primary button starts disabled; typing the exact instance name enables it;
   any other value (partial, wrong case, other instance) keeps it disabled.
4. Paste and drop into the confirm field are ignored — the value stays empty.
5. Non-prod launch is one click: no confirm field, button enabled immediately.
6. Schema rejects an audit row with NULL environment (not-null violation 23502) — layer 4
   is detection, so the stamp must be un-omittable, not merely conventional.
7. A prod run stamps `environment = 'prod'` on BOTH audit events (run.submitted,
   run.finished). (Nonprod 'test' stamps already asserted since SPEC-012.)
8. Registering one adapter instance for both env classes panics at wiring time — the
   portal must fail to boot rather than serve with shared engine credentials.

## Guardrails & audit

This spec IS the guardrail layers 1–4. No new audit fields; no secrets involved; banner
and ritual are pure frontend, layer 3 is boot-time, layer 4 is schema + tests.

## Mini-ADRs (agent-decided, in-WU)

- **Banner placement:** inside the context component (page/drawer top), spanning its full
  width — the app shell stays env-agnostic and fleet screens keep mixed-env neutrality.
- **Register panics rather than returning error:** a shared adapter is a wiring bug, not
  a runtime condition; panicking in `main` wiring fails closed before serving and keeps
  every call site unchanged. Adapters are compared by interface identity (pointers).
- **Exact-match confirm, case-sensitive:** instance name is the natural key; anything
  fuzzier weakens the ritual. Placeholder shows the expected name — this is deliberate
  friction, not a memory test.

## Out of scope / deferred

- Approval workflow, risk tiers, impact chips (Screen 4/7) — icebox.
- Window warning in the drawer — WU-023.
- Required reason/ticket on prod — travels with approvals (icebox).
- ~~AuthN/AuthZ actor in the ritual~~ → landed in WU-020/021: the ritual is
  now ALSO server-side (SPEC-021 mini-ADR 6 — POST /api/runs `confirm`
  must equal the instance name on prod) and the launch is attributed to
  the session identity in both audit rows.

## Open questions

None blocking. All decisions taken as mini-ADRs above.
