// Dry-run of .claude/workflows/research-sweep.js with a FAKE agent(): no model is called.
// Asserts the bounded-loop policy the script is supposed to enforce mechanically:
// <= 3 workers in flight, <= 3 repairs, stop after 2 iterations without progress, one
// escalation, explicit model on every call, incomplete results + coverage gaps preserved.
// Run by .claude/hooks/test-harness.sh (needs node).
const fs = require('fs')
const path = require('path')
const src = fs.readFileSync(path.join(__dirname, '..', 'workflows', 'research-sweep.js'), 'utf8').replace('export const meta', 'const meta')
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor
const run = new AsyncFunction('args', 'agent', 'parallel', 'pipeline', 'phase', 'log', 'budget', 'workflow', src)

let inflight = 0, peak = 0
const calls = []
const fake = async (prompt, opts) => {
  inflight++; peak = Math.max(peak, inflight)
  calls.push({ label: opts.label, model: opts.model, agentType: opts.agentType })
  await new Promise(r => setTimeout(r, 3))
  inflight--
  const id = opts.label.split(':')[1].split('#')[0]
  const attempt = Number(opts.label.split('#')[1] || 0)
  if (opts.label.startsWith('scout')) {
    if (id === 'esc') return { verdict: 'ESCALATE', answer: '?', evidence: [], searched: [], gaps: ['specs not read'], confidence: 'low' }
    // "moving" changes its evidence every attempt (progress each time) -> must hit the repair cap, not the stall stop
    return { verdict: 'FOUND', answer: 'a', evidence: [{ path: 'p', line: id === 'moving' ? String(attempt) : '1', quote: 'q' }], searched: ['s'], gaps: id === 'gap' ? ['docs/ not searched'] : ['none'], confidence: 'high' }
  }
  if (opts.label.startsWith('verify')) return { verdict: id === 'ok' || id === 'gap' ? 'PASS' : 'FAIL', reason: 'r', checks: [], gaps: [], confidence: 'high' }
  return { verdict: id === 'esc' ? 'NEEDS_MAIN_SESSION' : 'RESOLVED', answer: 'an', evidence: [], gaps: [], confidence: 'medium', open_question: 'oq' }
}

const questions = ['ok', 'stuck', 'esc', 'gap', 'moving', 'ok2', 'ok3', 'ok4', 'ok5', 'over1', 'over2'].map(id => ({ id, question: id }))
const fails = []
const must = (name, cond) => { console.log(`  ${cond ? 'ok   ' : 'FAIL '} ${name}`); if (!cond) fails.push(name) }

run({ questions, maxConcurrent: 99, maxRepairs: 99 }, fake, null, null, () => {}, () => {}).then(r => {
  const by = id => r.results.find(x => x.id === id)
  const scouts = id => calls.filter(c => c.label.startsWith(`scout:${id}#`)).length
  must('never more than 3 workers in flight (args cannot raise the cap)', peak <= 3 && r.policy.maxConcurrent === 3)
  must('args cannot raise the repair cap', r.policy.maxRepairs === 3)
  must('identical failing check: stops after 2 iterations without progress (3 scouts), then escalates once', scouts('stuck') === 3 && calls.filter(c => c.label === 'analyst:stuck').length === 1)
  must('failing check that keeps changing: 1 try + 3 repairs = 4 scouts, then stops', scouts('moving') === 4 && by('moving').attempts === 4)
  must('an analyst answer is labelled unverified, never "verified"', by('stuck').status === 'resolved-by-analyst-unverified' && !r.verified.includes('stuck'))
  must('scout ESCALATE goes to the analyst and comes back with its open question', by('esc').status === 'unresolved:NEEDS_MAIN_SESSION' && r.incomplete.some(i => i.id === 'esc' && i.openQuestion === 'oq'))
  must('coverage gaps are preserved ("none" is not a gap)', r.coverageGaps.some(g => g.id === 'gap' && g.gap === 'docs/ not searched') && !r.coverageGaps.some(g => /^none/i.test(g.gap)))
  must('questions over the cap are reported as dropped, not silently cut', r.dropped.length === 2 && r.results.length === 9)
  must('every worker call carries an explicit model that matches its role', calls.every(c => c.model === { scout: 'haiku', verifier: 'haiku', analyst: 'sonnet' }[c.agentType]))
  must('attempt trail is kept for unfinished questions', by('stuck').trail.length >= 6)
  console.log(`  dry-run: ${calls.length} fake agent calls, peak ${peak} in flight`)
  process.exit(fails.length ? 1 : 0)
}).catch(e => { console.error('  FAIL  workflow script threw:', e); process.exit(1) })
