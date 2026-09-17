# Standing context — stable facts (read on demand, NOT at session start)

> Moved verbatim out of `docs/agent/STATE.md` on 2026-09-17 (s40). This is a LIVE reference, not an archive:
> correct a fact here when it changes. It is not start-up reading — `grep` for the subsystem you are touching.
> Currency is NOT guaranteed: entries carry the session they were written in; the specs, ADRs and the code win.

## 1. Stable facts per subsystem (was STATE "Standing context")

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

## 2. Dev stack + demo heads-ups (were under STATE "Blocked / needs user")

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

## 3. Dev environment as last recorded (were sub-bullets of STATE "Next action"; verify before relying)

   - Dev facts carried: semaphore project 1, templates **dump:3, verify:4,
     restore:5, smoke:1**, pgtarget-env **3**; pgtarget host-port 5433, seeded
     appdb (reset = `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` + replay
     `infra/fixtures/pgtarget-init.sql` — the init only auto-runs on a fresh
     volume); minio bucket `dbportal-artifacts` (:9000); `.env` has the Semaphore
     token + `PGTARGET_*` + `MINIO_*`. Persistent `.env` STAYS
     `PORTAL_ENGINE_NONPROD=mock` + `PORTAL_SEMAPHORE_TEMPLATES=smoke:1`; drills
     use an isolated portal ([[live-drill-isolation]]) — full recipe in
     **docs/demo-m3.md** (setup + reset §§).
   - Dev stack was UP through s30 (postgres healthy 11d; the WU-044 live drill used an isolated
     scratch DB portal_maint_drill on it, now dropped — the persistent `portal` DB + demo :8080
     were untouched). WU-044 added NO migration (uses existing tables). New config (safe
     defaults, unset in the persistent .env): `PORTAL_MAINTENANCE_INTERVAL` (1h),
     `PORTAL_ARTIFACT_RETENTION` (2160h=90d), `PORTAL_AUDIT_RETENTION` (8760h=365d) — the
     maintenance loop runs in the demo/dev binary with these defaults (session GC + artifact
     reap are harmless on the tiny dev estate; the audit pass only observes). WU-043 added NO
     migration and NO service code (test-only). pgtarget
     sits RESTORED (widget 4 / ledger 200); minio holds the WU-039 drill artifacts (harmless,
     no retention until WU-044). The persistent dev `portal` DB is still at 0010 — WU-040 added
     0011 and WU-042 added 0012 to the BINARY, but neither migrated the persistent dev DB (all
     tests + drills use fresh scratch DBs that get 0012 on `up`); migrate it with `cd backend
     && go run ./cmd/portal migrate up` only if a future live drill on the `portal` DB needs
     0011/0012. New config (safe defaults, unset in the persistent .env): `PORTAL_LOCK_TTL`
     (30m), `PORTAL_PROTECTED_INSTANCES` (empty; the set still seeds with DBName="portal",
     which matches no real instance). `PORTAL_LOADTEST_INSTANCES` is HARNESS-ONLY (grows the
     load-test scratch estate for a pilot-scale drill; never set in prod/dev .env).
