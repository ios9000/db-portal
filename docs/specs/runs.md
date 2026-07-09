# SPEC-012 · Catalog + run-now dump (hero flow)

> Groomed 2026-07-07 for WU-012. Authority chain: DECISIONS.md → ARCHITECTURE.md
> §3 (hero workflow), §5 (audit record) → this file. Design brief Screen 3 drives
> the drawer; WU-005 fixed the Adapter contract; SPEC-010 fixed the inventory.

## Scope

The golden thread: pick an instance → launch drawer → POST run → audit
`submitted` → MockEngine job via the Registry (first portal wiring of the
engine seam) → status watched to terminal → run finalized + audit event →
runs visible in Activity. Explicitly NOT here: live log streaming + run detail
page (WU-013), failure email (WU-014), typed-name prod ritual + env banner
(WU-015), window check (WU-023), auth/actor identity (WU-020/021 — actor is a
placeholder), real artifact upload (O-1 → WU-035; metadata only here).

## Mini-ADRs (agent decisions, revisitable)

1. **Run vs audit split.** ARCHITECTURE §2 lists `runs` and `audit` as separate
   stores; §5's single-record shape has both `ts_submitted` and `ts_finished`,
   which cannot be append-only in one row. Resolution: `run` = operational row,
   app-mutable (state machine mirror of engine.JobState); `audit_event` =
   append-only rows carrying every §5 field, one event per transition
   (`run.submitted`, `run.finished`). §5's record = the join of a run's events.
2. **Append-only enforcement.** Dev app role owns the tables, so REVOKE alone
   is theater; a BEFORE UPDATE/DELETE trigger raising an exception enforces it
   for every role. Both are applied (revoke documents intent, trigger enforces).
3. **Catalog in code, served over API.** `internal/catalog` = static Go data
   (one op: dump) exposed at GET /api/operations. A DB table adds nothing until
   ops multiply or grow per-instance availability rules (post-MVP).
4. **Completion detection = in-process watcher.** One goroutine per active run
   polls Adapter.Status until terminal, then finalizes (run row + audit event)
   — the seam WU-033 swaps for webhook+poll. Lazy sync-on-read was rejected:
   finalization must not depend on someone loading a page (email WU-014).
5. **Orphan sweep.** MockEngine state dies with the process; on startup any
   non-terminal run is finalized `failed` with error `engine job lost
   (portal restart)` + audit event, so nothing sticks at "running" forever.
   Per-run best effort (WU-016): a finalize failure is logged and skipped —
   one broken run must not leave the other orphans live.
6. **Actor placeholder.** No authn until WU-020: `actor = 'local-dev'`
   everywhere. WU-021 replaces it with the AD identity end to end.
7. **Stranded-job repair (WU-016, m1-gate item 1).** If recording job_id
   fails after StartJob succeeded, Start cancels the job (best effort) and
   finalizes the run `failed` ("portal failed to record the engine job: …")
   on a fresh context — chosen over retry-then-adopt for determinism; the
   record failure is likely persistent and an untracked live job is worse
   than a killed one. The audit trail records the attempt either way.
8. **Single finalizer (WU-016, m1-gate item 2).** finalize's run UPDATE and
   the watcher's status mirror both carry `AND state NOT IN
   ('success','failed','canceled')`. Zero rows affected ⇒ another finalizer
   won: finalize skips the audit event + notification (first outcome
   stands), the watcher stops watching. Exactly one `run.finished` per run,
   enforced at the SQL layer rather than by goroutine coordination.

## Data (migration 0003, goose embed)

- `run`: `id PK`, `instance_id FK -> instance NOT NULL`, `operation text NOT
  NULL`, `environment text NOT NULL` (stamped at submit — guardrail layer 4),
  `engine_class text NOT NULL`, `playbook_tag text NOT NULL`, `job_id text
  NULL` (set once StartJob returns), `state text NOT NULL CHECK (state IN
  ('queued','running','success','failed','canceled'))`, `reason text NULL`
  (operator-entered ticket ref), `error text NULL`, `artifact_name text NULL`,
  `artifact_size_bytes bigint NULL`, `artifact_checksum text NULL`,
  `submitted_at timestamptz NOT NULL DEFAULT now()`, `started_at/finished_at
  timestamptz NULL`, `updated_at`.
- `audit_event`: `id PK`, `ts timestamptz NOT NULL DEFAULT now()`, `actor text
  NOT NULL`, `action text NOT NULL` (`run.submitted` | `run.finished`),
  `run_id FK -> run NOT NULL`, `instance_id FK -> instance NOT NULL`,
  `environment text NOT NULL`, `playbook_tag text NOT NULL`, `params_digest
  text NOT NULL` (sha256 hex of canonical params JSON), `final_status text
  NULL` (NULL on submitted; run state on finished), `window_warned boolean NOT
  NULL DEFAULT false` (column exists per §5; semantics = WU-023).
  Trigger `audit_event_immutable`: BEFORE UPDATE OR DELETE → RAISE EXCEPTION.
  Plus `REVOKE UPDATE, DELETE ON audit_event FROM portal`.

## Interfaces

- `GET /api/operations` → `{"operations":[{id,label,icon,description,
  duration_hint,online_hint}]}` — catalog for the UI (template/playbook_tag
  stay server-side).
- `POST /api/runs` `{instance, operation, reason?}` → 201 run JSON.
  400 unknown operation / bad body; 404 unknown instance; 502 engine refused
  (StartJob error — audit `submitted` row already written, run finalized
  `failed`). Flow: catalog lookup → instance lookup → ClassForEnv →
  Registry.For (fail closed) → INSERT run (`queued`) + audit `run.submitted`
  in one tx → StartJob → job_id onto run → watcher.
- `GET /api/runs` → `{"runs":[...]}` newest-first, limit 50. Optional
  `?instance=<name>`. Run JSON: `{id, instance, environment, operation, state,
  reason, error, job_id, submitted_at, started_at, finished_at,
  artifact: {name,size_bytes,checksum} | null}`.
- `GET /api/runs/{id}` → run JSON or 404 (WU-013 builds detail on this).
- Registry wiring (main): TWO MockEngine instances — `mock-prod` /
  `mock-nonprod` — one per env class (guardrail layer 3 structural from day
  one; WU-015 asserts distinctness).

## Behavior (testable)

1. Happy path: POST dump on a test instance → 201 `queued`; watcher drives to
   `success`; run has started/finished timestamps + artifact metadata (name,
   size, sha256 from MockEngine); exactly 2 audit events (`run.submitted` with
   final_status NULL, `run.finished` with final_status `success`).
2. Failure path: `params` fail injection (`mock_fail_at`) → run `failed`,
   error non-empty, `run.finished` audit event carries `failed`. (Injection is
   test-only: POST accepts no engine params from the client in MVP.)
3. Guardrail stamps: run + both audit events carry environment and
   playbook_tag; prod instance resolves the prod-class adapter, test/dev the
   nonprod one (JobID prefix proves it).
4. Audit immutability: UPDATE or DELETE on any audit_event row fails (trigger),
   for the app role, even mid-transaction.
5. Unknown instance → 404, no rows written. Unknown operation → 400, no rows.
6. Engine refusal (registry missing class → ErrNoAdapter): 502; run exists,
   finalized `failed`, both audit events present (submitted + finished/failed).
7. Orphan sweep: a run left `running` with no live engine job → on service
   start it becomes `failed` / `engine job lost (portal restart)` + finished
   audit event.
8. List: GET /api/runs returns newest-first; `?instance=` filters; unknown
   instance name filters to empty list (not 404 — it's a filter).

## UI (WU-012 slice)

- MyDatabases: `💾 Backup` button on each card + table row → launch drawer
  (Screen 3 pattern, adapted to `dump`): target block (instance name large +
  EnvBadge), info chips from catalog (`~25 min` · `Database stays online`),
  plain-language description, optional "Reason / ticket #" field, footer
  primary labeled with the consequence (`Run dump on billing-test`) — never
  "OK"/"Submit". No Schedule button (WU-022), no advanced-params accordion
  (dump has none), no typed-name prod ritual yet (WU-015 — one click
  everywhere this WU, mock engine only).
- After POST: drawer flips to a started state (run id + "View in Activity"
  link); no card progress state (WU-013 owns live progress).
- Activity: "Now running" behavior via polling the run list every 3s while any
  run is non-terminal (10s idle); table rows: RunStatus chip (WU-004),
  run id, operation, instance + EnvBadge, submitted, duration ("—" until
  finished). Screen 6 filters/export = WU-014.

## Out of scope / deferred

- Live logs, stage pipeline, follow mode → WU-013 (StreamLogs is ready).
- Failure email → WU-014. Window check/warn → WU-023. Approvals → post-MVP.
- Client-supplied engine params / advanced accordion → when a real op needs it.
- Artifact upload/registry table → WU-030/035 (metadata lives on run for now).
- Cancel/abort endpoint → WU-013 (pairs with the detail page's Abort button).

## Fixture / demo

Hero demo (M1 exit dry run): import fixture → open portal → Backup on
`billing-test` → watch Activity go running→success → artifact metadata on the
run → `psql`: 2 audit_event rows, UPDATE one → error.
