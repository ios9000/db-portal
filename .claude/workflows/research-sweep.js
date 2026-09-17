export const meta = {
  name: 'research-sweep',
  description: 'Bounded research sweep: max 3 concurrent Haiku scouts, independent Haiku verification, one Sonnet escalation per unresolved question; returns findings + explicit gaps to the coordinator',
  whenToUse: 'Three or more independent "where is / what references / does the doc match the code" questions across code and docs, or a sweep you will re-run. NOT for one lookup (grep it locally) and NOT for design, security or concurrency judgement. Workers never write shared state. Typical cost: 2 Haiku agents per question; worst case 9 agents per question (4 scouts + 4 verifiers + 1 Sonnet analyst).',
  phases: [
    { title: 'Scout', detail: 'one read-only scout per question, at most 3 in flight', model: 'haiku' },
    { title: 'Verify', detail: 'independent check of each answer; failed checks are repaired, bounded', model: 'haiku' },
    { title: 'Escalate', detail: 'one analyst pass for escalated, stalled or exhausted questions', model: 'sonnet' },
  ],
}

// ---- policy (CLAUDE.md "Delegation & model routing"). args may TIGHTEN these, never raise them.
const ROUTES = { scout: 'haiku', verifier: 'haiku', analyst: 'sonnet' } // explicit model per role
const MAX_CONCURRENT = 3 // workers in flight (the runtime's own cap is higher - this one is ours)
const MAX_REPAIRS = 3 // repair attempts per failing check
const MAX_STALLS = 2 // consecutive iterations without progress before the question is handed back
const MAX_QUESTIONS = 9 // per run; anything beyond is reported as dropped, never silently cut

const cfg = args && typeof args === 'object' ? args : {}
const tighten = (hard, v) => (Number.isInteger(v) && v >= 0 ? Math.min(hard, v) : hard)
const maxConcurrent = Math.max(1, tighten(MAX_CONCURRENT, cfg.maxConcurrent))
const maxRepairs = tighten(MAX_REPAIRS, cfg.maxRepairs)
const escalate = cfg.escalate !== false
const all = Array.isArray(cfg.questions) ? cfg.questions.filter(q => q && typeof q.question === 'string') : []
if (!all.length) {
  return { error: 'args.questions must be a non-empty array of {id?, question, scope?, check?}', results: [] }
}
const questions = all.slice(0, MAX_QUESTIONS)
const dropped = all.slice(MAX_QUESTIONS).map((q, i) => ({ id: q.id || `q${MAX_QUESTIONS + i + 1}`, question: q.question, why: `over the ${MAX_QUESTIONS}-question cap - run again with these` }))
if (dropped.length) log(`${dropped.length} question(s) over the cap were NOT run - listed under "dropped"`)

const CONF = { type: 'string', enum: ['high', 'medium', 'low'] }
const STRS = { type: 'array', items: { type: 'string' } }
const EVIDENCE = { type: 'array', items: { type: 'object', properties: { path: { type: 'string' }, line: { type: 'string' }, quote: { type: 'string' } }, required: ['path', 'quote'] } }
const SCOUT = {
  type: 'object',
  properties: { verdict: { type: 'string', enum: ['FOUND', 'NOT_FOUND', 'PARTIAL', 'ESCALATE'] }, answer: { type: 'string' }, evidence: EVIDENCE, searched: STRS, gaps: STRS, confidence: CONF, escalate_reason: { type: 'string' } },
  required: ['verdict', 'answer', 'evidence', 'searched', 'gaps', 'confidence'],
}
const VERIFY = {
  type: 'object',
  properties: { verdict: { type: 'string', enum: ['PASS', 'FAIL', 'INCONCLUSIVE', 'ESCALATE'] }, reason: { type: 'string' }, checks: STRS, gaps: STRS, confidence: CONF },
  required: ['verdict', 'reason', 'checks', 'gaps', 'confidence'],
}
const ANALYST = {
  type: 'object',
  properties: { verdict: { type: 'string', enum: ['RESOLVED', 'PARTIAL', 'UNRESOLVABLE_FROM_REPO', 'NEEDS_MAIN_SESSION'] }, answer: { type: 'string' }, evidence: EVIDENCE, prior_findings: { type: 'string' }, gaps: STRS, confidence: CONF, open_question: { type: 'string' } },
  required: ['verdict', 'answer', 'evidence', 'gaps', 'confidence'],
}

// Role contracts, inlined only when the project's custom agent types are not registered
// (e.g. a session started before .claude/agents/ existed). Routing is explicit either way.
const SHARED = 'Repo: /root/db-portal. You are a worker in a bounded research sweep. NEVER edit, create or delete files, never commit, never write docs/agent/STATE.md, JOURNAL.md or BACKLOG.md - your structured result is the only output; the coordinator owns shared state. docs/agent/archive/ and JOURNAL.md are history: never report them as the current state. Every claim needs path:line + a verbatim quote. Do not guess: if the evidence conflicts or the question needs judgement, use ESCALATE.'
const ROLE = {
  scout: `${SHARED} Role: scout - read-only research with Read/Grep/Glob. Stay inside the given scope; stop when answered.`,
  verifier: `${SHARED} Role: verifier - check ONE answer independently. Run only read-only commands or the check named in the brief. PASS only if the check ran and supports the claim; otherwise FAIL or INCONCLUSIVE.`,
  analyst: `${SHARED} Role: analyst - the escalation tier. Start from the attached findings: confirm or refute them, then settle the question or say exactly why it cannot be settled from the repo. Leave design / security / risk judgement to the main session (NEEDS_MAIN_SESSION).`,
}
let typesOk = cfg.useAgentTypes !== false
async function spawn(role, prompt, opts) {
  const base = { ...opts, model: ROUTES[role] }
  if (typesOk) {
    try {
      return await agent(prompt, { ...base, agentType: role })
    } catch (e) {
      typesOk = false
      log(`agent type '${role}' unavailable (${String(e).slice(0, 90)}) - using the default worker with the role contract inlined; model routing is unchanged`)
    }
  }
  return agent(`${ROLE[role]}\n\n${prompt}`, base)
}

const scopeOf = q => (q.scope ? `Scope (do not search outside it): ${q.scope}` : 'Scope: the repository, excluding node_modules, dist, .git.')
const scoutPrompt = (q, feedback) => `Question: ${q.question}\n${scopeOf(q)}${feedback ? `\n\nA previous answer was rejected - ${feedback}\nRe-examine with a DIFFERENT search; do not repeat the same one.` : ''}`
const verifyPrompt = (q, f) => `Independently check this answer. Do not trust it.\nQuestion: ${q.question}\n${scopeOf(q)}\nAnswer under test (${f.verdict}, confidence ${f.confidence}): ${f.answer}\nCited evidence: ${JSON.stringify((f.evidence || []).slice(0, 12))}\n${q.check ? `Run exactly this check: ${q.check}` : 'Open each cited path:line and confirm the quote is there verbatim; then run ONE search of your own (a different pattern from the obvious one) for a counter-example inside the scope.'}\nPASS only if the answer is supported and you found no counter-example. For a NOT_FOUND answer, PASS means your own search also found nothing.`
const analystPrompt = (q, trail, why) => `Escalated question (${why}): ${q.question}\n${scopeOf(q)}\nWhat the cheaper workers produced, oldest first:\n${JSON.stringify(trail).slice(0, 6000)}`

async function runQuestion(q, idx) {
  const id = q.id || `q${idx + 1}`
  const trail = [] // every attempt is kept: an unfinished question still returns what was learned
  let finding = null, check = null, repairs = 0, stalls = 0, lastSig = null, feedback = '', status = 'incomplete'
  for (;;) {
    finding = await spawn('scout', scoutPrompt(q, feedback), { label: `scout:${id}#${repairs}`, phase: 'Scout', schema: SCOUT }) // gather
    trail.push({ step: 'scout', attempt: repairs, model: ROUTES.scout, verdict: finding ? finding.verdict : 'NO_RESULT', confidence: finding && finding.confidence, answer: finding && finding.answer, evidence: finding && (finding.evidence || []).slice(0, 12) })
    if (!finding) { status = 'worker-failed'; break }
    if (finding.verdict === 'ESCALATE' || finding.confidence === 'low') { status = 'escalated-by-scout'; break }
    check = await spawn('verifier', verifyPrompt(q, finding), { label: `verify:${id}#${repairs}`, phase: 'Verify', schema: VERIFY }) // verify
    trail.push({ step: 'verify', attempt: repairs, model: ROUTES.verifier, verdict: check ? check.verdict : 'NO_RESULT', reason: check && check.reason })
    if (check && check.verdict === 'PASS') { status = 'verified'; break }
    if (!check || check.verdict === 'ESCALATE') { status = 'escalated-by-verifier'; break }
    const sig = JSON.stringify([finding.verdict, (finding.evidence || []).map(e => `${e.path}:${e.line}`).sort(), check.verdict])
    stalls = sig === lastSig ? stalls + 1 : 0 // progress = the answer, its evidence or the verdict changed
    lastSig = sig
    if (stalls >= MAX_STALLS) { status = 'stalled'; log(`${id}: ${MAX_STALLS} iterations without progress - stop and reassess`); break }
    if (repairs >= maxRepairs) { status = 'repairs-exhausted'; break }
    repairs++ // revise
    feedback = `verifier said ${check.verdict}: ${check.reason} | rejected answer: ${finding.answer}`
  }
  let analysis = null
  if (status !== 'verified' && escalate) {
    analysis = await spawn('analyst', analystPrompt(q, trail, status), { label: `analyst:${id}`, phase: 'Escalate', schema: ANALYST })
    trail.push({ step: 'analyst', model: ROUTES.analyst, verdict: analysis ? analysis.verdict : 'NO_RESULT', answer: analysis && analysis.answer, open_question: analysis && analysis.open_question })
    // an analyst answer is NOT independently verified - the status says so
    status = analysis ? (analysis.verdict === 'RESOLVED' ? 'resolved-by-analyst-unverified' : `unresolved:${analysis.verdict}`) : `${status}+analyst-failed`
  }
  const best = analysis && analysis.verdict === 'RESOLVED' ? analysis : finding
  const gaps = [...((finding && finding.gaps) || []), ...((check && check.gaps) || []), ...((analysis && analysis.gaps) || [])].filter(g => g && !/^none\.?$/i.test(g.trim()))
  return {
    id, question: q.question, status,
    answer: best ? best.answer : null,
    evidence: best ? (best.evidence || []).slice(0, 12) : [],
    searched: (finding && finding.searched) || [],
    gaps: [...new Set(gaps)],
    openQuestion: (analysis && analysis.open_question) || null,
    attempts: repairs + 1,
    tiers: [...new Set(trail.map(t => t.model))],
    trail,
  }
}

// at most `limit` questions in flight; each question runs its workers one after another,
// so at most `limit` workers exist at any moment.
async function pool(items, limit, fn) {
  const out = new Array(items.length)
  let next = 0
  const lane = async () => {
    while (next < items.length) {
      const i = next++
      try { out[i] = await fn(items[i], i) } catch (e) { out[i] = { id: items[i].id || `q${i + 1}`, question: items[i].question, status: 'error', error: String(e).slice(0, 300), gaps: ['not examined - the worker errored'], trail: [] } }
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, lane))
  return out
}

log(`research-sweep: ${questions.length} question(s), <= ${maxConcurrent} in flight, <= ${maxRepairs} repairs each, routes ${JSON.stringify(ROUTES)}`)
const results = await pool(questions, maxConcurrent, runQuestion)

return {
  policy: { maxConcurrent, maxRepairs, maxStalls: MAX_STALLS, routes: ROUTES, customAgentTypesUsed: typesOk },
  verified: results.filter(r => r.status === 'verified').map(r => r.id),
  incomplete: results.filter(r => r.status !== 'verified').map(r => ({ id: r.id, status: r.status, openQuestion: r.openQuestion || null })),
  coverageGaps: results.flatMap(r => (r.gaps || []).map(g => ({ id: r.id, gap: g }))),
  dropped,
  results,
  note: 'Workers wrote nothing to shared state. Only "verified" items had an independent check pass; everything else is a lead, not a fact. The coordinator decides what enters STATE.md / JOURNAL.',
}
