# M1 demo — the hero flow in five minutes

The management demo (ROADMAP M1 exit, D7 acceptance shape): CSV import → fleet →
one-click dump on TEST → live logs → artifact → a run that ends badly (audit + email) →
the PROD ritual. Runs against MockEngine (ADR-002) — every portal layer above the
engine seam is the real production code path.

Automated twin: `backend/e2e/golden_flow_test.go` (in `npm run check`, ADR-011).
Everything below was live-verified on the release binary 2026-07-08 (JOURNAL s07).

## Setup (before the clock — ~2 min once)

From the repo root on the VM:

```sh
npm run up                # compose: Postgres 16 + mailpit
cp .env.example .env      # defaults work as-is; PORTAL_NOTIFY_TO on = mail on
npm run build:release     # fe build → embed → static backend/bin/portal
npm run db-reset          # fresh portal DB (also how you reset between demos)
./backend/bin/portal migrate up
./backend/bin/portal &    # log must say: run notifications enabled
```

Open two browser tabs: **portal** <http://localhost:8080> · **mailpit** <http://localhost:8025>.

Since WU-020 the portal asks you to sign in first: `.env.example` ships
`PORTAL_AUTH_MODE=fake`, so use the dev directory account **dba1 / dba1**
(the badge top-right shows who's signed in; Sign out ends the session).

## The demo (target < 5 min total)

### 1 · Import the estate — ~30 s

```sh
./backend/bin/portal import infra/fixtures/instances.csv
```

Report line: `imported 8 new, updated 0, unchanged 0, quarantined 0`.
Run it **again** — `imported 0 new, updated 0, unchanged 8, quarantined 0`.
Talking point: idempotent by natural key; malformed rows quarantine with reasons
instead of poisoning the import (SPEC-010).

### 2 · See the fleet — ~30 s

Portal tab → **My Databases**: 8 instances, env badges (dev/test/prod), owner +
PG version + size. Flip **Cards ⇄ Table**, filter env to `test` (3 instances) —
filter and view live in the URL, so views are shareable.

### 3 · Dump on TEST: one click — ~45 s

Card **billing-test** → **Dump** → launch drawer. Talking point: non-prod is
deliberately frictionless — one click, optional reason. **Launch**, then **View run**.

### 4 · Live logs — same run

Run detail: TEST env banner, dark log pane streaming ansible-shaped output
(replay + follow over SSE), **Follow** pill, elapsed ticking live.

### 5 · Success + artifact — ~15 s

State chip flips to **success**; artifact strip shows name, size, sha256 checksum.
Talking point: D1 — the dump that the motivating incident skipped, now one click.

### 6 · A run that ends badly: audit + email — ~60 s

Start a dump on **crm-test**, and on the run page hit **Abort** while it streams.

- Run card goes calm **canceled** — no red panic, the state is just a fact.
- **Mailpit tab**: `[db-portal] RUN-<n> canceled — dump on crm-test (test)` with a
  link to the run. Who/what/where/status ONLY — reason, error text and params never
  leave the portal by policy (SPEC-014).
- **Activity screen**: the run in history with Requester; filter chips; **Export CSV**.
- Audit is append-only, enforced by the database itself:

```sh
docker compose -f infra/compose.yaml exec -T postgres psql -U portal -d portal \
  -c "SELECT run_id, actor, action, environment, final_status FROM audit_event ORDER BY id;" \
  -c "UPDATE audit_event SET actor = 'evil' WHERE id = 1;"
```

Every row names **who** (`actor = dba1` — the signed-in identity, WU-021), with
environment stamped throughout. The successful run has two rows
(`run.submitted` / `run.finished`); the aborted one has **three** — the extra
`run.cancel_requested` row pins who hit Abort. And the UPDATE **fails**:
`audit_event is append-only`.

Optional hard-failure variant (+60 s, skip when tight): `kill -9` the portal
mid-run, restart it — the orphan sweep finalizes the run **failed** and the
failure mail arrives unattended (proven live in WU-014).

### 7 · PROD is different — ~60 s

Card **billing-prod** → **Dump**. The drawer turns prod: red **PROD** banner
(text + color, never color alone), and the launch button stays locked until you
**type the exact instance name** — paste is disabled; friction, not memory.
Since WU-021 the API enforces the same ritual server-side (a scripted POST
without the typed name is refused), and mutations require the **dba** role.
Type `billing-prod`, launch, open the run: PROD banner on the run page, and the
engine job id is prefixed `mock-prod-` — prod jobs run through a **separate
engine adapter** (guardrail layer 3), even as mocks. Audit rows for this run
carry `environment = 'prod'`.

Talking point: D2 — one pane of glass for prod + non-prod WITH engineered
mix-up safeguards: banner (see it), ritual (mean it), credentials (contain it),
audit env stamp (prove it).

### 8 · Set it on a rhythm — schedules (M2 preview, optional +90 s)

**Schedules** → **New schedule**: pick **crm-test**, cron spec `* * * * *`,
reason "demo heartbeat". The row shows the server-computed **next fire**
(jittered up to 60 s so a fleet of 02:00 dumps never stampedes). Within
~2 minutes the run appears in **Activity** with requester
**schedule:dba1** — the same audit path as the button, attributed to the
schedule's owner (ADR-003). Toggle it disabled and the next fire goes
blank; nothing fires until re-enabled. A schedule on a prod instance
demands the same typed-name ritual — once, at creation, covering every
future fire. (Delete the heartbeat schedule before moving on.)

## Reset between demos

```sh
npm run db-reset && ./backend/bin/portal migrate up
./backend/bin/portal import infra/fixtures/instances.csv
```

(Restart the binary if you killed it in the optional beat; clear mailpit via its UI.)
