# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **M3 GATE PASSED; fix WUs landing. WU-038 DONE (s26) —
  schedule.Create launchable gate.** NEXT = **WU-039 → 040**, THEN groom + start Phase 4.
  WU-036 (s21 a + s22 b) closed M3 feature-complete; the gate (s23, recovered s24)
  passed with fix WUs, NO criticals (full record `docs/agent/reviews/m3-gate.md`).
  Execution order 030 → 032 → 031 → 033 → 034 → 035 → 036 → **M3 gate ✓** →
  **037 ✓** → **038 ✓** → 039 → 040 → Phase 4.
- **Status (s26, WU-038 DONE — schedule.Create launchable gate):** the MEDIUM
  guardrail-honesty fix. `schedule.Create` (schedule.go) tested catalog EXISTENCE
  only (`catalog.ByID`, which finds the non-launchable restore-chain steps too), so
  `POST /api/schedules {operation:"restore"}` (or verify/safety_dump) returned 201
  where SPEC-031 behavior 5 promises 400. The guardrail HELD (executor.fire never
  sets Internal → runs.Start rejected the fire) but the schedule was permanently,
  silently broken: every tick → fire()'s `default:` branch → `last_fire_status='error'`,
  no run, no chain, no mail, next_fire_at advancing forever. FIX: one line — Create now
  mirrors runs.Start's `!op.Launchable` gate (`op, ok := catalog.ByID; if !ok ||
  !op.Launchable → runs.ErrUnknownOperation`); the schedules handler already maps that
  to 400 "unknown operation", so a non-launchable op is refused at the door, same as
  the button path. `TestCreateValidation` extended to cover the three existing-but-non-
  launchable ids (previously only the absent "explode"); the entry's `count==0`
  assertion already proves no failed create writes a row. Gate GREEN: CHECK-EXIT:0
  (golangci 0, schedule pkg FRESH under -race 10.3s, golden flow TestGoldenFlow PASS
  not-skipped, vitest 116/116). No migration, no UI, no seam change. Go-test-shaped,
  no live stack touched (dev stack still up from s22).
- **Status (s25, WU-037 DONE — transient-error resilience):** the two M3-gate
  HIGHs are one mock-to-real root cause, fixed together — an assumption true under
  MockEngine ("any engine error == job lost") that a real adapter behind the
  unchanged seam quietly broke. (1) `runs.Service.watch` (service.go) now treats
  ONLY `engine.ErrUnknownJob` as a fatal finalize-failed; every other Status error
  (conn-refused / 5xx / timeout / decode) is transient → bounded retry with
  exponential backoff (new field `MaxStatusErrors` default 30, cap 30s) then an
  HONEST give-up ("engine status unavailable after N attempts", never the old lying
  "engine lost the job"). The "engine lost the job" message now means EXACTLY a lost
  job. (2) `chain.drive` (driver.go) no longer exits its goroutine on the first DB
  read error and wedges the chain `state='running'` forever — all three reads (load
  / `next()` / step-watch) retry via `loadChain`/`nextRetry`/`watchRun` up to
  `MaxReadErrors` (default 30, cap 15s), then **halt + notify** (Resume works,
  the DBA is mailed) instead of a silent exit. Chose bounded-retry-then-halt over a
  periodic sweep (self-halts promptly, symmetric with the watcher, no ticker/lifecycle
  surface added to main.go; the boot sweep still covers process death — main.go
  UNCHANGED). 5 new -race tests drive the real goroutines with injected transient
  errors (2 runs recover/give-up, 1 ErrUnknownJob, 2 chain self-heal/honest-halt).
  Gate GREEN: CHECK-EXIT:0 (golangci 0, go test -race all pkgs, golden flow
  TestGoldenFlow PASS not-skipped, vitest 116/116). No UI, no migration, no seam
  change. Dev stack still up (mailpit/postgres 9d, minio/pgtarget/semaphore 3-4d).
- **Status (s24, M3 GATE RECOVERED + CHECKPOINTED):** s23 (session `36caf224`,
  today 2026-07-16) ran the multi-agent M3 gate — workflow `wf_49ca1969-37a`, 5
  Sonnet reviewers, 605k tok / 205 calls / ~19.4 min, findings verified INLINE by
  the architect (M1/M2 light shape, no verifier agents) — and produced
  `docs/agent/reviews/m3-gate.md`: **8 findings, 8 confirmed, 0 refuted, 4 re-graded
  down, NO criticals → GATE PASSES with fix WUs.** An ssh reset killed s23 before
  it could checkpoint (STATE/JOURNAL unwritten; the doc + `.claude/workflows/
  m3-gate-review.js` uncommitted) — SAME failure mode as s21→s22
  ([[twin-session-hazard]]). s24 recovered it: no twin (`ps`: one `claude`, pts/1),
  confirmed the workflow genuinely completed (5 subagent transcripts under session
  36caf224) and spot-verified BOTH HIGHs against real code (service.go:267-272
  finalizes on ANY `Status` err incl. transient — should be ErrUnknownJob only;
  driver.go:31-33/50-54/108-112 wedges the chain `running` on any transient DB err,
  sweep boot-only, Resume 409-forever, no mail) → both REAL. Committed the two
  artifacts (**3a76918**); filed **WU-037** (2 HIGH — transient-error resilience,
  one mock-to-real root cause) / **WU-038** (schedule launchable gate, MED) /
  **WU-039** (restore.yml re-fetch footgun, MED) / **WU-040** (LOW bundle:
  parse/backfill/job_id); finding 8 (staging orphan) → M4. What HELD: EVERY
  guardrail invariant — no restore-without-safety-dump, no bare restore over
  /api/runs or the scheduler, no forged-webhook outcome, no `location` crossing the
  seam, no secret reachable, audit append-only, one-mail-per-halt. No Go/FE change
  this session (docs-only recovery; gate CHECK not re-run — tree unchanged since
  c13de14 except docs). Also s24: pre-approved ALL Bash in the gitignored
  `.claude/settings.local.json` (bare `Bash` allow at the top; deny + ask
  guardrails kept — sudo/rm-rf/force-push blocked, systemctl stop / docker compose
  down / volume rm / .env writes still gated) to end the per-command prompting.
- **Status (s22, WU-036 CLOSED — the product's loop is closed):** the SAME portal
  drives a REAL `pg_restore` of a REAL dump onto a live Postgres target through
  the `verify → safety_dump → restore` chain, behind the UNCHANGED engine.Adapter
  seam. SPEC-036 = `docs/specs/restore-playbook.md`; human twin + M3 exit evidence
  = **`docs/demo-m3.md`** (8 beats, 13.5 min budgeted ≤ the 15-min AC).
  **SPEC-036's open question is RESOLVED: Semaphore DOES forward the task
  `environment` as ansible `--extra-vars`** (mini-ADR 1 holds; the
  `lookup('env',…)` fallback dropped; no code change) — pinned by running verify
  first as the extra-vars smoke test, plus both mini-ADR 6 failure paths (no
  environment → preflight fails with ok=0; wrong checksum → assert fails, zero
  target contact). Bootstrap now yields **verify=4, restore=5** (dump 3, smoke 1).
  Rehearsal (isolated portal :8099, release binary): dump pgtarget → `DROP TABLE
  widget` → POST /api/restore → chain success → **widget back, 4 rows, original
  timestamps**; the `'safety'` artifact is itself `pg_restore --list`-restorable
  and its minio sha == its registry checksum. Tamper → chain HALTS at verify with
  steps 2+3 `run_id: null` (never created), target untouched, exactly ONE chain
  mail; fix + resume → success. Mock-vs-semaphore: same binary, same chain, only
  `location` differs (NULL vs `s3://…`). All 5 secrets absent from DB/log/task
  output. O-4 annotated RESOLVED in DECISIONS.md (dump half WU-034 + restore half
  WU-036) — a SPEC-036 scope item slice (a) had missed. Gate CHECK-EXIT:0
  (docs-only session, no Go/FE change). Drill torn down; demo :8080 untouched
  (same PID 2506685, healthz 200).
- **s22 also recovered s21's lost bookkeeping:** s21 committed slice (a) as
  868425a and an **ssh reset killed it before it could checkpoint** (no
  STATE/JOURNAL entry, commit unpushed). Tree was clean, no twin (`ps` + ancestry
  per [[twin-session-hazard]]); slice (a) re-verified rather than trusted (gate
  CHECK-EXIT:0 + `--syntax-check` EXIT:0 on all 3 playbooks), then pushed.
  SLICE (a) = SPEC-036 (6 mini-ADRs); `playbooks/verify.yml` (step 1 — `mc cat |
  sha256sum` vs expected checksum, ZERO target contact); `playbooks/restore.yml`
  (step 3 — `mc cp` → RE-verify sha256 → `pg_restore --clean --if-exists
  --no-owner --no-privileges --single-transaction`); `semaphore.go` `StartJob`
  forwards the fail-closed allowlist `forwardVars = {artifact_name, checksum}` as
  the task `environment` — `instance`/`artifact_id` never cross, params-free ops
  post a byte-identical body (+2 tests); `semaphore-bootstrap.sh` creates the
  verify + restore templates. NO migration, NO UI, NO chain/catalog/recipe change;
  MockEngine + all tests untouched (ADR-002).
- **Status (s20, WU-035 CLOSED):** the SAME portal now stores a REAL pg_dump in
  object storage. Twin-recovery session (see checkpoint log): adopted ~82 min of
  a parked twin's uncommitted, coherent WU-035 work after `ps`/`who`/per-pts
  `sshd` checks + SPEC verification, SIGTERM-reaped the idle twin, gated + drilled
  + committed. NO Go/FE change — the SPEC-034 result line already carries
  `location`; only its value changed (path → `s3://…` URL), opaque to the portal.
  compose gained `minio` (RELEASE.2025-09-07, :9000 API/:9001 console, `miniodata`
  vol) + `createbuckets` one-shot (mc mb --ignore-existing; minio has no
  healthcheck so the one-shot IS the readiness gate); the runner image bakes `mc`
  (RELEASE.2025-08-13); bootstrap folds `MC_HOST_dbportal` (jq @uri credentialed
  alias) + `MC_CONFIG_DIR=/tmp/.mc` + `DBPORTAL_BUCKET` into pgtarget-env (id 3,
  engine-side, ADR-004); `playbooks/dump.yml` uploads `mc cp` AFTER the sha/size
  stat, `mc stat --json` asserts stored size == local, THEN emits the result line
  (`location='s3://'~bucket~'/'~name`), then best-effort staging `rm`
  (failed_when:false) — the `artifacts` volume is STAGING ONLY now. O-1 annotated
  RESOLVED in DECISIONS.md. Gate GREEN (CHECK-EXIT:0, golangci 0, vitest 116/116).
- **Status (s19, WU-034 CLOSED):** the SAME portal now drives a REAL `pg_dump`
  of a compose target through Semaphore. slice (a) = infra + playbook (compose
  `pgtarget` postgres:16 seeded widget/ledger/ledger_totals via
  `infra/fixtures/pgtarget-init.sql`, host :5433; custom runner image
  `infra/semaphore.Dockerfile` = v2.17.39 + postgresql16-client + writable
  `/artifacts` owned 1001:0, compose `build:`s it as `dbportal-semaphore:v2.17.39-pg16`;
  `artifacts` named volume; `playbooks/dump.yml` = `pg_dump --format=custom
  --no-owner --no-privileges`, creds via engine-side libpq env — NO secret in
  the playbook, ADR-004; emits ONE `DBPORTAL_RESULT=<base64 json
  {name,size_bytes,sha256,location}>` line; bootstrap `pgtarget-env` env (id 3)
  + `dump` template (**id 3**); `dev-targets.csv` NOT `instances.csv` — 8-row
  test coupling). slice (b) = Go: `engine.Artifact` gains `Location`;
  `semaphore.go` `Status` parses the result line on terminal SUCCESS
  (regex `DBPORTAL_RESULT=([A-Za-z0-9+/=]+)` → base64 → json; nil on
  missing/bad/no-name, run still success); `finalize` writes `artifact.location`
  (`NULLIF($,'')`, so mock stays NULL). Tests: engine stub-HTTP parse cases +
  a fixed StatusMapping /output route + runs `TestArtifactLocationPersisted`
  (real Location stored) — mock's `TestArtifactRegisteredOnSuccess` still NULL.
  Live drill (isolated portal :8099 + portal_drill, semaphore engine,
  `dump:3,smoke:1`): dump on pgtarget → success, registry row REAL
  sha256/size/location (checksum == file's `sha256sum`), `pg_restore --list`
  OK; bad-creds dump → failed + no artifact + notify mail; no secrets in
  DB/log. Gate GREEN (CHECK-EXIT:0, golangci 0 issues, itest ran LIVE, vitest
  116/116). Drill torn down; demo :8080 untouched.
  (s18/WU-033's SemaphoreAdapter + webhook detail now lives in Standing context.)
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **START WU-039** (M3-gate fix — item 4, MEDIUM; BACKLOG WU-039). `restore.yml`'s
   fetch uses `creates: {{ staging_path }}` (restore.yml:65) on a DETERMINISTIC path
   on the persistent shared `/artifacts` volume: an interrupted fetch (task timeout,
   runner restart, killed container) leaves a partial file; the next attempt sees the
   path exists, SKIPS the fetch, hashes the partial, and the sha256 compare fails →
   the operator is told their GOOD backup is "tampered" — worst on the product's own
   advertised Resume-after-halt path. Fix: don't gate the fetch on `creates:` for a
   deterministic shared path (remove-first / unique-or-per-run temp path / always
   re-fetch); apply the same scrutiny to dump.yml's staging if it shares the pattern.
   **WU-039 WANTS A LIVE RESTORE DRILL** (demo-m3.md recipe) — re-check the dev stack
   is up first (was UP at s22: mailpit/minio/pgtarget/postgres/semaphore; NOT touched
   s23-s26). Verify: leftover partial `restore-<name>` on staging → re-fetches full +
   verifies clean (NOT a false "tampered"); genuinely corrupt object still halts at
   verify; `ansible-playbook --syntax-check` clean; live restore drill still passes
   end-to-end. Then **040** (LOW bundle: parseResultLine sha/size + 0009 backfill
   retention_class + job_id uniqueness — dev-only triggers, Go-test-shaped). Land ALL
   before any Phase-4 WU (M1/M2 protocol). ONLY then groom + start Phase 4.
   - WU-038 (s26) is DONE — the schedule launchable gate. If revisiting: the fix is
     `schedule.go` Create's opening gate (`!ok || !op.Launchable`); the schedules
     handler already maps `runs.ErrUnknownOperation` → 400 (no handler change was
     needed). Test lives in `TestCreateValidation` (schedule_test.go).
   - WU-037 (s25) is DONE — the two HIGHs. If revisiting: the shared `backoff`
     helper is duplicated in runs/service.go + chain/driver.go (packages stay
     decoupled by design); `MaxStatusErrors`/`MaxReadErrors` are Service fields
     tests dial low for fast give-up.
   - Organizational note reached (BACKLOG + ROADMAP): the **security vetting
     package** (ARCHITECTURE §8.2) becomes submittable at M3 exit.
   - Dev facts carried: semaphore project 1, templates **dump:3, verify:4,
     restore:5, smoke:1**, pgtarget-env **3**; pgtarget host-port 5433, seeded
     appdb (reset = `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` + replay
     `infra/fixtures/pgtarget-init.sql` — the init only auto-runs on a fresh
     volume); minio bucket `dbportal-artifacts` (:9000); `.env` has the Semaphore
     token + `PGTARGET_*` + `MINIO_*`. Persistent `.env` STAYS
     `PORTAL_ENGINE_NONPROD=mock` + `PORTAL_SEMAPHORE_TEMPLATES=smoke:1`; drills
     use an isolated portal ([[live-drill-isolation]]) — full recipe in
     **docs/demo-m3.md** (setup + reset §§).
   - WU-040 is Go-test-shaped (may not need the live stack); WU-039 wants a live
     restore drill. Dev stack was UP + healthy at s22
     (mailpit/minio/pgtarget/postgres/semaphore) — re-check before any live work.
2. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.

## Blocked / needs user

- Nothing.
- HEADS-UP (WU-036 DONE, s22): the Semaphore catalog now has **verify (id 4)** +
  **restore (id 5)** templates alongside dump (3) + smoke (1), all pinned to
  `pgtarget-env` (id 3); the bootstrap creates them idempotently and prints the
  ids. The **full drill/rehearsal recipe now lives in `docs/demo-m3.md`** (setup
  + reset §§) — prefer it over the WU-035 recipe below for anything restore-shaped.
  Two drill leftovers, both harmless: the bucket holds a handful of drill dumps
  incl. ONE permanently-corrupt object (`appdb-20260716T021530Z-80914329.dump` —
  s22 tampered it before a failed backup; nothing can match its recorded
  checksum again; no retention until M4), and `pgtarget` currently sits in the
  RESTORED state (widget 4 / ledger 200) rather than a freshly-seeded volume —
  identical content, but if a future test needs pristine, replay the reset in
  demo-m3.md. Scratch DBs `portal_drill036`/`portal_drill036m` were dropped.
- HEADS-UP (WU-035 DONE, s20): the dev stack now also runs **`minio`**
  (dbportal-dev-minio-1, 127.0.0.1:9000 API / :9001 console) with the artifact
  bucket **`dbportal-artifacts`** (created idempotently by the `createbuckets`
  one-shot; `miniodata` volume). The `semaphore` runner image
  (`dbportal-semaphore:v2.17.39-pg16`) now also bakes the **`mc`** client. The
  `pgtarget` service (postgres:16, 127.0.0.1:5433, seeded appdb) is unchanged
  from s19. To bring the WHOLE dev stack up from a fresh clone:
  `docker compose -f infra/compose.yaml --env-file .env up -d --build --wait`
  then `set -a; . ./.env; set +a; sh infra/semaphore-bootstrap.sh` — the
  bootstrap now folds the object-store creds (`MC_HOST_dbportal` + `MC_CONFIG_DIR`
  + `DBPORTAL_BUCKET`) into `pgtarget-env` (id 3) alongside the `PG*` vars, and
  still creates the `dump` template (id 3) + prints `SEMAPHORE_DUMP_TEMPLATE_ID`.
  Object-store + pgtarget creds live in `.env` as `MINIO_*` / `PGTARGET_*`
  (engine-side only, ADR-004; the Go portal NEVER reads them). Persistent `.env`
  stays `PORTAL_SEMAPHORE_TEMPLATES=smoke:1` + `PORTAL_ENGINE_NONPROD=mock`; the
  `dump:3` map + semaphore engine are used only by an isolated drill portal. The
  `artifacts` volume is STAGING ONLY now (dump.yml `rm`s the local copy after a
  verified upload); a few pre-WU-035 `.dump` files linger there (harmless) and
  the bucket holds a handful of drill dumps (harmless — no retention yet, M4).
- HEADS-UP (Semaphore, WU-033 DONE): the compose `semaphore` service is LEFT
  RUNNING on the VM at 127.0.0.1:3000 (`docker ps` → dbportal-dev-semaphore-1).
  The skip-gated itest depends on it PLUS the API token in gitignored `.env`
  (`PORTAL_SEMAPHORE_API_TOKEN`, minted by infra/semaphore-bootstrap.sh);
  `.env` also now carries `PORTAL_SEMAPHORE_WEBHOOK_SECRET` (used by slice-b's
  drill). If the service was restarted/reset (BoltDB in the `semaphore_data`
  volume — a `docker compose down -v` wipes it), re-run `docker compose -f
  infra/compose.yaml --env-file .env up -d --wait semaphore` then `set -a;
  . ./.env; set +a; sh infra/semaphore-bootstrap.sh` and update
  `PORTAL_SEMAPHORE_API_TOKEN` in `.env` with the freshly-printed token (tokens
  are shown ONCE, never re-listable). Verify: `curl 127.0.0.1:3000/api/ping`
  → 200, and `go test ./internal/engine/ -run TestSemaphoreIntegration` passes
  (not skips). NOTE the persistent `.env` stays `PORTAL_ENGINE_NONPROD=mock`
  with `PORTAL_SEMAPHORE_TEMPLATES=smoke:1` — the s18 full-portal drill ran an
  EPHEMERAL isolated portal (`portal_drill` DB on the dev PG, port :8099, env
  `PORTAL_ENGINE_NONPROD=semaphore` + `dump:1,smoke:1`), now torn down (portal
  stopped, `portal_drill` dropped). The `dump→smoke` template map was
  drill-only; WU-034 makes `dump` a REAL playbook template.
- HEADS-UP: demo portal runs as transient systemd unit `dbportal-demo` on
  :8080. **NOT touched at s18** (the WU-033 drill used an isolated portal, not
  the demo). Still the 270e665 WU-031 binary with MockEngine; dev DB at 0010
  (WU-031 adds no migration). Env `PORTAL_AUTH_MODE=fake` + demo break-glass
  hash (password "demo-glass", hash only in the unit env — recover via
  `systemctl show dbportal-demo -p Environment`; NOTE stopping the unit
  DELETES it and the env — the recipe is in JOURNAL s16; s17 reused the same
  captured hash `$2a$10$BQnBaouoM6y…olo5.`). Sign in dba1/dba1. Dev DB history
  now also includes the s17 live restore: chain 2 (restore on hr-test,
  success) with runs 25/26/27 and a 'safety' artifact on hr-test (run 26).
  Before any live check that runs its own portal: `systemctl stop
  dbportal-demo` — bind-in-use + two-portals-one-DB hazard. ALSO (bit s10
  twice): a backgrounded `portal &` may report a wrapper PID in `$!` — always
  kill the PID that `ss -ltnp` shows holding :8080.

## Standing context (stable facts worth re-stating)

- MVP scope = D1–D7 (DECISIONS.md). MockEngine is the forever default for dev
  + ALL tests (ADR-002); real Semaphore is opt-in per WU-033 (below).
- **SemaphoreAdapter (WU-033, SPEC-033 = docs/specs/semaphore.md):** real
  engine for the non-prod class, opt-in via `PORTAL_ENGINE_NONPROD=semaphore`,
  behind the UNCHANGED engine.Adapter seam. `internal/engine/semaphore.go` =
  StartJob/Status/StreamLogs(poll replay-then-follow)/Cancel over Semaphore's
  REST API; JobID = the durable Semaphore task id (no in-process job state;
  ErrUnknownJob falls out for a task Semaphore forgot). Tag→template-id is
  config (`PORTAL_SEMAPHORE_TEMPLATES=tag:id,…`), fail-closed on an unmapped
  tag. Webhook `POST /api/engine/semaphore/webhook` (server/webhook_http.go) is
  SESSION-LESS, shared-secret constant-time (`hmac.Equal`, header
  `X-Portal-Webhook-Secret`, empty secret ⇒ 401-only), reads ONLY the task id,
  re-polls the REAL task → `runs.ReconcileByJobID` (find run by job_id → ONE
  guarded Status→finalize). POLL is the finalization truth (ADR-002): the
  webhook only accelerates; webhook-down still finalizes; a lying payload can't
  force an outcome. Disjoint per-class config = guardrail 3 structural (Registry
  panics on a shared instance). When semaphore is on, the runs watcher polls at
  `PORTAL_SEMAPHORE_POLL_INTERVAL` (gentle, webhook-accelerated), not the mock's
  500ms. Compose Semaphore v2.17.39 BoltDB, creds `.env`-only; ONE skip-gated
  itest. Real dump + storage landed in WU-034/035 (below).
- **Real dump + object storage (WU-034 SPEC=docs/specs/dump-playbook.md,
  WU-035 SPEC=docs/specs/artifact-storage.md):** the SAME portal drives a REAL
  `pg_dump` of the compose `pgtarget` through Semaphore and stores the bytes in
  **minio** — behind the UNCHANGED engine.Adapter seam, nonprod-only, opt-in.
  `playbooks/dump.yml` = pg_dump -Fc (engine-side libpq creds) → sha256/size
  stat → `mc cp` upload to `s3://dbportal-artifacts/<name>` → `mc stat` size
  assert → emit ONE `DBPORTAL_RESULT=<base64 json {name,size_bytes,sha256,
  location}>` line → best-effort staging `rm`. `mc`/`pg_dump` read creds ONLY
  from the Semaphore `pgtarget-env` Environment (id 3: `PG*` + `MC_HOST_dbportal`
  + `MC_CONFIG_DIR` + `DBPORTAL_BUCKET`) — the playbook names NO credential
  (ADR-004). Go side (WU-034, unchanged by WU-035): `engine.Artifact.Location`;
  `semaphore.go` parses the result line on terminal SUCCESS (nil on missing/bad
  → run still succeeds, just no artifact); `finalize` writes `artifact.location`
  (`NULLIF`, mock stays NULL). Upload failure aborts the play → run failed, ZERO
  artifact (a dump that isn't stored registers nothing). Portal never touches
  object bytes — it stores/passes `location` strings; GET /api/artifacts does
  NOT expose `location` (SPEC-030). WU-036 restore reads `location` and fetches
  engine-side the same way.
- **Real restore (WU-036, SPEC-036 = docs/specs/restore-playbook.md; human twin
  = docs/demo-m3.md):** the WU-031 restore chain's three steps now have REAL
  playbooks under the opt-in Semaphore adapter — mock→semaphore same code, seam
  UNCHANGED. `playbooks/verify.yml` (step 1) = `mc cat | sha256sum` vs the
  expected checksum, names NO PG connection ⇒ ZERO target contact; a mismatch
  halts the chain BEFORE the safety dump. Step 2 `safety_dump` reuses `dump.yml`
  unchanged (registers its `'safety'` artifact). `playbooks/restore.yml` (step 3)
  = `mc cp` → RE-verify sha256 (defense in depth) → `pg_restore --clean
  --if-exists --no-owner --no-privileges --single-transaction` into the LIVE
  target (atomic: a mid-restore failure rolls back, no half-restored target;
  O-4 RESOLVED). Neither emits `DBPORTAL_RESULT` — verify/restore register no
  artifact, so the task status IS their outcome (only dump/safety_dump emit the
  sentinel). The fetch path is RECONSTRUCTED engine-side from `DBPORTAL_BUCKET` +
  the `artifact_name` var — **`location` never crosses the seam**. Go: ONE change
  — `semaphore.go` `StartJob` forwards the fail-closed allowlist `forwardVars =
  {artifact_name, checksum}` as the task `environment`, which **Semaphore
  forwards to ansible-playbook as `--extra-vars` (VERIFIED s22, task 2147483617
  — mini-ADR 1 holds)**; `instance`/`artifact_id` never cross; a params-free op
  posts NO `environment` key ⇒ byte-identical body to WU-033/034. Both playbooks
  preflight-assert the lineage (mini-ADR 6) so a mis-wire fails locally with zero
  target contact. Templates: verify **4**, restore **5**.
- **Chain engine (WU-032, SPEC-032 = docs/specs/chains.md):** chains are
  portal-assembled ONLY (no client create API; 031's restore POST is the
  first assembler). internal/chain imports catalog/runs, NEVER engine —
  steps ARE runs through runs.Service.Start (full guardrail/audit path),
  attributed `chain:<mover>` (creator, then resumer). Halt = ONE chain
  mail; run mail suppressed via StepRunFilter wired in main. Stored
  confirm replays verbatim per step (env promotion mid-chain fails the
  ritual visibly). Boot sweep AFTER runs.SweepOrphans. Step status derived
  from linked run; superseded runs keep history, leave the strip. UI
  step-status vocabulary = `pending` + run states, fixed; 031 consumes
  as-is.
- **Restore (WU-031, SPEC-031 = docs/specs/restore.md):** the FIRST chain
  assembler. `POST /api/restore {artifact_id, target, confirm, reason}` (dba)
  builds a `kind=restore` chain via the pure recipe `internal/restore.Steps` =
  `[verify, safety_dump, restore]` — the safety dump is a LITERAL in the slice,
  so "no skip affordance" is structural, not a runtime check (D1 incident). The
  ops verify/safety_dump/restore are catalog entries with `Launchable=false`;
  `runs.Start` refuses a non-launchable op unless `StartRequest.Internal` is
  set, which ONLY chain/driver.go does → bare restore over POST /api/runs or
  the scheduler = 400. `finalize` stamps `artifact.retention_class` from the
  catalog op ('safety' for safety_dump, else 'standard'); last_backup_at (dump
  only) ignores safety dumps. Prod target ritual enforced once in
  `chain.Create` before any row. 034/036 add the real verify/restore playbooks
  behind this same recipe. UI = RestoreDrawer from MyDatabases.
- **Delegation model (6 successes: WU-019, WU-020b, WU-021b, WU-022b,
  WU-032-UI, WU-031-UI):**
  implementation WUs with a tight brief run on a Sonnet 5 general-purpose
  subagent (Agent tool, `model: sonnet`, no git); architect (Fable/Opus) writes
  specs/briefs, implements security/concurrency-sensitive slices itself,
  reviews diffs, runs the gate, live-verifies, commits. WU-031-UI (s17,
  124k tok): reviewed line-by-line, only issue was a prettier gap the fe gate
  doesn't check but the pre-commit hook does (`npm run fmt` before committing
  delegated FE work). Workflow tool only for multi-agent pipelines (gate
  reviews). Phase 3 delegation candidates: playbook/compose scaffolds of
  033/034; chain engine core, SemaphoreAdapter concurrency, ritual/authz
  seams = architect.
  (WU-023 + WU-030 were architect-implemented end to end: S-sized, no/thin
  UI — delegation overhead exceeds the diff.)
- **Artifact registry (WU-030, SPEC-030 = docs/specs/artifacts.md):**
  `artifact` table (0009) = what can be restored; run's artifact_* columns =
  what this run produced — same values, same finalize tx (dual write,
  mini-ADR 1). Registration rides the WU-016 guarded terminal transition +
  UNIQUE(run_id); only state='success' with a non-nil engine artifact
  registers. retention_class 'standard'|'safety' stamped at registration —
  'safety' writer + Start→finalize plumbing = WU-031. `location` dormant
  until 034/035. GET /api/artifacts?instance= required-param read,
  unknown 404, newest 50, `location` not exposed.
- **Scheduler (WU-022 + WU-024 hardening, SPEC-022 = docs/specs/scheduler.md):**
  internal/schedule.Service = store + executor; tick loop (Tick 10s, Jitter
  60s defaults) fires `enabled AND next_fire_at <= now()` through
  runs.Service.Start as `schedule:<created_by>` with the STORED creation-time
  confirm (migration 0008) verbatim. next_fire_at persisted PRE-JITTERED,
  NULL iff disabled; stamp CASE-guards a concurrent disable (::timestamptz
  cast — bare param breaks pgx inference SILENTLY). Misfire = coalesced
  catch-up; overlap probe = ANY live run on the instance (manual runs block
  scheduled fires too); FireTimeout 30s; SetEnabled idempotent.
- **AuthZ (WU-021, SPEC-021):** roles in portal DB (role/user_role).
  Mutations need dba (requireRole); reads session-only. Denials =
  auth_event `authz.denied` (store failure = 500 never 403). Audit actor
  is an EXPLICIT param (StartRequest.Actor, Cancel actor arg) — never ctx
  magic; run.finished inherits submitter; cancel writes
  run.cancel_requested intent-first (3 rows on canceled runs). Prod ritual
  server-side: Confirm must equal instance name on prod. Grants at boot
  per auth mode; break-glass seeded dba in 0006.
- **AuthN (WU-020 + WU-025 honesty, SPEC-020):** every /api route needs a
  session except POST /api/auth/login; healthz + SPA public. Sessions in
  portal DB (sha256, 12h TTL); cookie portal_session httpOnly SameSite=Lax.
  PORTAL_AUTH_MODE ldap (default, fails closed) | fake (dba1/dba1,
  dba2/dba2) | off (local-dev). Login lowercases the username at the seam
  (one human = one actor; passwords NOT folded). Break-glass via
  PORTAL_BREAKGLASS_HASH, every use alarmed. auth_event append-only.
  authn.From(ctx) carries identity. Frontend: bootstrap gate (401=login,
  else retry screen), honest sign-out, /login outside Shell, api.ts
  onUnauthorized (401 only). ldap+CookieSecure=false → boot Warn.
- **Golden flow (ADR-011):** `backend/e2e/golden_flow_test.go` must stay
  green EVERY session. Beat 0 login; WU-021 beats (ritual 400, actor
  pinned, requested_by); Beat 8 (WU-022): live executor fires a due
  schedule, schedule:dba1 attribution; WU-030 asserts artifact registration
  w/ origin FK on both dump beats; Beat 9 (WU-032): 3-step chain halts on
  injected step-2 failure with ONE chain mail, HTTP resume re-fires as
  chain:dba1 to success. WU-031 extends with the restore chain.
  Human twin = docs/demo-m1.md (beats 1–7 + §8 schedules preview).
- Guardrails (WU-015, SPEC-015): EnvBanner on RunDetail + LaunchDrawer +
  schedule drawer; prod ritual = typed exact instance name, client AND
  server, launch AND schedule-create; Registry.Register panics on
  cross-class adapter sharing; env stamping pinned by tests.
- **Windows (WU-023, SPEC-023 = docs/specs/windows.md):** internal/window
  pure parser (`Day HH:MM-HH:MM`, weekly, wrap-capable, server-local —
  same clock as cron). runs.Start stamps audit_event.window_warned
  (column pre-provisioned in 0003) on run.submitted only; both launch
  paths share it. instance JSON window_state inside|outside|null is
  server-computed — the client never parses window text. D6: every
  parse/eval failure = no warning, never a block; garbage logs once per
  instance per process.
- Inventory (WU-010): cluster/instance tables (name natural key, env CHECK,
  window raw text), quarantine tables; `portal import <csv>` idempotent;
  engine.ClassForEnv = env authority. size_gb canonicalized (WU-018).
- Instance API (WU-011): GET /api/instances[?env=], detail 404 JSON;
  MyDatabases cards/table. last_backup_at = newest SUCCESSFUL dump.
- Runs (WU-012, SPEC-012): run mutable; audit_event append-only, env +
  playbook_tag stamped, job_id on run.finished only. runs.Service = ONLY
  Registry caller; single-finalizer guards incl. watcher mirror (WU-016);
  SweepOrphans best-effort on boot. POST /api/runs: 400/403/404/413/502.
- Run detail + logs (WU-013, SPEC-013): SSE replay→follow, ONE `end`,
  404/410; logs NOT persisted; cancel 202 async; client retry w/ backoff
  (WU-019).
- Notify + Activity (WU-014, SPEC-014): post-commit mail for
  failed|canceled (who/what/where/status + link ONLY). GET /api/runs
  filters ANDed (state/env/operation/instance/requested_by). Activity:
  chips + `by` filter, CSV export (formula neutralization).
- Test helpers: testutil.MigratedDB(t) scratch DB (skips w/o compose PG);
  testutil.DB(t) dev DB; testutil.FakeSMTP(t); scratchDSN(t) in db pkg
  tests (migration walks). goose down reverts ONE migration (0006 down
  caveat: fails if authz.denied rows exist — spec'd).
- Single-binary (WU-006): build:release embeds frontend/dist; degraded-mode
  healthz 503 without DB. Dev: Vite :5173 proxies /api → :8080.
- Engine seam (WU-005): engine.Adapter; Registry.For + ClassForEnv fail
  closed; params["mock_fail_at"] injects failure (tests only); MockConfig
  StepDelay throttles job speed (tests).
- Chassis/frontend (WU-003/004): config.Load env>file>defaults;
  server.NewRouter = test seam; react-router v7 ('react-router'); tokens
  ONLY in src/index.css; lib/api.ts = ALL failures are ApiError
  (errorDetail unwraps the {"error": ...} envelope). tsc strict.
- VM toolchain: Docker 29.6.1, Compose v5.3.0, node 22.23.1, Go 1.26.4,
  golangci-lint 2.12.2, gh 2.96.0 (authed ios9000). CI = check.yml, NO
  Postgres service (DB tests + golden flow skip there; VM gate is the real
  gate). Deps: go-ldap/v3, x/crypto, robfig/cron/v3 (direct).
- GitHub: private repo `ios9000/db-portal`; VM pushes via write deploy key.
- Research corpus on workstation (ADR-006); need a file → ask user to copy it.
- Verify-procedure gotcha (s05, s07, s10×2): pkill -f matches the tool shell;
  `$!` can be a wrapper PID. Always kill the exact PID from `ss -ltnp`.

## Checkpoint log (last 3, newest first)

- 2026-07-16 — **WU-038 DONE (s26) — schedule.Create launchable gate**: the
  MEDIUM guardrail-honesty fix (M3-gate item 3). `schedule.Create` tested catalog
  EXISTENCE only, so `POST /api/schedules {operation:"restore"}` (or verify/
  safety_dump) returned 201 where SPEC-031 promises 400 — the guardrail HELD
  (executor.fire never sets Internal → runs.Start rejected the fire) but the
  schedule was permanently, silently broken (every tick → `last_fire_status='error'`,
  no run, no mail, next_fire_at advancing forever). FIX: one line — Create mirrors
  runs.Start's `!op.Launchable` gate (`op, ok := catalog.ByID; if !ok ||
  !op.Launchable → runs.ErrUnknownOperation`); the schedules handler already maps
  that to 400 "unknown operation" (no handler change). `TestCreateValidation`
  extended to the three existing-but-non-launchable ids; the `count==0` assertion
  proves no failed create writes a row. GATE: CHECK-EXIT:0 (golangci 0, schedule
  pkg FRESH -race 10.3s, golden flow not-skipped, vitest 116/116). No migration/
  UI/seam change; Go-test-shaped, no live stack. Active → **WU-039**.
- 2026-07-16 — **WU-037 DONE (s25) — transient-error resilience, both M3-gate
  HIGHs**: one mock-to-real root cause fixed in two places. `runs.Service.watch`
  now finalizes failed ONLY on `engine.ErrUnknownJob`; every other Status error is
  transient → bounded retry+backoff (`MaxStatusErrors`, cap 30s) then an honest
  give-up ("engine status unavailable after N attempts"), killing the lying "engine
  lost the job" on every blip. `chain.drive` retries all three DB reads
  (load/next/step-watch) up to `MaxReadErrors` (cap 15s) then **halts + notifies**
  rather than silently exiting and wedging the chain `running` forever (Resume works,
  DBA mailed). Chose bounded-retry-then-halt over a periodic sweep (self-halts
  promptly, symmetric with the watcher, main.go UNCHANGED; boot sweep still covers
  process death). 5 new -race tests drive the real watch/drive goroutines with
  injected transient errors. GATE: CHECK-EXIT:0 (golangci 0, go test -race all pkgs,
  golden flow not-skipped, vitest 116/116). No UI/migration/seam change. Active →
  **WU-038**.
- 2026-07-16 — **M3 GATE PASSED — recovered s23's uncommitted review (s24)**:
  session `36caf224` (s23, today) ran the multi-agent M3 gate — workflow
  `wf_49ca1969-37a`, 5 Sonnet reviewers, 605k tok / ~19.4 min, architect-verified
  inline → **8 findings, 8 confirmed, 0 refuted, 4 re-graded down, NO criticals →
  GATE PASSES with fix WUs** (`docs/agent/reviews/m3-gate.md`). An ssh reset killed
  s23 before checkpoint (doc + `.claude/workflows/m3-gate-review.js` uncommitted,
  STATE/JOURNAL unwritten). s24 recovered it: no twin (`ps`: one `claude`, pts/1);
  confirmed the workflow completed (5 subagent transcripts under 36caf224) + BOTH
  HIGHs spot-verified against real code (service.go:267-272 finalizes on ANY
  `Status` err; driver.go:31-33/50-54/108-112 wedges the chain `running` on any
  transient DB err) → real. Committed the artifacts (**3a76918**); filed WU-037 (2
  HIGH — transient-error resilience) / WU-038 (schedule launchable gate) / WU-039
  (restore.yml re-fetch) / WU-040 (LOW bundle); finding 8 → M4. Every guardrail
  invariant HELD. No Go/FE change (docs-only recovery). Also s24: pre-approved all
  Bash in the gitignored `.claude/settings.local.json` (bare `Bash` allow; deny +
  ask guardrails kept). Active → **WU-037**.
