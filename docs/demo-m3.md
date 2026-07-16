# M3 demo — a real restore, end to end, in fifteen minutes

The M3 exit rehearsal (ROADMAP M3): a REAL `pg_dump` of a real Postgres target
stored in object storage, then a REAL `pg_restore` of it — driven by the SAME
portal code that runs the mock, through the `verify → safety_dump → restore`
chain, with the checksum gate and the unconditional safety dump doing their jobs
in front of a live database.

Where demo-m1.md shows the hero flow on MockEngine, this one closes the loop the
product exists for: **the data comes back.** Every portal layer is unchanged from
M1 — the engine underneath is real (SemaphoreAdapter → Semaphore → Ansible →
`pg_dump`/`pg_restore`/`mc`), swapped in behind the untouched `engine.Adapter`
seam by one env var.

Automated twin: `backend/e2e/golden_flow_test.go` Beats 9–10 (chain halt/resume +
the restore chain) in `npm run check` (ADR-011) — those run on MockEngine every
session. This doc is the human twin, and the only place the REAL playbooks run.

Live-verified on the VM release binary **2026-07-15** (JOURNAL s22).

## What this proves (M3 exit criteria)

- [x] **Restore rehearsal on a compose target passes** — §3–§5 below: a dropped
      table comes back with its rows from a portal-driven `pg_restore`.
- [x] **A 3-step chain halts on injected failure, notifies, resumes** — §6–§7: a
      tampered artifact halts the chain at `verify` with the target untouched and
      ONE mail; fix + resume → success.
- [x] **The SAME portal code runs Mock and Semaphore behind the adapter** — §8:
      the same binary, the same chain, one env var apart.

## Setup (before the clock — ~3 min once)

From the repo root on the VM. The whole dev stack, including the real engine, the
target database, and object storage:

```sh
docker compose -f infra/compose.yaml --env-file .env up -d --build --wait
set -a; . ./.env; set +a
sh infra/semaphore-bootstrap.sh      # idempotent; prints the template ids
npm run build:release                # fe build → embed → static backend/bin/portal
```

The bootstrap prints the map you need — as of s22:

```
SEMAPHORE_TEMPLATE_ID=1              # smoke
SEMAPHORE_DUMP_TEMPLATE_ID=3         # dump.yml      (chain step 2, safety_dump)
SEMAPHORE_VERIFY_TEMPLATE_ID=4       # verify.yml    (chain step 1)
SEMAPHORE_RESTORE_TEMPLATE_ID=5      # restore.yml   (chain step 3)
```

**Run the rehearsal on an isolated portal, never the demo one** — a scratch DB and
a spare port, so the `:8080` demo and its data are untouched
([[live-drill-isolation]]; the persistent `.env` deliberately stays
`PORTAL_ENGINE_NONPROD=mock`):

```sh
docker exec -e PGPASSWORD="$PORTAL_DB_PASSWORD" dbportal-dev-postgres-1 \
  psql -U "$PORTAL_DB_USER" -d postgres -c "CREATE DATABASE portal_drill036;"

export PORTAL_DB_NAME=portal_drill036
export PORTAL_HTTP_ADDR=:8099
export PORTAL_ENGINE_NONPROD=semaphore
export PORTAL_SEMAPHORE_TEMPLATES=verify:4,restore:5,dump:3,smoke:1
export PORTAL_AUTH_MODE=off

./backend/bin/portal migrate up
./backend/bin/portal import infra/fixtures/instances.csv     # 8 new
./backend/bin/portal import infra/fixtures/dev-targets.csv   # 1 new — pgtarget
./backend/bin/portal &
```

The boot log must say — this is the whole M3 swap, in one line:

```
"msg":"non-prod engine: Semaphore","url":"http://127.0.0.1:3000","project":1,
"templates":{"dump":3,"restore":5,"smoke":1,"verify":4}
```

Tabs: **portal** <http://localhost:8099> · **mailpit** <http://localhost:8025>.

`pgtarget` is `env=dev` → ClassNonProd → the real engine. Prod stays on the mock
in dev — guardrail 3 is structural (the Registry panics on a shared adapter), so
this rehearsal *cannot* reach a prod instance even by mistake.

## The demo (target ≤ 15 min total)

### 1 · The target is a real database — ~45 s

```sh
docker exec -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" \
  -c "SELECT * FROM widget ORDER BY id;" -c "SELECT * FROM ledger_totals;"
```

`widget` = 4 rows (alpha/beta/gamma/delta), `ledger` = 200 rows, and a
`ledger_totals` view reading 200 / 30150.00. Not a fixture in the portal — a
separate Postgres 16 the portal reaches only through the engine.

Talking point: the portal has no database credentials for this target and never
will. `PG*` and `MC_*` live engine-side in the Semaphore Environment (ADR-004);
the playbooks name no credential at all.

### 2 · Dump it — one click, real bytes — ~90 s

Portal → **My Databases** → `pgtarget` → **Dump**. Or:

```sh
curl -s -X POST http://127.0.0.1:8099/api/runs -H 'Content-Type: application/json' \
  -d '{"instance":"pgtarget","operation":"dump","reason":"M3 rehearsal"}'
```

Watch the live log stream: real Ansible output, real `pg_dump`. On success the run
carries a real artifact:

```json
{"name":"appdb-20260716T021530Z-80914329.dump","size_bytes":5382,
 "checksum":"3068bc7a07ee8661faa2830323cd4dd31338d043b773199ac64cd41c3a308345"}
```

Those bytes are in object storage, not on a volume — `mc ls dbportal/dbportal-artifacts`
shows the object; the registry row's `location` is
`s3://dbportal-artifacts/appdb-…dump`. The staging copy is already deleted
(SPEC-035: the `artifacts` volume is staging only).

Talking point: `GET /api/artifacts?instance=pgtarget` shows the artifact —
**without** `location`. The object path never leaves the engine side (SPEC-030).

### 3 · Break it — ~30 s

The honest part of any backup demo. Destroy real data:

```sh
docker exec -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" -c "DROP TABLE widget CASCADE;"
```

`\dt` now shows `ledger` only. The view is gone with it (CASCADE). This is the
incident.

### 4 · Restore it — the chain — ~3 min

Portal → **My Databases** → `pgtarget` → **Restore** → pick the artifact. Or:

```sh
curl -s -X POST http://127.0.0.1:8099/api/restore -H 'Content-Type: application/json' \
  -d '{"artifact_id":1,"target":"pgtarget","confirm":"pgtarget","reason":"M3 rehearsal"}'
```

One POST, three steps — the portal assembles the chain; the client cannot:

```json
{"id":1,"kind":"restore","state":"running","steps":[
  {"seq":1,"operation":"verify","status":"pending"},
  {"seq":2,"operation":"safety_dump","status":"pending"},
  {"seq":3,"operation":"restore","status":"pending"}]}
```

Watch the strip on the run page (`GET /api/runs/{id}/chain`) go green in order.
Each step is a full run: guardrails, audit, notify — attributed `chain:dba1`.

Talking point — **the safety dump is not a checkbox.** It is a literal element of
`restore.Steps` (SPEC-031), so there is no skip affordance to forget: restoring
over a database always snapshots it first. That is the D1 incident encoded in a
slice literal rather than a runtime check.

### 5 · The data is back — ~60 s

```sh
docker exec -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" \
  -c "SELECT * FROM widget ORDER BY id;" -c "SELECT * FROM ledger_totals;"
```

All 4 widgets, original `created_at` timestamps, `ledger_totals` = 200 / 30150.00.
**This is the beat the product exists for.**

And the chain left its own evidence — `GET /api/artifacts?instance=pgtarget`:

```
id  retention  name                                   size_bytes
2   safety     appdb-20260716T021836Z-68059239.dump   3912
1   standard   appdb-20260716T021530Z-80914329.dump   5382
```

The `safety` artifact is *smaller* — correct: it snapshotted the target **after**
`widget` was dropped. That is the pre-restore state, which is exactly what you'd
want if the restore itself turned out to be the mistake.

It is a real dump, not a stub — prove it (AC-3):

```sh
docker run --rm --network dbportal-dev_default \
  -e MC_HOST_dbportal="http://$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD@minio:9000" \
  dbportal-semaphore:v2.17.39-pg16 sh -c '
    mc cp dbportal/dbportal-artifacts/appdb-20260716T021836Z-68059239.dump /tmp/s.dump
    sha256sum /tmp/s.dump; pg_restore --list /tmp/s.dump'
```

The sha256 matches the registry checksum exactly, and the TOC lists `ledger`, its
sequence, the `ledger_totals` view, TABLE DATA, the pkey and the index — and no
`widget`, correctly.

### 6 · When the artifact is corrupt — the chain halts — ~4 min

The failure that matters most. Restore a *tampered* artifact and the target must
not be touched at all.

Take a fresh dump (§2), **back the object up first**, then corrupt it in storage:

```sh
OBJ=<the new artifact name>
# back up host-side (stdout redirect — the runner is uid 1001 and cannot write a root-owned mount)
docker run --rm --network dbportal-dev_default \
  -e MC_HOST_dbportal="http://$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD@minio:9000" \
  dbportal-semaphore:v2.17.39-pg16 mc cat "dbportal/dbportal-artifacts/$OBJ" > /tmp/good.dump
sha256sum /tmp/good.dump      # MUST equal the registry checksum before you tamper

docker exec -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" -c "DROP TABLE widget CASCADE;"

docker run --rm --network dbportal-dev_default \
  -e MC_HOST_dbportal="http://$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD@minio:9000" \
  dbportal-semaphore:v2.17.39-pg16 \
  sh -c "printf 'CORRUPTED' | mc pipe dbportal/dbportal-artifacts/$OBJ"
```

Now `POST /api/restore` on that artifact. The chain halts at step 1:

```json
{"id":2,"state":"halted","steps":[
  {"seq":1,"operation":"verify","run_id":6,"status":"failed"},
  {"seq":2,"operation":"safety_dump","run_id":null,"status":"pending"},
  {"seq":3,"operation":"restore","run_id":null,"status":"pending"}]}
```

Read `run_id: null` out loud on steps 2 and 3: the safety dump and the restore
**were never created** — not "ran and were skipped". The verify run's log says why:

```
msg: checksum mismatch for appdb-…dump — stored sha256 fde74992… != expected ed658267…
```

Check the target: `widget` is still absent — the chain touched nothing. `verify.yml`
names no PG connection at all; it fetches the object, hashes it, and stops
(SPEC-036 mini-ADR 4). Mailpit has exactly **one** new mail:

```
[db-portal] CHAIN-2 halted — restore on pgtarget (dev)
```

One chain mail, not three run mails (SPEC-032 `StepRunFilter`), and it carries
who/what/where/status + a link — never the error text or the reason (D7).

### 7 · Fix it and resume — ~2 min

Put the good bytes back, then resume the SAME chain — no re-typing, no new POST
body:

```sh
docker run --rm -i --network dbportal-dev_default \
  -e MC_HOST_dbportal="http://$MINIO_ROOT_USER:$MINIO_ROOT_PASSWORD@minio:9000" \
  dbportal-semaphore:v2.17.39-pg16 \
  sh -c "mc pipe dbportal/dbportal-artifacts/$OBJ" < /tmp/good.dump

curl -s -X POST http://127.0.0.1:8099/api/chains/2/resume -d '{}'
```

The chain re-fires from the failed step and runs to the end:

```json
{"id":2,"state":"success","halted_at":null,"steps":[
  {"seq":1,"operation":"verify","run_id":7,"status":"success"},
  {"seq":2,"operation":"safety_dump","run_id":8,"status":"success"},
  {"seq":3,"operation":"restore","run_id":9,"status":"success"}]}
```

`widget` is back: 4 rows. Note the step 1 run id moved 6 → 7 — the failed run is
superseded but keeps its history (SPEC-032); the audit trail still holds the
failure. The resumer is the attributed mover (`chain:<resumer>`), and the stored
confirm replays verbatim.

### 8 · Same code, both engines — ~90 s

The M3 exit criterion, shown rather than asserted. Same binary, one env var:

```sh
export PORTAL_DB_NAME=portal_drill036m   # a second scratch DB
export PORTAL_HTTP_ADDR=:8098
export PORTAL_ENGINE_NONPROD=mock        # <- the only difference
./backend/bin/portal migrate up && ./backend/bin/portal import infra/fixtures/dev-targets.csv
./backend/bin/portal &
```

Dump, then `POST /api/restore` exactly as in §4. The same three-step chain runs to
success — `verify → safety_dump → restore` — against MockEngine.

The one honest difference, straight from the two portal DBs:

```
mock:       location = NULL
semaphore:  location = s3://dbportal-artifacts/appdb-…dump
```

Talking point: nothing above the engine seam knows which engine ran. The chain,
the guardrails, the audit, the registry, the UI, the mail — one code path. The
adapter is the only thing that changed, and `location` is an opaque string the
portal stores and passes but never dereferences.

## Reset between rehearsals

```sh
# stop the drill portals — kill the exact PID holding the port (a backgrounded
# wrapper can make $! lie; see JOURNAL s10)
ss -ltnp | grep -E '8099|8098'
kill <pid>

docker exec -e PGPASSWORD="$PORTAL_DB_PASSWORD" dbportal-dev-postgres-1 \
  psql -U "$PORTAL_DB_USER" -d postgres \
  -c "DROP DATABASE portal_drill036;" -c "DROP DATABASE portal_drill036m;"

# re-seed the target (the init SQL only runs on a fresh volume, so replay it)
docker exec -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" \
  -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;"
docker exec -i -e PGPASSWORD="$PGTARGET_DB_PASSWORD" dbportal-dev-pgtarget-1 \
  psql -U "$PGTARGET_DB_USER" -d "$PGTARGET_DB_NAME" < infra/fixtures/pgtarget-init.sql
```

Drill artifacts accumulate in the bucket — harmless (no retention until M4).

## Known rough edges (say them before someone asks)

- **Single plain Postgres.** Patroni-aware sequencing (pause → restore → reinit
  replicas) is the named M3 hard problem and is iceboxed (ARCHITECTURE §7). This
  rehearsal proves the restore path, not HA orchestration.
- **Full restore only** (D4). No PITR, no table-level restore.
- **`--single-transaction`** makes a restore all-or-nothing, but a very large
  restore holds one long transaction; parallel `--jobs` is M4 load-test territory.
- **No retention enforcement yet.** `'safety'` artifacts are classified but never
  pruned (M4).
