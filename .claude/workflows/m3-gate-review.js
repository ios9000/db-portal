export const meta = {
  name: 'm3-gate-review',
  description: 'M3 milestone gate: 5 Sonnet reviewers over Phase 3 (chains/restore/semaphore/playbooks/artifacts); architect verifies findings inline',
  phases: [{ title: 'Review', detail: '5 independent Sonnet reviewers, one per dimension', model: 'sonnet' }],
}

const REPO = '/root/db-portal'

const COMMON = `
You are reviewing Phase 3 (milestone M3) of "DB Portal" — an internal self-service portal
for PostgreSQL day-2 ops (backup dump + restore, DBA-only, prod guardrails, audit trail).
Repo: ${REPO}. Backend: Go (chi router, pgx, goose migrations, embedded SPA). Frontend:
React + react-router v7 + Vite + TypeScript strict. The execution engine sits behind a
seam (engine.Adapter / Registry). Phases 1 and 2 were gated at M1/M2 (fix WUs 016-019 and
024-025 closed).

Phase 3 shipped, and is what this gate reviews (WU-030..036). It closes the product's
loop: the SAME portal code drives MockEngine and a REAL Semaphore/Ansible engine that
runs a real pg_dump into minio and a real pg_restore back onto a live Postgres target.

- Artifact registry (SPEC-030, docs/specs/artifacts.md): migration 0009 \`artifact\` table
  (id, run_id FK origin, name, size_bytes, checksum, retention_class CHECK
  'standard'|'safety', location text NULL) = what can be restored; the run's own
  artifact_* columns = what this run produced. DUAL WRITE: both in finalize()'s single
  guarded terminal transition, riding the WU-016 double-finalize guard + UNIQUE(run_id),
  so exactly-once is structural. GET /api/artifacts?instance= (session-gated, newest 50,
  unknown instance 404); \`location\` deliberately NOT exposed.
- Chain engine (SPEC-032, docs/specs/chains.md): migration 0010 \`chain\` + \`chain_step\`.
  Chains are PORTAL-ASSEMBLED only (no client create API). internal/chain imports
  catalog/runs, NEVER engine — steps ARE runs through runs.Service.Start (full
  guardrail/audit path), attributed \`chain:<mover>\` (creator, then resumer). Sequential
  steps; failure → chain halted + ONE chain mail (per-run mail suppressed via
  StepRunFilter wired in main); POST /api/chains/{id}/resume re-fires the FAILED step as
  a NEW run (superseded runs keep history) then continues; boot sweep AFTER
  runs.SweepOrphans halts orphaned running chains + notifies. Stored creation-time
  \`confirm\` replays verbatim per step (the WU-024 lesson: env promotion mid-chain must
  fail the ritual visibly, never auto-confirm).
- Restore (SPEC-031, docs/specs/restore.md): POST /api/restore {artifact_id, target,
  confirm, reason} (dba) assembles a kind=restore chain via the PURE recipe
  internal/restore.Steps = [verify, safety_dump, restore]. The safety dump is a LITERAL
  in that slice, so "no skip affordance" is structural, not a runtime check (this
  institutionalizes the motivating incident). verify/safety_dump/restore are catalog
  entries with Launchable=false; runs.Start refuses a non-launchable op unless
  StartRequest.Internal is set, which ONLY chain/driver.go sets → a bare restore over
  POST /api/runs or the scheduler = 400. finalize stamps artifact.retention_class from
  the catalog op ('safety' for safety_dump, else 'standard'); last_backup_at ignores
  safety dumps. Prod-target ritual enforced once in chain.Create BEFORE any row.
- SemaphoreAdapter (SPEC-033, docs/specs/semaphore.md): internal/engine/semaphore.go =
  StartJob/Status/StreamLogs(poll replay-then-follow)/Cancel over Semaphore's REST API,
  opt-in per class via PORTAL_ENGINE_NONPROD=semaphore. JobID = the durable Semaphore
  task id (no in-process job state). Tag→template-id is config
  (PORTAL_SEMAPHORE_TEMPLATES=tag:id,…), fail-closed on an unmapped tag. Webhook
  POST /api/engine/semaphore/webhook (server/webhook_http.go) is SESSION-LESS,
  shared-secret constant-time (hmac.Equal, header X-Portal-Webhook-Secret, empty secret
  ⇒ 401-only), reads ONLY the task id, re-polls the REAL task → runs.ReconcileByJobID.
- Real dump + object storage (SPEC-034 docs/specs/dump-playbook.md, SPEC-035
  docs/specs/artifact-storage.md): playbooks/dump.yml = pg_dump -Fc → sha256/size stat →
  \`mc cp\` upload to s3://dbportal-artifacts/<name> → \`mc stat\` size assert → emit ONE
  \`DBPORTAL_RESULT=<base64 json {name,size_bytes,sha256,location}>\` line → best-effort
  staging rm. semaphore.go parses that line on terminal SUCCESS (nil on missing/bad → the
  run still succeeds, just no artifact); finalize writes artifact.location (NULLIF, so
  mock stays NULL). An upload failure aborts the play → run failed, ZERO artifact.
- Real restore (SPEC-036, docs/specs/restore-playbook.md; human twin docs/demo-m3.md):
  playbooks/verify.yml (step 1) = \`mc cat | sha256sum\` vs the expected checksum, names NO
  PG connection ⇒ ZERO target contact; a mismatch halts the chain BEFORE the safety dump.
  Step 2 safety_dump reuses dump.yml unchanged. playbooks/restore.yml (step 3) = mc cp →
  RE-verify sha256 (defense in depth) → \`pg_restore --clean --if-exists --no-owner
  --no-privileges --single-transaction\` (atomic: mid-restore failure rolls back). Neither
  emits DBPORTAL_RESULT — verify/restore register no artifact, so the task status IS the
  outcome. The fetch path is RECONSTRUCTED engine-side from DBPORTAL_BUCKET + the
  artifact_name var — \`location\` NEVER crosses the seam. ONE Go change: semaphore.go
  StartJob forwards the fail-closed allowlist forwardVars = {artifact_name, checksum} as
  the task \`environment\`, which Semaphore forwards to ansible-playbook as --extra-vars
  (VERIFIED live); instance/artifact_id never cross; a params-free op posts NO
  \`environment\` key ⇒ a byte-identical body to WU-033/034. Both playbooks
  preflight-assert their lineage so a mis-wire fails locally with zero target contact.

KNOWN, DELIBERATE deferrals — do NOT report these as findings:
- NO retention ENFORCEMENT: retention_class is stored classification only (the reaper job
  is M4/icebox). Drill dumps linger in the bucket, including one deliberately-corrupt
  object; no object lifecycle/GC. No artifact deletion API.
- The portal NEVER proxies artifact bytes: it stores/passes location strings, and
  GET /api/artifacts does not expose \`location\`. That is the design (SPEC-030/035).
- MockEngine is the forever default for dev and ALL tests (ADR-002); Semaphore is opt-in
  per class, non-prod only; the prod class stays mock in dev. The compose stack
  (Semaphore BoltDB, minio, pgtarget) is DEV infra, not prod-shaped: single-node, admin
  creds in gitignored .env, no TLS between compose services, no HA. Not findings.
- No chain edit; no cancel-the-whole-chain API (cancelling a live step run halts the
  chain, resumable); no client-facing chain create API — chains are portal-assembled.
- Chain/schedule CRUD is not itself on the audit trail; the FIRED RUNS are.
- Run logs are NOT persisted (SSE replay from engine memory) — SPEC-013.
- CI has no Postgres service: DB tests + golden flow skip there; the VM gate is real.
- Patroni-aware sequencing / replica-first dump routing is explicitly OUT (ARCHITECTURE
  §7, iceboxed). Restore flags are a fixed vetted set with zero user-facing options (O-4,
  resolved deliberately).
- Phase-2 deferrals still stand (no session GC sweep, no lockout alarming, no role admin
  UI, CSRF = SameSite=Lax + JSON-only by design, single window per instance).
- Phase-1/2 code was gated at M1/M2 — re-review it ONLY where Phase 3 touched it
  (runs finalize/artifact plumbing, Start's Internal/launchable gate, ReconcileByJobID,
  server router, notify filter, api.ts, RunDetail).

Ground rules that must hold NOW (violations ARE findings):
- The safety dump is structural: NO code path, API shape, or param may restore without
  it. Internal:true appears ONLY in chain/driver.go; the recipe is the sole op source.
- Verify halts BEFORE the safety dump: a bad/tampered artifact = ZERO contact with the
  target (no runs created on it, no bytes written).
- POLL is the finalization truth; the webhook only ACCELERATES. Webhook down ⇒ still
  finalizes. A lying/forged payload can NEVER force an outcome — the receiver reads only
  the task id and re-polls the real task. Bad/missing secret ⇒ 401 + zero state change.
- internal/chain NEVER imports engine; internal/runs.Service is the ONLY Registry caller.
  Every step run goes through runs.Service.Start (guardrails + audit, no bypass).
- Artifact registration is exactly-once and rides the guarded terminal transition: only
  state='success' with a non-nil engine artifact registers. A dump that isn't stored must
  register NOTHING.
- Secrets never enter the repo, the portal DB, run params, or logs. Playbooks name NO
  credential — pg_dump/pg_restore/mc read creds ONLY from the engine-side Semaphore
  Environment (ADR-004).
- \`location\` never crosses the engine seam into playbook vars; forwardVars is a
  fail-closed ALLOWLIST (artifact_name, checksum only).
- Prod ritual is enforced server-side in chain.Create before any row, and the stored
  confirm replays verbatim per step (a mid-chain env promotion fails visibly).
- audit_event is append-only; the actor is EXPLICIT (chain:<mover>), never ctx magic.
- A halted chain sends EXACTLY ONE mail (the per-step run mail is suppressed).

Read the actual code with your tools (start from the spec files above if useful). Only
report findings you can anchor to a specific file (and line where possible) with a
concrete failure scenario: what inputs/state lead to what wrong behavior. Quality over
quantity — a milestone gate wants real defects, not style notes. Severity: critical =
guardrail bypass (restore without safety dump, bare restore reachable, forged webhook
forcing an outcome) / audit-trail corruption / data loss on a live target / secret leak;
high = user-visible wrong behavior or crash in a mainline flow; medium = edge-case wrong
behavior; low = latent risk / footgun.
Your final output goes through the StructuredOutput tool — return raw findings only.`

const FINDINGS = {
  type: 'object',
  required: ['findings'],
  properties: {
    findings: {
      type: 'array',
      items: {
        type: 'object',
        required: ['title', 'file', 'severity', 'description', 'failure_scenario'],
        properties: {
          title: { type: 'string' },
          file: { type: 'string', description: 'repo-relative path' },
          line: { type: 'integer' },
          severity: { enum: ['low', 'medium', 'high', 'critical'] },
          description: { type: 'string' },
          failure_scenario: { type: 'string', description: 'concrete inputs/state -> wrong outcome' },
          fix_sketch: { type: 'string' },
        },
      },
    },
  },
}

const DIMENSIONS = [
  {
    key: 'chain-lifecycle',
    charter: `Dimension: CHAIN ENGINE CONCURRENCY & LIFECYCLE.
Scope: backend/internal/chain/** (chain.go, driver.go), backend/internal/runs
(Start/watch/finalize/SweepOrphans interplay with chain stepping), backend/internal/notify
(StepRunFilter), backend/cmd/portal/main.go (sweep ordering, filter wiring, goroutine
lifecycle), migration 0010_chains.sql.
Hunt for: races between the chain driver and the runs watcher (who advances the chain —
can a step advance twice? can two goroutines fire step N+1?), resume single-flight holes
(concurrent POST /resume on the same chain, resume racing a live step's finalize, resume
of a chain the boot sweep is concurrently halting), the boot-sweep/SweepOrphans ordering
assumption (what if a step run is swept AFTER the chain sweep reads it? chain left running
forever?), crash windows (process dies between run success and step advance — does the
chain resume, stall, or double-fire?), chain_step.run_id NULL semantics (a step whose
Start errored vs never fired — distinguishable?), the ONE-mail rule under races (halt +
boot sweep both notifying? StepRunFilter missing a path? a canceled step run mailing
twice?), state machine holes (halted→running→halted, finished chain resumed, step seq
gaps), FK violations (chain_step.run_id → run on delete), unbounded goroutines, ctx
cancellation mid-step leaving a chain wedged, and whether a superseded run's late
finalize can advance the NEW chain step.`,
  },
  {
    key: 'restore-safety',
    charter: `Dimension: RESTORE SAFETY & GUARDRAIL INTEGRITY (the incident-killing guarantees).
Scope: backend/internal/restore/** (restore.go — the pure recipe), backend/internal/
server/restore_http.go, backend/internal/catalog/catalog.go (Launchable flags),
backend/internal/runs/service.go (Start's Internal/launchable gate, restore_gate_test.go),
backend/internal/chain/chain.go (Create: prod ritual before any row), backend/internal/
runs/artifacts.go (retention_class stamping, last_backup_at exclusion).
Hunt for: ANY path that reaches a restore without the safety dump (params, catalog
manipulation, chain assembly with a doctored step slice, Internal:true leaking to another
caller, scheduler reaching a non-launchable op, resume skipping step 2, a client-supplied
field influencing restore.Steps), the verify-before-safety-dump ordering (can step 2 fire
while step 1 is still running or after it FAILED? does a halted verify really leave zero
runs on the target?), prod-ritual bypasses on the restore door (unicode/whitespace/case
tricks on the TARGET name, ritual checked against the SOURCE instance instead of the
target, instance renamed/promoted between chain.Create and the step fires, ritual checked
after a row is written), artifact/target lineage (restoring artifact from instance A onto
instance B — is that allowed, intended, checked? is the checksum passed the one belonging
to the artifact_id?), retention_class stamping errors ('safety' on the wrong run, a
safety dump counted as last_backup_at), authz on POST /api/restore (dba required? body
cap? reason length?), and 404/400 semantics for unknown artifact/target.`,
  },
  {
    key: 'engine-semaphore',
    charter: `Dimension: SEMAPHORE ADAPTER + WEBHOOK (engine seam correctness & security).
Scope: backend/internal/engine/semaphore.go (StartJob/Status/StreamLogs/Cancel, the
DBPORTAL_RESULT parse, forwardVars), backend/internal/engine/registry.go, backend/internal/
server/webhook_http.go, backend/internal/runs (ReconcileByJobID, the watcher's poll path),
backend/internal/config (semaphore fields), backend/cmd/portal/main.go (adapter wiring).
Hunt for: webhook weaknesses (secret comparison, empty-secret handling, does the receiver
trust ANY payload field beyond the task id? can a forged id finalize someone else's run?
unauthenticated state change? does it rate-limit or is that an accepted gap? does a
webhook for an unknown/foreign task error loudly or silently?), ReconcileByJobID holes
(job_id collision across engines/classes, a job_id reused after Semaphore's BoltDB is
wiped, reconcile racing the poll watcher — double finalize?), result-line parsing
(injection: task output containing an attacker/table-controlled DBPORTAL_RESULT string —
who can influence dump output? multiple result lines, huge base64, malformed json, a
name with path traversal like ../ or an s3 URL pointing elsewhere, size/checksum type
confusion), forwardVars allowlist integrity (can a param named artifact_name carry shell
or ansible injection into --extra-vars? YAML/JSON escaping? does an unmapped tag really
fail closed? does a params-free op truly post no environment key?), StreamLogs
replay-then-follow bugs (missed/duplicated output, unbounded memory, the poll interval
racing terminal status), Cancel semantics (task already terminal, cancel of a foreign
task), and ErrUnknownJob handling on a task Semaphore forgot.`,
  },
  {
    key: 'playbooks-secrets',
    charter: `Dimension: PLAYBOOKS, INFRA & SECRET HANDLING (the engine-side half).
Scope: playbooks/{dump.yml,verify.yml,restore.yml,smoke.yml,localhost.ini},
infra/semaphore-bootstrap.sh, infra/compose.yaml, infra/semaphore.Dockerfile,
infra/fixtures/**, .env.example; specs docs/specs/{dump-playbook,artifact-storage,
restore-playbook}.md.
Hunt for: any credential named/echoed in a playbook, bootstrap, or compose file that could
reach the repo, task output, or a log (mc alias URLs embed the secret key — is it masked
on EVERY path including errors? no_log gaps? does a failing task print the environment?),
the checksum-verify logic (is the comparison actually fail-closed — empty/undefined
expected checksum, sha256sum output parsing, does a missing object fail or silently pass?
can verify.yml touch the target at all?), restore.yml correctness (is
--single-transaction genuinely atomic here given --clean --if-exists? what about a dump
containing CREATE DATABASE or roles? does pg_restore's exit code get honored — warnings vs
errors? partial restore on a non-fatal error?), the dump.yml result-line emission ordering
(upload asserted BEFORE the sentinel? can a failed upload still emit a result?), the
best-effort staging rm masking a real failure, ansible lazy-var gotchas (the WU-034
now()/random set_fact freeze lesson — any remaining lazy var?), preflight lineage asserts
(do they actually fail with ok=0 when a var is missing/empty?), bootstrap idempotency
(re-running duplicates templates/environments? template ids drifting from the config map?
does it print secrets?), the runner image (writable /artifacts ownership, tool versions),
compose exposure (are minio/pgtarget/semaphore bound to 127.0.0.1?), and .env.example
leaking a real value.`,
  },
  {
    key: 'artifacts-frontend-specs',
    charter: `Dimension: ARTIFACT REGISTRY + FRONTEND + SPEC CONFORMANCE (Phase-3 surface).
Scope: backend/internal/runs/artifacts.go + query.go (the finalize dual write),
backend/internal/server/artifacts_http.go + chains_http.go, migrations
0009_artifact_registry.sql + 0010_chains.sql (up/down symmetry, constraints, the backfill),
frontend/src/components/RestoreDrawer.tsx, frontend/src/pages/{RunDetail,MyDatabases}.tsx
(chain strip + resume), frontend/src/lib/api.ts (Artifact/Chain types); specs
docs/specs/{artifacts,chains,restore,semaphore,dump-playbook,artifact-storage,
restore-playbook}.md vs shipped code, docs/demo-m3.md.
Hunt for: dual-write divergence (a path writing the run's artifact_* columns without the
registry row or vice versa; UNIQUE(run_id) racing a retried finalize; a NULL checksum or
empty name registering), backfill correctness (idempotent across an up+down+up walk? does
it register safety dumps as 'standard'? historical runs with partial artifact columns),
GET /api/artifacts contract (does \`location\` leak in ANY response incl. errors or the
chain/run read models? required-param validation, unknown instance 404 vs empty 200,
newest-50 ordering ties, session gate), chains_http contracts (resume 409/403/404 shapes,
body caps, JSON snake_case, never-null arrays), RestoreDrawer bugs (artifact list
staleness, default-target rules diverging from the server's, ritual gating diverging from
LaunchDrawer's, form state leaking between opens, a disabled-button-only guard where the
server check is the real one), chain strip rendering (superseded runs, step status
vocabulary drift from the fixed pending+run-states set, resume button on a non-halted
chain). SPEC side: any place SPEC-030..036 text and the code disagree (either direction —
code wrong OR spec stale), and any docs/demo-m3.md step that would not actually work if
followed literally.`,
  },
]

const MAX_PER_DIM = 8

log('M3 gate: 5 Sonnet reviewers over Phase 3 (chains/restore/semaphore/playbooks/artifacts); findings return unverified — the architect verifies inline')

const perDim = await parallel(DIMENSIONS.map(d => () =>
  agent(`${COMMON}\n\n${d.charter}\n\nReturn your findings (empty array if the dimension is clean).`,
    { label: `review:${d.key}`, phase: 'Review', schema: FINDINGS, model: 'sonnet' })
    .then(rev => {
      if (!rev) { log(`review:${d.key} returned nothing`); return { dim: d.key, findings: [], dropped: [] } }
      const order = { critical: 0, high: 1, medium: 2, low: 3 }
      const sorted = [...rev.findings].sort((a, b) => order[a.severity] - order[b.severity])
      log(`review:${d.key}: ${sorted.length} finding(s)` + (sorted.length > MAX_PER_DIM ? ` — keeping top ${MAX_PER_DIM} by severity` : ''))
      return {
        dim: d.key,
        findings: sorted.slice(0, MAX_PER_DIM).map(f => ({ ...f, dimension: d.key })),
        dropped: sorted.slice(MAX_PER_DIM).map(f => ({ ...f, dimension: d.key })),
      }
    })
))

const batches = perDim.filter(Boolean)
const findings = batches.flatMap(b => b.findings)
const dropped = batches.flatMap(b => b.dropped)
log(`Gate review complete: ${findings.length} findings for inline verification (${dropped.length} over-cap, listed unreviewed)`)
return { findings, dropped }
