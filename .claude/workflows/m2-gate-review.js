export const meta = {
  name: 'm2-gate-review',
  description: 'M2 milestone gate: 5 Sonnet reviewers over Phase 2 (authn/authz/scheduler/windows); architect verifies findings inline',
  phases: [{ title: 'Review', detail: '5 independent Sonnet reviewers, one per dimension', model: 'sonnet' }],
}

const REPO = '/root/db-portal'

const COMMON = `
You are reviewing Phase 2 (milestone M2) of "DB Portal" — an internal self-service portal
for PostgreSQL day-2 ops (backup dump + restore, DBA-only, prod guardrails, audit trail).
Repo: ${REPO}. Backend: Go (chi router, pgx, goose migrations, embedded SPA). Frontend:
React + react-router v7 + Vite + TypeScript strict. Engine behind a seam (engine.Adapter /
Registry); MockEngine is the only adapter until WU-033 (M3).

Phase 2 shipped, and is what this gate reviews (WU-020..023):
- AuthN (SPEC-020, docs/specs/authn.md): DB sessions (sha256 of opaque token, 12h TTL,
  lazy expiry), cookie portal_session httpOnly SameSite=Lax, PORTAL_AUTH_MODE
  ldap|fake|off (unset = ldap = fail closed), LDAP template bind, break-glass local
  account (bcrypt env hash, every use alarmed), append-only auth_event table.
- AuthZ (SPEC-021, docs/specs/authz.md): role/user_role tables, requireRole middleware on
  ALL mutating routes (reads session-only), authz.denied rows on auth_event, EXPLICIT
  audit actor (runs.StartRequest.Actor / Cancel actor arg; run.finished inherits the
  submitted row's actor), run.cancel_requested intent-first, server-side prod ritual
  (Confirm == instance name), requested_by filter, body caps (64KiB runs/schedules POST,
  4KiB login, reason<=500).
- Scheduler (SPEC-022, docs/specs/scheduler.md): internal/schedule = store + executor;
  DB tick loop over persisted PRE-JITTERED next_fire_at (robfig/cron parser only);
  misfire = coalesced catch-up; fire-then-stamp (crash mid-fire retries via orphan sweep
  + catch-up composition); overlap = skip visibly; disabled = next_fire_at NULL,
  re-enable computes from now; actor = "schedule:<created_by>", Confirm set
  programmatically (human ritual at CREATION); /api/schedules CRUD.
- Windows (SPEC-023, docs/specs/windows.md): internal/window pure parser
  (Day HH:MM-HH:MM weekly, wrap-capable, start-incl/end-excl, end==start rejected,
  server-local time); runs.Start stamps audit_event.window_warned on run.submitted only;
  instance read model window_state inside|outside|null; D6 = warn NEVER block, every
  parse failure = no warning (logged once per instance per process).

KNOWN, DELIBERATE deferrals — do NOT report these as findings:
- No session GC sweep (lazy per-request expiry delete only); no lockout / denial-rate
  alarming; no break-glass email alarm (slog Error only); no role admin UI/CLI (ldap mode
  grants via manual psql INSERT); no LDAP-group->role mapping.
- CSRF posture is SameSite=Lax + JSON-only mutations, decided in SPEC-020 — no CSRF
  tokens by design.
- Scheduler: no edit-in-place (delete+recreate), no per-schedule timezone UI (typed
  CRON_TZ works), no cron preview/translation, no schedule-change audit ledger (schedule
  CRUD is not on the audit trail; FIRED RUNS are), boot-stampede spreading deferred to M4.
- Windows: single window per instance, no holiday grammar, no per-instance TZ;
  window_warned is NOT surfaced on the run read model / UI (audit column only).
- Run logs are NOT persisted (SSE replay from engine memory) — SPEC-013.
- CI has no Postgres service: DB tests + golden flow skip there; the VM gate is real.
- Phase-1 code was gated at M1 (fix WUs 016-019 closed) — re-review it ONLY where Phase 2
  touched it (runs actor/ritual/window changes, server router, api.ts, LaunchDrawer).

Ground rules that must hold NOW (violations are findings):
- audit_event AND auth_event are append-only (DB triggers incl. TRUNCATE).
- Denied must mean denied: a failing role store answers 500, never 403.
- The actor is explicit; 'local-dev' appears ONLY in mode=off bypass provisioning.
- Every mutating route sits behind requireRole(dba); reads need a session.
- Secrets never enter the repo, the portal DB, or logs (password never logged/stored;
  session table holds sha256 only; break-glass hash only via env).
- Prod ritual is enforced server-side for BOTH launch and schedule creation.
- A maintenance window can never block, delay, or fail a launch (D6).
- internal/runs.Service is the ONLY Registry caller; scheduler fires ONLY through
  runs.Service.Start.

Read the actual code with your tools (start from the spec files above if useful). Only
report findings you can anchor to a specific file (and line where possible) with a
concrete failure scenario: what inputs/state lead to what wrong behavior. Quality over
quantity — a milestone gate wants real defects, not style notes. Severity: critical =
auth bypass / audit-trail corruption / silently wrong prod action / secret leak; high =
user-visible wrong behavior or crash in a mainline flow; medium = edge-case wrong
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
    key: 'security-authn',
    charter: `Dimension: AUTHENTICATION SECURITY.
Scope: backend/internal/authn/**, backend/internal/server/auth_http.go + middleware,
backend/internal/db/migrations/0005_authn.sql, backend/internal/config (auth fields),
backend/cmd/portal/main.go (buildAuthenticator).
Hunt for: session token entropy/generation weaknesses, token comparison timing issues,
session fixation (is the cookie rotated on login?), lazy-expiry holes (expired session
still usable? clock skew?), cookie attribute gaps (httpOnly/SameSite/Secure/Path),
LDAP DN/filter injection through the username (check the allowlist), StartTLS downgrade,
empty/whitespace password unauthenticated-bind acceptance, user-exists oracles (timing or
message differences between unknown-user and bad-password), break-glass weaknesses
(bcrypt cost, hash handling, log leakage), password appearing in any log/error/audit row,
auth_event append-only bypass, PORTAL_AUTH_MODE=off reachable in a prod-shaped config
without loud warning, logout semantics (does it really revoke?).`,
  },
  {
    key: 'security-authz',
    charter: `Dimension: AUTHORIZATION SECURITY.
Scope: backend/internal/authz/**, backend/internal/server/{authz_http.go,server.go,
runs_http.go,schedules_http.go}, backend/internal/runs (actor plumbing, prod ritual),
backend/internal/schedule (Create confirm check, executor actor), migration 0006.
Hunt for: ANY mutating endpoint not behind requireRole (walk the FULL route tree in
server.go — including future-facing paths like logout, and confirm PATCH/DELETE guards),
guard ordering bugs (role check before body parse? actor extraction failure modes),
actor spoofing routes (can a request influence StartRequest.Actor or schedule
created_by?), prod-ritual bypasses on either door (launch confirm vs schedule-create
confirm: unicode/whitespace/case tricks, instance rename windows), authz fail-OPEN paths
(store error treated as allow anywhere?), denial-row content leaking request bodies,
Grant idempotency abuse, break-glass standing grant risks, privilege boundary between
read endpoints (is anything mutating disguised as a GET?).`,
  },
  {
    key: 'concurrency-scheduler',
    charter: `Dimension: SCHEDULER CONCURRENCY & LIFECYCLE.
Scope: backend/internal/schedule/** (store + executor), backend/internal/runs
(Start/watch/finalize/SweepOrphans interplay), backend/cmd/portal/main.go (goroutine
wiring, shutdown ordering).
Hunt for: races between the tick loop and CRUD (SetEnabled/Delete racing fire — deleted
schedule stamped? FK violation on last_run_id after delete?), fire-then-stamp crash
windows the spec's sweep+catch-up composition does NOT heal, overlap-check TOCTOU
(last run finalizes between check and Start — is double-launch possible? is that
acceptable?), coalescing bugs (catch-up firing more than once, next_fire_at computed
from the wrong instant, jitter pushing next fire BEFORE now or skipping a legit fire),
timezone/DST edges in Next() evaluation (server-local — what happens across a DST
transition for a 02:30 daily?), ticker drift/blocking (a slow fire delaying every other
due schedule), executor ctx cancellation mid-fire (partial state), unbounded goroutines,
the executor racing SweepOrphans at boot, schedule fires racing each other on the same
instance.`,
  },
  {
    key: 'correctness-backend',
    charter: `Dimension: BACKEND CORRECTNESS (Phase-2 surface).
Scope: backend/internal/window/**, backend/internal/schedule (CRUD semantics, validation),
backend/internal/server/{schedules_http.go,auth_http.go,authz_http.go} handler contracts,
backend/internal/runs (window stamp, requested_by filter, cancel_requested, body caps),
backend/internal/inventory (window_state), migrations 0005-0007 up/down symmetry.
Hunt for: window parser wrong-answers (day boundary math, wrap edge at exactly midnight,
minute 59, whitespace forms, the (Day+1)%7 arithmetic), window evaluated with the wrong
clock or cached staleness, window_state vs window_warned disagreement paths, schedule
validation gaps (cron specs robfig accepts that surprise: @every, CRON_TZ, 6-field,
seconds — what does Create store and what does the executor then do?), NULLIF/pointer
bugs on reason, JSON contract drift (snake_case, never-null arrays, status codes
400/403/404/409/413), SetEnabled on an enabled schedule silently moving next_fire_at,
List ordering, migration down caveats, error-message quality (parser messages reaching
users safely).`,
  },
  {
    key: 'frontend-and-specs',
    charter: `Dimension: FRONTEND CORRECTNESS + SPEC CONFORMANCE (Phase-2 surface).
Scope: frontend/src/pages/{Login,Schedules}.tsx, frontend/src/components/LaunchDrawer.tsx,
frontend/src/App.tsx (bootstrap gate), frontend/src/lib/api.ts (onUnauthorized, errorDetail,
patchJSON/deleteVoid, Schedule/Instance types); specs: docs/specs/{authn,authz,scheduler,
windows}.md vs shipped code, docs/demo-m1.md beats 7-8, docs/agent/BACKLOG.md WU-020..023
verify lines.
Hunt for: login flow bugs (401-redirect loops, bootstrap tri-state races, identity badge
staleness after logout), Schedules page state bugs (toggle response not matching list row
shape, two-step delete confirm resetting wrongly, form state leaking between opens, prod
ritual gating in the create drawer diverging from LaunchDrawer's), window warn line wrong
conditions, api.ts error-envelope handling on PATCH/DELETE, URL/param handling, list keys,
accessibility of role=alert usage. SPEC side: any place SPEC-020..023 text and code
disagree (either direction — code wrong OR spec stale), demo doc steps that would not
actually work if followed literally.`,
  },
]

const MAX_PER_DIM = 8

log('M2 gate: 5 Sonnet reviewers over Phase 2 (authn/authz/scheduler/windows); findings return unverified — the architect verifies inline')

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
