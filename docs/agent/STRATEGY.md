# AI-Harness Development Strategy

How we build DB Portal with an AI agent (Claude Fable 5 in Claude Code) as the primary
developer, without losing state to context limits, compaction, or session boundaries.
This document is the design; `SESSION-PROTOCOL.md` is the checklist form of it.

## 1. The problem being designed around

An agent session has a finite context window. As it fills: retrieval quality degrades,
then the harness auto-compacts (summarizes) the transcript, then the session ends.
Auto-compaction is a safety net, not a plan — summaries lose exactly the kind of detail
(exact file paths, half-finished edits, why an approach was abandoned) that makes the
next hour productive. A multi-week build cannot live inside any single context window,
so the strategy treats context as a **cache** and the repository as the **database**:

> Any fact needed tomorrow must be written to a file today.
> A fresh agent given only the repo must be able to continue within 5 minutes.

That second line is the acceptance test for all of this machinery. We will actually run
it: periodically start a cold session and measure time-to-first-productive-edit.

## 2. Durable state: where each kind of knowledge lives

| Knowledge | Home | Lifecycle |
|---|---|---|
| What is true of the system | code + tests | permanent |
| What we decided and why | `docs/DECISIONS.md` (ADRs) | append + supersede |
| What the system should become | `docs/ARCHITECTURE.md`, `docs/specs/` | revised deliberately |
| What to build, in what order | `docs/agent/BACKLOG.md` | groomed |
| Where we are RIGHT NOW | `docs/agent/STATE.md` | overwritten every checkpoint |
| What happened, in order | `docs/agent/JOURNAL.md` | append-only, one line per event |
| How to work | `CLAUDE.md` (auto-loaded) | rarely changes |

Two failure modes this table prevents:
- **State smeared across conversation** — banned; STATE.md is the single resume point.
- **Docs as write-only archive** — CLAUDE.md's session protocol forces STATE.md and the
  active WU's context brief to be read every session, so drift is caught immediately.

## 3. The unit of work: context-window-sized WUs

Every task is a Work Unit in `BACKLOG.md` with: goal, deliverables, acceptance criteria,
**verification commands** (executable, not prose), and a **context brief** — the exact
files/sections the implementing session needs to read. Sizing rule:

- **S** — fits comfortably with room for debugging (target: most WUs)
- **M** — fits one session but plan a mid-WU checkpoint
- **L** — forbidden. Split it in grooming before anyone starts it.

The context brief is the load-bearing part: it converts "read the whole repo to get
oriented" (the thing that burns 40% of a window before any work happens) into a
bounded, curated read. Writing the brief is part of grooming, done while the knowledge
is cheap — by the session that created the neighboring code.

## 4. The loop: Explore → Plan → Implement → Verify → Record

Each WU runs one pass of this loop; each step has a context-discipline rule attached.

1. **Explore** — establish ground truth before editing. Rule: bulk reading is delegated
   to Explore subagents (read-only, own context, return conclusions not file dumps);
   the main context receives paragraphs, not pages. Direct Reads only for files being edited.
2. **Plan** — for M-sized or design-heavy WUs, plan before code (plan mode, or a written
   plan at the top of the session). The plan names files to touch, in order, and the test
   strategy. S-sized mechanical WUs may skip to Implement.
3. **Implement** — code + tests together, committing at every green intermediate state.
   Match existing idiom; no drive-by refactors (file them as WU candidates instead).
4. **Verify** — run the WU's verification commands from BACKLOG.md, plus the global gate
   (`lint + typecheck + full test suite`). Where there's a runtime surface, drive it
   (curl the endpoint, click the flow) — a green unit suite is necessary, not sufficient.
5. **Record** — the checkpoint ritual: BACKLOG status, STATE.md rewrite, JOURNAL line
   with real command output evidence, commit. Recording is what makes step 1 cheap for
   the next WU.

## 5. Context-budget management (the token-limit playbook)

**Budget rule of thirds:** ~⅓ of a session's window for orientation + planning, ⅓ for
implementation, ⅓ reserved for debugging + the closing ritual. When the reserve starts
being spent on implementation, checkpoint.

**Checkpoint triggers — whichever comes first:**
- a natural green state (tests pass after a coherent step)
- about to start a risky/expansive tangent (dependency upgrade, debugging rabbit hole)
- the session has been long enough that a compaction may be near — checkpoint *before*
  it happens, so the summary has a clean anchor to summarize *around*
- any "let me just also…" impulse (that's scope creep; file it, checkpoint, continue)

**If compaction happens anyway:** treat the summary as unreliable narration. Re-read
STATE.md + `git status` + `git log --oneline -10` + the active WU entry; reconcile against
the summary; continue. This is the same procedure as a cold start — by design, recovery
from compaction and recovery from a killed terminal are the identical ritual.

**Offloading patterns** (keep the main window for decisions and edits):
- **Explore subagents** for any read wider than ~3 files: "how does X work / where is Y".
- **Plan subagents** for architecture questions mid-implementation.
- **Fork/background agents** for long-running verification (test suites, builds) whose
  output would flood the window.
- **Artifact-first communication:** long outputs (specs, reports) are written to files
  and referenced by path, never pasted into the conversation.

## 6. Multi-agent structure: single writer, many readers

One main agent owns the working tree and makes all edits — parallel writers on one
checkout create merge chaos that costs more than parallelism saves. Subagents are
readers/advisors (Explore, Plan, review). Two sanctioned exceptions:

- **Milestone review gates:** at each M-gate (ROADMAP.md), a multi-agent review pass —
  independent reviewers over correctness / security / spec-conformance dimensions with
  adversarial verification of findings. This is a Workflow-scale operation; the user
  opts in per gate (say "use a workflow for the M-gate review").
- **Isolated experiments:** a spike WU may run in a worktree-isolated agent, reporting
  findings; its code is treated as disposable reference, not merged blind.

## 7. Quality gates

- **Per-commit:** lint + typecheck + affected tests green (enforced by pre-commit once
  WU-001 lands).
- **Per-WU:** WU verification commands + full suite + runtime check of the changed flow.
- **Per-milestone:** the milestone's demo script executes end-to-end; multi-agent review
  gate; `docs/` reconciled with reality (ADRs for what changed, specs updated);
  cold-start test — fresh session, 5-minute productivity check.
- **Golden flow:** from M1 onward, an end-to-end test of the hero flow (dump run-now →
  status → audit record) must stay green in every session. It is the canary that layers
  beneath a WU didn't silently break.

## 8. Experiment instrumentation (this is also a harness test)

The project doubles as an evaluation of Fable + Claude Code on a complex build. JOURNAL.md
line format captures per-session: WU(s) advanced, checkpoint count, whether compaction
occurred, defects found later attributable to the session. Reviewable metrics:
- WU throughput per session; rework rate (WUs reopened after "done")
- cold-start time (target ≤ 5 min from session open to first productive edit)
- escaped defects per milestone (found after the WU's verification passed)
- how often the golden flow caught a regression

**Filled (WU-047, end of MVP): [`RETROSPECTIVE.md`](RETROSPECTIVE.md)** — each metric above
scored against the JOURNAL, plus the session-loss failure modes + mitigations, the
architect/implementer delegation outcomes + cost, and concrete "next experiment" changes.
Headline: ~47 WUs / 31 sessions, cold-start 3m42s, escaped defects 17→11→8 across M1→M3,
zero guardrail invariants escaped, and repo-as-memory survived 3+ process deaths with no
lost work.

If a rule here is generating friction without value, change the rule — in this file, via
a JOURNAL entry, so the change itself survives the session.
