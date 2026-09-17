# STATE — read me first

> Rewritten in full at every checkpoint. This file is the single resume point.
> If this file and the git tree disagree, the tree wins — then fix this file.
> The **state card** below is the head of this file: a SessionStart hook injects it on
> startup / resume / clear / compaction. On conflict: git tree > card > the sections under it.
> Card rules (when to rewrite, done vs verified, single owner): CLAUDE.md "State card".
> CURRENT STATE ONLY. Nothing accretes here: a finished WU lives in JOURNAL + its BACKLOG
> done-entry. Stable facts → `STANDING-CONTEXT.md`; old session write-ups →
> `archive/STATE-history.md`. Neither is start-up reading. `npm run check:state` enforces the
> structure + size of this file (it cannot tell whether the content is current — you can).

<!-- STATE-CARD:BEGIN · injected by .claude/hooks/session-state.sh · budget 5000 bytes (cut at a line boundary beyond that) · single writer = the summary owner · rewrite the whole block, never append -->
## Working state (card)

- **Last updated:** 2026-09-17T09:40Z (s40) · **Summary owner:** architect = the main interactive session (sole writer of this card)
- **Goal:** M5 — the portal runs `ansible-playbook` itself (`engine/local`, os/exec) behind the unchanged Adapter seam; manifest-driven playbook library; Semaphore to zero. Why: ADR-014 (`docs/DECISIONS.md`) · spec: SPEC-050 (`docs/specs/local-engine.md`).
- **Completion criteria:** M5 exit per `docs/ROADMAP.md` §M5 — WU-036 restore rehearsal green on `engine=local` with Semaphore deleted from the tree; drop-a-playbook drill (new op, no Go change); `m5-gate-review` passes. Active WU: every AC in BACKLOG WU-052 + `npm run check` green + CI green.
- **Active task:** **WU-052** — extra_vars contract + exec hardening (SPEC-050 mini-ADR 6) · status: NOT STARTED (BACKLOG: TODO) · owner: architect (not delegated)
- **Branch:** main (in sync with origin/main at f3861d8 when written)
- **Decisions (why → where):**
  - Local engine replaces Semaphore: the portal already owns the value layer, Semaphore only supervises processes + holds keys → ADR-014.
  - Params reach a playbook ONLY via `--extra-vars @file` (0600), never argv; the `dbportal_*` namespace is reserved for engine-injected vars → SPEC-050 mini-ADR 6.
  - Inventory hostvar `dbportal_env` stays distinct from extra-var `dbportal_environment`; playbooks read `dbportal_port` from the INVENTORY → SPEC-050 mini-ADR 5 delivery notes.
  - No Semaphore code is removed before the WU-055 parity drill passes on `local` → BACKLOG Phase 5 header.
  - MockEngine stays the dev + test default; CI needs no Ansible (stub binary) → ADR-002, SPEC-050.
  - s40 (meta) agent harness: state card + SessionStart hook (startup|resume|clear|compact); STATE.md = current state only, stable facts → `STANDING-CONTEXT.md`, history → `archive/STATE-history.md`; `npm run check:state` checks STRUCTURE, not currency; cheap-model routing `scout`/`verifier` (haiku) → `analyst` (sonnet) + bounded 3/3/2 loop → CLAUDE.md "Delegation & model routing", `docs/agent/HARNESS.md`.
- **Done (claimed — BACKLOG/JOURNAL):** M0–M4 exited; M5 groomed (s37, e648457); WU-050 local adapter core (28dd420); WU-051 connection tuple + per-job inventory, migration 0013 (2b2e483). Remaining: WU-052 → 053 → 054 (UI, delegation candidate) → 055 → 056 → 057.
- **Verified (command → result → evidence):**
  - WU-051: `npm run check` exit 0 + VM smoke on `local` → JOURNAL s39 line; CI run 31992600859 green on c48ccc3.
  - WU-050: `npm run check` exit 0 + VM smoke → JOURNAL s38 line; CI run 31988998973 green on aee3c94.
  - HEAD f3861d8: CI run 31992759517 green (`gh run list`, checked 2026-09-17).
  - 2026-09-17 `npm run check` → CHECK-EXIT:0: golangci 0 issues + vitest 116/116 ran FRESH; all Go test packages reported `(cached)` = NOT re-executed (Go code unchanged since s39). Whether the cached run had the DB tests un-skipped: UNKNOWN.
  - s40 harness: `npm run check:state` → validator OK + self-test 34/34 (fixtures + workflow dry-run); LIVE headless: all 4 SessionStart sources fired, answers matched `state-validate.sh --expect`; ACTUAL models from transcripts — scout/verifier = claude-haiku-4-5, analyst = claude-sonnet-5, per-call override wins, `research-sweep` workers = haiku (3/3 questions verified, found 1 stale ref, fixed) → JOURNAL s40 lines; `docs/agent/HARNESS.md` §3, §7.
- **Blockers:** none for WU-052. Standing, non-blocking: the security-vetting package (ARCHITECTURE §8.2) awaits the user.
- **Next step:** start WU-052 — read BACKLOG WU-052 + SPEC-050 mini-ADR 6, then ONLY `backend/internal/engine/local.go` (+ `local_test.go` stub vocabulary), `engine/semaphore.go` (forwardVars), `runs/service.go` (paramsDigest). Detail: "Next action" below; WU-050/051 write-ups: `docs/agent/archive/STATE-history.md` §A.
- **Unknown:** liveness of the demo (:8080) and the compose dev stack (not probed since s39); anything done on the VM outside git between 2026-08-16 and 2026-09-17. Two untracked July files sit at the repo root (`auto_proof.txt`, `settings.local.json` = the WDFabric permissions template, already ported in s24) — the user's to keep or delete.
- **Parallel task records:** none (`docs/agent/tasks/`, see its README).
<!-- STATE-CARD:END -->

## Now

- **RESUME HERE (fresh session — this is the whole resume context; do NOT reconstruct from
  any prior conversation).** Active WU = **WU-052** (extra_vars contract + exec hardening
  — SPEC-050 mini-ADR 6). Read, in order: (1) this "Now" block + "Next action" below;
  (2) the **WU-052** entry in `docs/agent/BACKLOG.md` and ONLY the files its context brief
  lists; (3) SPEC-050 mini-ADR 6 (`docs/specs/local-engine.md`) + **ADR-014** in
  DECISIONS.md (the pivot's why). NOT start-up reading: stable per-subsystem facts + dev-stack heads-ups =
  `docs/agent/STANDING-CONTEXT.md`; older session write-ups + "if revisiting" pointers
  (incl. the WU-050/051 detail) = `docs/agent/archive/STATE-history.md` — `grep` them for one
  specific fact when the WU needs it.
- **Active: PHASE 5 (M5 — Local Ansible engine & playbook platform), groomed s37;
  WU-050 DONE (s38) + WU-051 DONE (s39) — 052..057 remain.** THE PIVOT (user directive 2026-08-12, ADR-014): target user broadens to
  **Ansible playbook developers**; the portal executes playbooks ITSELF via os/exec
  (`ansible-playbook` on the host) behind the UNCHANGED Adapter seam; **Semaphore goes to
  zero** (decommission WU-056, strictly AFTER the WU-055 parity drill); the catalog becomes
  a manifest-driven playbook library (the delivery-platform half). Groomed **WU-050 → 051 →
  052 → 053 → 054 → 055 → 056 → 057** (order rationale in the BACKLOG phase header;
  WU-054 UI = Sonnet delegation candidate; exec/inventory/manifest-loader stay architect).
  MockEngine stays the dev + test default forever (ADR-002 reaffirmed); CI needs no
  Ansible (stub-binary strategy, SPEC-050). ROADMAP has the M5 section + exit criteria
  (rehearsal on `local`, drop-a-playbook drill, gate review). M0–M4 remain exited; the MVP
  demo (:8080) and compose dev stack still run. Organizational: security-vetting package
  (ARCHITECTURE §8.2) still awaits the user; NOTE the pivot will amend its engine sections
  (WU-057 reconciles docs — until then ADR-014 wins conflicts, ADR-006 precedence).
- **Where:** PRIMARY = VM #2 `dbportal-vm` (root@80.209.240.36, host "206610",
  8 vCPU / 31 GB / 387 GB, Ubuntu 24.04.4), repo `/root/db-portal`, bootstrapped via
  `infra/bootstrap-vm.sh` on 2026-07-06. Workstation `P:\Projects\db-portal` = docs-only
  secondary. VM #1 (80.85.254.99): decommissioned, deploy key revoked.
- **Branch:** main

## Next action (be exact)

1. **START WU-052 — extra_vars contract + exec hardening (SPEC-050 mini-ADR 6).**
   Full brief + ACs in BACKLOG (WU-052). Core: the reserved engine-injected `dbportal_*`
   extra-vars namespace (instance/environment/operation + artifact refs — porting the
   semaphore forwardVars allowlist to `dbportal_artifact_name`/`dbportal_checksum`);
   client params rejected on the reserved prefix BEFORE any row; scrubbed child env
   (allowlist PATH/HOME/LANG/ANSIBLE_* + configured extras — `PORTAL_DB_PASSWORD` must
   never reach a playbook; the stub can echo env/argv for assertion); extravars 0600 +
   argv assertion (no param value ever in argv); log caps end-to-end; workdir removal on
   every terminal path incl. cancel/timeout. `params_digest` auditing unchanged. NOTE
   from WU-051: inventory hostvars already use `dbportal_env` — keep it distinct from
   the extra-vars `dbportal_environment` (SPEC-050 mini-ADR 5 delivery notes), and any
   playbook port (WU-055) reads `dbportal_port` from the INVENTORY, not extra vars.
   Context brief: SPEC-050 mini-ADR 6; `engine/local.go` (+local_test stub vocabulary);
   `engine/semaphore.go` (forwardVars); `runs/service.go` (paramsDigest + params build).
   Then 053 (manifests) → 054 (UI, delegate) → 055 (parity drill — GATES 056) → 056
   (Semaphore removal) → 057 (docs sweep + m5 gate). Do NOT remove any Semaphore code
   before WU-055's rehearsal passes on `local`. NOTE for 055: ansible-core 2.16.3 is on
   the VM (apt, s38); the WU-050/051 smoke recipe (stub tests + isolated :8095 drill +
   exact-PID teardown) is in each BACKLOG done-entry.

## Blocked / needs user

- Nothing.
- Standing, non-blocking: the security-vetting package (ARCHITECTURE §8.2) awaits the user; the
  pivot amends its engine sections (WU-057 reconciles — until then ADR-014 wins, ADR-006 precedence).
