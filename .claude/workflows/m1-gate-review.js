export const meta = {
  name: 'm1-gate-review',
  description: 'M1 milestone gate: multi-agent review of Phase 1 code with adversarial verification of findings',
  phases: [
    { title: 'Review', detail: '5 independent reviewers, one per dimension' },
    { title: 'Verify', detail: 'two adversarial verifiers per finding' },
  ],
}

const REPO = '/root/db-portal'

const COMMON = `
You are reviewing Phase 1 (milestone M1) of "DB Portal" — an internal self-service portal
for PostgreSQL day-2 ops (backup dump + restore, DBA-only, prod guardrails, audit trail).
Repo: ${REPO}. Backend: Go (chi-style net/http router, pgx, goose migrations, embedded SPA).
Frontend: React + react-router v7 + Vite + TypeScript strict. Engine behind a seam
(engine.Adapter / Registry); MockEngine is the only adapter until WU-033.

KNOWN, DELIBERATE Phase-1 deferrals — do NOT report these as findings:
- No authentication/authorization at all. Arrives WU-020/021. actor='local-dev' is hardcoded.
- Prod guardrail (typed instance name) is CLIENT-SIDE ONLY; the API is deliberately
  unguarded until WU-021.
- Run logs are NOT persisted (SSE replay from in-memory buffer only) — by design, SPEC-013.
- maintenance_window is raw text, no semantics until WU-023.
- CI has no Postgres service: DB tests + golden-flow e2e skip in CI; the VM is the gate.
- Notifier errors are log-only by design; mail fires only for failed|canceled.
- CSRF/session hardening belongs to WU-020/021.

What DOES matter (ground rules that must hold NOW):
- audit_event is append-only (DB trigger raises on UPDATE/DELETE); one event per run
  transition; env + playbook_tag stamped on every event.
- Registry.For / engine.ClassForEnv fail closed; Registry.Register panics on
  cross-class adapter sharing.
- Secrets must never enter the repo, the portal DB, or logs.
- internal/runs.Service is the ONLY Registry caller.
- MockEngine job ids are nonce'd; stale job ids MUST fail.

Read the actual code with your tools. Only report findings you can anchor to a specific
file (and line where possible) with a concrete failure scenario: what inputs/state lead
to what wrong behavior. Quality over quantity — a milestone gate wants real defects, not
style notes. Severity: critical = data loss / audit-trail corruption / silently wrong
prod action; high = user-visible wrong behavior or crash in a mainline flow; medium =
edge-case wrong behavior; low = latent risk / footgun.
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

const VERDICT = {
  type: 'object',
  required: ['isReal', 'reasoning'],
  properties: {
    isReal: { type: 'boolean' },
    reasoning: { type: 'string' },
    adjustedSeverity: { enum: ['low', 'medium', 'high', 'critical'] },
  },
}

const DIMENSIONS = [
  {
    key: 'correctness-backend',
    charter: `Dimension: BACKEND CORRECTNESS.
Scope: backend/internal/{runs,engine,inventory,notify,server,db,config,catalog}, backend/cmd/portal.
Hunt for: run state-machine holes (Start -> watcher -> finalize; SweepOrphans on boot),
error paths that leave a run or audit trail inconsistent, transaction boundaries around
run transitions + audit_event writes, inventory CSV import idempotency (imported/updated/
unchanged/quarantined classification), registry fail-closed gaps, HTTP handler status-code
or encoding bugs (404/400/502 contracts, snake_case, never-null arrays), SSE endpoint
contract (replay then follow, exactly ONE end event, 404/410), migration correctness.
Also read the tests: a test that pins wrong behavior is a finding.`,
  },
  {
    key: 'concurrency',
    charter: `Dimension: CONCURRENCY & LIFECYCLE.
Scope: backend/internal/runs (service, logs, watcher), backend/internal/engine/mock.go,
backend/internal/notify, backend/internal/server (SSE handler), backend/cmd/portal/main.go.
Hunt for: races the -race detector wouldn't trip in tests (time-dependent interleavings,
map/slice sharing across goroutines), SSE replay->follow gap where log lines can be lost
or duplicated between buffer snapshot and subscription, goroutine leaks (watcher outliving
run, SSE clients never unsubscribed, notifier goroutine tracking), double-finalize or
finalize-vs-cancel races (cancel is 202 async), shutdown ordering (in-flight watchers vs
server close), orphan sweep racing live watchers after crash-restart, channel deadlocks,
context cancellation propagation.`,
  },
  {
    key: 'correctness-frontend',
    charter: `Dimension: FRONTEND CORRECTNESS.
Scope: frontend/src/** (App, pages/{MyDatabases,Activity,RunDetail,Schedules},
components/{EnvBanner,EnvBadge,LaunchDrawer,RunStatus,StatusFooter}, lib/{api,csv,format,version}).
Hunt for: SSE consumption bugs in RunDetail (reconnect duplicating lines, missed end event,
410/404 handling), LaunchDrawer typed-name prod ritual bypasses (whitespace/case/paste
quirks that let a mismatched name through, or that block a correct name), stale state when
switching runs/instances (effect cleanup, race between fetch and navigation), URL param
round-tripping on MyDatabases/Activity (view+env, filter chips), CSV export escaping
(commas/quotes/newlines/injection), api.ts error handling (non-2xx, malformed JSON),
date/format helpers, keys in lists, EnvBanner showing the WRONG env.`,
  },
  {
    key: 'security',
    charter: `Dimension: SECURITY (within Phase-1 scope — re-read the deferrals list; authn/authz
and API-side prod guard are OUT of scope).
Scope: whole repo — backend, frontend, migrations, npm/Go supply-chain manifests, .env handling.
Hunt for: SQL injection (string-built queries, filter params in runs/query.go and
inventory/store.go), secrets leaking into logs / audit events / mail bodies / error
messages (PORTAL_SMTP_* etc.), audit append-only trigger bypass routes (TRUNCATE? table
owner? separate connection role?), XSS via instance names / CSV-imported fields rendered
in React (dangerouslySetInnerHTML, href injection, mail link building from PORTAL_BASE_URL),
CSV formula injection in the Activity export, SSRF or header injection in notify SMTP,
path traversal in SPA fallback / embedded FS, zip-slip-ish issues in import, dependency
red flags in go.mod / package.json, .env / .env.example hygiene, mock_fail_at-style debug
params reachable in prod builds.`,
  },
  {
    key: 'spec-conformance',
    charter: `Dimension: SPEC CONFORMANCE.
Scope: compare code against the written contracts:
docs/specs/runs.md (SPEC-012), docs/specs/run-detail.md (SPEC-013),
docs/specs/activity-notify.md (SPEC-014), docs/specs/guardrails.md (SPEC-015),
docs/specs/inventory.md, docs/DECISIONS.md (D1-D7 + ADRs), docs/ARCHITECTURE.md.
Hunt for: any place the implementation diverges from its spec — endpoint shapes, status
codes, event names, mail content rules (who/what/where/status + link ONLY — no reason/
error text), audit event fields, import report line format ("imported N new, updated M,
unchanged U, quarantined Q"), guardrail behaviors (EnvBanner placement, typed-name exact
match), engine seam rules (only runs.Service calls Registry; only tests reference
MockEngine concretely). Also the reverse: spec text that no longer matches shipped
reality and needs a doc fix. Cite BOTH the spec line and the code location.`,
  },
]

const MAX_PER_DIM = 8

function reviewPrompt(d) {
  return `${COMMON}\n\n${d.charter}\n\nReturn your findings (empty array if the dimension is clean).`
}

function verifyPrompt(f, lens) {
  const base = `A milestone-gate reviewer reported this finding against ${REPO}:

TITLE: ${f.title}
FILE: ${f.file}${f.line ? ' line ~' + f.line : ''}
SEVERITY (claimed): ${f.severity}
DESCRIPTION: ${f.description}
FAILURE SCENARIO (claimed): ${f.failure_scenario}

Context: Phase 1 deliberately ships WITHOUT authn/authz (WU-020/021), WITHOUT API-side
prod guard (client-side ritual only), WITHOUT persisted run logs, and CI intentionally
skips DB tests. If the finding merely restates one of those deferrals, it is NOT real.`
  if (lens === 'refute') {
    return `${base}

Your job: try hard to REFUTE this finding. Read the actual code (and its tests). Look for
the guard clause, the test that pins the behavior, the caller that makes the scenario
impossible, or a misreading by the reviewer. Only concede isReal=true if, after genuinely
trying, the defect stands as described. If you cannot locate the cited code or reproduce
the reasoning, return isReal=false.`
  }
  return `${base}

Your job: independently trace the claimed FAILURE SCENARIO through the real code, step by
step — concrete request/input, state, and the exact line where behavior goes wrong. If you
cannot construct a concrete end-to-end path to the wrong outcome, return isReal=false.
If you can, return isReal=true and set adjustedSeverity based on realistic impact
(critical = data loss / audit corruption / wrong prod action).`
}

log('M1 gate review: 5 reviewers over Phase 1, adversarial verify on every finding')

const perDim = await pipeline(
  DIMENSIONS,
  d => agent(reviewPrompt(d), { label: `review:${d.key}`, phase: 'Review', schema: FINDINGS }),
  (rev, d) => {
    if (!rev) { log(`review:${d.key} returned nothing`); return [] }
    const order = { critical: 0, high: 1, medium: 2, low: 3 }
    const sorted = [...rev.findings].sort((a, b) => order[a.severity] - order[b.severity])
    if (sorted.length > MAX_PER_DIM) {
      log(`review:${d.key}: ${sorted.length} findings, verifying top ${MAX_PER_DIM} by severity (${sorted.length - MAX_PER_DIM} dropped, listed in result as unverified)`)
    } else {
      log(`review:${d.key}: ${sorted.length} finding(s)`)
    }
    return {
      dim: d.key,
      take: sorted.slice(0, MAX_PER_DIM).map(f => ({ ...f, dimension: d.key })),
      dropped: sorted.slice(MAX_PER_DIM).map(f => ({ ...f, dimension: d.key })),
    }
  },
  async (batch, d) => {
    const judged = await parallel(batch.take.map(f => () =>
      parallel([
        () => agent(verifyPrompt(f, 'refute'), { label: `refute:${d.key}:${(f.title || '').slice(0, 30)}`, phase: 'Verify', schema: VERDICT }),
        () => agent(verifyPrompt(f, 'trace'), { label: `trace:${d.key}:${(f.title || '').slice(0, 30)}`, phase: 'Verify', schema: VERDICT }),
      ]).then(([refute, trace]) => {
        const votes = [refute, trace].filter(Boolean)
        const real = votes.filter(v => v.isReal).length
        const status = votes.length === 0 ? 'unverified' : real === votes.length ? 'confirmed' : real > 0 ? 'plausible' : 'rejected'
        return {
          ...f,
          status,
          severity: (trace && trace.adjustedSeverity) || f.severity,
          verifier_notes: votes.map(v => v.reasoning),
        }
      })
    ))
    return { dim: batch.dim, judged: judged.filter(Boolean), dropped: batch.dropped }
  }
)

const all = perDim.filter(Boolean)
const judged = all.flatMap(b => b.judged)
const confirmed = judged.filter(f => f.status === 'confirmed')
const plausible = judged.filter(f => f.status === 'plausible')
const rejected = judged.filter(f => f.status === 'rejected')
const unverifiedDropped = all.flatMap(b => b.dropped)

log(`Gate result: ${confirmed.length} confirmed, ${plausible.length} plausible (split verdict), ${rejected.length} refuted, ${unverifiedDropped.length} uncapped/unverified`)

return { confirmed, plausible, rejected, unverified_dropped: unverifiedDropped }