# SPEC-022 · Portal-owned scheduler

> WU-022, implementing ADR-003 (scheduled runs ride the identical guardrail +
> audit path as button presses) and the D4 catalog line "Dump: scheduled AND
> run-now". Builds on SPEC-021's seams, both designed for this WU: the
> explicit-actor door (`StartRequest.Actor = "schedule:<owner>"`, mini-ADR 4)
> and the programmatic prod confirm (mini-ADR 6 — the human ritual for prod
> schedules happens at schedule creation).

## Mini-ADRs

1. **DB-driven tick loop; robfig/cron/v3 is the parser, not the runner.**
   The executor is a plain ticker: every tick, fire every schedule with
   `enabled AND next_fire_at <= now()`. robfig's in-memory `cron.Cron` runner
   would hold a second copy of schedule state needing sync on every
   CRUD/restart; the DB already IS the state. The library earns its keep as
   `cron.ParseStandard` + `Schedule.Next(t)` — battle-tested five-field
   parsing and next-fire math (the part worth not writing). A persisted
   `next_fire_at` also gives the UI its "next fire" column for free.

2. **Misfire policy: coalesced catch-up.** A schedule whose `next_fire_at`
   passed while the portal was down fires ONCE on the first tick after boot,
   then recomputes from now (a @daily schedule missed three times fires one
   catch-up, not three). This falls out of mini-ADR 1 unaided — due-in-the-
   past is just due. Rationale is D1 verbatim: the motivating incident was a
   skipped dump; a late dump beats a missing one. Skipping misfires (classic
   cron) re-creates the incident; full replay is load with no information.
   Boot-stampede spreading (500 catch-ups on first tick after long downtime)
   is deferred to M4 hardening with the load-test WU.

3. **Fire-then-stamp, and the crash window heals itself.** Per fire:
   `runs.Service.Start(...)` FIRST, then one UPDATE stamping `last_fired_at`,
   `last_run_id`, `last_fire_status`, and the recomputed `next_fire_at`.
   A crash between the two leaves `next_fire_at` in the past, so boot fires a
   catch-up — but the half-fired run was queued/running when the process
   died, so the existing orphan sweep (SPEC-012 mini-ADR 5) has already
   finalized it `failed` and mailed the DBA list (SPEC-014). Net effect: a
   crash mid-fire RETRIES the dump, never silently loses it and never leaves
   two live copies. Stamp-first has the opposite window: a fire lost with no
   trail — exactly the D1 incident. Normal restarts double-fire nothing:
   after a clean fire `next_fire_at` is in the future.

4. **Overlap policy: skip, visibly.** If the schedule's previous run
   (`last_run_id`) is still queued/running at fire time, this fire is
   skipped: `last_fire_status = 'skipped_overlap'`, `next_fire_at` advances,
   no run row. Overlapping dumps of the same instance are a load hazard, and
   queueing behind a stuck run builds an invisible backlog. The skip is not
   an audit event (audit_event is run-centric and there is no run; a skip is
   the guardrail working, not an actor acting — same reasoning as SPEC-021's
   unconfirmed-ritual 400) but it is never silent: schedule row status + one
   slog Warn.

5. **Enable/disable: disabled means the human said stop.** `enabled = false`
   freezes the schedule: `next_fire_at` goes NULL (the partial index and the
   tick query both key on enabled). Re-enabling computes `next_fire_at`
   fresh from now — fires that would have happened while disabled are NOT
   made up. Misfire catch-up (mini-ADR 2) exists because the PORTAL failed
   to keep a promise; a disabled schedule is a promise explicitly revoked.

6. **Jitter is baked into `next_fire_at` at computation time.**
   `next_fire_at = cron.Next(from) + rand(0..jitter)` (jitter default 60s,
   a Scheduler field; tests set 0). Persisting the jittered instant means
   the UI shows the true planned time, misfire math needs no slack term,
   and the tick loop stays a dumb comparator. Purpose: hundreds of
   "@daily at 02:00" dumps must not hit the estate in the same second.

7. **Timezone: specs evaluate in the portal server's local time.** Specs
   parse with `cron.ParseStandard` (five-field + `@daily`-style
   descriptors); `Next` is evaluated in server-local time, stated in the UI
   ("server time"). A typed `CRON_TZ=Europe/Berlin ...` prefix works because
   the parser honors it, but per-schedule timezone UI is deferred — one
   clock for MVP, same clock WU-023's windows will assume.

8. **Prod ritual at creation; the executor confirms programmatically.**
   POST /api/schedules carries `confirm`; a schedule on a prod instance is
   refused (400, same message and predicate as SPEC-021 mini-ADR 6) unless
   `confirm` equals the instance name exactly. The executor then fires with
   `Confirm: <instance>` — the human meant it once, at creation, for every
   future fire. Schedule mutations (create/toggle/delete) sit behind
   `requireRole(dba)` like every other mutation; list stays session-gated.

9. **Fired runs are attributed `schedule:<owner>`; owner = creator.**
   `StartRequest.Actor = "schedule:" + created_by` (ADR-003's shape). Actor
   means *on whose behalf* (SPEC-021 mini-ADR 4): the run happens on the
   standing instruction of the DBA who created the schedule, and both audit
   rows plus `requested_by` filtering carry it. Schedule CRUD itself is NOT
   on the audit ledgers in MVP: the row records created_by/created_at, and
   every consequence (each fired run) has a full trail; a schedule-change
   ledger is deferred (icebox) with role admin.

10. **A broken fire never wedges the loop.** Any Start error still stamps
    the schedule (`last_fire_status = 'error'`, `next_fire_at` advances) —
    a schedule that errors every fire logs once per fire, not per tick.
    Engine refusal (ErrEngine) counts as 'fired': the run row exists,
    finalized failed, and SPEC-014 already mails the DBA list — scheduled
    dumps inherit failure notification with zero new code.

## Data (migration 0007)

```sql
CREATE TABLE schedule (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance_id  bigint NOT NULL REFERENCES instance(id) ON DELETE CASCADE,
    operation    text NOT NULL,
    cron_spec    text NOT NULL,
    reason       text,
    enabled      boolean NOT NULL DEFAULT true,
    created_by   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    next_fire_at timestamptz,                    -- NULL iff disabled
    last_fired_at   timestamptz,
    last_run_id     bigint REFERENCES run(id),
    last_fire_status text                        -- fired | skipped_overlap | error
);
CREATE INDEX schedule_due_idx ON schedule (next_fire_at) WHERE enabled;
```

- `ON DELETE CASCADE`: a schedule cannot outlive its instance (fired runs
  and their audit rows survive independently — run has its own columns).
- No uniqueness across (instance, operation, spec): duplicates are harmless
  and legitimate (e.g. staggered pairs).

## Interfaces

- New package `internal/schedule`: store + executor in one (the executor is
  the store's only writer besides CRUD).
  - `Store` methods: `Create(ctx, CreateRequest) (Schedule, error)` —
    validates spec via ParseStandard, operation via catalog.ByID, instance
    lookup + prod-confirm check (`ErrProdUnconfirmed` re-exported semantics:
    return `runs.ErrProdUnconfirmed`), computes first `next_fire_at`;
    `List(ctx) ([]Schedule, error)` (joined instance name/env, newest
    first); `SetEnabled(ctx, id, bool) (Schedule, error)` (recomputes or
    NULLs next_fire_at); `Delete(ctx, id) error`. Not-found → `ErrNotFound`.
  - `CreateRequest{Instance, Operation, CronSpec, Reason, Confirm,
    CreatedBy string}`; bad spec → `ErrBadSpec` (parser message wrapped).
  - `Scheduler` (same package): `Tick` (default 10s) and `Jitter` (default
    60s) fields; `Run(ctx)` blocking loop, started as a goroutine in
    main.go after SweepOrphans, stopped by ctx cancel on shutdown. Fires
    through a `Starter` interface `{ Start(context.Context,
    runs.StartRequest) (runs.Run, error) }` — the seam tests fake.
- `server.Deps` gains `Schedules ScheduleService`. Routes:
  - `GET /api/schedules` (session) → `{"schedules": [...]}` with
    `{id, instance, env, operation, cron_spec, reason, enabled, created_by,
    created_at, next_fire_at, last_fired_at, last_run_id, last_fire_status}`.
  - `POST /api/schedules` (dba) body `{instance, operation, cron_spec,
    reason?, confirm?}` → 201 schedule JSON; 400 bad spec (parser message) /
    unknown operation / prod unconfirmed / reason > 500; 404 unknown
    instance; 413 over 64 KiB (same caps as runs).
  - `PATCH /api/schedules/{id}` (dba) body `{enabled: bool}` → 200 schedule
    JSON; 404 unknown id.
  - `DELETE /api/schedules/{id}` (dba) → 204; 404 unknown id.

## Behavior (testable)

1. Create with a bad cron spec / unknown operation → 400, no row; on a prod
   instance without exact `confirm` → 400 `prod launch requires typing the
   instance name`, no row; with it → 201 and `next_fire_at` within
   (cron next, cron next + jitter].
2. A due schedule (`next_fire_at` in the past, enabled) fires on the next
   tick: run exists via the REAL runs path — both audit rows carry actor
   `schedule:<owner>`; `last_run_id`/`last_fired_at`/`status='fired'`
   stamped; `next_fire_at` moved to the future (M2 exit criterion).
3. Misfire catch-up: `next_fire_at` set days in the past fires exactly ONCE,
   then next_fire_at > now (coalescing).
4. Overlap: due schedule whose last_run is still running → no new run,
   `status='skipped_overlap'`, next_fire_at advances; once the run
   finalizes, the next due fire proceeds.
5. Disabled schedule never fires (due-in-the-past + disabled stays silent);
   disable NULLs next_fire_at; re-enable recomputes from now (never fires
   the disabled gap).
6. Prod schedule fires WITHOUT a human: the executor's programmatic Confirm
   passes the server-side ritual; audit rows say env=prod,
   actor=schedule:<owner>.
7. Start failure (engine refusal): schedule stamped 'fired', run finalized
   failed (mail path fires per SPEC-014); a Start error before the run row
   (e.g. store lookup) stamps 'error'; either way next_fire_at advances —
   the loop never hot-loops or wedges.
8. requireRole guards POST/PATCH/DELETE (403 + authz.denied row via
   existing middleware); GET needs only a session.
9. Migration 0007 up/down round-trips; CASCADE removes schedules with
   their instance; runs survive schedule deletion.

## UI slice contract

- `api.ts`: `Schedule` type; `fetchSchedules()`, `createSchedule(...)`,
  `setScheduleEnabled(id, enabled)`, `deleteSchedule(id)` — ApiError
  everywhere, `errorDetail` already unwraps the envelope.
- Schedules page (replaces stub): table (instance + env badge, operation,
  cron spec, next fire, last fire status with last_run_id linking to
  /runs/{id}, enabled toggle, delete) + "New schedule" drawer: instance
  picker (reuse fleet fetch), operation (dump), cron spec text input with
  "server time" hint, optional reason (maxLength 500), prod ritual
  typed-name block (same UX as LaunchDrawer when the picked instance is
  prod), server 400/403 rendered via err.detail. Empty state kept friendly.
- Times render like Activity does; `next_fire_at` is the source of truth
  (already jittered — display, don't recompute).

## Out of scope / deferred

- Cron→human-language translation, next-3-fires preview endpoint → icebox.
- Edit-in-place (spec/reason changes = delete + recreate in MVP).
- Per-schedule timezone picker (typed CRON_TZ works; UI later).
- Schedule-change audit ledger, ownership transfer → icebox with role admin.
- Boot-stampede spreading for mass catch-up → M4 (load test WU).
- Pause-on-repeated-failure / failure budgets → post-MVP.
