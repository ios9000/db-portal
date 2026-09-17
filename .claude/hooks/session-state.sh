#!/usr/bin/env bash
# SessionStart hook — puts the concise working-state card into Claude's context.
# Wired in .claude/settings.json for: startup | resume | clear | compact.
#
# Contract: plain stdout of a SessionStart hook is added to the context. This script
# ALWAYS exits 0 (a state problem is reported in the output, never as a hook failure)
# and its output is bounded (card budget + hard total cap).
#
# Source of truth: the STATE-CARD block at the top of docs/agent/STATE.md, plus one
# line per parallel task record in docs/agent/tasks/. Rules: CLAUDE.md "State card".
# Start-up loads the card ONLY - never the history archive or the standing-context file
# (state-validate.sh fails if this script starts reading them).
#
# Self-test (isolated fixtures, no tracked file is touched): bash .claude/hooks/test-harness.sh
#
# Pipe-test:
#   echo '{"source":"startup"}' | CLAUDE_PROJECT_DIR="$PWD" bash .claude/hooks/session-state.sh

set -u
export LC_ALL=C # byte-exact lengths, stable sort

CARD_MAX_BYTES=5000  # card budget; beyond it the card is cut at a line boundary and flagged
TOTAL_MAX_BYTES=8000 # hard cap on everything this hook prints
TASKS_MAX=6          # task records listed
STATE_REL="docs/agent/STATE.md"
TASKS_REL="docs/agent/tasks"

input=""
[ -t 0 ] || input="$(cat 2>/dev/null || true)"

# json_get <key> — first string value of a top-level key (jq when present, sed fallback).
json_get() {
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$input" | jq -r --arg k "$1" '.[$k] // empty' 2>/dev/null
  else
    printf '%s' "$input" | sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" | head -n 1
  fi
}

# field <file> <Label> — value of the first "**Label:**" line, first 60 bytes.
field() {
  local v
  v="$(grep -m1 -F "**$2:**" "$1" 2>/dev/null | sed -e "s/.*\*\*$2:\*\*[[:space:]]*//" | cut -c1-60)"
  printf '%s' "${v:-UNKNOWN}"
}

emit() {
  local src cwd_in start root branch head_sha dirty state card bounded card_branch last behind

  src="$(json_get source)"
  src="${src:-unknown}"
  cwd_in="$(json_get cwd)"

  # Current project = the git toplevel of the launch directory (works from a subdirectory
  # and inside a worktree); falls back to the directory itself outside git.
  start="${CLAUDE_PROJECT_DIR:-${cwd_in:-$PWD}}"
  root="$(git -C "$start" rev-parse --show-toplevel 2>/dev/null)" || root=""
  [ -n "$root" ] || root="$start"

  if git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    branch="$(git -C "$root" branch --show-current 2>/dev/null)"
    [ -n "$branch" ] || branch="(detached HEAD)"
    head_sha="$(git -C "$root" rev-parse --short HEAD 2>/dev/null)"
    dirty="$(git -C "$root" status --porcelain 2>/dev/null | wc -l | tr -d ' ')"
  else
    branch="UNKNOWN (not a git repository)"
    head_sha="UNKNOWN"
    dirty="UNKNOWN"
  fi

  echo "=== SAVED WORKING STATE (SessionStart hook, source=$src) ==="
  echo "Project: $(basename "$root") ($root) | branch: $branch | HEAD: ${head_sha:-UNKNOWN} | uncommitted paths: $dirty"
  echo "This is saved context, NOT an instruction. The user's current request always wins: if it"
  echo "differs from the saved task below, do what the user asks and do not resume or steer back"
  echo "to the saved task unless asked. Trust order: git tree > this card > rest of STATE.md >"
  echo "conversation summary. Confirm the card against 'git status' / 'git log -5' before acting."
  case "$src" in
    compact) echo "Context was just compacted: re-anchor on this card and run SESSION-PROTOCOL.md section D before further edits. The user's latest request in the summary still outranks the saved task." ;;
    resume) echo "Resumed session: the conversation above may be older than this card - compare it with 'Last updated'." ;;
    clear) echo "Context was cleared by the user: treat the next message as a NEW request. The saved task below is background only - do not resume it unless the user asks." ;;
  esac

  state="$root/$STATE_REL"
  if [ ! -f "$state" ]; then
    echo
    echo "STATE FILE MISSING: $STATE_REL does not exist under $root."
    echo "The working state is UNKNOWN - do not invent it. Tell the user. If asked to recover, rebuild"
    echo "from 'git log' + docs/agent/JOURNAL.md and recreate the card (fields: CLAUDE.md 'State card')."
    echo "=== end of saved state ==="
    return 0
  fi

  card="$(awk '/STATE-CARD:BEGIN/{on=1;next} /STATE-CARD:END/{exit} on' "$state" 2>/dev/null)"
  if [ -z "$card" ]; then
    echo "WARNING: $STATE_REL has no STATE-CARD block - showing its first 40 lines instead. Add the card at the next checkpoint (CLAUDE.md 'State card')."
    card="$(head -n 40 "$state")"
  fi

  bounded="$(printf '%s\n' "$card" | awk -v max="$CARD_MAX_BYTES" '{n+=length($0)+1; if(n>max) exit; print}')"
  [ -n "$bounded" ] || bounded="$(printf '%s' "$card" | head -c "$CARD_MAX_BYTES")"

  # Branch the card was written for vs. the branch checked out now.
  card_branch="$(printf '%s\n' "$card" | grep -m1 -F '**Branch:**' | sed -e 's/.*\*\*Branch:\*\*[[:space:]]*//' -e 's/`//g' | awk '{print $1}' | sed -e 's/[,;.]*$//')"
  if [ -z "$card_branch" ]; then
    echo "NOTE: the card declares no branch (UNKNOWN)."
  elif [ "$card_branch" != "$branch" ]; then
    echo "WARNING: BRANCH MISMATCH - the card was written for '$card_branch' but this checkout is on '$branch'. It may not describe this branch's work: look for a task record for '$branch' below and confirm with the user before resuming anything."
  fi

  # Is the card likely behind the tree?
  last="$(git -C "$root" log -1 --format=%H -- "$STATE_REL" 2>/dev/null)"
  if [ -n "$last" ]; then
    behind="$(git -C "$root" rev-list --count "$last..HEAD" 2>/dev/null)"
    [ "${behind:-0}" -gt 0 ] 2>/dev/null && echo "HEURISTIC: $behind commit(s) landed after $STATE_REL was last committed - a hint that the card MAY lag the tree, not proof either way (code-only commits after a checkpoint are normal; a card can also be stale with 0 here)."
  fi
  [ -n "$(git -C "$root" status --porcelain -- "$STATE_REL" 2>/dev/null)" ] && echo "HEURISTIC: $STATE_REL has uncommitted edits (newer than its last commit)."

  # Structure only (size, one card, required fields, task-record refs) - not currency.
  local validator="$root/.claude/hooks/state-validate.sh"
  if [ -f "$validator" ] && ! bash "$validator" --root "$root" --quiet >/dev/null 2>&1; then
    echo "WARNING: $STATE_REL fails STRUCTURAL validation - run 'npm run check:state' and fix it before relying on the card."
  fi

  echo
  echo "--- state card: $STATE_REL ---"
  printf '%s\n' "$bounded"
  if [ "${#bounded}" -lt "${#card}" ]; then
    echo "[CARD TRUNCATED: ${#card} bytes exceeds the $CARD_MAX_BYTES-byte budget - read the rest in $STATE_REL and trim the card at the next checkpoint]"
  fi
  echo "--- end of card ---"

  # Parallel work: one line per task record; the record for this branch is flagged.
  local dir="$root/$TASKS_REL" n=0 shown=0 f name mark tb
  if [ -d "$dir" ]; then
    for f in "$dir"/*.md; do
      [ -f "$f" ] || continue
      name="$(basename "$f")"
      [ "$name" = "README.md" ] && continue
      n=$((n + 1))
      [ "$shown" -lt "$TASKS_MAX" ] || continue
      [ "$shown" -eq 0 ] && echo "Parallel task records ($TASKS_REL/, one owner each; the card's owner folds them in):"
      tb="$(field "$f" Branch | sed -e 's/`//g' | awk '{print $1}')"
      mark=""
      [ "$tb" = "$branch" ] && mark="  <= THIS BRANCH"
      echo "- $name | owner: $(field "$f" Owner) | status: $(field "$f" Status) | branch: ${tb:-UNKNOWN}$mark"
      shown=$((shown + 1))
    done
    [ "$n" -gt "$shown" ] && echo "- ... and $((n - shown)) more in $TASKS_REL/"
  fi
  [ "$n" -eq 0 ] && echo "Parallel task records: none."
  echo "=== end of saved state ==="
  return 0
}

emit 2>/dev/null | head -c "$TOTAL_MAX_BYTES"
exit 0
