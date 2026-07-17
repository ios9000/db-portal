# SPEC-041 · Staging seed (realistic estate fixture)

> Just-in-time spec for WU-041 (M4 grooming, s26). Keep under ~120 lines; link, don't repeat.

## Scope

A deterministic generator that produces a realistic ~500-instance estate and loads it into
the portal DB, so the load test (WU-043), cold-start (WU-045), and pilot demos (WU-046) run
against fleet-scale inventory instead of the 8-row test fixture. It generates a **valid
SPEC-010 CSV** and feeds it through the **existing `inventory.Import` path** — so idempotency,
cluster resolution, per-row validation, and the import report all come for free, and the seed
can never drift from the import contract. It is a dev/ops convenience, NOT a schema change and
NOT a runtime feature: no new tables, no API, no UI. The tiny committed fixtures
(`instances.csv`, `dev-targets.csv`) are untouched — the estate is generated on demand, not a
new committed file.

## Interfaces

- `inventory.GenerateEstate(n int, seed int64) string` — pure, deterministic: returns a
  SPEC-010 CSV (exact header) of `n` instance rows for the given `seed`. Same `(n, seed)` →
  byte-identical output. No DB, no I/O — unit-testable in isolation.
- `portal seed [--instances N] [--seed S]` — new subcommand (defaults `N=500`, `S=41`).
  Generates the CSV and imports it via `inventory.Import(ctx, pool, "seed", …)`, printing the
  same one-line report `portal import` prints. Exit 0 on success (quarantined rows would be a
  generator bug — a test asserts zero).

## Data

No migration. Writes only through `inventory.Import` → `cluster` + `instance` (+ the
`inventory_import` bookkeeping row), keyed on the `instance.name` natural key (0002). Every
generated field satisfies the 0002 CHECKs and the csv.go validators (env ∈ dev|test|prod,
platform ∈ k8s_patroni|vm, name matches `^[a-z0-9][a-z0-9-]{0,62}$`, pg_version + owner
non-empty, size_gb finite).

## Behavior

1. **Deterministic** — `GenerateEstate(n, s)` is a pure function of `(n, s)`; two calls return
   identical bytes. A different seed returns a different estate. RNG = `math/rand/v2` seeded
   from `s` (codebase idiom; no gosec in the lint set).
2. **Parses clean** — every generated row is accepted by `inventory.Parse`: zero rejects,
   exactly `n` rows. The generator IS the contract, so a reject is a generator bug (tested).
3. **Idempotent** — importing the same `(n, seed)` estate twice: first run `new=n,
   quarantined=0`; second run `new=0, unchanged=n` (names are the natural key).
4. **Realistic distribution** — for `n≈500`: all three envs present; **prod is a non-empty
   minority** (~18%, ≥1 for any n≥1) so prod-ritual/window paths get load coverage; **non-prod
   (dev+test) dominates**; **≥5 distinct clusters**. Env counts are computed up front and
   shuffled (deterministic), so the ratios hold exactly rather than probabilistically.
5. **Cluster/platform consistency** — each cluster is assigned ONE platform for the whole run
   (a cluster with two platforms would self-quarantine per csv.go behavior 8); owner is
   `<cluster>-team`. Clusters count scales with `n` (`max(5, n/8)`).
6. **Window coverage** — prod instances always carry a maintenance window (from a small pool),
   test instances sometimes, dev none — exercising the WU-023 parser at scale.

## Guardrails & audit

None new. The seed rides `inventory.Import`, which is not an audited operation (import
bookkeeping only, per 0002). No secrets involved. The generated estate is data, not an
execution — no run/guardrail/engine surface is touched.

## Out of scope / deferred

- Live-target seeding (real Postgres instances behind the estate) — the estate is inventory
  metadata only; the ONE real dev target stays `dev-targets.csv`/`pgtarget`.
- Randomized "chaos" fields (bad rows to exercise quarantine) — the seed is clean-only; the
  quarantine path is already covered by `import_test.go`.
- Committing a generated fixture file — regenerate on demand; keeps the repo small.

## Open questions

None blocking. `agent` decided in-WU: distribution ratios (18% prod / 32% test / 50% dev),
default `N=500`/`S=41`, cluster count `max(5, n/8)` — all tunable constants, pinned by tests.
