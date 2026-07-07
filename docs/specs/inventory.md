# SPEC-010 · Inventory (schema + CSV import)

> Groomed 2026-07-07 for WU-010. Authority chain: DECISIONS.md → ARCHITECTURE.md §2
> ("Inventory", "Portal DB") → this file. Design-brief sample data drives the fixture.

## Scope

The portal DB becomes the working copy of the estate from day one: `cluster` +
`instance` tables and an idempotent CSV import with per-row validation and quarantine.
Explicitly NOT here: connectivity probes (needs real targets — validation is
structural only), UI upload (import is a CLI subcommand for MVP), Excel/.xlsx,
CMDB export, maintenance-window *parsing* (stored raw; semantics = WU-022).

## Interfaces

- `portal import instances.csv` — new subcommand beside `migrate` (mini-ADR: CLI
  not HTTP for MVP — no auth exists yet (WU-020/021), single-binary ops style,
  M1 demo scripts it; UI/API upload goes to Icebox).
  Exit 0 = file processed (even with quarantined rows; report printed),
  exit 1 = file-level failure (unreadable, bad header, empty).
- Output report (stdout, one line): `imported N new, updated M, unchanged U, quarantined Q`.
- No HTTP endpoints in this WU (list/detail = WU-011).

## Data

Migration(s) via existing goose embed. All timestamps `timestamptz`, UTC.

- `cluster`: `id PK`, `name text UNIQUE NOT NULL`, `platform text NOT NULL CHECK
  (platform IN ('k8s_patroni','vm'))`, `patroni_scope text NULL`, `created_at`.
  (Cluster = HA/ownership grouping; auto-created on first reference by an import row.)
- `instance`: `id PK`, `name text UNIQUE NOT NULL` (**natural key** for idempotency),
  `cluster_id FK -> cluster NOT NULL`, `env text NOT NULL CHECK (env IN
  ('dev','test','prod'))`, `pg_version text NOT NULL`, `size_gb numeric NULL`,
  `owner text NOT NULL`, `maintenance_window text NULL` (raw string — O-3 field,
  WU-022 owns semantics), `created_at`, `updated_at`.
- `inventory_import`: `id PK`, `ts`, `filename`, `rows_total/rows_new/rows_updated/
  rows_unchanged/rows_quarantined int` — one row per import run (bookkeeping;
  real audit trail schema is WU-012's).
- `inventory_import_reject`: `id PK`, `import_id FK`, `row_number int`,
  `raw text` (the CSV line verbatim), `reasons text[]` — the quarantine.

Invariants: instance.env is the ONLY env source; engine class derives via
`engine.ClassForEnv` (fails closed on garbage — never bypass it). Every future
run/audit row FKs `instance.id` (ARCHITECTURE §2).

## Behavior

CSV contract: header REQUIRED, exactly
`instance_name,cluster_name,env,platform,pg_version,size_gb,owner,maintenance_window`
(order fixed, UTF-8, `size_gb`/`maintenance_window` may be empty).

1. Happy path: fixture CSV (8 rows) → 8 instances, 6 clusters, report
   `imported 8 new, ... quarantined 0`; import row records counts.
2. Idempotent re-import: same file again → 0 new, 0 updated, 8 unchanged; row
   counts in `instance` unchanged. (BACKLOG verify line.)
3. Update: same natural key, changed `pg_version` → 1 updated (values overwritten,
   `updated_at` bumped), not duplicated.
4. Per-row quarantine: unknown env (`prod2`), blank `instance_name`, non-numeric
   `size_gb`, wrong column count → row lands in `inventory_import_reject` with ALL
   its reasons (accumulate, don't stop at first); valid rows still import. A
   malformed-rows fixture ships with the tests.
5. Duplicate natural key within one file: first row wins, later duplicates
   quarantined (`duplicate instance_name in file`).
6. File-level failure: missing/unknown/reordered header, empty file, unreadable
   path → exit 1, nothing written (no partial import record).
7. Names: `instance_name`/`cluster_name` must match `^[a-z0-9][a-z0-9-]{0,62}$`
   (DNS-label-ish; they become CLI/URL/audit identifiers). Else quarantine.
8. Cluster consistency: two rows naming the same cluster with different
   `platform` → later row quarantined (`cluster platform conflict`).

## Guardrails & audit

Env values constrained by CHECK + `engine.ClassForEnv` failing closed (guardrail
layer 3 continuity). Import bookkeeping is queryable for the M1 demo; formal
append-only audit rows attach to *operations*, not imports, and arrive in WU-012.
No secrets anywhere in inventory: connection credentials are engine-side
(Ansible/Vault) by design — the portal stores names, not passwords.

## Out of scope / deferred

- Connectivity probe on import → needs real targets (M3+; note in BACKLOG WU-010).
- UI/API upload + import history screen → Icebox.
- Excel (.xlsx) ingestion → Icebox (CSV covers MVP; research corpus said "Excel/CSV").
- Maintenance-window parsing/TZ → WU-022 (stored raw here).
- Portal self-target ban → Icebox line exists (enforce when real targets exist).
- `last_backup`/bloat/health columns shown in design-brief screens → derived from
  runs (WU-012+) or probes (M3+), NOT inventory columns now. WU-011 renders "—".

## Open questions

- None blocking `user`. Agent decisions taken above and revisitable in-WU:
  CLI-over-HTTP import; first-row-wins duplicate policy; auto-created clusters.

## Fixture

`infra/fixtures/instances.csv` — the 8 design-brief instances (billing-prod/-test,
shop-prod, crm-prod/-test, wms-prod, analytics-dev, hr-test) with env/platform/
version/size/owner/window values consistent with the brief's screens.
