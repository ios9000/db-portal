# SPEC-030 · Artifact registry (metadata-first)

> Groomed 2026-07-11 for WU-030 (first WU of Phase 3). Authority chain:
> DECISIONS.md → ARCHITECTURE.md §3 ("artifact registered (checksum, retention
> class)") → this file. SPEC-012 owns the run/audit model this rides on.

## Scope

Dump artifacts become first-class queryable rows — the restore workflow's
source of truth (WU-031 reads ONLY the registry) — instead of three
denormalized columns on `run`. This WU: migration 0009 (table + backfill),
registration inside `finalize`, `GET /api/artifacts`. Explicitly NOT here:
artifact bytes (mock artifacts stay metadata-only forever; real bytes = WU-034
volume, WU-035 minio), retention ENFORCEMENT (class is stored classification
only — job is M4/icebox), restore itself (031/036), any UI (031's drawer).

## Mini-ADRs (agent decisions, revisitable)

1. **Dual write: run columns stay.** The registry row and run's `artifact_*`
   columns carry the same values, written in the SAME finalize transaction.
   Run columns = "what this run produced" (run read model + `last_backup_at`
   keep reading them — zero churn); registry = "what can be restored". They
   cannot diverge today (one tx); when M4 retention enforcement deletes or
   expires artifacts, the registry row is what changes and the run columns
   remain historical record — that split is the point.
2. **Retention class = stored classification, stamped at registration.**
   CHECK `'standard' | 'safety'`, default `'standard'`. Class is a property of
   the launch's intent (a pre-restore safety dump), not of the bytes, so it is
   written at registration and never derived at read time. WU-030 only ever
   writes `'standard'` — no safety dumps exist until 031, and how the chain
   plumbs `'safety'` through Start→finalize is a SPEC-031 mini-ADR.
3. **One artifact per run — `UNIQUE (run_id)`.** Exactly-once registration is
   structural twice over: the INSERT rides finalize's guarded terminal
   transition (SPEC-012 mini-ADR 8 — a losing finalizer affects 0 rows and
   never reaches the INSERT), and the unique index makes a duplicate
   physically impossible. One dump = one file pre-O-1 and in reality; an op
   that produces multiple artifacts must relax this deliberately.
4. **`?instance` is required; unknown instance is 404.** The registry read is
   the restore drawer's per-instance feed, anchored on a real instance — a
   global artifact browse is not an MVP surface. Missing param → 400; unknown
   name → 404 `"no such instance"` (matches `GET /api/instances/{name}`;
   "instance gone" ≠ "no artifacts yet"). Contrast `/api/runs?instance=`,
   where instance is an optional filter and unknown values filter to empty.
5. **Registry code lives in `internal/runs`.** finalize is the only writer and
   the table FKs to run; a separate package would import runs for nothing.
   Server seam is its own small interface (`ArtifactReader`, `Deps.Artifacts`)
   per the established per-seam-stub pattern; main wires *runs.Service into
   both. Split into an artifact package only if restore (031) grows one.

## Data (migration 0009)

- `artifact`: `id PK`, `run_id bigint NOT NULL UNIQUE REFERENCES run(id)`
  (origin), `name text NOT NULL`, `size_bytes bigint NOT NULL`, `checksum
  text NOT NULL`, `retention_class text NOT NULL DEFAULT 'standard' CHECK
  ('standard'|'safety')`, `location text NULL` — DORMANT until WU-034/035
  (the 0003 `window_warned` pattern; comment says so), `created_at
  timestamptz NOT NULL DEFAULT now()`.
- Backfill (in 0009 up): INSERT…SELECT every `state = 'success'` run whose
  three `artifact_*` columns are all non-NULL; `created_at =
  COALESCE(finished_at, updated_at)`; `ON CONFLICT (run_id) DO NOTHING`
  (idempotence is belt-and-braces — goose applies once, the conflict clause
  covers manual re-runs).
- Down: `DROP TABLE artifact`. No new indexes beyond the unique — list joins
  through run at `/api/runs?instance=` scale.

## Interfaces

- `finalize` (internal): after the guarded run UPDATE affects a row, if
  terminal state is `success` AND the engine returned an artifact → INSERT the
  registry row in the same tx, class `'standard'`.
- `runs.Service.ListArtifacts(ctx, instance)` → newest 50 (`created_at DESC,
  id DESC`), `ErrUnknownInstance` for a name not in inventory.
- `GET /api/artifacts?instance=<name>` (session-gated read, no dba — SPEC-021
  mini-ADR 2) → 200 `{"artifacts":[{id, run_id, name, size_bytes, checksum,
  retention_class, created_at}]}` (never null; `location` NOT exposed until it
  means something — WU-034). 400 missing/empty `instance`; 404 unknown
  instance; 401 no session.

## Behavior (testable)

1. Successful dump — button AND scheduled, both flow through Start/finalize —
   registers exactly one row: `run_id` = origin run, name/size/checksum
   identical to the run's columns (same tx), class `'standard'`.
2. Failed, canceled, engine-refused and orphan-swept runs never register
   (finalize with non-success state or nil artifact).
3. Double-finalize race (watcher vs cancel vs sweep): loser affects 0 rows,
   inserts nothing — exactly one registry row survives, enforced at SQL layer.
4. Backfill: every historical successful dump gets exactly one row,
   `created_at` = its `finished_at`; failed/artifact-less runs skipped;
   0009 up→down→up lands the identical set (no dupes, none lost).
5. List: newest-first, cap 50; unknown instance 404; missing param 400;
   no session 401; success payload is `[]`, never null, for a known instance
   with no artifacts.

## Guardrails & audit

No new audit actions: registration is part of the already-audited run
finalization (`run.finished`), not a separate act — the registry row is
derived data whose provenance IS the origin run's audit trail (env,
playbook_tag, actor all reachable via `run_id`). Read endpoint session-gated.
No secrets: name/size/checksum are not sensitive; `location` stays NULL and
unexposed this WU.

## Out of scope / deferred

- Retention ENFORCEMENT (delete/expire job) → icebox (M4+ policy work).
- `location` semantics → WU-034 (compose volume path), WU-035 (minio + bytes).
- `'safety'` class writers + Start→finalize plumbing → WU-031 (chain step 2).
- Restore reads/verify → WU-031/036. UI surface → WU-031 drawer.
- Artifact bytes/download → O-1 / WU-035.

## Open questions

None blocking. `'safety'` plumbing is SPEC-031 mini-ADR (owner: agent, at 031).
