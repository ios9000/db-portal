# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** PHASE 3 (M3) — **WU-036 (Restore playbook — real + M3 rehearsal, M)
  is NEXT** (the LAST M3 WU; then the M3 gate review).
  **WU-035 (O-1 storage: minio, S) DONE s20** (fe70fbc impl + closeout) — dump
  artifact bytes now live in object storage; all 3 AC + Verify met, live drill
  passed on the VM (object in minio, mc-side hash match, injected upload
  failure, no-secrets). SPEC-035 = `docs/specs/artifact-storage.md`.
  **WU-034 (Dump playbook — real) DONE s19.** Execution order 030 → 032 → 031 →
  033 → 034 → 035 → **036**.
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
- **Status (s18, WU-033 CLOSED):** slice (b) landed the webhook accelerator +
  `runs.ReconcileByJobID` + the fallback/auth tests + the live drill. The SAME
  portal now drives REAL Semaphore for the non-prod class, opt-in via
  `PORTAL_ENGINE_NONPROD=semaphore`, behind the UNCHANGED engine.Adapter seam.
  Webhook = `POST /api/engine/semaphore/webhook`, session-less, shared-secret
  constant-time (`hmac.Equal`, header `X-Portal-Webhook-Secret`), empty secret
  ⇒ 401-only (poll-only mode); it reads ONLY the task id and re-polls the REAL
  task — POLL is the finalization truth (ADR-002), so webhook-down still
  finalizes and a lying payload can't force an outcome. `ReconcileByJobID`
  (find run by job_id → one guarded Status→finalize) is the webhook's only
  entry into runs; no-ops on unknown/terminal/non-terminal/ErrUnknownJob. Also
  wired: when `EngineNonProd=semaphore`, the runs watcher polls at
  `PORTAL_SEMAPHORE_POLL_INTERVAL` (not the mock's tight 500ms) so a real REST
  engine isn't hammered and the webhook meaningfully accelerates.
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **START WU-036 (Restore playbook — real + M3 rehearsal — M).** Read its
   BACKLOG entry + context brief: ARCHITECTURE §3 (restore) + §7
   (do-not-discover-twice); SPEC-031 (chain assembly + verify-step semantics);
   playbooks/dump.yml (the dump/result-line + `mc` engine-side pattern to
   mirror); docs/demo-m1.md (rehearsal-doc pattern); infra/compose.yaml.
   Deliver: `playbooks/restore.yml` = fetch the artifact FROM `location` (minio;
   `mc cp <alias>/… → local` with the `s3://`→`<alias>/` rewrite, SPEC-035
   mini-ADR 2), **sha256 verify BEFORE touching the target** (mismatch = fail,
   ZERO target writes), `pg_restore` with a vetted flag set (`--clean --if-exists`
   vs drop/create — O-4-style mini-ADR), machine-readable result line; catalog
   `restore` template pinned to it (the WU-031 restore recipe's `restore` op
   becomes REAL — mock→semaphore same code, behind the unchanged Adapter seam).
   Then `docs/demo-m3.md` (human twin, demo-m1.md pattern) = the M3 exit
   rehearsal: seed → portal dump → destroy a table → portal restore
   (verify→safety_dump→restore chain) → data verified back + safety artifact
   registered; halt+resume on injected failure; mock-vs-semaphore same-code beat.
   Write SPEC-036 just-in-time. AC: live rehearsal ≤15 min on the VM release
   binary, every M3 exit criterion ticked in the doc; checksum-tamper → chain
   halts at verify, target untouched, mail, fix+resume → success; the safety-dump
   artifact is itself `pg_restore --list`-restorable. `npm run check` green.
   - Dev facts carried: semaphore project 1, dump template **3**, restore
     template = **NEW** (bootstrap creates it), pgtarget-env **3**, smoke
     template 1; pgtarget host-port 5433, seeded appdb; the minio bucket
     `dbportal-artifacts` (:9000) holds REAL dumps to restore from; `.env` has
     the Semaphore token + `PGTARGET_*` + `MINIO_*`. Persistent `.env` STAYS
     `PORTAL_ENGINE_NONPROD=mock`; drills use an isolated portal (see the WU-035
     drill recipe below / [[live-drill-isolation]]).
2. Housekeeping note (carried): demo-m1.md header still says "live-verified
   2026-07-08"; beats re-verified through s13 — refresh the line when the
   doc is next touched.

## Blocked / needs user

- Nothing.
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

- 2026-07-12 — WU-035 DONE (s20, fe70fbc impl + closeout): dump artifact bytes
  get a real home in object storage (O-1 resolved), behind the unchanged engine
  seam. TWIN-RECOVERY session: opened alongside a still-attached prior ssh
  (pts/0, `claude --resume`, sleeping/parked) that had written ~82 min of
  uncommitted, coherent WU-035 work + rebuilt the runner image + brought the
  minio stack up, but never gated/drilled/committed. Ran the twin-hazard drill
  (`ps`+`who`+per-pts `sshd` — NOT orphaned, its ssh was live/just parked),
  verified the on-disk work vs SPEC-035, user chose "adopt here", SIGTERM-reaped
  the idle twin (sole writer), verify-don't-redo. THE WORK: SPEC-035
  (docs/specs/artifact-storage.md, 5 mini-ADRs); compose `minio` + `createbuckets`
  + `miniodata`; semaphore.Dockerfile bakes `mc`; bootstrap folds `MC_HOST_dbportal`
  (jq @uri) + `MC_CONFIG_DIR` + `DBPORTAL_BUCKET` into pgtarget-env id 3; dump.yml
  `mc cp` upload AFTER sha/size stat + `mc stat --json` size assert + emit
  `location=s3://<bucket>/<name>` + best-effort staging `rm`; .env.example MINIO_*;
  O-1 annotated RESOLVED. NO Go/FE change (the SPEC-034 result line already
  carries `location`; only its value changed, opaque to the portal). Gate GREEN
  (CHECK-EXIT:0, golangci 0, race pass, vitest 116/116). LIVE DRILL (isolated
  portal :8099 + portal_drill035, semaphore engine, dump:3): AC-1 dump pgtarget →
  success, registry `location=s3://dbportal-artifacts/appdb-…​.dump`, `mc cat|
  sha256sum` == recorded sha (d1ad0cfe…​) mc-side, staging file cleaned, API
  doesn't expose location; AC-2 minio stopped → dump FAILED at the `mc cp` step
  (pg_dump ok, PLAY RECAP failed=1), ZERO artifact, mail "RUN-2 failed — dump on
  pgtarget (dev)"; AC-3 pg_dump portal DB + portal log + BOTH task outputs → all
  4 secrets (PG/token/webhook/minio) absent, mc masked the alias even on the
  connection-refused path (SPEC-035 open-question resolved, no no_log needed).
  Drill torn down (drill portal killed by the exact ss :8099 PID; portal_drill035
  dropped); demo :8080 confirmed active + /healthz 200 + same PID throughout; dev
  stack left up. All 3 AC + Verify met. Active → WU-036.
- 2026-07-11 — WU-034 DONE (s19, 36d08ab slice a + slice b): the SAME portal
  drives a REAL pg_dump of a compose target through Semaphore, behind the
  unchanged engine.Adapter seam. slice (a) infra+playbook: compose `pgtarget`
  (seeded) + custom runner image (`infra/semaphore.Dockerfile`, postgresql16-client
  + writable `/artifacts`) + `artifacts` volume; `playbooks/dump.yml` (pg_dump
  -Fc, engine-side libpq creds — NO secret in playbook, `DBPORTAL_RESULT=<base64
  json>` result line — base64 to survive ansible's debug-callback escaping);
  bootstrap `pgtarget-env` + `dump` template (id 3); `dev-targets.csv` (NOT
  instances.csv — 8-row test coupling); SPEC-034. BUG fixed: now()/random in
  ansible vars: are lazy → set_fact freezes the artifact name. slice (b) Go:
  `Artifact.Location` + semaphore.go result parsing on terminal SUCCESS +
  finalize writes `artifact.location` (NULLIF, mock stays NULL); engine stub
  parse tests + runs `TestArtifactLocationPersisted`. Gate GREEN (CHECK-EXIT:0,
  golangci 0 issues, itest LIVE, vitest 116/116). LIVE DRILL (isolated portal
  :8099 + portal_drill, semaphore engine, dump:3): dump on pgtarget → success,
  registry REAL sha256/size/location (== file `sha256sum`), `pg_restore --list`
  OK (ledger/widget/ledger_totals + DATA); bad-creds dump → failed + no artifact
  + mail "RUN-2 failed — dump on pgtarget (dev)"; no secret in DB/log. Drill
  torn down; demo :8080 untouched. All 4 AC + Verify met. Active → WU-035.
- 2026-07-11 — WU-033 DONE (s18, slice b): SemaphoreAdapter complete — the
  SAME portal drives REAL Semaphore for nonprod, opt-in, behind the unchanged
  engine.Adapter seam. Slice (b) shipped: `runs.ReconcileByJobID` (find run by
  job_id → ONE guarded Status→finalize; no-ops on unknown/terminal/
  non-terminal/ErrUnknownJob) + the session-less webhook route
  `POST /api/engine/semaphore/webhook` (server/webhook_http.go: constant-time
  `hmac.Equal` shared secret via header, empty secret ⇒ 401-only, body-capped,
  reads ONLY the task id, re-polls the REAL task — mini-ADR 2+5) + main wiring
  (Engine reconciler + secret; watcher polls at PORTAL_SEMAPHORE_POLL_INTERVAL
  when semaphore, not 500ms). Tests: reconcile_test (fake-adapter accelerate /
  unknown / already-terminal / ErrUnknownJob-defer / non-terminal + the
  no-webhook poll fallback) + webhook_http_test (auth-fails-closed / disabled /
  task-id shapes / bad-JSON / no-id / 413 / error-swallowed). Gate GREEN both
  stacks (golangci 0 issues, race pass, vitest 116/116); itest ran LIVE
  (smoke→success 17s streamed, cancel→canceled) AND skips clean w/ token unset.
  FULL-PORTAL LIVE DRILL on an ISOLATED portal (portal_drill DB + :8099, real
  Semaphore, `dump→smoke` map): run1 dump billing-test→success (task 2147483634,
  ~18s) w/ 34 SSE ansible lines + `end`; run2 cancel-mid-run→canceled; audit
  parity vs mock (submitted→finished / submitted→cancel_requested→finished,
  job_id = task id); run4 webhook-accelerated finalize (watcher parked 30s,
  webhook→success on the spot) + bad/no secret→401 zero-change. Drill torn down
  (portal stopped, portal_drill dropped); demo :8080 untouched. Active → WU-034.
