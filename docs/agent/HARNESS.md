# Agent harness — what is enforced, what is asked, how to test it, how to undo it

The harness is the set of files that keeps an agent oriented across restarts, resumes,
`/clear` and compaction, and keeps delegated work cheap and bounded. Rules live in
`CLAUDE.md`; this file is the operator's reference. It is NOT start-up reading.

## 1. Pieces

| Piece | File | Runs when |
|---|---|---|
| State card | `docs/agent/STATE.md` (`STATE-CARD` block) | rewritten by the card's owner |
| SessionStart hook | `.claude/hooks/session-state.sh` via `.claude/settings.json` | startup · resume · clear · compact |
| Structure validator | `.claude/hooks/state-validate.sh` | `npm run check:state` ⊂ `npm run check` ⊂ CI · pre-commit · one line in the hook |
| Self-test | `.claude/hooks/test-harness.sh` | `npm run check:state` · pre-commit when `.claude/hooks/` is staged |
| Routed workers | `.claude/agents/{scout,verifier,analyst}.md` | spawned by the coordinator (Agent tool) or a workflow |
| Bounded sweep workflow | `.claude/workflows/research-sweep.js` | on request, for ≥ 3 independent questions |
| Parallel task records | `docs/agent/tasks/<id>.md` | created by the card's owner per concurrent task |
| Stable facts | `docs/agent/STANDING-CONTEXT.md` | on demand (`grep`) |
| History | `docs/agent/archive/STATE-history.md` | on demand (`grep`); never quoted as current |

## 2. Enforced mechanically vs. asked of the agent

**Mechanical (a script or the runtime does it; no judgement involved):**

- The card is injected at startup / resume / clear / compact, with project, branch, HEAD and
  dirty count; a missing state file, a branch mismatch, a truncated card and a structurally
  invalid state file are each reported in that output. Output ≤ 8000 B, card ≤ 5000 B, exit 0.
- `state-validate.sh` FAILS (pre-commit blocks the commit; `npm run check` and CI go red) on:
  STATE.md > 16 KB · not exactly one card · card > the hook's budget · a required field
  missing, duplicated or empty · `Last updated` not a UTC timestamp · STATE.md citing a task
  record that does not exist · a task record nobody cites or lacking Owner/Status/Branch ·
  `CLAUDE.md` `@`-importing, or the hook reading, the archive / standing context.
- Start-up never loads the archive: the hook reads only the card + task-record headers, and the
  validator fails if that changes (self-test asserts it with sentinel strings).

**Heuristic (printed, never pass/fail):** card age, commits since STATE.md was committed,
uncommitted STATE edits, card branch vs. checkout, BACKLOG status of the active task. A card
can be current with later commits and stale with none; a timestamp is whatever was typed.

**Instruction-based (only as good as the agent following `CLAUDE.md`):** rewriting the card
after a milestone/decision and before a handoff · the content being true and current · done
vs. verified discipline · `UNKNOWN` instead of guessing · a new user request beating the saved
task · not opening the archive at start-up · one owner per file. `git commit --no-verify`
skips the pre-commit gate; CI still runs the validator.

## 3. Live test (≈ 3 minutes; touches no tracked file)

1. **Expected values first** — they change at every checkpoint, so derive them, don't remember them:
   `bash .claude/hooks/state-validate.sh --expect` → note `ACTIVE_TASK`, `LAST_UPDATED`, `CARD_BRANCH`.
2. **Fresh launch:** exit any open session (a running session keeps the hook snapshot it
   started with), run `claude` in the repo, ask: *"From the saved state only, without reading
   files: active task, its Last-updated timestamp, and the source= value in the hook header?"*
   Expect the step-1 values and `startup`.
3. **Request priority:** ask something unrelated (*"what is 17×3?"*). Expect just the answer —
   no steering back to the saved task.
4. **`/compact`**, then repeat the step-2 question: same values, `source=compact`.
5. **`/clear`**, then repeat it: same values, `source=clear`, and the saved task is NOT resumed
   unless you ask for it.
6. **Failure paths — in fixtures, never by moving `STATE.md`:** `npm run check:state` builds
   throwaway repos in a temp dir and asserts missing-state-file, branch-mismatch, oversize,
   no-card, archive-isolation, every validator failure, the routing config and the workflow's
   bounds (34 cases; the count grows with the harness).

Reading the hook's freshness lines during these tests: `HEURISTIC: N commit(s) landed after…`
is normal right after code commits and proves nothing by itself; treat it as "glance at
`git log`", not as "the card is wrong".

Headless equivalent of steps 2–5 (cheap; prints the hook event itself):
`claude -p "<question>" --model haiku --tools "" --output-format stream-json --verbose --include-hook-events | jq -r 'select(.subtype=="hook_response") | .hook_name'`
then `--resume <session_id>` with `/compact` or `/clear` as the prompt.

## 4. Rollback

- **Routing layer off:** delete `.claude/agents/{scout,verifier,analyst}.md` and
  `.claude/workflows/research-sweep.js` (+ `.claude/hooks/research-sweep.dryrun.js` and the
  "Delegation routing config" block in `test-harness.sh`), and the "Delegation & model
  routing" section of `CLAUDE.md`. No settings key was added for it, so nothing else changes.
- **Hook off, everything else kept:** delete the `hooks` key in `.claude/settings.json`, or set
  `"disableAllHooks": true` in `.claude/settings.local.json`.
- **Validator out of the gate:** in `package.json` set `check` back to
  `npm run check:be && npm run check:fe`; delete the two state blocks at the end of `.githooks/pre-commit`.
- **STATE.md layout:** `git revert <commit>` of the split restores the single 88 KB file
  byte-for-byte (the split moved lines verbatim; nothing was rewritten in the moved blocks).
- **Everything:** `git revert` the harness commits (`git log --oneline -- .claude/hooks docs/agent/HARNESS.md`).

## 5. Model routing

Policy (verbatim, user, 2026-09-17): *"Use sub-agents with less expensive models where
appropriate to search and verify information across the codebase and documentation."*

| Tier | Who | Model | Tools | Use for | Hands off when |
|---|---|---|---|---|---|
| 0 | main session, locally | — | Grep / Read | one known file, symbol or value; ≤ ~3 tool calls | it turns into a sweep |
| 1 | `scout` | haiku | Read, Grep, Glob (no Bash, no CLAUDE.md) | bounded read-only research across code + docs | ESCALATE · low confidence · PARTIAL that matters |
| 1 | `verifier` | haiku | Read, Grep, Glob, Bash | ONE stated claim, with the check named in the brief | INCONCLUSIVE · ESCALATE · the failure needs interpreting |
| 2 | `analyst` | sonnet | Read, Grep, Glob, Bash | what tier 1 could not settle; multi-hop code-vs-spec questions | NEEDS-MAIN-SESSION (judgement) · UNRESOLVABLE-FROM-REPO |
| 3 | main session | session model | all | design, security, concurrency judgement; anything still ambiguous | asks the user |

- Resolution order in the runtime (2.1.251+): per-invocation `model` parameter → agent
  frontmatter `model` → `CLAUDE_CODE_SUBAGENT_MODEL` → the session model. So a one-off
  escalation can also be "same agent, `model: "sonnet"`"; the `analyst` file just makes the
  tier explicit and gives it the right contract. Forks ignore model overrides — never fork to save money.
- Implementation delegation (one Sonnet `general-purpose` implementer per tight-brief WU) is a
  separate, older practice and is unchanged.
- A brief for a worker names: the ONE question or claim, the scope (paths), the check to run
  (verifier), and what is already known. It never asks the worker to update shared state.

## 6. Bounded execution loop

gather evidence → act → verify → revise → checkpoint, with these limits to start from:
**≤ 3 concurrent workers · ≤ 3 repair attempts per failing check · after 2 iterations without
progress, stop and reassess** (progress = the answer, its evidence or the check's verdict
changed). Reassess means: different approach, escalate a tier, or ask the user — not a fourth
identical attempt and not a wider fan-out. Whatever is unfinished is reported as unfinished:
status per item, the attempt trail, and the coverage gaps (what was not searched or not
checked). Only a finding whose independent check passed is called "verified"; an analyst's
answer is `resolved-by-analyst-unverified` until someone checks it.

`research-sweep` takes `args = {questions: [{id?, question, scope?, check?}], maxConcurrent?,
maxRepairs?, escalate?, useAgentTypes?}`; args can only TIGHTEN the limits. Typical cost: 2 Haiku
agents per question; worst case 9 per question (4 scouts + 4 verifiers + 1 analyst); at most 9
questions per run, the rest come back under `dropped`. It returns
`{policy, verified, incomplete, coverageGaps, dropped, results, note}` to the coordinator and
writes nothing.

## 7. Checking the ACTUAL model a worker ran on

Do not trust a worker's self-report. Read the transcripts:

```bash
# Agent-tool workers of a session (SID = the session id):
D=~/.claude/projects/-root-db-portal/$SID/subagents
for m in "$D"/*.meta.json; do f="${m%.meta.json}.jsonl"
  echo "$(jq -r .agentType "$m") -> $(jq -r 'select(.type=="assistant")|.message.model' "$f" | sort -u | tr '\n' ' ')"; done
# Workflow workers: same loop over the run's transcript directory (printed in the Workflow result).
```

Verified 2026-09-17 on Claude Code 2.1.274 (evidence: JOURNAL s40): with a Sonnet main session,
`scout` and `verifier` ran on `claude-haiku-4-5-20251001`, and `analyst` called with
`model: "haiku"` ran on Haiku (per-invocation beats frontmatter); with a Haiku main session,
`analyst` ran on `claude-sonnet-5` (frontmatter beats inheritance).

## 8. Routing layer — enforced vs. asked

**Mechanical:** the model each agent type runs on (frontmatter; the runtime applies it) ·
worker tool lists (`scout` cannot run commands or write; `verifier`/`analyst` have no
Edit/Write) · `maxTurns` per worker · in `research-sweep`: ≤ 3 workers in flight, ≤ 3
repairs, stall-stop at 2, one escalation, 9-question cap with `dropped`, explicit `model` on
every call, unverified answers never labelled verified · `test-harness.sh` fails if an agent
loses its pinned model / tool list or the workflow's bounds regress (dry-run, no model calls).
The project's permission deny/ask rules apply to workers as to the main session.

**Instruction-based:** whether to delegate or stay local · when the coordinator escalates
outside a workflow · the 3 / 3 / 2 limits when the main session runs the loop by hand ·
`verifier`/`analyst` not mutating anything through Bash · stating cost before a workflow ·
the coordinator alone writing STATE.md / JOURNAL / BACKLOG. Not applied: a session-wide cap
via `env.CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` — it exists in this CLI, but its effect on the
5-reviewer gate workflows is unverified.
