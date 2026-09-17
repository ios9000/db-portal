#!/usr/bin/env bash
# Structural validator for the working-state files (docs/agent/STATE.md + docs/agent/tasks/).
#
# Runs from: `npm run check:state` (part of `npm run check`, so CI too), the pre-commit
# hook (= every checkpoint, which always commits STATE.md), and - one line only - the
# SessionStart hook.
#
# TWO SEPARATE QUESTIONS, deliberately kept apart in the output:
#   STRUCTURE - is the state file well-formed? Mechanical. Decides the exit code.
#   FRESHNESS - might the content be out of date? Heuristic hints only (timestamps, commit
#               counts, branch, BACKLOG status). NEVER decides the exit code: a card can be
#               current with later commits and stale without them.
# Neither says the content is TRUE. Only a review against the tree / git log / JOURNAL can.
#
# Usage: state-validate.sh [--root DIR] [--quiet] [--expect]
#   --quiet   print failures and the result line only
#   --expect  print the values a live test should see (derived from the card) and exit
# Exit: 0 structure OK (warnings allowed) | 1 structural failure | 2 usage error

set -u
export LC_ALL=C

STATE_WARN_BYTES=12000    # STATE.md: warn above
STATE_FAIL_BYTES=16000    # STATE.md: fail above (it was 88 KB before the s40 split)
STANDING_WARN_BYTES=32000 # STANDING-CONTEXT.md: warn above
REQUIRED_FIELDS="Last updated|Summary owner|Goal|Completion criteria|Active task|Branch|Decisions|Done|Verified|Blockers|Next step|Unknown|Parallel task records"

root="" quiet=0 expect=0
while [ $# -gt 0 ]; do
  case "$1" in
    --root) root="${2:-}"; shift 2 || exit 2 ;;
    --quiet) quiet=1; shift ;;
    --expect) expect=1; shift ;;
    *) echo "usage: state-validate.sh [--root DIR] [--quiet] [--expect]" >&2; exit 2 ;;
  esac
done
if [ -z "$root" ]; then
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || root="$PWD"
fi
state="$root/docs/agent/STATE.md"
tasks="$root/docs/agent/tasks"
hook="$root/.claude/hooks/session-state.sh"

fails=0 warns=0
ok() { [ "$quiet" -eq 1 ] || echo "  ok    $*"; }
warn() { warns=$((warns + 1)); [ "$quiet" -eq 1 ] || echo "  warn  $*"; }
fail() { fails=$((fails + 1)); echo "  FAIL  $*"; }
hint() { [ "$quiet" -eq 1 ] || echo "  hint  $*"; }

card() { awk '/STATE-CARD:BEGIN/{on=1;next} /STATE-CARD:END/{exit} on' "$state" 2>/dev/null; }
# value of a top-level card field whose label STARTS with $1 (labels may carry a suffix,
# e.g. "**Decisions (why -> where):**"); sub-bullets count as a value.
field_value() {
  card | awk -v lab="$1" '
    index($0, "- **" lab) == 1 { s = $0; sub(/^- \*\*[^*]*\*\*[ \t]*/, "", s); print s; grab = 1; next }
    grab && /^  +- / { print; next }
    { grab = 0 }'
}
# "Summary owner" shares the "Last updated" line, so count labels anywhere in the card.
field_count() { card | grep -o -F "**$1" | wc -l | tr -d ' '; }

if [ "$expect" -eq 1 ]; then
  [ -f "$state" ] || { echo "STATE FILE MISSING: $state"; exit 1; }
  echo "ACTIVE_TASK=$(field_value 'Active task' | head -n 1 | cut -c1-120)"
  echo "LAST_UPDATED=$(field_value 'Last updated' | grep -o -E '[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}Z' | head -n 1)"
  echo "CARD_BRANCH=$(field_value 'Branch' | sed -e 's/`//g' | awk 'NR==1{print $1}')"
  exit 0
fi

[ "$quiet" -eq 1 ] || echo "state-validate: $state"
[ "$quiet" -eq 1 ] || echo "STRUCTURE (mechanical - decides the exit code)"

if [ ! -f "$state" ]; then
  fail "docs/agent/STATE.md does not exist"
else
  # --- oversized state
  bytes="$(wc -c <"$state" | tr -d ' ')"
  if [ "$bytes" -gt "$STATE_FAIL_BYTES" ]; then
    fail "STATE.md is $bytes B (> $STATE_FAIL_BYTES): history is accreting - move finished-WU detail to JOURNAL/BACKLOG (long blocks: docs/agent/archive/)"
  elif [ "$bytes" -gt "$STATE_WARN_BYTES" ]; then
    warn "STATE.md is $bytes B (> $STATE_WARN_BYTES warn, $STATE_FAIL_BYTES fail): trim it at this checkpoint"
  else
    ok "STATE.md size $bytes B (warn > $STATE_WARN_BYTES, fail > $STATE_FAIL_BYTES)"
  fi
  n="$(grep -c -E '^- \*\*Status \(s[0-9]' "$state")"
  [ "$n" -gt 1 ] && warn "$n per-session 'Status (sNN ...)' blocks in STATE.md - that is how it reached 88 KB; keep at most the current one"

  # --- duplicate / malformed summary block
  b="$(grep -c 'STATE-CARD:BEGIN' "$state")"
  e="$(grep -c 'STATE-CARD:END' "$state")"
  h="$(grep -c -E '^## Working state \(card\)' "$state")"
  bl="$(grep -n 'STATE-CARD:BEGIN' "$state" | head -n 1 | cut -d: -f1)"
  el="$(grep -n 'STATE-CARD:END' "$state" | head -n 1 | cut -d: -f1)"
  if [ "$b" -ne 1 ] || [ "$e" -ne 1 ] || [ "$h" -ne 1 ]; then
    fail "expected exactly one state card, found BEGIN=$b END=$e headings=$h (duplicate or missing summary block - the hook reads only the first)"
  elif [ "$bl" -ge "$el" ]; then
    fail "STATE-CARD:END (line $el) comes before STATE-CARD:BEGIN (line $bl)"
  else
    ok "exactly one state card (lines $bl-$el)"
  fi

  # --- card within the hook's injection budget (single source of truth = the hook script)
  budget="$(sed -n 's/^CARD_MAX_BYTES=\([0-9][0-9]*\).*/\1/p' "$hook" 2>/dev/null | head -n 1)"
  budget="${budget:-5000}"
  cb="$(card | wc -c | tr -d ' ')"
  if [ "$cb" -gt "$budget" ]; then
    fail "card is $cb B (> $budget B hook budget): the SessionStart hook would inject it truncated"
  else
    ok "card size $cb B (hook budget $budget)"
  fi

  # --- required fields: present once, non-empty
  missing="" dup="" empty=""
  old_ifs="$IFS"; IFS='|'
  for f in $REQUIRED_FIELDS; do
    c="$(field_count "$f")"
    if [ "$c" -eq 0 ]; then missing="$missing $f;"
    elif [ "$c" -gt 1 ]; then dup="$dup $f(x$c);"
    elif [ "$f" != "Summary owner" ] && [ -z "$(field_value "$f" | tr -d ' \t\n')" ]; then empty="$empty $f;"
    fi
  done
  IFS="$old_ifs"
  [ -n "$missing" ] && fail "card is missing required field(s):$missing  (write UNKNOWN rather than dropping a field)"
  [ -n "$dup" ] && fail "card field(s) appear more than once:$dup"
  [ -n "$empty" ] && fail "card field(s) are empty:$empty  (write UNKNOWN or 'none')"
  [ -z "$missing$dup$empty" ] && ok "all required card fields present once and non-empty"

  ts="$(field_value 'Last updated' | grep -o -E '[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}Z' | head -n 1)"
  if [ -n "$ts" ]; then ok "'Last updated' is a UTC timestamp ($ts)"; else fail "'Last updated' has no UTC timestamp of the form YYYY-MM-DDTHH:MMZ"; fi

  # --- task-record references, both directions
  refs="$(grep -o -E '(docs/agent/)?tasks/[A-Za-z0-9._-]+\.md' "$state" | sed -e 's#^docs/agent/##' | sort -u)"
  broken=""
  for r in $refs; do [ -f "$root/docs/agent/$r" ] || broken="$broken $r;"; done
  [ -n "$broken" ] && fail "STATE.md references task record(s) that do not exist:$broken"
  orphan="" malformed=""
  if [ -d "$tasks" ]; then
    for f in "$tasks"/*.md; do
      [ -f "$f" ] || continue
      name="$(basename "$f")"
      [ "$name" = "README.md" ] && continue
      printf '%s\n' "$refs" | grep -q -x -F "tasks/$name" || orphan="$orphan $name;"
      for lab in Owner Status Branch; do
        v="$(grep -m1 -F "**$lab:**" "$f" | sed -e "s/.*\*\*$lab:\*\*[[:space:]]*//")"
        [ -n "$v" ] || malformed="$malformed $name:$lab;"
      done
    done
  fi
  [ -n "$orphan" ] && fail "task record(s) not referenced from STATE.md (the card's owner must list every parallel record):$orphan"
  [ -n "$malformed" ] && fail "task record(s) missing a required label:$malformed"
  [ -z "$broken$orphan$malformed" ] && ok "task-record references consistent ($(printf '%s' "$refs" | grep -c . ) referenced)"

  # --- evidence links in the card (warn only: a moved file is worth a look, not a red gate)
  gone=""
  for p in $(card | grep -o -E '`(docs|backend|frontend|infra|playbooks|\.claude)/[A-Za-z0-9._/-]+`' | tr -d '`' | sort -u); do
    [ -e "$root/$p" ] || gone="$gone $p;"
  done
  [ -n "$gone" ] && warn "card cites path(s) that do not exist:$gone"
fi

# --- start-up isolation: the archive / standing context must never be pulled in at session start
if grep -q -E '@(\./)?docs/agent/(archive|STANDING-CONTEXT)' "$root/CLAUDE.md" 2>/dev/null; then
  fail "CLAUDE.md @-imports the archive or STANDING-CONTEXT.md - that loads it into EVERY session"
elif grep -v '^[[:space:]]*#' "$hook" 2>/dev/null | grep -q -E 'archive/|STANDING-CONTEXT'; then
  fail ".claude/hooks/session-state.sh reads the archive or STANDING-CONTEXT.md - start-up must load the card only"
else
  ok "start-up path (CLAUDE.md imports + SessionStart hook) does not load the archive or standing context"
fi
sc="$root/docs/agent/STANDING-CONTEXT.md"
if [ -f "$sc" ]; then
  sb="$(wc -c <"$sc" | tr -d ' ')"
  [ "$sb" -gt "$STANDING_WARN_BYTES" ] && warn "STANDING-CONTEXT.md is $sb B (> $STANDING_WARN_BYTES): prune facts the specs already carry"
fi

# --- FRESHNESS: hints only
if [ "$quiet" -eq 0 ] && [ -f "$state" ]; then
  echo "FRESHNESS (heuristics - hints only, never pass/fail)"
  if [ -n "${ts:-}" ]; then
    then_s="$(date -u -d "$ts" +%s 2>/dev/null)" && now_s="$(date -u +%s)" &&
      hint "card last updated $(((now_s - then_s) / 86400)) day(s) ago ($ts) - a timestamp is whatever the last writer typed"
  fi
  if git -C "$root" rev-parse --git-dir >/dev/null 2>&1; then
    last="$(git -C "$root" log -1 --format=%H -- docs/agent/STATE.md 2>/dev/null)"
    if [ -n "$last" ]; then
      hint "$(git -C "$root" rev-list --count "$last..HEAD" 2>/dev/null) commit(s) since STATE.md was last committed (code-only commits after a checkpoint are normal)"
    fi
    [ -n "$(git -C "$root" status --porcelain -- docs/agent/STATE.md 2>/dev/null)" ] && hint "STATE.md has uncommitted edits"
    cur="$(git -C "$root" branch --show-current 2>/dev/null)"
    cbr="$(field_value 'Branch' | sed -e 's/`//g' | awk 'NR==1{print $1}' | sed -e 's/[,;.]*$//')"
    if [ -n "$cur" ] && [ -n "$cbr" ] && [ "$cur" != "$cbr" ]; then hint "card was written for branch '$cbr', checkout is on '$cur'"; fi
  fi
  wu="$(field_value 'Active task' | grep -o -E 'WU-[0-9]+[A-Za-z]?' | head -n 1)"
  if [ -n "$wu" ] && [ -f "$root/docs/agent/BACKLOG.md" ]; then
    st="$(awk -v h="## $wu " 'index($0, h) == 1 {on = 1; next} on && /^\*\*Status:\*\*/ {print; exit} on && /^## / {exit}' "$root/docs/agent/BACKLOG.md")"
    case "$st" in
      *DONE* | *done*) hint "BACKLOG marks the card's active task $wu as done ($st) - the card may be behind" ;;
      "") hint "active task $wu has no '## $wu' entry with a Status line in BACKLOG.md" ;;
      *) hint "BACKLOG status of active task $wu: $st" ;;
    esac
  fi
fi

if [ "$fails" -gt 0 ]; then
  echo "RESULT: STRUCTURE FAILED ($fails failure(s), $warns warning(s)). Fix the state file; this check says nothing about whether its content is current."
  exit 1
fi
echo "RESULT: structure OK (0 failures, $warns warning(s)). Well-formed is not the same as true or current - only a review against the tree, git log and JOURNAL establishes that."
exit 0
