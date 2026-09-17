# Task records — parallel work only

One file per task that runs **concurrently** with other work (a delegated implementer, a
worktree, a second session): `docs/agent/tasks/<task-id>.md`, e.g. `WU-054.md`.
Sequential single-session work does NOT need a record — the state card in `../STATE.md` is enough.

## Ownership

- A record has exactly **one owner** (named in the file). Only the owner writes it.
- The state card in `STATE.md` has exactly **one owner**: the main (architect) session. Task
  owners never edit the card; the card's owner folds finished records into it.
- Lifecycle: the card's owner creates the record when it hands the task out → the task owner
  keeps it current (same trigger as the card: milestone, decision, handoff) → when the task is
  folded into the card + JOURNAL, the card's owner deletes the record in that same commit
  (git history keeps it).

The SessionStart hook lists every record here on one line (owner · status · branch) and flags
the one whose `Branch` matches the current checkout. It parses the `**Owner:**`,
`**Status:**` and `**Branch:**` lines — keep those labels exact.

## Template

```markdown
# <task-id> — <title>

- **Owner:** <one agent/session, e.g. "sonnet implementer (spawned s41)">
- **Status:** not started | in progress | blocked | done-unverified | verified
- **Branch:** <branch or worktree branch>
- **Last updated:** <UTC timestamp>
- **Goal / completion criteria:** <ACs or a link to the BACKLOG entry>
- **Decisions (why):** <decision — rationale — link>
- **Done (claimed):** <what exists, with commits>
- **Verified (command → result → evidence):** <only what was actually run; else "none yet">
- **Blockers:** <or "none">
- **Next step:** <exact>
- **Unknown:** <say so explicitly; never guess>
```
