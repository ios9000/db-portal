# SPEC-023 · Maintenance windows, warn-only

> WU-023, resolving O-3's semantics debt: `instance.maintenance_window` has
> been raw text since WU-010 (the 0002 comment crediting "WU-022" predates a
> backlog renumbering — this WU owns it). D6 is the law here: windows
> **warn, never block**. Closes Phase 2; the M2 gate follows.

## Mini-ADRs

1. **Grammar: `Day HH:MM-HH:MM`, weekly, wrap-capable.** Exactly what the
   estate CSV contains — a 3-letter English day (case-insensitive; the
   field is unvalidated text, so gratuitous case strictness just converts
   typos into silently-missing warnings), a space, and a 24h
   `start-end` pair at minute precision. Start is inclusive, end exclusive.
   `end <= start` wraps past midnight into the next day — not an edge case,
   the fixture ships one (`wms-prod: Sat 22:00-02:00`). Exception:
   `end == start` is unparseable (a zero-length window is a typo; reading
   it as 24h would be a surprising gift). Anything else — extra tokens,
   bad day, 25:00 — is unparseable. One window per instance; lists,
   holidays and exceptions are post-MVP grammar.

2. **One parser, server-side: `internal/window`, pure functions.**
   `Parse(raw) (Window, error)` + `Contains(t)` — no DB, no clock of its
   own. Both consumers use it: the stamp point (runs.Start) and the
   instance read model. The frontend never parses window text; it displays
   a server-computed state (mini-ADR 5). A TS re-implementation would be
   the drift kind of bug: the banner saying one thing and the audit stamp
   another.

3. **Every failure mode defaults to "no warning" (D6).** Empty window =
   no warning. Unparseable window = no warning, logged once per instance
   per process — at the stamp point only, never from the read path (a
   list request must not spam logs per row). A window can therefore never
   block, delay, or fail a launch; garbage text degrades to exactly the
   pre-WU-023 behavior.

4. **The stamp is a column, not an action:** migration 0008 adds
   `audit_event.window_warned boolean NOT NULL DEFAULT false`, stamped
   true on the `run.submitted` row when a parseable window exists and the
   launch instant falls outside it. A separate action row would put a
   second actor-attributed event on every warned run for what is one fact
   about one decision. Submitted-only: `run.finished` inherits the stamps
   that *identify* the run (actor, env, tag); the warning qualifies the
   launch decision, which happens once. Because the check lives in
   runs.Service.Start — after the instance lookup, before any row — the
   drawer path and the scheduler path are the same implementation; a
   scheduled fire outside the window carries the flag with zero
   scheduler-side code.

5. **Instance read model gains `window_state`:**
   `"inside" | "outside" | null` (null = no window or unparseable),
   computed at read time from the same parser, server-local now. The
   drawer shows a warn line when `"outside"` — text plus the raw window so
   the operator sees *what* they're outside of. Cheap per-row pure
   computation; no caching, no new endpoint.

6. **Timezone: server-local**, same clock SPEC-022 mini-ADR 7 pinned for
   cron. One portal, one wall clock, stated in the UI hint. Per-instance
   timezones ride O-3's post-MVP grammar.

## Data (migration 0008)

```sql
ALTER TABLE audit_event
    ADD COLUMN window_warned boolean NOT NULL DEFAULT false;
```

Down drops the column. DDL is untouched by the append-only triggers
(UPDATE/DELETE/TRUNCATE); existing rows read false — honest, they predate
window semantics.

## Interfaces

- `internal/window` (new, pure): `type Window {Day time.Weekday; Start,
  End int // minutes since midnight}`; `Parse(raw string) (Window, error)`;
  `(Window) Contains(t time.Time) bool` (wrap-aware).
- `runs.Service.Start`: instance SELECT extended with
  `maintenance_window`; computes `warned` (parse → outside?), stamps it
  into the `run.submitted` INSERT. Unparseable → `warnOnce` (sync.Map,
  per instance name per process, slog Warn).
- `inventory.Instance` gains `WindowState *string` (`window_state` JSON),
  set post-scan in List/Get.
- Frontend `api.ts Instance` gains `window_state: 'inside' | 'outside' | null`;
  LaunchDrawer renders a `.drawer-warn` alert line when `'outside'`:
  "Outside this instance's maintenance window (<raw window>). You can
  still launch — windows warn, never block."

## Behavior (testable)

1. Parse: fixture forms parse (incl. the wrap window); case-insensitive
   day; rejects bad day / bad time / end==start / trailing junk / empty.
2. Contains: inside true; outside false; start inclusive, end exclusive;
   wrap window contains late-Sat AND early-Sun instants, excludes Sun
   afternoon and Friday.
3. Start on an instance whose window excludes now → run launches normally,
   `run.submitted` has window_warned=true, `run.finished` false.
4. Window containing now → false. No window → false. Garbage window →
   run still launches, flag false (never blocks) — and the parse warning
   logs once, not per launch.
5. A scheduled fire outside the window carries window_warned=true on its
   submitted row (proves the shared stamp point through the executor).
6. Instance JSON: window_state inside/outside tracks a window computed
   around now; null for empty and for garbage windows.
7. Migration 0008 up adds the column (default false on old rows), down
   removes it; `npm run check` green.

## UI slice

Small enough to stay architect-side (no delegation): api.ts type +
LaunchDrawer warn line + `.drawer-warn` style + component tests
(outside → alert with window text; inside/null → absent).

## Out of scope / deferred

- Window hints in the schedule-create drawer ("this cron fires outside
  the window") — needs cron×window intersection; icebox.
- Surfacing window_warned on the run read model / Activity / RunDetail —
  icebox (the audit column is queryable; UI when a consumer asks).
- Window editing UI, multi-window/holiday grammar, per-instance TZ → O-3
  post-MVP.
