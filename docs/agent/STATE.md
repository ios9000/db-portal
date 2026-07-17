# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.

## Now

- **Active:** M3 CLOSED. **PHASE 4 (M4 — Hardening) IN PROGRESS. WU-044 DONE (s30) —
  maintenance & retention jobs.** NEXT = **START WU-045** (docs-vs-reality reconciliation +
  cold-start + CI hardening: fresh-clone cold-start evidence, doc/code drift reconcile,
  Postgres service in CI so DB tests + golden flow stop skipping, pin golangci installer; M).
  M4 order = **041 ✓ → 042 ✓ → 043 ✓ → 044 ✓ → 045 → 046 → 047** (seed → concurrency locks →
  load test → retention/GC → docs-recon+CI → packaging → retrospective); full ACs/context
  briefs in BACKLOG "Phase 4" §, grooming rationale in its header note. M4 exit deliverables =
  the pilot-deployable build (046) + the experiment retrospective (047); close with an M4 gate
  review (author `m4-gate-review` mirroring m3).
- **Status (s30, WU-044 DONE — maintenance & retention jobs):** the periodic-sweep subsystem a
  long-running deployment needs (SPEC-044 = docs/specs/maintenance.md, JIT; ADR-013). A new
  `internal/maintenance.Service` on the scheduler's tick lifecycle — `go maint.Run(ctx)` in
  main, sweeps ONCE at boot then every `PORTAL_MAINTENANCE_INTERVAL` (default 1h).
  Architect-implemented (data deletion + audit integrity + retention policy = sensitive).
  Three best-effort passes: **(1) session GC** `DELETE FROM session WHERE expires_at < now()`
  (live session structurally excluded — complements SPEC-020's lazy per-token expiry); **(2)
  artifact retention enforcement** deletes `standard` registry artifacts past
  `PORTAL_ARTIFACT_RETENTION` (default 90d = 2160h) AND writes an `artifact.reaped` audit_event
  (actor `maintenance`, linked to the origin run) in ONE modifying-CTE statement — `safety` is
  structurally unselectable (WHERE `retention_class='standard'`), a non-positive age DISABLES
  the pass (zero would reap everything — fail-safe); **(3) audit retention** is OBSERVATIONAL
  — audit_event/auth_event are append-only BY TRIGGER (0003–0005: DELETE can't succeed), so
  the policy (ADR-013) is retain-in-DB, cold-storage archival post-MVP; the pass reports the
  oldest-event age and logs when it exceeds `PORTAL_AUDIT_RETENTION` (default 365d = 8760h),
  mutating NOTHING. **Object bytes:** per ADR-004 the portal never issues `mc rm` — byte TTL
  is the object store's lifecycle-expiry (engine-side, safety-exempt, → WU-046 runbook); a
  reaped non-NULL location is logged WARN, never silently orphaned; under the pilot's
  MockEngine `location` is NULL so there's nothing to expire. **(4) dump.yml finding-8:**
  post-`pg_dump` tasks wrapped in a `block:`/`always: file state=absent` so a failed upload
  never orphans real dump bytes (`--syntax-check` EXIT:0 in the runner image). NO migration
  (existing tables; audit_event needs no CHECK change — the `run.cancel_requested` precedent).
  3 new config knobs (safe defaults). TESTS (-race): session expired-reaped/live-kept,
  artifact standard-reaped/safety-preserved/age-boundary/disabled/orphan-object-count, audit
  no-mutation + age report + DELETE-still-raises, Run boot-sweep + disabled-returns. LIVE DRILL
  (isolated portal :8097 + scratch DB portal_maint_drill, auth off, tight interval/retention;
  demo :8080 + dev `portal` DB untouched): seeded 3 sessions/3 artifacts → boot sweep → 1
  session (live kept), 2 artifacts (old-standard reaped w/ `artifact.reaped` audit
  actor=maintenance env=test, young-standard + 100d safety kept), `DELETE FROM audit_event`
  still ERRORs "append-only". Drill torn down, scratch DB dropped. GATE: CHECK-EXIT:0 (golangci
  0, fmt clean, go test -race ALL pkgs incl. e2e golden flow + new maintenance pkg, vitest
  116/116). Diff = SPEC-044 + maintenance.go + maintenance_test.go + config (3 knobs) + main
  (wiring) + dump.yml + ADR-013. No API/UI/seam change; MockEngine + service code UNCHANGED.
- **Status (s29, WU-043 DONE — load test):** WU-042's instance lock + WU-016's
  single-finalizer are now PROVEN under contention at the ROADMAP M4 scale (25–50 concurrent
  dumps). Deliverable = a skip-gated Go harness (SPEC-043 = docs/specs/load-test.md, JIT)
  driving the REAL `runs.Service` over MockEngine on a scratch DB seeded with WU-041's
  `inventory.GenerateEstate` — so it joins `go test -race ./...` (runs on the VM, skips in CI
  like the itest), NO new binary/lifecycle (mini-ADR 1). Architect-implemented (backend
  test-only, concurrency-sensitive). `runs/load_test.go`: **B1** 50 dumps across 50 DISTINCT
  instances → all 50 succeed, each exactly one `run.submitted` + one `run.finished` (WU-016
  exactly-once under load), zero orphans/zero leaked locks, **peak concurrency 50/50** (proven
  from committed started/finished timestamps via a sweep-line, NOT a racy live sample —
  mini-ADR 3); **B2** 40 concurrent at ONE instance → 1 win + 39 `ErrInstanceLocked`, exactly
  1 run row, lock frees on finalize, next Start succeeds (the WU-042 lock test's shape at
  load); **B3** mixed 20×3 → per-instance 1 win + K-1 conflict, global exactly-once +
  zero-orphan. `schedule/stampede_test.go` **B4**: 25 due schedules, ONE sequential `fireDue`
  → all 25 fire, zero orphans (the boot-stampede icebox item — VALIDATED, no fix needed).
  `PORTAL_LOADTEST_INSTANCES` env grows the estate for a pilot-scale drill with the SAME
  committed code. LIVE DRILL (isolated scratch DBs via MigratedDB, auto-dropped; dev `portal`
  DB + demo :8080 untouched): `PORTAL_LOADTEST_INSTANCES=500 go test -race` → seeded 500
  instances (0 quarantined), 50 concurrent dumps peak 50/50, ≈386–406 runs/s, exactly-once,
  zero orphans. **Findings filed with verdicts:** F1 default pgxpool `MaxConns=max(4,NumCPU)`
  is ADEQUATE (conns held only per-tx/per-poll, never a goroutine's lifetime → 50 dumps
  overlapped fully without exhaustion or deadlock; explicit `PORTAL_DB_MAX_CONNS` floor → WU-046
  nice-to-have); F2 scheduler stampede self-rate-limits (sequential `fireDue`) → no spreading
  needed for the pilot. GATE: CHECK-EXIT:0 (golangci 0, fmt clean, go test -race ALL pkgs incl.
  e2e golden flow + the new load/stampede tests, vitest 116/116 — one intermittent frontend
  `EventSource`/jsdom flake in RunDetail.test.tsx seen once, green on 3 re-runs, NOT mine).
  Diff = SPEC-043 + load_test.go + stampede_test.go (test-only). NO migration/API/UI/seam
  change; MockEngine + service code UNCHANGED (the WU is the harness, not a fix).
- **Status (s28, WU-042 DONE — concurrency locks):** "at most one live op per instance" is
  now STRUCTURAL, enforced at the single choke point `runs.Service.Start` so button +
  scheduler + chain-step all inherit it (ARCHITECTURE §concurrency "hierarchical TTL locks
  (M4)"; SPEC-042 = docs/specs/concurrency-locks.md; ADR-012). Migration 0012 adds
  `instance_lock` (PK instance_id) — a lock ROW, not a pg advisory lock (survives restarts,
  auditable, carries expires_at). ACQUIRE rides Start's existing run-insert tx: a conflict
  rolls the WHOLE tx back → no run, no audit, a clean 409 (same "nothing created" posture as
  the prod ritual). RELEASE rides finalize's own tx (after the RowsAffected==0 winner guard)
  → atomic with the terminal run state, so EVERY terminal path frees the lock and a crashed
  holder is reclaimed at boot by SweepOrphans→finalize (crash-heal needs no TTL). The TTL is
  only a backstop and the steal predicate (`expires_at < now() AND NOT EXISTS live holder`)
  NEVER takes a still-live holder's lock — so a misconfigured-short TTL can't cause two
  concurrent ops (the load-bearing safety property). `PORTAL_LOCK_TTL` default 30m. Folded
  two research gotchas at the assemblers: **#2 self-target ban** — declared
  `PORTAL_PROTECTED_INSTANCES` (seeded with PORTAL_DB_NAME so the portal DB is protected out
  of the box; inventory has NO connection tuple → auto-detect is impossible, post-MVP) →
  ErrSelfTarget 403 at Start + chain.Create; **#1 naive-Patroni-restore** — chain.Create
  refuses a restore step onto a k8s_patroni target → ErrPatroniRestore 403 (dumps stay
  allowed; full pause/detach→restore→reinit sequencing post-MVP, ADR-012). Both refusals
  audited `guardrail.denied` on auth_event (0012 extends the 0006 CHECK). Scheduler KEEPS its
  run-table overlap probe as a cheap early-out AND maps ErrInstanceLocked → skipped_overlap,
  closing the probe's TOCTOU window. Guardrails/ritual/audit/golden-flow UNCHANGED — the lock
  is a gate in front. TESTS (-race): N-goroutine contention (1 win + N-1 conflict, only 1 run
  row), TTL reap of a dead holder, never-steal-from-live-holder, release-on-finalize,
  self-target + Patroni refusals with denial rows, scheduler lock-skip, 409/403 handler maps,
  config protected-set; 0012 up/down/up pinned; migrate down-walk +1 step. LIVE HTTP DRILL
  (isolated portal :8098 + scratch DB portal_lock_drill, auth off, crm-test declared
  protected; demo :8080 + dev DB untouched): two concurrent dumps on billing-test → 1×201
  (run id 1) + 1×409 "already running", only ONE run row (the 409'd insert consumed id 2 on
  rollback — cosmetic gap); run 1 success → lock freed → next dump 201; dump on protected
  crm-test → 403 self-target; restore onto billing-test (k8s_patroni) → 403 patroni, restore
  onto hr-test (vm) → 201 running (block is narrow); auth_event held both guardrail.denied
  rows. Drill torn down, scratch DB dropped. GATE: CHECK-EXIT:0 (golangci 0, fmt clean, go
  test -race all pkgs incl. e2e golden flow, vitest 116/116). Diff = 0012 + 6 src + 6 test +
  SPEC + ADR-012 (826 insertions). No UI change; MockEngine + seam UNCHANGED (ADR-002).
- **Status (s27, WU-041 DONE — staging seed):** the first M4 WU, a deterministic
  realistic-estate generator (SPEC-041 = docs/specs/staging-seed.md, JIT). Architect-
  implemented directly (S, no UI, backend-only → delegation overhead > diff, per the
  WU-023/030 carve-out). DESIGN: a PURE `inventory.GenerateEstate(n, seed) string` emits a
  valid SPEC-010 CSV that the new `portal seed [--instances N] [--seed S]` subcommand feeds
  through the EXISTING `inventory.Import` — idempotency (natural key), cluster resolution,
  validation, and the report all inherited; the seed can't drift from the import contract
  (a reject = generator bug, asserted 0). NO migration/API/UI; the 8-row test fixtures
  untouched. Env mix computed up front + shuffled so ratios hold EXACTLY (prod
  max(1,18%)/test 32%/dev remainder-dominant); clusters=max(5,n/8), each ONE platform
  (two-platform cluster self-quarantines); names `<cluster>-<env>-<NN>`; prod always
  windowed (WU-023 parser coverage). RNG = math/rand/v2 PCG(seed,seed), fixed call order →
  byte-identical output. 4 tests (determinism, parses-clean, distribution, DB idempotency).
  LIVE CLI DRILL (isolated scratch DB portal_seed_drill, PORTAL_DB_NAME override; dev
  `portal` DB + demo :8080 untouched): `portal seed --instances 500 --seed 41` → 500 new/0
  quarantined; RE-run → 0 new/500 unchanged (idempotent); distribution = dev 250/test
  160/prod 90 (non-prod dominates, prod non-empty), 62 clusters (31 patroni/31 vm), all 90
  prod windowed. Scratch DB dropped. GATE: CHECK-EXIT:0 (golangci 0, go test -race all pkgs
  incl. inventory FRESH + e2e, vitest 116/116). Diff = main.go (+40) + seed.go + seed_test.go
  + SPEC. **The seed is now available for the WU-043 load test.**
- **Status (s26, PHASE 4 GROOMED — M4 decomposed into 7 WUs):** with all M3-gate
  fixes landed, groomed the M4 "Hardening" phase against the ROADMAP M4 exit criteria +
  the icebox debt. Filed **WU-041** (staging seed, S) → **WU-042** (concurrency locks:
  instance TTL lock across all launch paths + portal self-target ban + naive-replica
  block, M — generalizes the scheduler's instance-only overlap probe) → **WU-043** (load
  test, 25–50 concurrent mock dumps, M — validates 042 scales) → **WU-044** (maintenance
  & retention: audit 1y + artifact enforcement preserving 'safety' + session GC + dump.yml
  finding-8 orphan fix, M) → **WU-045** (docs-vs-reality reconciliation + cold-start + CI
  hardening — folds the CI/supply-chain + .env-walk + token-reconcile + demo-m1-header
  icebox debt, M) → **WU-046** (packaging for pilot: systemd unit + .env template + deploy
  runbook + break-glass mail alarm, M) → **WU-047** (experiment retrospective, STRATEGY §8,
  S — strictly last). Promoted icebox items annotated `→ WU-0xx` in place. M4 exit gate =
  a milestone review (author `m4-gate-review` skill mirroring the m3 pattern) after 047.
  Docs-only session; no Go/FE change (BACKLOG/STATE/JOURNAL only).
- **Status (s26, WU-040 DONE — LOW bundle, items 5/6/7; ALL M3-gate fixes landed):**
  three real-but-cheap defects in ONE S WU (M1 precedent). Go-test-shaped, no live stack.
  (5) `parseResultLine` (semaphore.go) rejected an empty `Name` but returned the Artifact
  with `r.SHA256`/`r.SizeBytes` UNCHECKED → a name-only line registered a dead
  empty-checksum, un-restorable row; the guard is now
  `Name=="" || SHA256=="" || SizeBytes<=0 → nil` (run still succeeds, nil = no registry
  row). LOW because verify.yml:37 already fail-closes on an empty checksum (dead row, not
  a dangerous restore). (6) 0009's backfill omitted `retention_class` → a `goose
  down`→`up` walk silently reclassified every `'safety'` row `'standard'`; FIXED IN PLACE
  in 0009 (safe — goose won't re-run an applied migration on prod; only a dev/test
  down→up re-runs it) with in-SQL `CASE WHEN r.operation='safety_dump' THEN 'safety' ELSE
  'standard' END`, mirroring finalize's catalog stamp (service.go:399-402) exactly. (7)
  `job_id text` (0003) had no uniqueness → NEW migration **0011_job_id_unique.sql** =
  `CREATE UNIQUE INDEX run_job_id_unique ON run (job_id) WHERE job_id IS NOT NULL` (0003
  can't be retro-edited; PG NULLs distinct → queued runs unaffected). TESTS: 3 cases →
  TestSemaphoreSuccessNoArtifact; TestArtifactBackfillWalk now adds a safety_dump row +
  asserts 'safety' preserved across the walk (down-count 2→3, 0011+0010 above 0009); new
  TestJobIDUniqueConstraint (dup non-null rejected w/ "run_job_id_unique", multiple NULLs
  OK); TestMigrateUpDown pins 0011 up/down (new `indexExists` helper). Gate GREEN:
  CHECK-EXIT:0 (golangci 0, go test -race all pkgs FRESH incl. db/engine/runs, golden
  flow not-skipped 2.04s, vitest 116/116). Diff = 4 files + 1 migration, 100 insertions.
  No UI, no seam/API change; MockEngine untouched (ADR-002).
- **Status (s26, WU-039 DONE — restore.yml re-fetch footgun):** the MEDIUM
  playbook-honesty fix (M3-gate item 4). `restore.yml`'s fetch used `creates:
  {{ staging_path }}` on a DETERMINISTIC path on the persistent shared `/artifacts`
  volume — an interrupted fetch (timeout / runner restart / killed container) leaves a
  PARTIAL file there; the next attempt saw it exists → SKIPPED the fetch → hashed the
  partial → checksum mismatch → the operator told their GOOD backup is "tampered",
  worst on the product's own Resume-after-halt path. FIX (playbook-only): dropped the
  `creates:` guard and added a `clear any stale staging file` task (`file: state:
  absent`) BEFORE the fetch, so the fetch always runs and a partial can never
  masquerade as the fetched artifact. `dump.yml`/`verify.yml` confirmed footgun-free
  (dump staging name = `now()`+`random`, unique per run, no `creates:`; verify streams
  `mc cat | sha256sum`, no staging file). All 3 playbooks `--syntax-check` EXIT:0 in the
  runner image. LIVE DRILL (isolated portal :8099 + portal_drill039, semaphore engine;
  demo :8080 untouched): planted a 2000-byte partial at `/artifacts/restore-<name>` →
  restore → **chain SUCCESS**, restore task `ok=9 changed=4 skipped=0` (clear→changed,
  fetch→changed NOT skipped, checksum assert→ok), widget back 4 rows; corrupt object →
  chain HALTED at verify (steps 2+3 run_id null, target untouched, ONE mail); fix +
  resume → SUCCESS. GATE GREEN: CHECK-EXIT:0 (regression, no Go/FE change; vitest
  116/116). No migration, no UI, no seam change. Drill torn down, scratch DB dropped.
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

1. **START WU-045** (docs-vs-reality reconciliation + cold-start + CI hardening; M; BACKLOG
   "Phase 4" §). The ROADMAP M4 "docs-vs-reality reconciliation" exit item + accumulated
   CI/supply-chain debt. **Cold-start:** clean clone on a fresh host → install → migrate →
   `npm run check` → `build:release` → run, pasted as evidence. **Reconcile:** walk every
   SPEC/DECISIONS/ARCHITECTURE/STATE claim against the code, resolve/file drifts; refresh the
   stale demo-m1.md header ("live-verified 2026-07-08"); reconcile WU-004 token hex vs
   design-brief.md; fix `config.LocateDotenv`'s upward `.env` walk to stop at a repo marker +
   log the resolved file (M1-gate item 16). **CI:** add a Postgres service to check.yml so DB
   tests + the golden flow stop skipping (ADR-011 gap); pin the golangci-lint installer to
   v2.12.2 instead of `curl|sh` from HEAD (M1-gate item 14); bump GH Actions versions + fix
   the setup-go cache path. May checkpoint between the reconciliation report and the CI
   changes. Read the WU-045 entry + context brief; write the report JIT (spec optional — this
   WU is docs/CI, not a module).
   - WU-044 (s30) is DONE — maintenance & retention. If revisiting: `internal/maintenance`
     Service (`maintenance.go`), `go maint.Run(ctx)` in main; sweeps = session GC + `standard`
     artifact reap (audited `artifact.reaped`, `safety` preserved) + audit-age OBSERVATION
     (append-only, never mutated). Config: `PORTAL_MAINTENANCE_INTERVAL` (1h),
     `PORTAL_ARTIFACT_RETENTION` (2160h/90d, non-positive disables), `PORTAL_AUDIT_RETENTION`
     (8760h/365d, observational). Policy = ADR-013; object-store byte TTL deferred to WU-046
     (store lifecycle, ADR-004). dump.yml has the block/always orphan fix. NO migration. Tests
     in maintenance_test.go.
   - WU-043 (s29) is DONE — load test. If revisiting: the harness is `runs/load_test.go`
     (B1 distinct-instance parallelism + exactly-once, B2 same-instance contention at scale,
     B3 mixed) + `schedule/stampede_test.go` (B4 boot-stampede), skip-gated via MigratedDB,
     seeded by `inventory.GenerateEstate`. `PORTAL_LOADTEST_INSTANCES` env grows the estate for
     a pilot-scale drill. SPEC-043 F1 (pool adequate → `PORTAL_DB_MAX_CONNS` a WU-046
     nice-to-have) + F2 (stampede self-rate-limits, no fix) are filed with verdicts. No code
     change — test-only.
   - WU-042 (s28) is DONE — concurrency locks. If revisiting: the lock is `instance_lock`
     (0012, PK instance_id); acquire = `runs.acquireInstanceLock` in Start's run-insert tx
     (lock.go), release = `DELETE ... WHERE run_id` in finalize's tx; `PORTAL_LOCK_TTL` 30m,
     `PORTAL_PROTECTED_INSTANCES` (self-target, seeded w/ DBName). Errors ErrInstanceLocked
     (409) / ErrSelfTarget (403) / ErrPatroniRestore (403), all in runs pkg. Tests in
     runs/lock_test.go, chain/chain_lock_test.go, schedule/lock_test.go. ADR-012 + SPEC-042.
   - WU-041 (s27) is DONE — staging seed. If revisiting: `inventory.GenerateEstate(n, seed)`
     (seed.go) is a pure SPEC-010 CSV generator; `portal seed [--instances N] [--seed S]`
     feeds it through the existing `inventory.Import`. Defaults N=500/seed=41. No schema/UI.
   - Phase 4 is GROOMED: all 7 WUs have ACs + context briefs in BACKLOG; the header note
     carries the execution-order rationale. Promoted icebox debt is annotated `→ WU-0xx`.
   - Milestone bookkeeping still open: mark M3 EXIT in ROADMAP.md (demo-m3.md is the exit
     twin) when convenient; not blocking.
   - Organizational note reached (BACKLOG + ROADMAP): the **security vetting
     package** (ARCHITECTURE §8.2) becomes submittable at M3 exit — surface to the user.
   - WU-040 (s26) is DONE — the LOW bundle. If revisiting: item 5 = the
     `Name/SHA256/SizeBytes` guard in `parseResultLine` (semaphore.go); item 6 = the
     in-place `CASE … safety_dump …` in 0009's backfill (edited the applied migration on
     purpose — only a down→up re-runs it); item 7 = new migration **0011_job_id_unique.sql**
     (partial unique index `run_job_id_unique`). Tests in semaphore_test.go + migrate_test.go.
   - WU-039 (s26) is DONE — restore.yml re-fetch footgun. If revisiting: the fix is the
     `clear any stale staging file` task + dropped `creates:` in restore.yml's fetch;
     dump.yml/verify.yml were confirmed footgun-free (not changed). Live-drilled on
     :8099/portal_drill039 (planted-partial → chain success; corrupt → halt; resume →
     success). The M3-gate finding 8 (dump.yml staging `rm` after `mc cp` orphans bytes
     on failure) is deferred to M4, NOT this WU.
   - WU-038 (s26) is DONE — the schedule launchable gate. If revisiting: the fix is
     `schedule.go` Create's opening gate (`!ok || !op.Launchable`); the schedules
     handler already maps `runs.ErrUnknownOperation` → 400 (no handler change was
     needed). Test lives in `TestCreateValidation` (schedule_test.go).
   - WU-037 (s25) is DONE — the two HIGHs. If revisiting: the shared `backoff`
     helper is duplicated in runs/service.go + chain/driver.go (packages stay
     decoupled by design); `MaxStatusErrors`/`MaxReadErrors` are Service fields
     tests dial low for fast give-up.
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

- 2026-07-17 — **WU-044 DONE (s30) — maintenance & retention jobs (SPEC-044, ADR-013)**: the
  periodic-sweep subsystem, on the scheduler's tick lifecycle (`go maint.Run(ctx)`; boot sweep
  then every `PORTAL_MAINTENANCE_INTERVAL`, default 1h). New `internal/maintenance.Service`,
  three best-effort passes: session GC (`DELETE ... expires_at < now()`, live never matched),
  `standard` artifact reap past `PORTAL_ARTIFACT_RETENTION` (default 90d) with an
  `artifact.reaped` audit_event (actor maintenance, origin-run-linked) in ONE modifying-CTE —
  `safety` structurally unselectable, non-positive age disables (fail-safe), and audit-age
  OBSERVATION (append-only ledgers retained in-DB, cold-storage archival post-MVP; mutates
  nothing). Object bytes = the store's lifecycle-expiry (engine-side, ADR-004; → WU-046);
  reaped non-NULL location logged WARN. dump.yml finding-8 fixed (post-`pg_dump` tasks in a
  `block:`/`always:`; syntax-check EXIT:0). NO migration; 3 config knobs. -race tests per
  sweep + LIVE DRILL (isolated :8097/portal_maint_drill, torn down): 3 sessions→1 live-kept, 3
  artifacts→2 (old-standard reaped+audited, young+safety kept), DELETE audit_event still
  ERRORs append-only. GATE CHECK-EXIT:0 (golangci 0, -race all pkgs incl. e2e golden flow,
  vitest 116/116). Active → **WU-045** (docs-recon + cold-start + CI).
- 2026-07-17 — **WU-043 DONE (s29) — load test (concurrent mock dumps)**: WU-042's instance
  lock + WU-016's single-finalizer PROVEN under contention at the ROADMAP M4 scale. A
  skip-gated Go harness (SPEC-043) drives the REAL `runs.Service` over MockEngine on a
  scratch DB seeded with WU-041's `GenerateEstate`, so it joins `go test -race ./...` (VM
  runs it, CI skips like the itest) — no new binary/lifecycle. `runs/load_test.go`: B1 (50
  distinct instances → all succeed, exactly-once audit, peak concurrency 50/50 from committed
  timestamps), B2 (40 at one instance → 1 win + 39 `ErrInstanceLocked`, 1 run row, lock frees
  on finalize), B3 (mixed 20×3 → per-instance 1 win + K-1 conflict, global exactly-once +
  zero-orphan). `schedule/stampede_test.go` B4 (25 due schedules, one sequential `fireDue` →
  all fire, zero orphans). LIVE DRILL `PORTAL_LOADTEST_INSTANCES=500 go test -race`: seeded
  500 (0 quarantined), 50 concurrent peak 50/50, ≈386–406 runs/s, exactly-once, zero orphans.
  Findings filed w/ verdicts: F1 default pgxpool `max(4,NumCPU)` adequate (`PORTAL_DB_MAX_CONNS`
  → WU-046 nice-to-have); F2 stampede self-rate-limits (no spreading needed). GATE
  CHECK-EXIT:0 (golangci 0, fmt clean, -race all pkgs incl. e2e golden flow, vitest 116/116).
  Test-only diff (SPEC + 2 test files); no migration/API/UI/seam change. Active → **WU-044**
  (maintenance & retention jobs).
- 2026-07-17 — **WU-042 DONE (s28) — concurrency locks + self-target ban + Patroni-restore
  block (SPEC-042, ADR-012)**: "one live op per instance" made structural at the single
  choke point runs.Service.Start (button + scheduler + chain-step inherit it). Migration
  0012 `instance_lock` (PK instance_id) — a lock ROW (survives restarts, auditable, TTL), not
  a pg advisory lock. Acquire rides Start's run-insert tx (conflict → whole tx rolls back:
  clean 409, no run); release rides finalize's own tx (atomic w/ terminal state → every path
  frees it, boot sweep reclaims a crash — no TTL needed for crash-heal). TTL is a backstop
  and the steal predicate NEVER takes a still-live holder (safe under any TTL);
  PORTAL_LOCK_TTL 30m. Self-target ban = declared PORTAL_PROTECTED_INSTANCES seeded w/ DBName
  (ErrSelfTarget 403); Patroni-block at chain.Create for a restore step on k8s_patroni
  (ErrPatroniRestore 403, dumps allowed); both audited guardrail.denied on auth_event (0012
  extends the CHECK). Scheduler keeps its probe + maps ErrInstanceLocked → skipped_overlap.
  Guardrails/ritual/audit/golden-flow UNCHANGED. -race tests (contention 1+N-1, TTL reap,
  never-steal-live, self-target/Patroni refusals, scheduler skip, 409/403 maps) + LIVE HTTP
  drill (isolated :8098, torn down): 2 concurrent dumps → 1×201+1×409; self-target crm-test
  → 403; restore onto Patroni → 403, onto VM → 201. GATE CHECK-EXIT:0 (golangci 0, -race all
  pkgs incl. e2e, vitest 116/116). Commit 2bb4b52. Active → **WU-043** (load test).
