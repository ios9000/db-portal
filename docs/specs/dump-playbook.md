# SPEC-034 · Dump playbook (real pg_dump via Semaphore)

> Groomed 2026-07-11 for WU-034 (Phase 3). Authority chain: O-4 (DECISIONS
> §Open: `pg_dump -Fc`, small vetted option set) → ADR-004 (target creds
> engine-side ONLY; portal DB / logs / params carry no secrets) → ADR-002
> (one Adapter, MockEngine is the forever default for dev + ALL tests;
> Semaphore is opt-in) → SPEC-033 (docs/specs/semaphore.md: the adapter, the
> tag→template map, poll-is-truth) → this file. Rides SPEC-012 (runs.Service
> ↔ Adapter: Start → watch Status→terminal → finalize) and SPEC-030
> (docs/specs/artifacts.md: the `artifact` registry row, `location` column
> dormant until now).

## Scope

The FIRST real operation: a `pg_dump -Fc` of a compose Postgres target,
executed by the SemaphoreAdapter, producing a genuinely restorable artifact
on a shared volume whose real sha256/size/location land in the registry. No
portal code above the engine seam changes. Delivers:

1. Compose `pgtarget` (postgres:16, seeded schema+rows via init script) — a
   real database to back up, reachable by the Semaphore runner over the
   compose network as `pgtarget:5432`.
2. A custom Semaphore image (`infra/semaphore.Dockerfile`) that bakes in
   `postgresql16-client` and provisions a writable `/artifacts` dir — the
   runner is non-root, so a runtime install is impossible (mini-ADR 2).
3. `playbooks/dump.yml`: `pg_dump --format=custom` → shared `artifacts`
   volume, sha256 + size computed with Ansible's `stat`, ONE machine-readable
   result line the adapter parses into `engine.Artifact` (mini-ADR 4).
4. `engine.Artifact` gains `Location`; `semaphore.go` parses the result line
   on terminal SUCCESS and populates it; `finalize` stores it as
   `artifact.location` (the 0009 dormant column goes live). MockEngine leaves
   Location empty.
5. A dev-only inventory fixture + a bootstrap extension that create the
   `pgtarget-env` Semaphore Environment (engine-side creds, ADR-004) and the
   `dump` template pinned to `dump.yml`.

**Explicitly NOT here:** object storage / moving bytes off the volume (WU-035
— `location` is a filesystem path for now); the real restore playbook + the
M3 rehearsal (WU-036); prod-class Semaphore (prod stays mock in dev);
Patroni/replica-aware sequencing (iceboxed); any user-facing dump options
(O-4: fixed vetted flag set).

## Deviations from the WU-034 context brief (surfaced, with cause)

- **pgtarget goes in a NEW `infra/fixtures/dev-targets.csv`, NOT `instances.csv`.**
  The brief says "add a pgtarget row to instances.csv". But `instances.csv` is
  the canonical shared test fixture: `inventory`, `runs`, `chain`, `schedule`
  tests and the golden flow assert **exactly 8 rows** (`require.Len(…, 8)`,
  `imported 8 new`, `require.Equal(t, 8, rep.New)`). A 9th row reddens the
  whole suite. `dev-targets.csv` (one `pgtarget`, env=dev → ClassNonProd) is
  imported at drill time only; import is idempotent + additive (SPEC-010), so
  it composes with the 8-instance import and touches no test.

## Mini-ADRs (agent decisions, revisitable)

1. **`pg_dump --format=custom --no-owner --no-privileges --file=<path>` — the
   fixed, vetted flag set (O-4).** Custom format is compressed, holds a TOC
   `pg_restore --list` reads, and supports selective restore — the O-4 recommendation.
   `--no-owner --no-privileges` drop role/ACL statements so the dump restores
   cleanly onto a target whose roles differ from the source (defense for
   WU-036's restore, and correct for a portable backup). No `--jobs` (custom
   format is single-file; directory-format parallelism is post-MVP). ZERO
   user-facing options: the portal sends no dump params, the playbook exposes
   no toggles — the incident-avoidance posture (D1) says a backup's shape is
   not a per-click decision.

2. **postgresql-client is baked into a custom image (build layer), not
   installed at runtime.** The Semaphore runner executes as uid 1001
   (non-root, group root); `apk add` needs root, so a playbook setup task
   fails. `infra/semaphore.Dockerfile` = `FROM semaphoreui/semaphore:v2.17.39`
   → `USER root` → `apk add --no-cache postgresql16-client` (pg_dump/pg_restore
   16.14 dump a pg16 server exactly) → `mkdir -p /artifacts && chown 1001:0
   /artifacts && chmod 0775` → `USER 1001`. A fresh named `artifacts` volume
   inherits the image mountpoint's 1001:0 ownership on first mount, so the
   runner can write. Compose switches the `semaphore` service to `build:` +
   a stable `image:` tag; the BoltDB state (project, smoke template, API
   token) lives in the persisted `semaphore_data`/`semaphore_config` volumes
   and survives the image swap and container recreate.

3. **Target credentials live in a Semaphore Environment object (engine-side),
   injected as libpq env vars; the playbook never names a credential
   (ADR-004).** `infra/semaphore-bootstrap.sh` gains a `pgtarget-env`
   Environment whose `env` sets `PGHOST=pgtarget PGPORT=5432 PGUSER=… PGPASSWORD=…
   PGDATABASE=…` — PGHOST/PGPORT are fixed compose facts; DB/USER/PASSWORD are
   read from `.env` (gitignored) at bootstrap time. The `dump` template
   attaches this Environment. `pg_dump` reads PG* from its process environment
   via libpq automatically, so the playbook contains no password, no
   `lookup('env','PGPASSWORD')`, nothing to leak into task output. No secret
   enters the repo, the portal DB, run params, audit rows, or logs.
   `.env.example` documents SHAPE only.

4. **The engine→portal result contract is ONE base64-wrapped JSON line with a
   sentinel.** The playbook emits `DBPORTAL_RESULT=<base64(json)>`, json =
   `{"name","size_bytes","sha256","location"}`. base64's alphabet has no
   quotes, spaces, or newlines, so the token survives Ansible's default-
   callback wrapping (`"msg": "DBPORTAL_RESULT=eyJ…"`) intact — the adapter
   regex-extracts `DBPORTAL_RESULT=([A-Za-z0-9+/=]+)`, base64-decodes, and
   unmarshals. This is robust where scraping raw JSON out of `debug`'s escaped,
   multi-line block is not. The sentinel is distinctive enough that no other
   output line matches. sha256+size are computed by Ansible's `stat`
   (`checksum_algorithm: sha256`) — no shelling out, no client-version drift.

5. **The adapter builds `engine.Artifact` from the result line on terminal
   SUCCESS only; poll stays the truth (SPEC-033 mini-ADR 2).** `semaphore.go`
   `Status`, when it maps a task to `StateSuccess`, fetches the task output
   once, scans for the sentinel, and attaches `Artifact{Name, SizeBytes,
   Checksum, Location}`. A success with NO parseable result line → nil
   Artifact: the run still succeeds, no registry row is written (honest — a
   dump that didn't announce a file isn't a registrable artifact), and the
   adapter logs a Warn. Non-success never carries a result line. Both the
   watcher and `ReconcileByJobID` (the webhook) go through `Status`, so
   acceleration and the poll fallback both carry the artifact identically.
   The portal forwards no extra-vars in WU-034 (StartJob keeps ignoring
   params): connection AND artifact naming come entirely from the engine-side
   Environment, so there is nothing for the portal to pass. (Per-target
   extra-vars is a post-MVP multi-target concern.)

6. **`engine.Artifact` gains `Location string`; MockEngine leaves it empty;
   `finalize` stores it as `artifact.location` via `NULLIF($,'')`.** Location
   is the file path on the shared `artifacts` volume now; WU-035 swaps it for
   an object URL — same column, same code path, the 0009 dormant `location`
   goes live (the 0003 `window_warned` pattern). The run's own `artifact_*`
   columns are unchanged: location lives ONLY on the registry row (SPEC-030
   mini-ADR 1). GET /api/artifacts still does NOT expose `location` (SPEC-030
   holds) — it is an engine-side path, not a client concern.

## Interfaces

- `engine.Artifact` (engine.go): add `Location string` — "where the bytes
  live; empty for the mock". Immutable once set on a JobStatus.
- `engine.SemaphoreAdapter.Status` (semaphore.go): on `StateSuccess`, fetch
  `/output`, parse the sentinel, set `JobStatus.Artifact`. New unexported
  `parseResultLine([]semOutputLine) *Artifact`.
- `runs.Service.finalize` (service.go): the `INSERT INTO artifact` gains
  `location` = `NULLIF($,'')` from `artifact.Location`.
- `infra/compose.yaml`: `pgtarget` service (postgres:16, seed init, 127.0.0.1
  bind), `semaphore` service switches to `build:` with the `artifacts` volume
  mounted at `/artifacts`; new named volumes `artifacts`, `pgtargetdata`.
- `infra/semaphore.Dockerfile`: the custom runner image (mini-ADR 2).
- `infra/fixtures/pgtarget-init.sql`: seed schema + rows for `docker-entrypoint-initdb.d`.
- `infra/fixtures/dev-targets.csv`: the `pgtarget` dev instance row (drill import).
- `infra/semaphore-bootstrap.sh`: create `pgtarget-env` Environment + `dump`
  template (idempotent, beside the smoke objects); print the dump template id.
- `playbooks/dump.yml`: the real dump playbook.
- `.env.example` / `.env`: `PGTARGET_DB_NAME/USER/PASSWORD` (shape/real).

## Behavior (testable)

1. **Result-line parse (unit, stub HTTP Semaphore):** a success task whose
   output carries `DBPORTAL_RESULT=<b64>` → `Status` returns
   `Artifact{Name,SizeBytes,Checksum,Location}` with the decoded values. A
   success with no sentinel, malformed base64, or bad JSON → nil Artifact,
   Status still success (logged Warn). Non-success → nil Artifact.
2. **Location persisted (unit, DB):** finalize with an Artifact carrying a
   Location writes that string to `artifact.location`; an Artifact with empty
   Location (the mock) writes NULL. The run's `artifact_*` columns are
   unchanged either way.
3. **MockEngine untouched:** the golden flow's dump beats still register an
   artifact row (location NULL); `npm run check` is green with ZERO Semaphore
   dependence.
4. **Live drill (AC evidence, VM, isolated portal):** portal dump on pgtarget
   through real Semaphore → a real `.dump` on the `artifacts` volume;
   `pg_restore --list` on it succeeds; the registry row carries the REAL
   sha256/size/location (sha256 == `sha256sum` of the file). A bad-creds /
   unreachable-target run → failed, honest error, NO artifact row, notify
   mail. `grep` + audit-row eyeball: no secret in repo, portal DB, params, or
   logs.

## Guardrails & audit

Everything above the seam is unchanged, so all four guardrail layers, the
prod ritual, authz, windows, and the audit trail apply to a real dump exactly
as to a mock one (ADR-002). pgtarget is env=dev → ClassNonProd → the nonprod
Semaphore adapter; prod stays mock in dev (guardrail 3, structural). No
secret enters the repo, portal DB, params, audit rows, or logs — creds are
Environment-injected engine-side (ADR-004, mini-ADR 3). Poll remains the
finalization authority, so exactly-once (WU-016) and the artifact tx
(SPEC-030) are untouched — the only new byte in that tx is `location`.

## Split (as sized in BACKLOG)

- **Slice (a) — infra + playbook. CHECKPOINT.** `pgtarget` service + seed;
  `semaphore.Dockerfile` + compose `build:` + `artifacts` volume;
  `dev-targets.csv`; `dump.yml`; bootstrap `pgtarget-env` + `dump` template;
  `.env(.example)`. Live-pin the Semaphore Environment/template mechanics and
  prove `dump.yml` produces a `.dump` + a parseable result line by running the
  template directly in Semaphore (no portal yet). No Go changes.
- **Slice (b) — adapter parse + Location + finalize + tests + live drill.**
  `Artifact.Location`; `semaphore.go` result parsing; `finalize` location
  write; unit tests (stub HTTP + DB finalize); the full-portal live drill on
  an isolated portal (dump success → registry row with real sha/size/location
  → `pg_restore --list`; bad-creds failure path → no artifact + mail).

Delegation: both slices are architect-implemented. Slice (a) reads as
"scaffold" but is entangled with ADR-004 secrets, the result-line contract
the adapter must parse, non-root image mechanics, and live Semaphore REST
pinning — one tight iteration loop; delegation overhead exceeds the diff
(the WU-023/WU-030 call). Slice (b) is the finalize-adjacent adapter seam.

## Out of scope / deferred

- Object storage / `location` as an object URL → WU-035.
- Real restore playbook + M3 rehearsal → WU-036.
- Per-target extra-vars / multi-target template resolution → post-MVP.
- Prod-class Semaphore, runner topology, Semaphore HA → post-MVP.
- `npm run up` passing `--env-file .env` (the compose GOTCHA) → BACKLOG idea.

## Open questions

- **Exact Semaphore Environment/template payload** (env-vars field name, how
  the template binds the Environment, whether local `command` inherits the
  process env) is pinned in slice (a) against the running service; the
  interface above is stable regardless. Fallback if env is NOT inherited by
  the local `command`: set the task `environment:` from `lookup('env',…)`
  with `no_log: true`. Owner: agent.
</content>
</invoke>
