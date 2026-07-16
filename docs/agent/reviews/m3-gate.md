# M3-gate review — findings record (2026-07-16, s23)

Method: the M1-lesson light shape (the cost-sensitivity agreement), same as M2:
workflow `wf_49ca1969-37a` ran **5 independent Sonnet 5 reviewers**, one per
dimension (chain-lifecycle, restore-safety, engine-semaphore, playbooks-secrets,
artifacts-frontend-specs), each briefed with the SPEC-030…036 summaries, the nine
ground rules that must hold now, and the deliberate-deferral list. **No verifier
agents** — every finding below was verified inline by the architect against the
cited code before it entered this record. Cost: 605,044 subagent tokens / 205 tool
calls / ~19.4 min (M1's original design was ~50 agents; this was 5).

Result: 8 findings raised, **8 confirmed, 0 refuted**, 4 re-graded DOWN in severity
during inline verification (one reviewer's stated *mechanism* was wrong while its
outcome was right — noted in finding 3). **No criticals.** Verdict: **GATE PASSES
with fix WUs** — same protocol as M1/M2: WU-037/038/039 land before any Phase-4 WU.

## What held

Every guardrail invariant survived. No reviewer found a path to restore-without-
safety-dump, a bare restore reachable over `POST /api/runs` or the scheduler, a
forged webhook forcing an outcome, poll-vs-webhook truth inversion, `location`
crossing the engine seam, a chain bypassing `runs.Service.Start`, an audit-trail
write outside the guarded transition, or a secret reachable from the repo / portal
DB / params / logs. Those were the nine rules each reviewer was briefed to attack
by name; all five came back clean on them. The M3 exit evidence (docs/demo-m3.md)
stands.

## The theme: Mock-to-real regressions in transient-error handling

Both HIGHs are the same root cause in two places, and it is the class of defect
this milestone was structurally at risk of: **an assumption that was TRUE under
MockEngine and silently stopped being true when a real engine landed behind the
unchanged seam.** Neither is reachable from the demo-m3 rehearsal — the happy path
and the deliberate-failure path both work correctly. They need a *transient* fault
(a dropped connection, a proxy 502, a DB failover) to appear, which is exactly what
a compose stack on one VM never produces and a ~500-instance estate produces
routinely. → **WU-037 fixes both together.**

## Confirmed — HIGH

1. **`watch()` finalizes a run permanently FAILED on ANY `Status()` error, not just
   `ErrUnknownJob`** — `backend/internal/runs/service.go:267-272`.
   The poll loop every live run rides does `st, err := adapter.Status(...)`; on any
   non-nil `err` it calls `finalizeLogged(..., StateFailed, "engine lost the job: "+
   err.Error(), ...)` and returns. Because `finalize` uses the WU-016 guarded
   terminal UPDATE, that outcome is **permanent and uncorrectable**.
   This was correct under MockEngine: `MockEngine.Status` (internal/engine/mock.go:92)
   only ever returns what `e.job(id)` returns, and `job()` can only fail with
   `ErrUnknownJob` (mock.go:151-157) — so "any error" and "job lost" were literally
   the same set. WU-033 made `Status()` a real HTTP call and the blanket handling
   was never revisited. `SemaphoreAdapter.Status` (semaphore.go:132-136) wraps its
   error in `asUnknownJob`, which converts **only** HTTP 404/400 into `ErrUnknownJob`
   (semaphore.go:354-363); everything else passes through verbatim —
   connection-refused from `a.http.Do` (semaphore.go:322-325), any 5xx `httpError`
   (semaphore.go:328-331), a context timeout, or a JSON decode failure
   (semaphore.go:333-335). `watch()` never inspects the error type, so all of them
   become a permanent FAILED whose message ("engine lost the job") is simply false.
   Note the asymmetry **inside the same function**: the state-mirror UPDATE 15 lines
   below (service.go:285-287) treats a DB error as retryable — `case err != nil:` logs
   and falls through to `time.Sleep` and loops. The Status call gets no such tolerance.
   **Verified:** mock's error set (mock.go:92-99 → job() → ErrUnknownJob only);
   `asUnknownJob`'s 404/400-only conversion; `do()`'s four distinct non-404 error
   surfaces; watch()'s type-blind branch.
   **Reviewer's mechanism CORRECTED:** the finding's title claims this "enables a
   concurrent second restore" by defeating an overlap probe. That is wrong — **there
   is no overlap probe on the restore path at all.** `runs.Start` has no instance-busy
   gate, `chain.Create` has none, `restore_http.go` has none; the only probe is the
   scheduler's (schedule/executor.go:121), which guards *scheduled* fires. Two
   concurrent restores are already reachable today by pressing Restore twice, and
   instance TTL locking is explicitly M4 roadmap scope — a known deferral, not a gate
   finding. **The outcome is nonetheless real by a different road:** the false failure
   halts the chain → the product mails the DBA and offers Resume → Resume re-fires
   `pg_restore` while the first one is *genuinely still running* on the target. The
   product's own advertised recovery affordance is the second-restore vector.
   Severity HIGH upheld (a false permanent failure on a mainline flow, plus a lying
   error message, plus a resume-into-live-restore path).
   → **WU-037**

2. **Chain driver permanently wedges a chain at `state='running'` on any transient DB
   error — no self-heal short of a full process restart** —
   `backend/internal/chain/driver.go:50-54` and `:108-112`.
   Both of `drive()`'s polling loops treat any error from a plain read as fatal to the
   goroutine: `s.next()`'s lookup and the inner step-watch's `s.runs.Get` each do
   `s.log.Error(...); return`, killing the driver with the chain row left `running`.
   This is **deliberate and documented** (driver.go:31-33: "DB errors exit the loop and
   leave the chain `running` for the boot sweep to repair; there is no safe in-process
   retry that can't also fail") — so this finding challenges the *rationale*, not an
   oversight. The rationale does not hold, on three verified legs:
   - **The sweep is boot-only.** `chain.SweepOrphans` is called from exactly one place,
     `cmd/portal/main.go:128`, at startup. No ticker exists in main.go. The triggering
     scenario is precisely one where the process does **not** crash (a single dropped
     conn, a brief failover, a statement_timeout, or the pgx pool recycling a stale conn
     during one of the driver's frequent polls across a multi-minute restore chain), so
     nothing ever runs the repair.
   - **No recovery path.** `Resume` flips `WHERE id = $1 AND state = 'halted'`
     (chain.go:224-226); a wedged `running` chain affects 0 rows → `ErrNotResumable` →
     409, forever. There is no cancel-chain or force-halt API (chains_http.go exposes
     only GET `.../chain` and POST `.../resume`).
   - **No alert.** `haltLogged`/`notifyHalted` is never reached on this path, so zero
     mail. The chain simply shows `running` in the UI indefinitely.
   The only remedy is bouncing the whole portal binary (which disturbs every other
   in-flight run and chain), and that works only incidentally, because SweepOrphans
   happens to run again at the next boot.
   **Verified:** the two `return`-on-error sites; SweepOrphans call sites (boot-only,
   no ticker); Resume's `state = 'halted'` guard (and its own comment at chain.go:222
   confirming a running chain gets ErrNotResumable); the absent notify on this path.
   → **WU-037**

## Confirmed — MEDIUM

3. **`schedule.Create` doesn't gate non-launchable operations — SPEC-031's documented
   "likewise POST /api/schedules" rejection is false** —
   `backend/internal/schedule/schedule.go:123`.
   `runs.Start`'s launchable gate (`runs/service.go:126`: `if !op.Launchable &&
   !req.Internal`) is the single choke point keeping verify/safety_dump/restore
   unreachable outside the chain driver. `schedule.Create` has a *separate* check that
   tests catalog **existence only**: `if _, ok := catalog.ByID(req.Operation); !ok`.
   `catalog.ByID` deliberately finds non-launchable ops — that is how the chain driver
   reaches them (`catalog.go:73-76`: "Internal chain-step operations are reachable only
   through ByID"; only `All()` filters on `Launchable`). So `restore` sails through.
   SPEC-031 behavior 5 (docs/specs/restore.md:172-175) states: "POST /api/runs
   {operation:\"restore\"} (or verify/safety_dump) → 400 unknown operation; **likewise
   POST /api/schedules**". The second half is not implemented.
   **The guardrail itself HOLDS** — this is not a restore-without-safety-dump bypass.
   `executor.fire()` (schedule/executor.go:148) never sets `Internal`, so `runs.Start`
   rejects the fire. The damage is a **permanently, silently broken schedule**: the
   POST returns 201 where the spec promises 400, and thereafter every cron tick hits
   `fire()`'s `default:` branch (executor.go:166-168) → logs + stamps
   `last_fire_status='error'`, no run row, no chain, **no mail** (only the `ErrEngine`
   branch mails), while `next_fire_at` advances to retry forever. The sole operator
   signal is a column on the schedules list.
   Untested: `schedule_test.go`'s `TestCreateValidation` only exercises
   `operation="explode"` — an id absent from the catalog entirely — so it never covers
   the ids that DO exist and slip through.
   **Verified:** schedule.go:123's existence-only check; catalog.go's ByID/All split;
   restore.md:172-175's text; executor.go:148 (no Internal) and :166-168 (silent
   default branch).
   → **WU-038**

4. **`restore.yml`'s fetch uses `creates:` on a deterministic path — a stale/partial
   file silently blocks re-fetch and produces a FALSE "tampered artifact" diagnosis** —
   `playbooks/restore.yml:65`.
   The fetch task sets `creates: "{{ staging_path }}"`, and `staging_path` is
   `"{{ staging_dir }}/restore-{{ artifact_name | default('artifact') }}"`
   (restore.yml:36) — fully deterministic per artifact, on the **persistent shared
   `/artifacts` volume**. If a fetch is interrupted (task timeout, runner restart,
   killed container) it leaves a partial file. On the next attempt `creates:` sees the
   path exists and **skips the fetch entirely**; the play then hashes the *partial*
   file, the sha256 comparison fails, and the operator is told
   "checksum mismatch for X — fetched sha256 … != expected …" (restore.yml:89).
   The failure is worst on the product's own advertised recovery path: a chain halts,
   the DBA presses Resume, and the portal reports **their good backup is corrupt** when
   the real problem is a leftover temp file. In a restore incident, teaching a DBA to
   distrust a valid backup is a serious operational harm — this is the reason it stays
   MEDIUM rather than dropping to LOW despite needing an interruption to trigger.
   **Verified:** the `creates:` line; staging_path's determinism; the shared-volume
   persistence (STATE: "the artifacts volume is STAGING ONLY now"); the mismatch
   message's wording.
   → **WU-039**

## Confirmed — LOW (re-graded down during verification; bundled)

> All four are real and cheaply fixable, but each needs a dev-only trigger or has no
> consumer yet. M1 precedent: the low bundle (items 10-13) rode one S-sized WU.

5. **`parseResultLine` validates `Name` but not `SHA256`/`SizeBytes`** —
   `backend/internal/engine/semaphore.go:209-212` *(reviewer said MEDIUM)*.
   The parser rejects `r.Name == ""` and then returns
   `&Artifact{... Checksum: r.SHA256 ...}` unchecked, so a result line carrying a name
   but no sha256 registers an artifact with an **empty checksum**. The inconsistency is
   the defect: the function exists precisely to not trust the playbook's line, and it
   half-trusts it.
   **Re-graded MEDIUM → LOW:** such an artifact is harmless because `verify.yml`
   fail-closes on it — its preflight asserts `(checksum | default('')) | length > 0`
   (verify.yml:37), so a restore of an empty-checksum artifact dies locally with **zero
   target contact**. The impact is a dead registry row that can never be restored, not
   a dangerous restore. Reachable only by editing dump.yml (which always emits sha256).
   → **WU-040**

6. **`0009` backfill re-derives `retention_class` as `'standard'` for everything,
   losing `'safety'` on a down+up walk** —
   `backend/internal/db/migrations/0009_artifact_registry.sql:26-34` *(reviewer said MEDIUM)*.
   The backfill's INSERT column list is `(run_id, name, size_bytes, checksum,
   created_at)` — `retention_class` is omitted, so every backfilled row takes the column
   DEFAULT `'standard'`. Correct when authored (WU-030 predates safety dumps entirely);
   stale since WU-031. A `goose down` to 0008 (dropping `artifact`) followed by `up`
   now silently reclassifies every `'safety'` artifact as `'standard'`.
   **Re-graded MEDIUM → LOW:** a fresh-clone forward replay is unaffected (the run table
   is empty when 0009 runs, so the backfill inserts nothing, and finalize stamps the
   class correctly thereafter). The only trigger is a **down-migration walk on a DB that
   already holds safety artifacts** — a dev/test maintenance action; prod never runs
   `goose down`. And there is no consumer: retention is stored classification only, with
   no enforcement until M4. Fix is cheap: derive the class in the backfill by joining
   `run.operation` (`'safety'` when `operation = 'safety_dump'`).
   → **WU-040**

7. **`job_id` has no uniqueness constraint; JobID durability is assumed absolute** —
   `backend/internal/db/migrations/0003_runs_audit.sql:14` *(reviewer said MEDIUM)*.
   The column is a bare `job_id text`. If a Semaphore-side task-id reset happened while
   a run was still in-flight, `ReconcileByJobID` could attribute an unrelated later
   task's status/artifact to the stale run.
   **Re-graded MEDIUM → LOW:** this requires wiping Semaphore's BoltDB (`docker compose
   down -v`) **while a run is in-flight** *and* the new task reusing the exact id *and*
   the stale run still being non-terminal. That is a dev-stack scenario; a real
   Semaphore is not wiped under a live portal, and nothing in M3 claims to survive an
   engine wipe. Worth the cheap constraint anyway (PG allows multiple NULLs, so queued
   runs are unaffected).
   → **WU-040**

8. **Staging cleanup is the last task, so any failure after `pg_dump` orphans the real
   dump bytes on the shared volume forever** — `playbooks/dump.yml:83-92`
   *(reviewer said MEDIUM)*.
   The `mc cp` upload deliberately aborts the play on non-zero exit ("a dump that isn't
   stored must never reach the emit step" — correct, and the WU-035 AC pins it), but the
   best-effort staging `rm` sits *after* it. So every failed upload/stat leaves a real
   dump file on `/artifacts` with nothing to reap it.
   **Re-graded MEDIUM → LOW and routed to M4:** unbounded growth on the runner volume is
   real, but retention/GC is explicitly M4 scope ("retention job (1y audit)" +
   hardening), and STATE already records pre-WU-035 leftovers as known-harmless. Trivial
   fix when M4 touches it: wrap the post-dump tasks in `block:` with an `always:` doing
   `file: state=absent`.
   → **M4 grooming** (noted here so it is not rediscovered)

## Ledger

| # | Severity | Dimension | Fix |
|---|----------|-----------|-----|
| 1 | HIGH | engine-semaphore | WU-037 |
| 2 | HIGH | chain-lifecycle | WU-037 |
| 3 | MEDIUM | restore-safety | WU-038 |
| 4 | MEDIUM | playbooks-secrets | WU-039 |
| 5 | LOW | artifacts-frontend-specs | WU-040 |
| 6 | LOW | artifacts-frontend-specs | WU-040 |
| 7 | LOW | engine-semaphore | WU-040 |
| 8 | LOW | playbooks-secrets | M4 grooming |

Raised 8 · confirmed 8 · refuted 0 · re-graded down 4 · criticals 0.
