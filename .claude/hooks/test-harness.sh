#!/usr/bin/env bash
# Self-test for the state harness: session-state.sh (SessionStart hook) + state-validate.sh.
# Every case runs in a throwaway git repo under a temp dir - NO tracked file is touched.
# Runs from `npm run check:state` (so: `npm run check` and CI). ~1s.
#
#   bash .claude/hooks/test-harness.sh        # all cases
#   TMPDIR=/some/dir bash .../test-harness.sh # choose where fixtures are built

set -u
export LC_ALL=C
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK="$here/session-state.sh"
VALIDATE="$here/state-validate.sh"

T="$(mktemp -d "${TMPDIR:-/tmp}/state-harness.XXXXXX")" || exit 2
cleanup() { case "$T" in */state-harness.*) [ -d "$T" ] && rm -r "$T" ;; esac; }
trap cleanup EXIT

pass=0 failed=0
check() { # check <name> <condition exit code>
  if [ "$2" -eq 0 ]; then pass=$((pass + 1)); echo "  ok    $1"; else failed=$((failed + 1)); echo "  FAIL  $1"; fi
}
has() { grep -q -F -- "$2" "$1"; }
hasnt() { ! grep -q -F -- "$2" "$1"; }

valid_card() { # valid_card [branch] [timestamp]
  cat <<EOF
<!-- STATE-CARD:BEGIN -->
## Working state (card)

- **Last updated:** ${2:-2026-01-02T03:04Z} (s1) · **Summary owner:** main session
- **Goal:** fixture goal
- **Completion criteria:** fixture criteria
- **Active task:** **WU-900** — fixture task · owner: main
- **Branch:** ${1:-main}
- **Decisions (why → where):**
  - fixture decision → because
- **Done (claimed):** nothing
- **Verified (command → result → evidence):**
  - none yet
- **Blockers:** none
- **Next step:** fixture next step
- **Unknown:** UNKNOWN
- **Parallel task records:** none
<!-- STATE-CARD:END -->
EOF
}

mkrepo() { # mkrepo <name> -> path of an initialised repo carrying the harness + a valid state
  local d="$T/$1"
  mkdir -p "$d/docs/agent/tasks" "$d/docs/agent/archive" "$d/.claude/hooks" "$d/backend"
  cp "$HOOK" "$VALIDATE" "$d/.claude/hooks/"
  { echo "# STATE"; echo; valid_card; echo; echo "## Now"; echo "- fixture"; } >"$d/docs/agent/STATE.md"
  echo "# tasks" >"$d/docs/agent/tasks/README.md"
  echo "ARCHIVE-SENTINEL-7f3a old history" >"$d/docs/agent/archive/STATE-history.md"
  echo "STANDING-SENTINEL-9c1d stable facts" >"$d/docs/agent/STANDING-CONTEXT.md"
  echo "# project rules" >"$d/CLAUDE.md"
  git -C "$d" init -q -b main
  git -C "$d" config user.email harness@test
  git -C "$d" config user.name harness
  git -C "$d" add -A
  git -C "$d" commit -q -m fixture
  echo "$d"
}
run_hook() { # run_hook <repo> <source> [cwd] -> output file path
  local out="$T/out.$RANDOM.txt"
  echo "{\"hook_event_name\":\"SessionStart\",\"source\":\"$2\",\"cwd\":\"${3:-$1}\"}" | env -u CLAUDE_PROJECT_DIR bash "$1/.claude/hooks/session-state.sh" >"$out" 2>&1
  echo "$?" >"$out.rc"
  echo "$out"
}
run_validate() { # run_validate <repo> -> output file path
  local out="$T/val.$RANDOM.txt"
  bash "$1/.claude/hooks/state-validate.sh" --root "$1" >"$out" 2>&1
  echo "$?" >"$out.rc"
  echo "$out"
}
rc() { cat "$1.rc"; }

echo "SessionStart hook"
d="$(mkrepo base)"
for src in startup resume clear compact; do
  o="$(run_hook "$d" "$src")"
  [ "$(rc "$o")" -eq 0 ] && has "$o" "source=$src" && has "$o" "WU-900" && has "$o" "The user's current request always wins"
  check "source=$src: exit 0, card injected, user-request-wins rule present" $?
done
o="$(run_hook "$d" clear)"; has "$o" "treat the next message as a NEW request"; check "clear: saved task is background only" $?
o="$(run_hook "$d" compact)"; has "$o" "Context was just compacted"; check "compact: re-anchor line" $?
o="$(run_hook "$d" startup)"; hasnt "$o" "ARCHIVE-SENTINEL" && hasnt "$o" "STANDING-SENTINEL"; check "start-up output contains neither the archive nor the standing context" $?
[ "$(wc -c <"$o")" -le 8000 ]; check "output within the 8000-byte hard cap" $?
o="$(run_hook "$d" startup "$d/backend")"; has "$o" "Project: base ("; check "launched from a subdirectory: resolves the project root" $?

d="$(mkrepo missing)"; git -C "$d" rm -q docs/agent/STATE.md; git -C "$d" commit -q -m gone
o="$(run_hook "$d" startup)"; [ "$(rc "$o")" -eq 0 ] && has "$o" "STATE FILE MISSING" && has "$o" "UNKNOWN"
check "missing state file: reported explicitly, state UNKNOWN, exit 0" $?

d="$(mkrepo branch)"
printf -- '- **Owner:** worker A\n- **Status:** in progress\n- **Branch:** wip/x\n' >"$d/docs/agent/tasks/WU-901.md"
git -C "$d" add -A; git -C "$d" commit -q -m rec; git -C "$d" checkout -q -b wip/x
o="$(run_hook "$d" resume)"; has "$o" "BRANCH MISMATCH" && has "$o" "WU-901.md" && has "$o" "<= THIS BRANCH"
check "branch mismatch flagged + this branch's task record highlighted" $?
echo x >"$d/backend/x"; git -C "$d" add -A; git -C "$d" commit -q -m code
o="$(run_hook "$d" startup)"; has "$o" "HEURISTIC: 2 commit(s) landed after" && has "$o" "not proof either way"
check "lagging-card note is worded as a heuristic" $?

d="$(mkrepo big)"
{ echo '<!-- STATE-CARD:BEGIN -->'; echo '- **Branch:** main'; i=0; while [ "$i" -lt 250 ]; do echo "- padding line $i padding padding padding padding padding padding padding"; i=$((i + 1)); done; echo '<!-- STATE-CARD:END -->'; } >"$d/docs/agent/STATE.md"
o="$(run_hook "$d" startup)"; has "$o" "CARD TRUNCATED" && has "$o" "fails STRUCTURAL validation" && [ "$(wc -c <"$o")" -le 8000 ]
check "oversize card: truncated at budget, flagged, structural warning shown" $?

d="$(mkrepo nocard)"; printf '# STATE\nno card here\n' >"$d/docs/agent/STATE.md"
o="$(run_hook "$d" startup)"; has "$o" "has no STATE-CARD block"; check "no card block: falls back to the first lines with a warning" $?

echo "State validator"
d="$(mkrepo v-ok)"; o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 0 ] && has "$o" "RESULT: structure OK"; check "valid state passes" $?

d="$(mkrepo v-stale)"; { echo "# STATE"; valid_card main 2020-01-01T00:00Z; } >"$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 0 ] && has "$o" "FRESHNESS (heuristics" && has "$o" "day(s) ago (2020-01-01T00:00Z)"
check "well-formed but 6-year-old card: structure PASSES, age only hinted (structure != currency)" $?

d="$(mkrepo v-big)"; i=0; while [ "$i" -lt 400 ]; do echo "- accreted history line $i ....................................."; i=$((i + 1)); done >>"$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "history is accreting"; check "oversized STATE.md fails" $?

d="$(mkrepo v-dup)"; valid_card >>"$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "expected exactly one state card"; check "duplicate summary block fails" $?

d="$(mkrepo v-field)"; grep -v -F '**Next step:**' "$d/docs/agent/STATE.md" >"$d/s" && mv "$d/s" "$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "missing required field(s): Next step;"; check "missing required field fails and is named" $?

d="$(mkrepo v-empty)"; sed -e 's/^- \*\*Blockers:\*\* none$/- **Blockers:**/' "$d/docs/agent/STATE.md" >"$d/s" && mv "$d/s" "$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "are empty: Blockers;"; check "empty required field fails" $?

d="$(mkrepo v-ts)"; sed -e 's/2026-01-02T03:04Z/yesterday/' "$d/docs/agent/STATE.md" >"$d/s" && mv "$d/s" "$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "has no UTC timestamp"; check "non-timestamp 'Last updated' fails" $?

d="$(mkrepo v-ref)"; sed -e 's#^- \*\*Parallel task records:\*\* none#- **Parallel task records:** `docs/agent/tasks/WU-999.md`#' "$d/docs/agent/STATE.md" >"$d/s" && mv "$d/s" "$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "do not exist: tasks/WU-999.md;"; check "broken task-record reference fails" $?
printf -- '- **Owner:** w\n- **Status:** in progress\n- **Branch:** main\n' >"$d/docs/agent/tasks/WU-999.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 0 ]; check "...and passes once the referenced record exists" $?

d="$(mkrepo v-orphan)"; printf -- '- **Owner:** w\n- **Status:** in progress\n' >"$d/docs/agent/tasks/WU-902.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "not referenced from STATE.md" && has "$o" "WU-902.md:Branch;"
check "orphan task record + record missing a label both fail" $?

d="$(mkrepo v-import)"; echo '@docs/agent/archive/STATE-history.md' >>"$d/CLAUDE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "loads it into EVERY session"; check "CLAUDE.md importing the archive fails (start-up isolation)" $?

d="$(mkrepo v-missing)"; rm "$d/docs/agent/STATE.md"
o="$(run_validate "$d")"; [ "$(rc "$o")" -eq 1 ] && has "$o" "does not exist"; check "missing STATE.md fails" $?

echo "Delegation routing config (static: what the runtime will enforce)"
repo="$(cd "$here/../.." && pwd)"
fm() { awk -v k="$2" 'NR==1 && $0=="---"{on=1;next} on && $0=="---"{exit} on && index($0, k ":")==1 {sub(/^[^:]*:[ \t]*/, ""); print; exit}' "$repo/.claude/agents/$1.md" 2>/dev/null; }
for spec in scout:haiku verifier:haiku analyst:sonnet; do
  a="${spec%%:*}"; want="${spec##*:}"
  [ "$(fm "$a" name)" = "$a" ] && [ "$(fm "$a" model)" = "$want" ] && [ -n "$(fm "$a" description)" ] && [ -n "$(fm "$a" maxTurns)" ]
  check "agent '$a': model pinned to $want, maxTurns bounded, name/description present" $?
  case ",$(fm "$a" tools | tr -d ' ')," in *,Edit,* | *,Write,* | *,NotebookEdit,* | ,,) false ;; *) true ;; esac
  check "agent '$a': explicit tool list without Edit/Write (workers cannot edit shared state with file tools)" $?
done
case ",$(fm scout tools | tr -d ' ')," in *,Bash,*) false ;; *) true ;; esac; check "agent 'scout' is read-only (no Bash)" $?
if command -v node >/dev/null 2>&1; then
  node "$here/research-sweep.dryrun.js" >"$T/dryrun.txt" 2>&1; drc=$?
  sed -e 's/^/  /' "$T/dryrun.txt" | grep -E 'FAIL|dry-run:' || true
  check "research-sweep workflow bounds hold in a dry-run (fake agent, no model calls)" $drc
else
  echo "  skip  node not found - research-sweep dry-run NOT executed (coverage gap, not a pass)"
fi

echo "RESULT: $pass passed, $failed failed"
[ "$failed" -eq 0 ]
