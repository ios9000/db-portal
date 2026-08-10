# SPEC-042 · Concurrency locks — instance TTL locks + self-target ban

> Status: authoritative for WU-042 (Phase 4, M4). Written just-in-time.
> Precedes: `ARCHITECTURE.md` §concurrency ("Locking: same op, same instance,
> two clickers — hierarchical TTL locks (M4)"; "Portal's own DB is NEVER a
> portal target"). Business precedence unchanged (D1–D7).

## Problem

Three launch paths funnel through `runs.Service.Start` — the button
(`POST /api/runs`), the scheduler (`schedule/executor.go`), and each chain step
(`chain/driver.go`, `Internal:true`). Only the scheduler guards overlap, and
only its own: `fire()` skips when a run is already live on the instance
(executor.go:118, an instance-scoped run-table probe). The button and chain
paths are unguarded, so on a 500-instance estate with multiple DBAs two dumps
can load one database at once, or a dump can race a restore — the exact hazard
ARCHITECTURE flagged for M4. This WU makes "at most one live operation per
instance" a **structural** property enforced at the single choke point, plus
two target guardrails the same choke point can cheaply carry.

## Decisions (mini-ADRs)

### 1. A lock **row**, not a pg advisory lock

A `pg_try_advisory_lock` is cheap but **session-scoped** (tied to one pooled
connection), carries no TTL, and leaves no trace — a leaked advisory lock is
invisible and un-auditable. A lock **table** row survives restarts, carries an
`expires_at` for self-heal, names its holder run + actor (auditable), and is
reaped by the same boot sweep that already repairs orphaned runs. Migration
0012 adds `instance_lock` (PK `instance_id`, one row ⇒ one live op per
instance):

```
instance_lock(instance_id PK →instance, run_id →run, actor, acquired_at, expires_at)
```

### 2. Acquire **inside** the run-insert transaction; release **inside** finalize's

`Start` already writes `run` + `audit run.submitted` in one tx before touching
the engine. The lock upsert joins that tx: if it fails to take the lock the tx
**rolls back** — no run, no audit, a clean conflict (the same "nothing created"
posture as the prod-ritual and unknown-instance refusals). If it takes the
lock, run + audit + lock commit together.

`finalize` already writes the terminal `run` UPDATE + `audit run.finished` in
one tx, guarded so only the **winning** finalizer (the one whose UPDATE touched
a row) proceeds past `RowsAffected()==0`. The lock `DELETE` joins that tx, after
the guard — so the lock is released **atomically with the terminal state**, and
a losing finalizer (cancel/sweep/watcher race, SPEC-012 mini-ADR 8) never
touches it. Two consequences fall out for free:

- **Every** terminal path releases the lock, because every terminal path is a
  `finalize` (watcher, engine-refusal, stranded-job repair, cancel-via-watcher,
  `SweepOrphans`). No new release call sites.
- **Crash recovery needs no TTL**: a process that dies mid-run leaves the run
  live *and* its lock held; on reboot `SweepOrphans` finalizes the orphan →
  same tx releases the lock. The instance is free the moment the portal is back
  (and while it is down, nobody can launch anyway). This is the "symmetric with
  SweepOrphans, not a wedge" the WU asks for.

### 3. TTL is a **backstop**, and stealing never harms a live holder

The acquire upserts with a steal predicate:

```sql
INSERT INTO instance_lock (instance_id, run_id, actor, expires_at)
VALUES ($1, $2, $3, now() + make_interval(secs => $4))
ON CONFLICT (instance_id) DO UPDATE
    SET run_id = EXCLUDED.run_id, actor = EXCLUDED.actor,
        acquired_at = now(), expires_at = EXCLUDED.expires_at
    WHERE instance_lock.expires_at < now()
      AND NOT EXISTS (SELECT 1 FROM run r
          WHERE r.id = instance_lock.run_id AND r.state IN ('queued','running'))
RETURNING run_id;
```

- Fresh instance → plain INSERT succeeds → holder.
- Held by a **live** run → `NOT EXISTS(live holder)` is false → no steal → 0
  rows → `RETURNING` empty (`pgx.ErrNoRows`) → **conflict**. The `expires_at`
  clause is irrelevant here: **a lock whose holder run is still live is never
  stolen, regardless of TTL** — so a misconfigured-short TTL can never cause two
  concurrent ops. This is the load-bearing safety property.
- Held by a **dead** holder (run terminal/gone) **and** expired → steal
  (reap). This is the TTL's only job: reclaim a lock whose `finalize` release
  somehow didn't run (near-impossible given mini-ADR 2's same-tx release, so
  the backstop rarely fires) or whose run row was deleted.

`PORTAL_LOCK_TTL` (default `30m`) sizes the backstop. Because the live-holder
guard prevents any live-steal, the default is safe irrespective of run
duration; it only bounds how long a genuinely-leaked lock lingers. Concurrent
`Start`s serialize on the `instance_id` PK (Postgres row lock on conflict), so
exactly one of N racers commits the fresh INSERT and the rest see the committed
row and conflict — the AC's "1 success + N-1 conflict", race-free.

Lock **renewal / heartbeat** (TTL independent of run duration) is deliberately
out of scope — the same-tx release + boot sweep + live-holder-guard already make
the lock correct; renewal is a post-MVP refinement noted in Open Questions.

### 4. The scheduler's probe stays as a cheap early-out; the lock is the authority

`executor.fire()` keeps its run-table overlap probe (unchanged skip-visibly
semantics, and it avoids a doomed run-insert+rollback for the common "already
busy" case), and additionally maps `runs.ErrInstanceLocked` from `Start` to the
same `statusSkipped` stamp. The probe has a TOCTOU window (a button press
between the probe and the fire's `Start`); the lock closes it atomically. So the
existing behavior is **preserved** and the race is **subsumed** — the lock is
the real guarantee, the probe an optimization. No scheduler test changes.

### 5. Self-target ban (research gotcha #2) — config-declared, audited denial

The portal's own DB must never be a portal target (self-upgrade deadlock —
ARCHITECTURE §concurrency). The inventory schema carries **no connection tuple**
(host/port/dbname) — an instance is only a name, and the name→host mapping lives
engine-side (ADR-004) — so the portal cannot *auto-detect* that an inventory row
resolves to its own DB. The honest mechanism is a **declared protected set**:
`PORTAL_PROTECTED_INSTANCES` (comma-separated, case-insensitive), seeded with
the portal's own `PORTAL_DB_NAME` so an instance literally named the same as the
portal DB is refused out of the box. Enforced at both assemblers:

- `runs.Start` (covers button + schedule + any direct chain step) → refuse
  `ErrSelfTarget` before the prod ritual / any row.
- `chain.Create` (covers the restore path, so `POST /api/restore` gets the
  distinct error **synchronously**, not an async halt) → refuse `ErrSelfTarget`.

The denial is recorded on **auth_event** as `guardrail.denied` (the security
ledger, beside `authz.denied`) — a refused target never becomes a run, so it
does not belong on the run-centric `audit_event`. 0012 extends the
`auth_event_action_check` (the 0006 pattern).

### 6. Naive-Patroni-restore block (research gotcha #1, partial)

The inventory has no primary/replica flag — only `cluster.platform`
(`k8s_patroni` | `vm`). ARCHITECTURE §7 says a restore into a Patroni-managed
cluster needs pause/detach → restore → reinit sequencing the portal does not
implement; a naive `pg_restore` behind Patroni's back diverges the cluster.
Dumps are safe (even desirable from a replica), so the block is narrow:
**`chain.Create` refuses a chain containing a `restore` step when the target's
cluster platform is `k8s_patroni`** (`ErrPatroniRestore`, audited
`guardrail.denied`). Full leader/replica sequencing stays post-MVP; recorded in
DECISIONS.md. (The golden flow's restore targets `crm-test`/`hr-test`, both
`vm` — unaffected.)

**Amended by WU-048 (m4-gate finding 1, CRITICAL):** this mini-ADR originally
said "`chain.Create` is the sole gate — no `runs.Start` change". That was
wrong: the check read platform at *creation* time only, and an ordinary
re-import can re-platform the target `vm → k8s_patroni` between create and a
step's (re-)fire (`inventory.upsertInstance` UPDATEs `cluster_id` in place) —
a resumed chain then ran `pg_restore` behind Patroni's back. Since WU-048,
**`runs.Start` re-validates the block with a FRESH platform read at every
fire** (the same per-fire treatment as the self-target ban and the prod
ritual), so every fire path — create-drive, Resume, mid-chain — inherits it;
a refused fire halts the chain visibly (mail, resumable) with a
`patroni-restore` denial on auth_event. `chain.Create`'s check remains as the
synchronous front-door 403. Dumps/verify (incl. the chain's safety dump) stay
allowed on Patroni.

## HTTP mapping

| error | status | where |
|---|---|---|
| `ErrInstanceLocked` | **409 Conflict** | `POST /api/runs` (button) |
| `ErrSelfTarget` | **403 Forbidden** | `POST /api/runs`, `POST /api/restore` |
| `ErrPatroniRestore` | **403 Forbidden** | `POST /api/restore` |

The restore/chain path takes the lock **asynchronously** (its steps fire from
the driver goroutine after `Create` returns 201), so `ErrInstanceLocked` never
surfaces to the restore handler — a locked target simply halts the chain at its
first step, visibly, with the standard halt mail. `ErrSelfTarget` /
`ErrPatroniRestore` are refused synchronously in `Create`, before the chain row.

## Acceptance (mirrors the BACKLOG WU-042 ACs)

1. N goroutines `Start` one instance → exactly one proceeds, N-1 get
   `ErrInstanceLocked` (button 409; scheduler → skip; chain step → halt).
2. Lock releases on terminal finalize; a dead+expired holder is reaped on the
   next acquire (tested by planting an expired lock over a terminal run).
3. Self-target op refused `ErrSelfTarget` on button and restore paths; an
   `auth_event guardrail.denied` row is written.
4. Patroni-restore refused `ErrPatroniRestore`; guardrails/ritual/audit and the
   golden flow unchanged and green.

## Tests

- `runs`: lock acquire/release primitive under N-goroutine contention (1 win,
  N-1 conflict) on a scratch DB; TTL reap (planted expired lock stolen);
  release-on-finalize (button, engine-refusal, cancel, sweep); self-target
  refusal + `guardrail.denied` row. Existing Start/List tests unchanged (they
  `waitTerminal` between same-instance Starts → finalize releases first).
- `chain`: self-target + Patroni-restore refusal at `Create` (no chain row,
  denial audited); an existing `vm` restore chain still creates. WU-048:
  re-platform between create and fire — via Resume
  (`TestRePlatformHaltsResumedRestore`) and mid-chain
  (`TestRePlatformMidChainBlocksRestore`) — halts at the restore step with the
  denial audited; verify + safety_dump still succeed on the Patroni target.
- `runs` (WU-048): `TestPatroniRestoreRefusedAtStart` — fire-time refusal, no
  run row, denial row; verify/safety_dump on Patroni + restore on vm allowed.
- `schedule`: `ErrInstanceLocked` → `statusSkipped` (probe-independent path).
- `server`: 409 / 403 mappings on `/api/runs` + `/api/restore`.
- Golden flow: green as-is (the restore + chain beats target `vm` instances,
  and every beat serializes its instance).

## Open questions / post-MVP

- Lock **renewal/heartbeat** so TTL is independent of run duration (not needed
  for correctness here — see mini-ADR 3).
- Full **Patroni-aware** leader/replica restore sequencing (DECISIONS.md).
- Cross-**resource** locking (e.g. a whole cluster) — this WU is per-instance,
  matching the stated hazard.
