export const meta = {
  name: 'm4-gate-review',
  description: 'M4 milestone gate: 5 Sonnet reviewers over Phase 4 (locks/retention/deploy-hardening/CI-config/seed-loadtest-specs); architect verifies findings inline',
  phases: [{ title: 'Review', detail: '5 independent Sonnet reviewers, one per dimension', model: 'sonnet' }],
}

const REPO = '/root/db-portal'

const COMMON = `
You are reviewing Phase 4 (milestone M4 — "Hardening") of "DB Portal" — an internal
self-service portal for PostgreSQL day-2 ops (backup dump + restore, DBA-only, prod
guardrails, audit trail). Repo: ${REPO}. Backend: Go (chi router, pgx/pgxpool, goose
migrations, embedded SPA). Frontend: React + react-router v7 + Vite + TypeScript strict.
The execution engine sits behind a seam (engine.Adapter / Registry); MockEngine is the
forever default (ADR-002), a real Semaphore/Ansible engine is opt-in per class. Phases
1/2/3 were gated at M1/M2/M3 and their fix WUs closed (016-019, 024-025, 037-040).

Phase 4 shipped and is what THIS gate reviews (WU-041..047). It is the hardening milestone
that makes the MVP pilot-deployable. **The M4 diff is the commit range \`24008da..HEAD\`
(from the WU-040 "M3 close" commit to now).** The changed files below are the M4 surface;
you may read unchanged neighbors for context, but findings must anchor to code that M4
shipped or to a Phase-1/2/3 invariant that M4 changed the behavior of.

WHAT M4 SHIPPED (WU by WU):

- WU-041 STAGING SEED (SPEC-041 docs/specs/staging-seed.md): a PURE
  \`inventory.GenerateEstate(n, seed) string\` (backend/internal/inventory/seed.go) emits a
  valid SPEC-010 CSV that the new \`portal seed [--instances N] [--seed S]\` subcommand
  (cmd/portal/main.go) feeds through the EXISTING \`inventory.Import\` — so idempotency,
  cluster resolution, validation and the reject report are all inherited (a reject = a
  generator bug; the test asserts 0 rejects). Env mix computed up-front + shuffled so ratios
  hold exactly (prod max(1,18%) / test 32% / dev remainder); clusters = max(5, n/8), each ONE
  platform; prod instances always windowed. RNG = math/rand/v2 PCG(seed,seed), fixed call
  order → byte-identical output. No migration/API/UI.

- WU-042 CONCURRENCY LOCKS (SPEC-042 docs/specs/concurrency-locks.md, ADR-012): "at most
  one live op per instance" made STRUCTURAL at the single choke point
  \`runs.Service.Start\`, so button + scheduler + chain-step all inherit it. Migration
  0012_instance_locks.sql adds \`instance_lock\` (PK instance_id) — a lock ROW (survives
  restarts, auditable, carries expires_at), NOT a pg advisory lock. ACQUIRE
  (runs/lock.go, called from Start) rides Start's existing run-insert tx: a conflict rolls
  the WHOLE tx back → no run, no audit, a clean 409 ErrInstanceLocked. RELEASE rides
  finalize's tx AFTER the RowsAffected==0 double-finalize winner guard → atomic with the
  terminal run state, so EVERY terminal path frees the lock; a crashed holder is reclaimed
  at boot by SweepOrphans→finalize (crash-heal needs no TTL). The TTL is only a backstop:
  the steal predicate (\`expires_at < now() AND NOT EXISTS a live holder\`) must NEVER take
  a still-live holder's lock — so a misconfigured-short PORTAL_LOCK_TTL (default 30m) can't
  cause two concurrent ops (the load-bearing safety property). Two research gotchas folded
  at the assemblers: (#2) SELF-TARGET BAN — PORTAL_PROTECTED_INSTANCES (seeded with
  PORTAL_DB_NAME so the portal's own DB is protected out of the box) → ErrSelfTarget 403 at
  Start + chain.Create; (#1) NAIVE-PATRONI-RESTORE — chain.Create refuses a restore step
  onto a k8s_patroni target → ErrPatroniRestore 403 (dumps still allowed). Both refusals
  audited \`guardrail.denied\` on auth_event (0012 extends the 0006 CHECK). The scheduler
  KEEPS its run-table overlap probe as a cheap early-out AND maps ErrInstanceLocked →
  skipped_overlap (executor.go), closing the probe's TOCTOU window.

- WU-043 LOAD TEST (SPEC-043 docs/specs/load-test.md): a skip-gated Go harness
  (runs/load_test.go + schedule/stampede_test.go) driving the REAL runs.Service over
  MockEngine on a scratch DB seeded with WU-041's GenerateEstate — joins \`go test -race
  ./...\`. B1 50 dumps × 50 distinct instances → all succeed, exactly one submitted + one
  finished each, zero orphans/zero leaked locks, peak concurrency proven via a sweep-line
  over COMMITTED started/finished timestamps (not a racy live sample). B2 40 concurrent at
  ONE instance → 1 win + 39 ErrInstanceLocked, exactly 1 run row. B3 mixed. B4 (stampede)
  25 due schedules, one sequential fireDue → all fire, zero orphans. Test-only, no service
  change. Findings filed with verdicts: F1 default pgxpool MaxConns adequate
  (PORTAL_DB_MAX_CONNS a nice-to-have), F2 scheduler self-rate-limits.

- WU-044 MAINTENANCE & RETENTION (SPEC-044 docs/specs/maintenance.md, ADR-013): a new
  \`internal/maintenance.Service\` on the scheduler's tick lifecycle — \`go maint.Run(ctx)\`
  in main sweeps ONCE at boot then every PORTAL_MAINTENANCE_INTERVAL (default 1h). Three
  best-effort passes: (1) SESSION GC \`DELETE FROM session WHERE expires_at < now()\` (a
  live session is structurally excluded); (2) ARTIFACT RETENTION deletes \`standard\`
  registry artifacts past PORTAL_ARTIFACT_RETENTION (default 90d) AND writes an
  \`artifact.reaped\` audit_event (actor \`maintenance\`) in ONE modifying-CTE statement —
  \`safety\` is structurally unselectable (WHERE retention_class='standard'), and a
  NON-POSITIVE age DISABLES the pass (zero would reap everything — fail-safe); (3) AUDIT
  RETENTION is OBSERVATIONAL — audit_event/auth_event are append-only BY TRIGGER (a DELETE
  cannot succeed), so the pass only reports the oldest-event age and logs when it exceeds
  PORTAL_AUDIT_RETENTION (default 365d), mutating NOTHING. Object BYTES are the object
  store's lifecycle-expiry (engine-side, ADR-004), never \`mc rm\` from the portal; a reaped
  non-NULL location is logged WARN (under MockEngine location is NULL). Also (4) dump.yml
  post-pg_dump tasks wrapped in \`block:\`/\`always: file state=absent\` so a failed upload
  never orphans real dump bytes (M3-gate finding 8). No migration.

- WU-045 DOCS-RECON + COLD-START + CI HARDENING (report
  docs/agent/reviews/wu-045-reconciliation.md): reconciled 7 docs-vs-reality drifts. Code
  change: \`config.LocateDotenv\` (config.go) now BOUNDS the upward .env walk at the REPO
  ROOT (nearest ancestor with \`.git\`; go.mod is NOT the boundary — it's in backend/) and
  honors a \`PORTAL_DOTENV\` explicit-path/disable override; main logs the resolved path.
  This fixes the M1-gate foreign-.env footgun (the walk could hit the filesystem root and
  adopt a stray parent .env). CI (.github/workflows/check.yml): added a Postgres 16 service
  (health-gated) + PORTAL_DB_* env so DB tests + golden flow + the load/stampede harness RUN
  in CI instead of skipping; PINNED golangci-lint to v2.12.2 (installer script AND binary,
  not curl|sh from HEAD); bumped actions to checkout/setup-node/setup-go @v7 +
  cache-dependency-path. .env.example gained the WU-041..044 knobs.

- WU-046 PACKAGING FOR PILOT (docs/deploy.md; no module → no SPEC): turned the
  build:release binary into a pilot-deployable, reboot-surviving service. (1)
  infra/dbportal.service — a REAL (non-transient) systemd unit: \`EnvironmentFile=\` (config
  from a file, never inline), \`Restart=on-failure\`, \`WantedBy=multi-user.target\` (the
  reboot-survival guarantee), sandbox hardening (NoNewPrivileges / ProtectSystem=strict /
  PrivateTmp / …), and \`Environment=PORTAL_DOTENV=\` to DISABLE the WU-045 .env search on
  the host. (2) infra/portal.env.template — deploy config, SHAPE ONLY (no secrets),
  [REQUIRED] markers. (3) docs/deploy.md — the fresh-host runbook (build → provision
  user/dirs → install binary+env+unit → migrate → enable --now → verify), incl. break-glass,
  TLS/CookieSecure, upgrade/rollback. HARDENING (code): (a) BREAK-GLASS MAIL ALARM —
  \`notify.Mailer.BreakGlassUsed\` (D7 content: who/where/when, NEVER the password/token) +
  a one-method \`authn.Alarmer\` seam (authn stays a leaf pkg — NO notify import) +
  \`Service.Alarm\` wired in main to the Mailer when PORTAL_NOTIFY_TO is set; fired ASYNC
  best-effort OFF the login path (a slow/failed SMTP must never block emergency access — the
  audit row + error log are the durable record, mail only accelerates). (b)
  \`config.Validate()\` — fail closed on a misconfigured SERVER boot (ldap mode requires
  PORTAL_LDAP_URL + PORTAL_LDAP_BIND_TEMPLATE) BEFORE the pool/port; called AFTER subcommand
  dispatch so migrate/import/seed skip it. (c) VERSION PROVENANCE — \`var Commit\` /
  \`var BuildDate\` stamped by build-release.sh via \`-ldflags -X\`; \`portal version\`
  prints commit/date; build-release also emits a \`.sha256\` checksum.

- WU-047 EXPERIMENT RETROSPECTIVE (docs/agent/RETROSPECTIVE.md, STRATEGY §8): docs-only, the
  M4 exit deliverable — scored the harness-development experiment against STRATEGY §8 metrics
  with cited JOURNAL/gate/memory evidence. No code.

KNOWN, DELIBERATE deferrals — do NOT report these as findings:
- Object-store BYTES are reaped by the store's own lifecycle-expiry, engine-side (ADR-004);
  the portal reaps only the REGISTRY row and NEVER issues \`mc rm\`. Under MockEngine
  \`location\` is NULL so there is nothing to expire. Not a finding.
- Audit retention is OBSERVATIONAL by design (retain-in-DB; cold-storage archival is
  post-MVP). The audit pass mutating nothing is CORRECT, not a bug.
- PORTAL_DB_MAX_CONNS floor is a WU-046 nice-to-have (WU-043 F1: default pgxpool
  MaxConns=max(4,NumCPU) proven adequate at 50 concurrent). Scheduler stampede self-rate-
  limits (F2). Neither is a finding.
- Auto-detecting the portal's OWN instance is impossible (inventory carries no connection
  tuple), so PORTAL_PROTECTED_INSTANCES is a manual set; auto-detect is post-MVP.
- Full Patroni pause/detach→restore→reinit sequencing is post-MVP (ADR-012); the
  restore-onto-k8s_patroni block is DELIBERATELY narrow (dumps stay allowed).
- A rolled-back conflicting insert consumes (burns) a run-id sequence value — a documented
  COSMETIC gap, not a defect (the run row is never created).
- The compose stack (Semaphore BoltDB, minio, pgtarget) is DEV infra: single-node, admin
  creds in gitignored .env, no TLS between compose services, no HA. Not prod-shaped, not a
  finding. The persistent dev \`portal\` DB sits at migration 0010 by design (tests + drills
  use fresh scratch DBs that get 0012 on \`up\`).
- CI has a Postgres service now (WU-045), but the itest and some drills remain VM-only where
  they need the full compose stack; that is intended.
- Phase-1/2/3 deferrals still stand (no role admin UI; run logs not persisted; single window
  per instance; CSRF = SameSite=Lax + JSON-only; chain/schedule CRUD not itself audited — the
  FIRED RUNS are).

Ground rules that must hold NOW (violations ARE findings):
- AT MOST ONE LIVE OP PER INSTANCE is structural: no code path, API shape, race, or param
  may create two concurrent live runs on one instance. Acquire rides Start's run-insert tx
  (conflict → whole tx rolls back → no run, no audit, 409); release rides finalize's tx
  after the winner guard. The TTL steal predicate must NEVER reclaim a still-live holder's
  lock. EVERY terminal path must free the lock; a crashed holder is reclaimed at boot.
- The SELF-TARGET ban (ErrSelfTarget 403) and the PATRONI-RESTORE block (ErrPatroniRestore
  403) fire at the assembler (Start / chain.Create) before any row, and write a
  \`guardrail.denied\` audit row. No case/unicode/whitespace trick on the target name, no
  renamed/re-platformed instance, may slip past.
- The safety dump remains structural (M3 invariant) and the lock is a GATE IN FRONT of the
  unchanged guardrail/audit/ritual path — locks must not weaken any M3 guarantee.
- \`safety\` artifacts are STRUCTURALLY unreapable (the reap's WHERE clause selects only
  \`standard\`). A NON-POSITIVE PORTAL_ARTIFACT_RETENTION disables the reap (it must never
  reap everything). The reap writes an accurate \`artifact.reaped\` audit row.
- audit_event / auth_event are append-only by trigger; the maintenance audit pass mutates
  NOTHING; session GC never deletes a live (unexpired) session.
- The break-glass alarm NEVER contains the password or token (D7: who/where/when only), and
  is fired ASYNC best-effort — a slow/failed/blocking Mailer must NEVER delay or fail the
  emergency login. \`authn\` must not import \`notify\` (leaf-pkg boundary; the Alarmer seam).
- \`config.Validate()\` fails a misconfigured SERVER boot closed (an ldap door nobody can
  open) BUT must not block the migrate/import/seed subcommands.
- The systemd unit must actually survive reboot (WantedBy=multi-user.target), take config
  from EnvironmentFile (never inline secrets), and its sandboxing must not break the binary.
  \`PORTAL_DOTENV=\` disables the host .env walk.
- Secrets never enter the repo, the portal DB, run params, or logs — portal.env.template and
  .env.example are SHAPE ONLY; no real credential in any infra/CI/deploy file.
- The .env walk is bounded at the repo root (\`.git\`) and cannot adopt a stray parent .env.

Read the actual code with your tools (start from the spec files if useful). Only report
findings you can anchor to a specific file (and line where possible) with a concrete failure
scenario: what inputs/state lead to what wrong behavior. Quality over quantity — a milestone
gate wants real defects, not style notes. Severity: critical = a guardrail bypass (two
concurrent live ops on one instance, self-target/Patroni block defeated, restore without
safety dump, a safety artifact reaped, audit mutated, config-validate letting a broken-auth
door boot, a secret/break-glass credential leaked to repo/DB/log/mail); high = user-visible
wrong behavior or crash in a mainline flow (a lock never released wedging an instance
forever, the break-glass alarm blocking login, a systemd unit that won't boot or won't
survive reboot); medium = edge-case wrong behavior; low = latent risk / footgun.
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
    key: 'concurrency-locks',
    charter: `Dimension: INSTANCE CONCURRENCY LOCKS + GUARDRAIL ASSEMBLERS (WU-042 — the core M4 safety surface).
Scope: backend/internal/runs/lock.go (acquire/release/steal SQL), backend/internal/runs/
service.go (Start's acquire inside the run-insert tx, finalize's release after the winner
guard, SweepOrphans crash-heal), backend/internal/chain/chain.go (Create: self-target +
Patroni-restore refusals before any row), backend/internal/schedule/executor.go (overlap
probe + ErrInstanceLocked→skipped_overlap mapping), backend/internal/config/config.go
(PORTAL_LOCK_TTL, PORTAL_PROTECTED_INSTANCES parse + DBName seeding),
backend/internal/db/migrations/0012_instance_locks.sql (up/down, the 0006 CHECK extension),
backend/internal/server/{runs_http.go,restore_http.go} (409/403 mapping), and the lock
tests (runs/lock_test.go, chain/chain_lock_test.go, schedule/lock_test.go).
Hunt for: ANY interleaving that lets two live runs exist on one instance (acquire NOT inside
Start's tx, a release that fires before the terminal state commits, a conflict that leaves a
row or audit behind, a gap between the overlap probe and the lock); the TTL steal predicate
taking a STILL-LIVE holder's lock (is the "NOT EXISTS a live holder" leg correct and race-
free? what run states count as "live"? a very short PORTAL_LOCK_TTL must stay safe); a lock
that is NEVER released (a terminal path that skips release → the instance is wedged 409
forever until TTL, or forever if steal is broken; does EVERY finalize branch — success,
failed, unknown-job, sweep-reclaim — free it?); crash-heal holes (SweepOrphans→finalize
must release; a lock whose run row was never committed); self-target bypass (case/unicode/
whitespace/trailing-dot tricks on the target name vs PROTECTED set membership, an instance
added to inventory AFTER boot, the set seeded with DBName but the real target compared
differently); Patroni-restore bypass (platform read from the wrong column, a chain assembled
so the restore step's target differs from Create's check, dumps mis-blocked or restores
mis-allowed); the guardrail.denied audit row missing/wrong actor; 0012 up/down/up
correctness and the CHECK constraint. The run-id burned on a rolled-back conflicting insert
is a KNOWN cosmetic gap — NOT a finding.`,
  },
  {
    key: 'retention-safety',
    charter: `Dimension: MAINTENANCE & RETENTION SAFETY (WU-044 — data-deletion correctness).
Scope: backend/internal/maintenance/maintenance.go (the three passes + Run's boot-sweep/
ticker loop), backend/internal/maintenance/maintenance_test.go, backend/cmd/portal/main.go
(go maint.Run wiring, ctx/lifecycle), backend/internal/config/config.go
(PORTAL_MAINTENANCE_INTERVAL / PORTAL_ARTIFACT_RETENTION / PORTAL_AUDIT_RETENTION parse +
defaults), playbooks/dump.yml (the block:/always: staging-rm orphan fix). Cross-ref the
append-only triggers in migrations 0003-0005 and the artifact retention_class semantics
(0009, finalize's stamp).
Hunt for: ANY way a \`safety\` artifact gets reaped (a WHERE clause that isn't strictly
retention_class='standard', a join that widens the set, a modifying-CTE that deletes before
it filters); the NON-POSITIVE age guard failing OPEN (0, negative, unset, or a parse
fallback that yields 0 → the pass reaping EVERYTHING, or an interval so the boot sweep reaps
brand-new artifacts); the \`artifact.reaped\` audit row being wrong (wrong actor, wrong
run linkage, written when nothing was deleted, or the DELETE and the audit INSERT not atomic
so a crash leaves one without the other); the audit pass MUTATING anything (it must be
observational — does any code path DELETE/UPDATE audit_event or auth_event? does it just
mis-log?); session GC deleting a LIVE (unexpired) session or racing an active request;
a reaped non-NULL \`location\` being silently orphaned instead of logged; the boot sweep or
ticker leaking a goroutine / not honoring ctx cancel / a panic in one pass killing the
others; error handling that turns a transient DB error into data loss; and dump.yml's
block:/always: actually removing the staging file on the failure path (and NOT masking a
real upload failure).`,
  },
  {
    key: 'deploy-hardening',
    charter: `Dimension: DEPLOY HARDENING — break-glass alarm, config-validate, systemd, provenance (WU-046).
Scope: backend/internal/authn/service.go (the break-glass path + the Alarmer seam +
Service.Alarm), backend/internal/notify/notify.go (BreakGlassUsed — the mail body), backend/
cmd/portal/main.go (Alarm wiring, the config.Validate call site vs subcommand dispatch, the
version subcommand), backend/internal/config/config.go (Validate()), backend/internal/
version/version.go (Commit/BuildDate), infra/dbportal.service, infra/portal.env.template,
infra/build-release.sh (ldflags -X + .sha256), docs/deploy.md.
Hunt for: the break-glass MAIL or LOG leaking the password/token/hash or any secret (D7 =
who/where/when ONLY — inspect exactly what BreakGlassUsed and its call site put in the body
and in every log line on that path); the alarm being SYNCHRONOUS or able to block/fail the
emergency login (a slow/hung/erroring Mailer must not delay or fail auth — is it truly
async, best-effort, and off the hot path? is a nil Alarm safe?); a \`notify\` import inside
\`authn\` (the leaf-pkg boundary the Alarmer seam exists to preserve); config.Validate
letting a misconfigured SERVER boot (ldap mode with a missing URL/bind-template — a door
nobody can open) OR wrongly blocking migrate/import/seed (the call must be after subcommand
dispatch); the systemd unit NOT surviving reboot (WantedBy=multi-user.target present and
correct?), taking secrets inline instead of via EnvironmentFile, a sandbox directive
(ProtectSystem=strict / PrivateTmp / NoNewPrivileges / ReadWritePaths) that would break the
binary's real needs (writing its state dir, reading the env file, binding the port,
resolving DNS/LDAP), or a User/Group/paths mismatch with deploy.md; \`Environment=PORTAL_DOTENV=\`
actually disabling the walk; portal.env.template or the unit carrying a REAL secret or a
default that is unsafe in prod (CookieSecure off, a bind-all, a weak default); build-release
stamping bad provenance or a broken checksum; and any docs/deploy.md step that would NOT work
if followed literally on a fresh host (wrong order, a command that needs a dir that isn't
created yet, migrate run as the wrong user, enable without the unit installed).`,
  },
  {
    key: 'ci-supply-chain-config',
    charter: `Dimension: CI, SUPPLY-CHAIN PINNING & DOTENV BOUNDARY (WU-045).
Scope: .github/workflows/check.yml (the Postgres 16 service, PORTAL_DB_* env, the golangci
pin, the @v7 action bumps, cache-dependency-path), backend/internal/config/config.go
(LocateDotenv — the repo-root-bounded walk + PORTAL_DOTENV override), backend/internal/
config/config_test.go, backend/cmd/portal/main.go (the "loaded/no dotenv" logging), .env.example,
docs/agent/reviews/wu-045-reconciliation.md (do its claims match the code?).
Hunt for: the .env walk STILL able to adopt a stray parent .env (does the boundary really
stop at the nearest \`.git\`? what about a repo with NO \`.git\` — a release tarball, a
worktree with a \`.git\` FILE not dir, a symlinked ancestor; the "outside repo → check CWD
only" and self-skip guards; does PORTAL_DOTENV='' truly disable vs PORTAL_DOTENV=/path load
that exact file, and what on a missing path — silent skip or hard fail?); the CI Postgres
service not actually gating the DB tests (health-check wrong, PORTAL_DB_* not matching the
service, so tests SILENTLY skip again while the job stays green — verify the env wiring makes
the golden flow + load/stampede harness genuinely RUN, not skip); the golangci pin being
partial (installer script pinned but the resolved binary still floats, or a version drift vs
CLAUDE.md's 2.12.2 / the VM); an action still on a floating major or an unpinned third-party
action; cache-dependency-path pointing at the wrong go.sum; .env.example leaking a REAL value
or missing/mis-defaulting one of the WU-041..044 knobs (LOCK_TTL, PROTECTED_INSTANCES,
MAINTENANCE_INTERVAL, ARTIFACT_RETENTION, AUDIT_RETENTION); and any claim in the reconciliation
report that the current code contradicts.`,
  },
  {
    key: 'seed-loadtest-specs',
    charter: `Dimension: STAGING SEED + LOAD-TEST SOUNDNESS + SPEC/DOC CONFORMANCE (WU-041, WU-043, WU-047 + all M4 specs).
Scope: backend/internal/inventory/seed.go (GenerateEstate) + seed_test.go, backend/cmd/portal/
main.go (the seed subcommand flags/wiring), backend/internal/runs/load_test.go +
backend/internal/schedule/stampede_test.go (the harness itself), and the SPEC/doc surface:
docs/specs/{staging-seed,concurrency-locks,load-test,maintenance}.md vs the shipped code,
docs/DECISIONS.md (ADR-012/013), docs/ARCHITECTURE.md + docs/ROADMAP.md M4 edits,
docs/agent/RETROSPECTIVE.md, and the M4 claims in docs/agent/archive/STATE-history.md (moved out of STATE.md, s40).
Hunt for (SEED): a generated CSV row that \`inventory.Import\` would actually REJECT
(so the "0 rejects" claim is false) — bad env value, a name collision, a two-platform cluster
that self-quarantines unexpectedly, a windows-string prod row the WU-023 parser rejects;
non-determinism (any map iteration / time / un-seeded rand / non-fixed call order breaking
byte-identical output for a given seed); the env-mix math being wrong at small N (max(1,18%)
rounding, prod empty, ratios drifting) or clusters=max(5,n/8) producing a bad platform split;
non-idempotency on re-import. Hunt for (LOAD-TEST SOUNDNESS — does the harness actually PROVE
what STATE/SPEC claim?): the peak-concurrency sweep-line computing the wrong number or being
secretly racy; the "exactly-once" assertion that would pass even if a run double-fired; the
zero-orphan/zero-leaked-lock check not actually querying what it claims; a seeded estate that
quarantines rows so the test runs on fewer instances than stated; B2's "1 win + N-1
ErrInstanceLocked" assertion being satisfiable by a wrong error; skip-gating that makes the
harness a silent no-op in the very CI WU-045 wired it into. Hunt for (SPECS/DOCS): any place
an M4 spec's stated behavior and the shipped code disagree in EITHER direction (code wrong OR
spec/STATE/RETROSPECTIVE stale/overclaiming) — especially a ground-rule invariant a doc
describes but the code doesn't enforce, or a RETROSPECTIVE metric the repo doesn't support.`,
  },
]

const MAX_PER_DIM = 8

log('M4 gate: 5 Sonnet reviewers over Phase 4 (locks/retention/deploy-hardening/CI-config/seed-loadtest-specs); findings return unverified — the architect verifies inline')

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
