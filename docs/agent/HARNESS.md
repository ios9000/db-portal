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
   no-card, archive-isolation and every validator failure (26 cases).

Reading the hook's freshness lines during these tests: `HEURISTIC: N commit(s) landed after…`
is normal right after code commits and proves nothing by itself; treat it as "glance at
`git log`", not as "the card is wrong".

Headless equivalent of steps 2–5 (cheap; prints the hook event itself):
`claude -p "<question>" --model haiku --tools "" --output-format stream-json --verbose --include-hook-events | jq -r 'select(.subtype=="hook_response") | .hook_name'`
then `--resume <session_id>` with `/compact` or `/clear` as the prompt.

## 4. Rollback

- **Hook off, everything else kept:** delete the `hooks` key in `.claude/settings.json`, or set
  `"disableAllHooks": true` in `.claude/settings.local.json`.
- **Validator out of the gate:** in `package.json` set `check` back to
  `npm run check:be && npm run check:fe`; delete the two state blocks at the end of `.githooks/pre-commit`.
- **STATE.md layout:** `git revert <commit>` of the split restores the single 88 KB file
  byte-for-byte (the split moved lines verbatim; nothing was rewritten in the moved blocks).
- **Everything:** `git revert` the harness commits (`git log --oneline -- .claude/hooks docs/agent/HARNESS.md`).
