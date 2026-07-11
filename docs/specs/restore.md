# SPEC-031 · Restore workflow (MockEngine)

> Groomed 2026-07-11 for WU-031 (Phase 3, after WU-030 + WU-032). Authority
> chain: D4 (catalog = Backup + Restore) + D5 (chains halt/notify/resume) →
> ARCHITECTURE §3 (Restore) → this file. Rides SPEC-030 (artifact registry =
> the restore source of truth) and SPEC-032 (chain engine = the mechanism).
> The motivating incident (D1): a dump was skipped before a critical op — this
> WU institutionalizes the missing step as an UNCONDITIONAL pre-restore safety
> dump that no code path can skip.

## Scope

Restore a registered artifact onto an EXPLICIT target, as a portal-assembled
`kind=restore` chain (SPEC-032) of three steps fired through `runs.Service.Start`
— the identical guardrail + audit path as the button and the scheduler:

1. **verify** — checksum-verify the source artifact (MockEngine: injectable
   failure; real bytes verified in WU-036 too).
2. **safety_dump** — the UNCONDITIONAL pre-restore dump of the TARGET,
   registering with `retention_class = 'safety'`.
3. **restore** — `pg_restore` of the artifact onto the target.

This WU: the `verify` / `safety_dump` / `restore` catalog operations, the
`'safety'` retention plumbing through `finalize`, `POST /api/restore` (the first
chain assembler), the restore-recipe unit, the golden-flow restore beat, and the
Restore drawer (UI slice). Explicitly NOT here: real playbooks / real bytes /
real checksum verification (WU-036), object storage (WU-035), Patroni-aware
sequencing (ARCHITECTURE §7, iceboxed), restore of a partial/PITR (out of MVP),
retention ENFORCEMENT of `'safety'` artifacts (M4/icebox). **No migration** —
0009 already carries `artifact.retention_class` with its CHECK; every other
piece is Go data, service logic, or an HTTP seam.

## Mini-ADRs (agent decisions, revisitable)

1. **Verify is its own chain step** — NOT a param the restore job re-checks
   engine-side (the STATE-named open question, resolved). As step 1, a bad
   artifact halts the chain BEFORE the safety dump (step 2) and the restore
   (step 3): the target is never dumped or restored. Folding verify into the
   restore job would take the safety dump first and only then discover the
   mismatch — a wasted safety artifact for a restore that can never proceed,
   and the wrong order for the incident this WU exists to kill. Verify as a
   step is also RESUMABLE: fix the artifact, resume re-fires the verify step
   (SPEC-032 mini-ADR 3). Pre-O-1 there are no portal-readable bytes, so the
   MockEngine "verifies" with `mock_fail_at` standing in for a checksum
   mismatch; WU-036's real restore playbook verifies the bytes again before
   touching the target (defense in depth). The verify run is filed under the
   target (the chain's single instance) but performs NO target mutation and
   produces NO artifact — so "verify-fail leaves the target untouched" means
   no `safety_dump` and no `restore` run exists; the only target run is the
   read-only failed verify.

2. **Retention class rides the catalog operation; `finalize` stamps it — no
   schema change.** SPEC-030 mini-ADR 2 deferred the `'safety'` plumbing and
   fixed that class is "a property of the launch's intent, stamped at
   registration." So the safety dump is a DISTINCT operation `safety_dump`
   (Template `dump`, `RetentionClass: "safety"`), exactly parallel to how
   `Template` and `PlaybookTag` are static engine properties of an operation.
   On registering an artifact, `finalize` reads the run's `operation` and
   stamps `artifact.retention_class` from the catalog (empty → `'standard'`).
   No `StartRequest` field, no `run`/`chain_step` column, no migration. The
   registry INSERT already lives inside the WU-016 guarded terminal tx, so a
   losing finalizer still never reaches it (SPEC-030 mini-ADR 3 holds).
   Consequence, and a feature: `safety_dump ≠ dump`, so the WU-011R
   `last_backup_at` query (`operation = 'dump'`) does NOT count a pre-restore
   snapshot as the target's backup — correct, since a safety dump is not the
   user's backup regime and its `'safety'` class marks it for aggressive future
   pruning.

3. **Internal (non-launchable) operations + a single launchable gate in
   `runs.Start`.** `verify`, `safety_dump`, `restore` are catalog operations
   but not INDEPENDENTLY launchable. `catalog.All()` (the `/api/operations`
   feed and the launch drawer) returns only launchable ops; `catalog.ByID`
   still finds every op (Start/chain/schedule validation). `runs.Start`
   refuses a non-launchable op unless `StartRequest.Internal` is set — a flag
   ONLY the chain driver sets. So POST /api/runs and the scheduler (both leave
   `Internal` false) reject `restore`/`verify`/`safety_dump` at the door
   (`ErrUnknownOperation` → 400), while chain steps fire them. This is the
   structural half of AC-4: the only way to run a restore is the assembled
   chain, which always contains the unconditional safety dump. A single choke
   point in Start (not a check duplicated across every user-facing entry) is
   what keeps the invariant from rotting as new entry points appear.

4. **Restore is a client POST that assembles a chain; the recipe is a pure
   function.** `POST /api/restore {artifact_id, target, confirm, reason}`
   (dba-gated, body-capped) is SPEC-032's promised first chain assembler — no
   generic client chain-create API. The handler loads the artifact from the
   registry (SPEC-030 is the source of truth; unknown id → 404), builds the
   fixed 3-step recipe via `restore.Steps(...)` — a pure, unit-tested function
   that ALWAYS returns `[verify, safety_dump, restore]` in order, so "no skip
   affordance" is structural, not a runtime check — and calls
   `chain.Service.Create` (kind `restore`, instance = target). `chain.Create`
   enforces the prod ritual on the target (`Confirm == target name`) before
   inserting, so a prod-target restore without the typed name is 400 with NO
   chain row. Response 201 = the chain read model, so the UI navigates to the
   chain view.

5. **Explicit target, default non-prod; no same-instance special-casing.** The
   target is an explicit field; the drawer defaults it to a non-prod instance.
   Restoring an artifact onto its own origin instance (rollback) and onto a
   different one (clone) are both allowed and mechanically identical. The
   portal-self-target ban (research gotcha #2) and cross-version/cluster
   compatibility stay out (no real targets until WU-034+; a real playbook
   concern for WU-036).

## Catalog (Go data — internal/catalog)

`Operation` gains two fields: `Launchable bool` and `RetentionClass string`.
`All()` now returns only launchable operations (the user-facing catalog);
`ByID` is unchanged (finds all, for Start/chain/schedule validation).

| id | Launchable | Template | RetentionClass | notes |
|----|-----------|----------|----------------|-------|
| `dump` | true | `dump` | `standard` | unchanged; the only run-now / schedulable op |
| `safety_dump` | false | `dump` | `safety` | chain step 2; same dump playbook, safety intent |
| `verify` | false | `verify` | — (no artifact) | chain step 1; read-only, injectable failure |
| `restore` | false | `restore` | — (no artifact) | chain step 3 |

MockEngine (`internal/engine/mock.go`) gains `verify` to `scriptFor` and its
`restore` script is trimmed to restore-only lines (the safety dump is now its
own step, not folded into the restore job's log). Only Template `dump` produces
an `Artifact`, so `verify`/`restore` runs never register — unchanged.

## Interfaces

- `catalog.Operation{…, Launchable bool, RetentionClass string}`;
  `catalog.All()` = launchable only; `catalog.ByID` = all.
- `runs.StartRequest` gains `Internal bool` (chain driver sets true). `Start`:
  `op, _ := catalog.ByID(req.Operation); if !op.Launchable && !req.Internal →
  ErrUnknownOperation`. Nothing else in Start changes.
- `runs.Service.finalize`: on a successful terminal transition with an
  artifact, `SELECT operation FROM run WHERE id = $1`, derive the class via
  `catalog.ByID`, and INSERT `retention_class` accordingly (default
  `'standard'`). The guarded UPDATE stays an `Exec` (WU-016 double-finalize
  guard untouched).
- `runs.Service.GetArtifact(ctx, id) (RegisteredArtifact, error)` — by-id
  registry read; `ErrArtifactNotFound` for an unknown id. (`ListArtifacts`
  stays the per-instance feed.)
- `internal/restore.Steps(artifactID int64, checksum, name string)
  []chain.StepSpec` — the pure recipe: verify + safety_dump + restore, the
  artifact lineage (`artifact_id`, `checksum`, `artifact_name`) in the verify
  and restore step params only (ids + checksums, never secrets — SPEC-032).
  `restore.Kind = "restore"`.
- Chain driver (`internal/chain/driver.go`) sets `Internal: true` on every
  step's `StartRequest` (launchable steps are unaffected; internal steps
  require it).
- HTTP `POST /api/restore` (dba, body-capped) `{artifact_id, target, confirm,
  reason}` → 201 `chain.Chain`. Errors: 400 missing `artifact_id`/`target`,
  400 reason too long, 404 unknown artifact, 404 unknown target instance, 400
  prod target unconfirmed. Server `ChainService` gains
  `Create(ctx, chain.CreateRequest) (chain.Chain, error)`; `ArtifactReader`
  gains `GetArtifact`.

## Behavior (testable)

1. **Recipe (pure):** `restore.Steps` returns exactly `[verify, safety_dump,
   restore]` in order; the verify and restore steps carry the artifact
   lineage, the safety_dump step carries none; there is no arrangement of
   inputs that omits `safety_dump`.
2. **Happy restore, end to end (golden-flow beat):** POST /api/restore of a
   registered artifact onto a non-prod target assembles a running chain; it
   drives to `success` with all three step runs fully audited (`chain:<actor>`,
   env stamped). The safety_dump step registers ONE artifact for the target
   with `retention_class = 'safety'`; the restore step's params reference the
   source artifact id + checksum (lineage tied). No prod ritual on non-prod.
3. **Prod-target ritual:** POST /api/restore to a prod target with no/ wrong
   `confirm` → 400, NO chain row created, zero runs. With `confirm` == target
   name → the chain is created (one click stays for non-prod).
4. **Injected verify failure:** a restore chain whose verify step fails halts
   at step 1 — `safety_dump` and `restore` never fire (no run of either
   operation on the target), exactly ONE chain halt mail, chain resumable;
   after "fixing" the artifact, resume carries it to `success`.
5. **No bare restore path:** POST /api/runs `{operation:"restore"}` (or
   `verify`/`safety_dump`) → 400 unknown operation; likewise POST
   /api/schedules; `/api/operations` lists only `dump`. Grep: `restore`/
   `safety_dump`/`verify` fire only via the chain assembler.
6. **Retention plumbing:** a `safety_dump` run registers `'safety'`; a `dump`
   run still registers `'standard'`; `verify`/`restore` register nothing.
   `last_backup_at` ignores safety dumps.

## Guardrails & audit

All four guardrail layers arrive via `runs.Start` per step (env banner is the
UI slice's job); the prod ritual is enforced once at chain creation on the
target and fired verbatim on every step, resume included (SPEC-032 mini-ADR 3).
No new audit actions — every step is a run, audited by Start/finalize; the
chain row is the chain-level record. Mail content stays D7 (who/what/where/
status + link): the halt mail is the chain's (SPEC-032 mini-ADR 5). Step params
carry only artifact ids + checksums — no secrets, no bytes. `POST /api/restore`
is body-capped and reason-length-capped like every mutation (m1-gate item 15).

## UI slice (checkpoint boundary — Sonnet-brief candidate)

Restore drawer, reached from an instance's context (My Databases card / detail):
pick a source artifact (`GET /api/artifacts?instance=` — the origin instance's
registry, newest first, with checksum + size + class), pick an explicit target
(default a non-prod instance), env banner for the TARGET's env + the typed-name
prod ritual (reuse `EnvBanner` + the LaunchDrawer confirm pattern), a primary
button that names the consequence, and on submit → `POST /api/restore` → the
chain view (RunDetail's chain strip already renders steps + Resume, WU-032).
The safety dump is shown as an unconditional, non-optional step in the drawer's
preview — never a checkbox. `api.ts` gains `fetchArtifacts(instance)` and
`startRestore({artifactId, target, confirm, reason})`.

## Out of scope / deferred

- Real restore playbook + bytes + real checksum verify → WU-036.
- Object storage / `location` → WU-034/035.
- Patroni-aware restore sequencing → ARCHITECTURE §7 (iceboxed).
- `'safety'` retention ENFORCEMENT (prune job) → icebox (M4).
- Portal-self-target ban → icebox (real targets first).
- PITR / partial restore → out of MVP (D4 is full restore).

## Open questions

None blocking. The `'safety'` plumbing question SPEC-030 left open is resolved
by mini-ADR 2 (catalog-carried, finalize-stamped); the verify-step question
STATE named is resolved by mini-ADR 1 (its own step). Owner: agent.
