# SPEC-032 · Chain engine

> Groomed 2026-07-11 for WU-032. Authority chain: D5 ("halt + notify DBA +
> resume from failed step") → ARCHITECTURE §3 (Chains) → this file. SPEC-012
> owns the run/audit model steps ride on; SPEC-022 is the fire-with-stored-
> evidence precedent (m2-gate item 1).

## Scope

Portal-level sequential step execution over runs: a `chain` is an ordered list
of steps, each fired as a normal run through `runs.Service.Start` — the
identical guardrail + audit path as the button and the scheduler. Step failure
(or cancel) halts the chain and sends ONE mail; a DBA resumes from the failed
step. This WU: migration 0010, `internal/chain` service + driver + boot sweep,
`GET /api/runs/{id}/chain`, `POST /api/chains/{id}/resume`, RunDetail chain
strip (separate UI slice). Explicitly NOT here: restore assembly + `'safety'`
plumbing (031), a client-facing chain-create API (chains are portal-assembled;
031's restore POST is the first assembler), parallel steps/DAGs (post-MVP).

## Mini-ADRs (agent decisions, revisitable)

1. **Step-run actor = `chain:<mover>`**, extending `schedule:<owner>`
   (ARCHITECTURE §5 vocabulary grows one form). Runs fired by machinery on a
   human's behalf are attributed to the machinery + the human who set it in
   motion. Mover = chain creator on the original pass, the RESUMER from a
   resume onward — dba2 resuming dba1's chain must not audit as dba1 (one
   human = one actor, SPEC-020). No persistence: the mover is whoever started
   the current driver pass. Plain-initiator was rejected: it would make a
   chain-fired run indistinguishable from a hand launch, and the prefix is
   also what the mail filter (mini-ADR 5) keys on.
2. **FK direction: `chain_step.run_id → run(id)`, NULL until fired, UNIQUE.**
   `run` gains nothing; runs stay chain-agnostic (steps ARE runs — Activity,
   notify, artifact registry all unchanged). Resume re-points `run_id` at the
   new attempt: the superseded run keeps its run + audit rows forever (history
   kept where history lives) but drops off the chain strip — the strip shows
   the chain's current truth, not an attempt log. An attempt table was
   rejected as a table for a rare event with no MVP reader.
3. **Resume after cancel = resume after failure. No fresh prod ritual.** The
   stored creation-time confirm fires verbatim on every step, resume included
   (the schedule.confirm port: env promotion mid-chain fails the ritual in
   Start, visibly — no run row, chain halts — never auto-confirms). Cancel
   halts the chain (the human said stop; nothing auto-continues); resume is a
   fresh, DBA-gated, audited human act (`chain:<resumer>`) — that is the
   confirmation. Re-typing the instance name would out-friction the scheduler
   for no added authority: the resumer holds the same role the creator did.
4. **No `state` column on `chain_step`** (deviation from the BACKLOG sketch —
   also `name` ≡ `operation`, the catalog id Start needs; display names come
   from the catalog like everywhere else). Step status is DERIVED from the
   linked run: `run_id` NULL → `pending`, else the run's state. The run's
   transitions are already single-finalizer-guarded (WU-016); a mirrored
   column would be a second copy of guarded state, transitioned in lockstep
   forever. Chain-level state is NOT derivable (halted-with-intent vs
   mid-advance) and IS stored.
5. **ONE mail per halt — and it's the chain's.** Transition to `halted` sends
   one `ChainHalted` mail (D7 content: who/what/where/status + link to the
   newest linked run, else BaseURL). The step run's own SPEC-014 failure mail
   is SUPPRESSED by a chain-owned `runs.Notifier` decorator keyed on the
   `chain:` RequestedBy prefix — no schema coupling into runs, and race-free:
   the prefix is stamped at submit, before any finalize can mail. Two mails
   for one incident was rejected (the run mail adds nothing the chain mail's
   run link doesn't reach); querying `chain_step` from the notifier was
   rejected (racy: `run_id` links only after Start returns, so a fast failure
   mails before linkage).
6. **One async driver for create and resume.** `drive(chain, mover)`: find the
   lowest step whose run is missing or not-success → fire it (or adopt a live
   run) → watch to terminal → advance or halt. Create inserts chain + steps
   (one tx) then spawns it; Resume does a guarded `halted → running` flip
   (0 rows → conflict) then spawns it. Fire-time errors (ritual, engine
   refusal) surface as a visible halt + mail — the scheduler's
   `last_fire_status='error'` posture — not as sync HTTP errors.
7. **Chain states: `running | halted | success`. No terminal failure.** D5
   chains halt, resumable indefinitely. Every transition is guarded
   (`WHERE state='running'`, resume `WHERE state='halted'`), so halt-mail
   exactly-once and resume single-flight are structural, boot sweep included.

## Data (migration 0010)

- `chain`: `id PK`, `kind text NOT NULL` (no CHECK — the kind catalog is
  031's), `instance_id bigint NOT NULL REFERENCES instance(id)` (plain FK,
  like `run` — a chain is history, not future intent), `created_by text NOT
  NULL`, `confirm text NOT NULL DEFAULT ''` (stored ritual evidence, 0008
  pattern), `reason text` (propagated to every step run), `state text NOT
  NULL DEFAULT 'running' CHECK (running|halted|success)`, `created_at` /
  `updated_at timestamptz NOT NULL DEFAULT now()`, `halted_at` /
  `finished_at timestamptz` (halted_at cleared on resume).
- `chain_step`: `id PK`, `chain_id bigint NOT NULL REFERENCES chain(id) ON
  DELETE CASCADE`, `seq int NOT NULL`, `operation text NOT NULL`, `params
  jsonb NOT NULL DEFAULT '{}'`, `run_id bigint UNIQUE REFERENCES run(id)`,
  `UNIQUE (chain_id, seq)`. No indexes beyond the uniques (sweep scans a
  small hot set). Down: drop both.

## Interfaces

- `chain.Service` (imports runs, NEVER engine): `Create(ctx, CreateRequest
  {Kind, Instance, Actor, Confirm, Reason, Steps []StepSpec{Operation,
  Params}})` — validates catalog ops + instance, prod ritual at creation
  (`env=='prod' && Confirm != Instance` → `runs.ErrProdUnconfirmed`, nothing
  inserted), stores confirm, spawns driver. `Resume(ctx, id, actor)` —
  guarded flip; `ErrNotResumable` (→409) when not halted. `Get(ctx, id)`,
  `ForRun(ctx, runID)` (strip lookup, `ErrNotFound`), `SweepOrphans(ctx)`.
  `Notifier` field (chain's own interface; `notify.Mailer` implements
  `ChainHalted`), `PollInterval`, `Wait()` — the runs.Service idioms.
- Read model `Chain`: `{id, kind, instance, env, state, created_by, reason,
  created_at, halted_at, finished_at, steps: [{seq, operation, run_id,
  status}]}` — env from the instance join; step status per mini-ADR 4.
- `chain.StepRunFilter{Next runs.Notifier}` — the mini-ADR 5 decorator; main
  wires `runSvc.Notifier = filter(mailer)` + `chainSvc.Notifier = mailer`.
- HTTP: `GET /api/runs/{id}/chain` (session) → 200 Chain | 404 (unknown run
  OR not a step — same shape). `POST /api/chains/{id}/resume` (dba, no body)
  → 200 Chain | 404 | 409 not halted.
- main.go order: runSvc → notifier wiring → runs.SweepOrphans →
  **chain.SweepOrphans** (must see swept step runs) → scheduler → serve.

## Behavior (testable)

1. 3-step chain: runs fire strictly sequentially (N+1 only after N success),
   each fully audited via Start (submitted/finished, env, window_warned,
   params_digest); last success → chain `success` + finished_at.
2. Injected failure at step 2 (`mock_fail_at`): step 1 run + audit intact,
   chain `halted` + halted_at, exactly ONE mail (chain's; run mail
   suppressed), step 3 never fires.
3. Resume on halted: failed step re-fires as a NEW run attributed
   `chain:<resumer>` (old run + audit survive; step re-linked), remaining
   steps continue to `success`.
4. Single-flight: resume on running / concurrent double-resume → exactly one
   proceeds, the rest `ErrNotResumable`/409. Non-DBA resume → 403 +
   `authz.denied` row (middleware).
5. Cancel of a live step run → run `canceled` (existing path), chain `halted`,
   ONE mail, resumable per (3).
6. Boot sweep (after runs sweep): chains `running` with no live linked run →
   `halted` + ONE mail each; halted chains only move on human resume — no
   step double-fires across restart.
7. Env promotion mid-chain (non-prod creation, instance flips to prod): next
   fire fails the ritual in Start — NO run row for that step — chain halts
   visibly + mail. Creation-time ritual: prod target needs Confirm ==
   instance name, else nothing inserted.
8. Engine refusal at fire: run exists finalized failed (Start's contract) →
   chain halted, mail links that run.

## Guardrails & audit

No new audit_event actions: every action ON an instance is a run, audited as
one by Start/finalize; the chain row is the chain-level lifecycle record.
Attribution per mini-ADR 1; stored confirm per mini-ADR 3; all four guardrail
layers arrive via Start (env banner is the UI slice's job). Mail content D7:
who/what/where/status + link only. `params` may not carry secrets (they enter
`params_digest`' preimage and the engine — same rule as runs; 031's artifact
refs are ids + checksums). Resume takes no body; nothing to cap.

## Out of scope / deferred

- Restore chain assembly, `'safety'` retention plumbing, verify step → WU-031.
- Chain-create HTTP API → none in MVP (portal-assembled only, via 031).
- Attempt history UI / superseded-run navigation → icebox if ever.
- Chain list/browse surface, retention of old chains → icebox.
- Parallel steps, DAGs, conditional steps → post-MVP (D5 is sequential).

## Open questions

None blocking. Step-status vocabulary exposed to the UI (`pending` + run
states) is fixed here; 031 consumes as-is (owner: agent).
