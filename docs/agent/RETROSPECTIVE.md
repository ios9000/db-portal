# Experiment retrospective — AI-agent-driven development of DB Portal

WU-047, the last work unit. This evaluates the STRATEGY.md experiment: building the
DB Portal MVP with an AI agent (Fable / Opus in Claude Code) as the primary developer,
without losing state to context limits, compaction, or session boundaries. Every claim
below is grounded in `JOURNAL.md` (31 sessions, s01–s31), the gate reviews under
`docs/agent/reviews/`, and the memory files. It fills STRATEGY.md §8.

## Headline

The MVP shipped. M0–M3 closed with milestone gates; M4 (Hardening) is complete through
WU-046 with this retrospective as the final WU. **~47 `done` journal entries across 31
sessions (WU closes WU-000→WU-047 plus the M0–M3 milestone and gate closes), and the
repo-as-memory design held across at least three process deaths with zero lost work.**
The single most important result: *session death became a non-event.* The same STATE-first
ritual recovers from a killed terminal, a compaction, and a cold start — so the failure
modes that usually derail a multi-week agent build were absorbed, not fatal.

## §8 metrics

### 1. WU throughput & rework rate
- **Throughput:** ~47 `done` entries across 31 sessions. Early sessions bundled several
  S-WUs (s01: WU-000/001/001R/002; s02: 003/004/005; s04: 010/011/012; s10: 011R/018/019/020),
  because S-WUs on a fresh chassis are cheap. As WUs got harder (auth, chains, real engine,
  concurrency), sessions settled to **one WU each** — the sizing rule (§3) self-corrected
  without a policy change.
- **Rework rate:** **2 literal reopens** in the whole build — `WU-001R` (s01) and `WU-011R`
  (s10, a "Last backup: —" gap where WU-011 built the placeholder and WU-012 built the data
  source but nothing connected them). Both were genuine gaps filed retroactively, not
  regressions. Escaped defects were handled as *new fix WUs* (below), not reopened WUs — a
  deliberate model that keeps "done" meaning done.

### 2. Cold-start time (target ≤ 5 min to first productive edit)
- **Met, and measured.** s02: user-timed **3m42s** from STATE open to first productive edit
  (the journal's own "~4 min" estimate, tightened by the user). s07: "STATE-first protocol
  sufficed from a bare 'continue' — no summary carryover, no missteps." s03 was an
  *emergency handoff* (the user's console died mid-s02 close) and "STATE.md cold-start worked
  as designed." WU-045 (s31) re-proved the *build* cold-start end-to-end (fresh clone → install
  → migrate → check → build:release → run, all exit 0).
- The **context brief** (§3) is what bought this: s09 — "smallest WU yet, no surprises — the
  gate record's context brief was exact." A precise brief converts "read the repo to orient"
  into a bounded read.

### 3. Escaped defects per milestone (found after a WU's verification passed)
- **M1 gate (s08):** 24 raw findings → **~17 confirmed** (2 HIGH, 6 MEDIUM, 9 LOW) → fix WUs
  016/017/018/019 (+ body-cap→021, CI-pin & LocateDotenv→icebox→WU-045).
- **M2 gate (s12):** **11 findings, 11 confirmed** → WU-024/025.
- **M3 gate (s23):** **8 findings, 8 confirmed, 0 criticals** → WU-037/038/039/040.
- **The trend is the finding: 17 → 11 → 8, declining each milestone.** The process learned.
  And critically, across all three gates **no guardrail invariant ever escaped** — every gate
  re-confirmed no restore-without-safety-dump, no bare restore over the API/scheduler, no
  forged webhook outcome, no secret reachable, audit append-only. The escaped defects were
  lifecycle/robustness edges (transient-error handling, idempotency, a re-fetch footgun), not
  safety holes. That is the right failure distribution for a guardrail-centric product.

### 4. How often the golden flow caught a regression
- The golden flow (`backend/e2e/golden_flow_test.go`, ADR-011) was **extended at every
  milestone** — authenticates for real from s10 (beat 0 asserts 401), prod-ritual beat at
  s11, live scheduler fire at beat 8 (s12), the full restore chain at M3 — and **stayed green
  in every session as the e2e canary.**
- **It never went red in-session — which is the success signal, not a gap.** The per-WU
  discipline (code+tests together, gate before commit) meant regressions were caught *before*
  reaching the canary. The broader suite did catch real bugs during development: WU-016's new
  concurrency test found a **bonus resurrection bug** beyond the gate finding (a competing
  finalizer's run was resurrected by the next poll); WU-023's fresh-DB migrate walk caught a
  **spec drift** (a draft invented migration 0008 for a column 0003 already provisioned). The
  golden flow's value was **preventative**: it forced full production wiring to stay working
  across ~47 WUs, and each fix WU added a beat so a *future* silent break would be caught.
- Gap it exposed: the golden flow (and all DB tests) **skipped in CI** from M1 until WU-045
  (s31) added a Postgres service — for most of the build the canary ran only on the VM gate,
  not in CI. See "What to change."

## Recurring failure modes & the mitigations that worked

The dominant risk was never a bug — it was **losing a session's uncommitted work**.

- **SSH reset killing pre-checkpoint work (7 JOURNAL mentions).** The sharpest evidence:
  **s21 left no JOURNAL entry at all** — an ssh reset killed it before it could checkpoint,
  and s22 recovered its committed-but-unrecorded work (that is why the session count is 30
  journalled across s01–s31). Same pattern s23→s24 (the M3 gate). *Mitigation that worked:*
  run `claude` inside **tmux** so the agent survives the ssh drop ([[accidental-rejections]]),
  and **recover-don't-redo** — survey the tree + `git log`, re-run the gate, adopt what's
  there rather than blindly re-doing it.
- **Twin-session hazard (14 mentions: s16, s20, s24).** After an ssh reset the "dead" agent
  sometimes **survived headless on the old pty and kept working concurrently** with the new
  session — twice running the same close-out. *Mitigation:* `ps`/`who`/tty checks **before**
  heavy writes, verify-don't-redo, and never kill a live-pts process without the user's say-so
  ([[twin-session-hazard]]). This one is discipline-only and recurred three times — it wants a
  harness-level guard (below).
- **Compaction: only 2 mentions in 31 sessions.** The checkpoint-before-compaction trigger
  (§5) worked — repeated "Compactions: 0" in the session-end metas. When context got heavy the
  agent checkpointed rather than letting the summary eat detail.

Unifying insight: **repo-as-memory + STATE-first recovery made all three the same, cheap
ritual.** The design's acceptance test ("a fresh agent given only the repo continues in 5
minutes") was met repeatedly under real adversity, not just in theory.

## Architect / implementer delegation

The single-writer rule (§6) held, with a productive split that **emerged at WU-019 and should
have been the default from WU-001**:

- **Six UI delegations to a Sonnet subagent, all first-pass-green:** WU-019; WU-020b
  (126,662 tok / 61 calls / ~10 min, 73/73); WU-021b (97,920 / 65 / ~5.5 min, 3/3); WU-022b
  (114,335 / 42 / ~6.5 min, 4/4); WU-031-UI (124,388 / 42 / ~8.5 min); WU-032-UI. Agents even
  fixed real bugs beyond brief (a genuine `App.test` timing flake; an `errorDetail()` envelope
  that leaked raw JSON on 403). Cost ~**100–126k tokens / ~5–10 min** each — cheap for a full
  tested UI slice.
- **The architect (Fable/Opus) kept every security/concurrency slice**: authn, authz, the
  chain engine, instance locks, break-glass, config validation. None delegated. This is where
  the invariants live, and the declining escaped-defect trend suggests it was the right line.
- **S-sized / no-UI WUs were architect-implemented directly** (023, 030, 041, 044, 046) — a
  cost-sensitivity call: delegation overhead exceeds the diff ([[workflow-cost-sensitivity]]).
- **Multi-agent gates are workflow-scale and opt-in** (M3 gate: 605k tok / 205 calls / ~19 min,
  5 reviewers). When a gate fan-out was interrupted (M1, s08), the journal was **harvested**
  rather than re-run — recovering ~75% of the value at ~zero extra agent cost.

## What the harness rules bought (rule-by-rule verdict)

- **Repo-as-memory (§2): load-bearing.** Survived 3+ process deaths with zero lost work. The
  one non-negotiable rule.
- **STATE.md single resume point + context brief (§2–3): the cold-start engine.** 3m42s,
  emergency handoffs, bare-"continue" resumes.
- **One WU per session (§3): held once WUs hardened.** Early S-WU bundling was fine, not a
  violation.
- **Verify-don't-claim + the gate (§4, §7): caught real defects and kept "done" honest** —
  every done entry carries pasted command output.
- **Just-in-time specs (§3): specs didn't rot** — they were written the session before their WU.
- **Milestone gates (§7): the declining 17→11→8 escaped-defect trend is the proof they work.**

## What to change for the next experiment

1. **CI parity from M0, not M4.** DB tests + the golden flow skipped in CI from M1 to WU-045.
   The canary should run *everywhere* from the day it exists — stand up the CI DB service in
   the walking-skeleton WU.
2. **Give the twin-session hazard a harness-level guard.** It recurred 3× on discipline alone.
   A repo PID/lockfile the agent checks on start (and a stale-lock reaper) would make it
   structural instead of a checklist item.
3. **Make the architect/implementer split the default for UI from WU-001.** It was discovered
   at WU-019 after the architect hand-wrote early UI; six-for-six first-pass-green says start
   there.
4. **Icebox debt needs a target milestone, not open-ended.** LocateDotenv (foreign-`.env`
   footgun) and the CI installer pin sat in the icebox from the M1 gate (items 14/16) until
   WU-045 — most of the build. File discovered debt against a milestone so it can't drift.
5. **Checkpoint before every long workflow run.** Both the M1 and M3 gate fan-outs were
   interrupted by ssh resets mid-run; a checkpoint immediately before launching the workflow
   makes harvest-and-resume trivial.

## Bottom line

The experiment worked. An AI agent built a guardrail-centric, tested, documented MVP across a
month of sessions, and the repo-as-memory machinery turned the expected killers (context
limits, compaction, terminal death) into a routine recover-from-STATE step. The residual
friction is real but bounded and named above — none of it is a reason not to run this harness
again, and most of it is a one-time fix (CI parity, a twin lockfile) rather than a standing tax.
