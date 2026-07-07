# Session Protocol

The checklist form of `STRATEGY.md`. Every development session follows this shape.

## A. Session start (≤ 5 minutes)

1. Read `docs/agent/STATE.md`. It names the active WU and the exact next action.
2. `git status` + `git log --oneline -5` — confirm the tree matches what STATE.md claims.
   Mismatch? Reconcile FIRST (see D. Recovery) and journal what you found.
3. Read the active WU in `docs/agent/BACKLOG.md`, then ONLY the files in its context brief.
4. State (to the user, one paragraph): what you're doing, expected deliverable this session.

Do NOT: re-read the whole docs tree, re-derive past decisions, or start by "getting an
overview of the codebase" — the context brief exists so you don't have to.

## B. During work

- One WU at a time. New ideas / discovered debt → one line under "Icebox" in BACKLOG.md.
- Bulk exploration → Explore subagent. Long-running commands → background. Keep the main
  window for decisions and edits.
- Commit at every green state. Message format: `WU-012: add run-now endpoint (3/5 AC)`.
- If blocked on a genuine user decision: record the question and options in STATE.md
  under "Blocked", checkpoint, then ask — so the question survives even if the session dies.

## C. Checkpoint ritual (do at natural boundaries; NEVER skip before session end)

1. Commit working code (tests green; if not green, commit to `wip/WU-xxx` and say so).
2. Rewrite `docs/agent/STATE.md` (template in that file — full rewrite, no appending).
3. Append ONE line to `docs/agent/JOURNAL.md` (format in that file), including verification
   evidence if a WU was completed.
4. If the WU is done: flip its BACKLOG.md status to `done`, set STATE.md to the next WU.
5. Commit the docs: `chore: checkpoint WU-012 (done) → WU-013`.

## D. Recovery (cold start, killed session, or post-compaction — same procedure)

1. Trust order: `git log`/`git diff` > STATE.md > JOURNAL.md > conversation summary.
2. `git status` — uncommitted changes? Read the diff before touching anything; decide
   continue / commit-as-wip / discard, and journal the decision.
3. On a `wip/` branch? STATE.md's "Resume point" says exactly where to pick up.
4. If STATE.md contradicts the tree (says WU done but code absent, etc.): believe the tree,
   fix STATE.md, journal the discrepancy — that's a protocol failure worth recording.

## E. Session end

Checkpoint ritual (C) + one closing paragraph to the user: what was delivered (with
evidence), what's next, any decision needed from them. The next session must be able to
start from files alone — assume this conversation is never seen again.

## F. Compaction posture

Don't fear compaction; make it harmless. If you notice the session is very long and a
heavy task remains: checkpoint NOW, then continue — the post-compaction agent inherits a
clean anchor. After a compaction: run D before any further edits.
