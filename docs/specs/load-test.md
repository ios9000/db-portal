# SPEC-043 · Load test — concurrent mock dumps

> Status: authoritative for WU-043 (Phase 4, M4). Written just-in-time.
> Validates WU-042 (SPEC-042 instance locks) + WU-016 (single-finalizer) at
> the scale ROADMAP M4 names ("load test — 25–50 concurrent dumps"). Business
> precedence unchanged (D1–D7). MockEngine only (ADR-002).

## Problem

WU-042 made "at most one live operation per instance" a structural property at
`runs.Service.Start`, and WU-016 made finalize exactly-once. Both were argued
correct and unit-tested at small N (the lock test races 8 goroutines). The
ROADMAP M4 exit list calls for a *load test* — 25–50 concurrent dumps — as the
proof those invariants hold under real contention on a 500-instance estate, and
as the place any load-only weakness (DB-pool sizing, the scheduler
boot-stampede) surfaces before a pilot. This WU builds the harness, runs it
clean, records numbers, and files every load-only finding with a verdict. It is
not a bug-fix WU: findings get fixed only if they threaten the pilot.

## Decisions (mini-ADRs)

### 1. The harness is a skip-gated Go test, not a separate binary

The invariants under test are internal (`runs.Service` + the `instance_lock`
row + the audit trail), so the harness drives the **real** `runs.Service` over
**MockEngine** against a **scratch DB** via `testutil.MigratedDB` — the same
seam every runs test uses. That makes it part of `go test -race ./...` (the
gate) with zero new lifecycle: it runs on the VM (dev Postgres up) and *skips*
in CI (no Postgres), exactly like the Semaphore itest. A standalone
`portal loadtest` binary was rejected — it would duplicate the service wiring,
escape `-race`, and need its own teardown. The full-scale run (500-instance
seed, 50 concurrent) is a **live drill** of the same code paths on the VM, its
numbers pasted into the journal; the gate test stays fast (≈80-instance seed).

### 2. The estate comes from the WU-041 seed, not the 8-row fixture

25–50 *distinct* instances need more than the 8-row `instances.csv`. The
harness seeds a realistic estate with `inventory.GenerateEstate(n, seed)` fed
through the existing `inventory.Import` (SPEC-041) — the same generator the
pilot cold-start uses — so contention is measured against realistic names,
clusters, and the prod/test/dev mix. Draw dump targets from the imported rows.

### 3. Parallelism is proven from committed timestamps, not by sampling

"Cross-instance runs proceed in parallel" is asserted deterministically:
after the batch settles, read every run's `started_at`/`finished_at` and
sweep-line the intervals for peak concurrency. A live sample
(`count(*) WHERE state='running'`) would race the watchers; the committed
interval overlap cannot. The gate asserts peak ≥ 2 (genuine overlap, not an
accidentally-serial run) and logs the actual peak; the drill reports the real
number.

### 4. Contention and parallelism are separate scenarios

Two things must hold at once and they pull opposite ways, so the harness proves
each cleanly rather than muddling them:

- **Parallelism / exactly-once** — N dumps across N *distinct* instances: all N
  win their own lock and succeed, each finalized exactly once, zero orphans.
- **Serialization at scale** — N dumps at *one* instance concurrently: exactly
  one wins, N-1 get `ErrInstanceLocked`, exactly one run row exists, the lock
  frees on finalize (the WU-042 lock test's shape, at load).
- **Mixed** — M instances × K concurrent each: per instance exactly one success
  + K-1 conflicts; the global exactly-once and zero-orphan invariants hold over
  the whole batch (both properties in one run).

### 5. The scheduler boot-stampede is validated, not re-engineered

The folded icebox item ("after long downtime many coalesced catch-ups fire in
one tick — spread them"). `fireDue` already iterates due schedules
**sequentially**, one bounded `fire` at a time (executor.go), so a stampede is
naturally rate-limited — it is not a thundering herd of parallel `Start`s. The
harness proves it: seed the estate, make M distinct-instance schedules all due,
run ONE `fireDue`, assert all M fire and every run settles with zero orphans.
Verdict (mini-ADR 6) records that no spreading is needed for the pilot.

## Behaviors (acceptance)

1. **B1 (parallelism + exactly-once).** N∈[25,50] dumps across N distinct
   instances → N successes; each run has exactly one `run.submitted` and one
   `run.finished` audit row; peak concurrency ≥ 2; zero rows left
   `queued`/`running` after settle.
2. **B2 (serialization at scale).** N concurrent dumps at one instance → exactly
   1 success + N-1 `ErrInstanceLocked`; exactly 1 run row for that instance; 0
   `instance_lock` rows after the winner finalizes; a subsequent `Start`
   succeeds.
3. **B3 (mixed).** M×K launches (M instances, K each) → M successes + M·(K-1)
   conflicts; exactly M run rows; every terminal run finalized once; 0 orphans,
   0 locks after settle.
4. **B4 (scheduler stampede).** M distinct-instance schedules all due, one
   `fireDue` → M fired, all runs settle, 0 orphans; the tick returns promptly.
5. **B5 (numbers + findings).** The drill's throughput and a DB-pool
   recommendation are recorded; every load-only finding (pool, stampede) is
   filed with a verdict.

## Findings (load-only; B5)

Filled in from the live drill; each carries a verdict (pilot-blocking or not).

### F1 — DB connection-pool sizing

`db.NewPool` uses pgxpool defaults: `MaxConns = max(4, NumCPU)` (8 on the VM).
Connections are held only per-query or per-tx — `Start` holds one conn across
its run-insert tx (released before `StartJob`), `finalize` holds one across its
tx, and the watcher acquires/releases per poll — so no goroutine pins a
connection for its lifetime and 50 concurrent dumps + their watchers cannot
exhaust or deadlock the pool; they queue briefly on it. **Verdict:** measured
below; the default is adequate for the pilot's expected concurrency (a handful
of DBAs over 500 instances). An explicit `PORTAL_DB_MAX_CONNS` floor to
decouple the pool from core count is a nice-to-have for packaging (WU-046), not
a blocker — filed to the icebox.

### F2 — Scheduler boot-stampede

`fireDue` fires due schedules sequentially, each bounded by `FireTimeout`; the
instance lock (WU-042) serializes any same-instance collisions to
`skipped_overlap`. **Verdict:** the sequential loop is self-rate-limiting; no
stampede spreading is needed for the pilot. If a future estate makes even
sequential catch-up too bursty, add per-tick jitter/spread — kept in the icebox,
not built now.
