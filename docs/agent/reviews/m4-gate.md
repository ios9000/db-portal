# M4-gate review — findings record (2026-08-10, s32)

Method: the same light shape as M1/M2/M3 (the cost-sensitivity agreement).
Workflow `wf_d38775e1-d1f` (`.claude/workflows/m4-gate-review.js`) ran **5 independent
Sonnet 5 reviewers**, one per dimension (concurrency-locks, retention-safety,
deploy-hardening, ci-supply-chain-config, seed-loadtest-specs), each briefed with the
SPEC-041…046 summaries, the WU-041..047 shipped surface, the M4 diff range
(`24008da..HEAD`), the eleven ground rules that must hold now, and the deliberate-deferral
list. **No verifier agents** — every finding below was verified inline by the architect
against the cited code (and, for the systemd/shell findings, reproduced empirically on the
VM) before it entered this record. Cost: 519,248 subagent tokens / 189 tool calls /
~51.7 min. (The first launch died with "Login expired" before any reviewer did work — 0
tokens; the record above is the clean re-run.)

Result: **4 findings raised, 4 confirmed, 0 refuted, 1 re-graded DOWN** in severity during
inline verification. **One CRITICAL.** Verdict: **GATE DOES NOT PASS CLEAN.** Unlike M1/M2/M3
(no criticals → "passes with fix WUs"), this gate found a real guardrail bypass on a live
target. **M4 EXIT / MVP-COMPLETE is BLOCKED until WU-048 (the critical) lands**; the two
deploy-packaging HIGHs (WU-049) block a real pilot deploy and must land before the packaging
is called pilot-ready. Two dimensions — retention-safety and seed-loadtest-specs — came back
completely clean.

## What held

Most guardrail invariants survived. No reviewer found: two concurrent live ops on one
instance (the WU-042 instance lock — acquire-in-Start's-tx / release-in-finalize's-tx /
never-steal-a-live-holder — held under every interleaving examined, and the WU-043 load
harness genuinely proves it); a `safety` artifact reaped (the reap's `WHERE
retention_class='standard'` is structurally exclusive) or the artifact sweep reaping
everything on a non-positive age (the fail-safe guard holds); the maintenance audit pass
mutating an append-only ledger (it is observational, correct by design); a session GC
deleting a live session; the break-glass alarm leaking a password/token or blocking the login
path (it is async, best-effort, off the hot path, and `authn` does NOT import `notify` — the
Alarmer seam is intact); `config.Validate()` letting a broken-auth door boot or wrongly
blocking migrate/import/seed; the `.env` walk adopting a stray parent `.env` (bounded at the
`.git` repo root); a secret in any infra/CI/deploy file; the seed generator emitting a row
`inventory.Import` would reject, or drifting from byte-identical determinism. The
**retention-safety** and **seed-loadtest-specs** dimensions found nothing.

## The theme: guardrails and docs that are right at the front door but not re-checked downstream

Three of the four findings share a shape: **a check that is correct where it was first
written, but not re-applied at the second entry point that M4 added or exposed.** The Patroni
block is enforced once at `chain.Create` and never again when the driver re-fires a step on
`Resume` (the critical). The `portal.env.template` values are readable by dev's godotenv but
not by systemd's stricter `EnvironmentFile=` parser (the two HIGHs — one root cause, two
surfaces: the unit boot and the documented migrate command). The dotenv path is logged as
"loaded" based on the resolved *string*, not on whether the file was actually *read* (the
LOW). None of these is reachable from the WU-046 live systemd drill or the demo happy paths —
the drill used hand-written clean env values, so it exercised the `.service` unit and the
binary-under-systemd correctly while never copying the template verbatim; that is exactly the
gap a fresh-host operator following the runbook literally would hit.

## Confirmed — CRITICAL

1. **The Patroni-restore guardrail is enforced only once at `chain.Create` and is never
   re-validated on `Resume` or at fire time — a re-platformed instance's restore step slips
   past the block and runs a naive `pg_restore` against a Patroni-managed cluster** —
   `backend/internal/chain/chain.go:218` (the sole check) · `chain.go:278-300` (Resume) ·
   `backend/internal/chain/driver.go:49-130` (drive) · `backend/internal/runs/service.go:154-195`
   (Start).
   The naive-Patroni-restore block (SPEC-042 mini-ADR 6, `ErrPatroniRestore`) is enforced
   **exactly once**, synchronously inside `chain.Create` (chain.go:218: `if platform ==
   "k8s_patroni" && hasRestoreStep(req.Steps)`), reading the target cluster's platform *at
   creation time*. `chain.Resume` (chain.go:278-300) only flips `chain.state` `halted →
   running` and re-invokes `s.drive`; it performs **no** guardrail check. `drive` / `next` /
   `loadChain` (driver.go) never read cluster platform — `loadChain` selects `i.name,
   c.confirm, c.reason` only. Each step re-fires through `runs.Service.Start` with
   `Internal:true`, and `runs.Start` has **no** Patroni awareness: I grepped every
   `k8s_patroni` reference in the backend — the only enforcement site is chain.go:218; the
   rest are the error definition (service.go:56-61), the HTTP 403 mapping
   (restore_http.go:88), and comments. So a restore step re-fired via Resume executes
   unconditionally with respect to Patroni.
   **Verified — the re-platform is reachable without any trick.** The scenario hinges on an
   instance moving `vm → k8s_patroni` between create and resume, which ordinary inventory
   re-import performs silently: `inventory.upsertInstance` (import.go:161-173) UPDATEs
   `cluster_id` in place whenever `curClusterID != clusterID`, and `resolveCluster`
   (import.go:105-124) only quarantines a *same-name, different-platform* conflict — an
   instance listed under a **new/renamed** cluster whose platform is `k8s_patroni` sails
   through as a normal update. I confirmed both legs against the code.
   **The asymmetry that makes this a real gap:** the two *other* Create-time guardrails ARE
   re-validated at fire time, because every step re-fires through `runs.Start`, which
   re-checks the **self-target ban** (service.go:183) and the **prod ritual** (service.go:193,
   via the persisted-confirm replay — mini-ADR 3, the deliberate "env promoted mid-chain fails
   visibly" design). The Patroni block was simply never given the same per-fire treatment.
   **Failure scenario:** (1) a restore chain is created for `app-01` while its cluster is `vm`
   — Create's Patroni check passes, the 3 steps (verify, safety_dump, restore) are inserted.
   (2) `verify` fails (a checksum mismatch, a transient artifact-read error) → the chain
   halts, the DBA is mailed. (3) Before resume, a routine estate re-import lists `app-01`
   under a new `app-patroni` cluster (platform `k8s_patroni`) — a DB genuinely migrated onto
   Patroni; `upsertInstance` re-platforms it in place, no quarantine. (4) The DBA presses
   Resume. `chain.Resume` flips to running and re-drives with no re-check; the driver re-fires
   `verify` (now succeeding), `safety_dump`, then `restore` via `runs.Start(Operation:
   "restore", Internal:true, Instance:"app-01")` — **no platform check anywhere on this path**
   → `pg_restore` runs directly against the now-Patroni-managed cluster, behind Patroni's
   back. This is the exact divergence hazard (research gotcha #1) the guardrail exists to
   prevent, defeated through the product's own advertised halt-then-resume recovery flow.
   **Severity CRITICAL, upheld.** The gate's own rubric lists "Patroni block defeated" and
   "no renamed/re-platformed instance may slip past" as critical-tier by name; the finding is
   precisely that. It does **not** meet the M3-gate downgrade criteria (a dev-only trigger, or
   no consumer) — every step is a legitimate production action and the consumer (a live
   Patroni restore) is the harm itself. **Honest mitigations, stated for scope, not to soften
   the grade:** the chain still takes a `safety_dump` before the restore (dumps are allowed on
   Patroni by design), so the incident-killing safety guarantee is intact — the hazard is
   Patroni replica/primary divergence, not unrecoverable data loss; and the trigger needs the
   re-platform to land specifically in the halt→resume window. Neither removes a guardrail
   bypass on a live target.
   **Fix (WU-048):** re-validate the Patroni-restore guardrail where a restore step is about
   to fire — read the target's *current* cluster platform fresh, either in `chain.Resume`
   before the state flip (refuse with `ErrPatroniRestore` when a not-yet-succeeded `restore`
   step now targets `k8s_patroni`) or in `drive`/`next` immediately before firing a `restore`
   step. Prefer the driver site so a mid-chain re-platform is caught even without a resume,
   and cover it with a test that re-platforms between create and fire.
   → **WU-048** (BLOCKS M4 exit)

## Confirmed — HIGH (one root cause, two surfaces → WU-049)

2. **`portal.env.template`'s inline trailing comments break systemd `EnvironmentFile=`
   parsing — the pilot unit, configured exactly as the template and runbook instruct,
   crash-loops and never boots** — `infra/portal.env.template:13,29,37,60,61,62` (and others)
   · `infra/dbportal.service:25` · `backend/internal/config/config.go:64,67,47-49,176`.
   Most `[REQUIRED]`/`[recommended]` lines put an explanatory comment *after* the value on the
   same line, e.g. `PORTAL_COOKIE_SECURE=true           # [recommended] …` (line 37),
   `PORTAL_MAINTENANCE_INTERVAL=1h      # sweep cadence …` (line 60), plus the two other
   retention durations and `PORTAL_LDAP_INSECURE=false`. `dbportal.service:25` loads this file
   directly via `EnvironmentFile=/etc/dbportal/portal.env`. systemd's `EnvironmentFile=`
   parser treats `#` as a comment **only** at the start of a line — it does NOT strip a
   trailing `# …` after a value; the rest of the line becomes part of the value.
   **Verified empirically on the VM (systemd 255):** a transient unit
   (`systemd-run -p EnvironmentFile=…`) running `/usr/bin/env` printed
   `PORTAL_COOKIE_SECURE=true           # [recommended] true when TLS terminates in front` as
   the literal variable value, comment included. `config.go` binds `CookieSecure bool` (:67),
   `LDAPInsecure bool` (:64), and `MaintenanceInterval`/`ArtifactRetention`/`AuditRetention
   time.Duration` (:47-49) through `caarlos0/env`; `strconv.ParseBool` / `time.ParseDuration`
   on `true           # …` / `1h      # …` fails, so `env.ParseWithOptions` (config.go:176)
   returns an error → `config.Load` errors → `run()` returns it → the process exits 1. With
   `Restart=on-failure` + `RestartSec=5s`, the unit crash-loops forever. The dev `.env.example`
   uses only whole-line comments (godotenv tolerates trailing ones anyway) — the WU-046
   template departed from that safe style, and the WU-046 live drill missed it because it used
   hand-written clean values rather than a verbatim copy of the template.
   **Fix (WU-049):** rewrite `portal.env.template` so every note is its own `#`-prefixed line
   above the `KEY=VALUE`, never trailing on a value line (match `.env.example`). Consider a
   cheap CI/smoke check that loads the template through a real `EnvironmentFile=` (or
   `systemd-analyze`) so a regression can't reach a pilot.
   → **WU-049**

3. **`docs/deploy.md`'s documented migrate command is broken by the same unstripped inline
   comments — `env` tries to exec `#` and no migration runs** — `docs/deploy.md:74-75`
   (paired with finding 2's template).
   Step 4 tells the operator to run
   `sudo -u dbportal env $(sudo cat /etc/dbportal/portal.env | grep -v '^#' | xargs)
   /opt/dbportal/portal migrate up`. `grep -v '^#'` drops only whole-line comments; the
   inline trailing comments survive. `xargs` then flattens the file to one space-separated
   arg list, so each trailing `# …` becomes bare-word args after the `KEY=VALUE`. Because this
   is inside `$(…)` command substitution, the `#` is not a shell comment — it reaches `env` as
   a literal argument, and GNU `env` treats the first non-`NAME=VALUE` token as the command to
   exec.
   **Verified empirically:** `env $(cat file | grep -v '^#' | xargs) /bin/true` with a
   template-shaped file failed with `env: '#': No such file or directory`. On a fresh host
   the very first schema step is blocked.
   **Fix (WU-049):** with finding 2's template fixed (comments off value lines), replace the
   fragile `env $(cat|grep|xargs)` recipe with `set -a; . /etc/dbportal/portal.env; set +a;
   sudo -u dbportal /opt/dbportal/portal migrate up` (source, don't word-split). Fix both
   together — the sourcing form still mis-handles inline comments if any remain.
   → **WU-049**

## Confirmed — LOW (re-graded down during verification; bundled into WU-049)

4. **`PORTAL_DOTENV` pointing at a missing file logs "loaded dotenv file" though nothing was
   read** — `backend/cmd/portal/main.go:54-62` · `backend/internal/config/config.go:197,163`
   *(reviewer said MEDIUM)*.
   `LocateDotenv` returns the `PORTAL_DOTENV` value verbatim with no existence check
   (config.go:197), unlike the implicit walk which only returns a path `isRegularFile` has
   confirmed. `config.Load` then treats a missing file as `fs.ErrNotExist` → silent "no dotenv
   — env + defaults only" (config.go:163). But `main` chooses its log line on whether the
   resolved *string* is non-empty (`if dotenv != ""` → `"loaded dotenv file"`, main.go:59-60),
   not on whether a file was actually parsed. So `PORTAL_DOTENV=/typo/path.env` logs
   `loaded dotenv file path=/typo/path.env` while every value silently fell back to process
   env + defaults.
   **Re-graded MEDIUM → LOW:** the trigger is narrow and there is no behavior corruption — the
   pilot **systemd** unit sets `PORTAL_DOTENV=` *empty*, which resolves to the correct "no
   dotenv file found" branch, so this only bites an operator running the binary **by hand**
   with a typo'd/not-yet-deployed explicit path, and only misleads a log line (the config
   still loads deterministically from process env). It is a diagnostic-honesty footgun that
   undercuts the WU-045 "provenance visible at boot" goal, worth the cheap fix, not a
   mainline-flow defect. (A `PORTAL_DOTENV` path that exists but is unreadable already fails
   loudly — `config.Load` returns `config: read …`; only the not-exist case is silent.)
   **Fix (WU-049):** have `Load` report whether it actually read a file (a `loaded bool`, or a
   distinct sentinel when an explicit `PORTAL_DOTENV` path is absent) and log "loaded" only
   when a file was truly parsed — otherwise log "dotenv path configured but not found; using
   process env + defaults" so the two cases are distinguishable.
   → **WU-049**

## Ledger

| # | Severity | Dimension | Fix |
|---|----------|-----------|-----|
| 1 | **CRITICAL** | concurrency-locks | **WU-048** (blocks M4 exit) |
| 2 | HIGH | deploy-hardening | WU-049 |
| 3 | HIGH | deploy-hardening | WU-049 |
| 4 | LOW | ci-supply-chain-config | WU-049 |

Raised 4 · confirmed 4 · refuted 0 · re-graded down 1 · **criticals 1**.
Clean dimensions: retention-safety, seed-loadtest-specs.

**Gate outcome:** M4 does NOT exit yet. Land **WU-048** (the Patroni-resume critical) →
re-confirm, then **WU-049** (the deploy-packaging HIGH bundle + the dotenv-log LOW) so the
pilot packaging is genuinely deployable, then mark **M4 EXIT** in ROADMAP.md and the MVP is
complete. This is the same fix-WU protocol as M1/M2/M3, except the presence of a critical
means the exit is gated on the fix rather than granted with follow-ups.

---

## Re-confirm (s35, 2026-08-11) — all 4 findings FIXED, gate CLEAR

Method: **inline spot-verify** (user-chosen over a full workflow re-run — every fix already
carried live-drill proof from its WU). Each finding re-checked at HEAD (220a496 + docs):
the enforcement/fix site grepped in the committed tree AND the finding's tests re-run fresh
(`-race -count=1`), on top of the empirical drill evidence recorded per WU.

1. **CRITICAL, Patroni fire-time — FIXED (WU-048, s33, d909cbb).** `runs.Start` re-validates
   with a FRESH platform read on every fire (service.go — the same per-fire treatment as the
   self-target ban/prod ritual); chain.go:222 keeps the door 403. Tests fresh-green:
   `TestPatroniRestoreRefusedAtStart`, `TestRePlatformHaltsResumedRestore`,
   `TestRePlatformMidChainBlocksRestore` (written first, red pre-fix). Live drill: real
   re-import re-platform → resume → chain HALTED, restore step run=NULL, denial row.
2. **HIGH, env-template — FIXED (WU-049, s34, 220a496).** Zero value lines carry `#`
   (grep-confirmed); `TestDeployEnvTemplateSystemdSafe` (red vs the old template) fresh-green
   and guards CI. Live: `systemd-run EnvironmentFile=` env-dump clean ×5 typed values; the
   real unit booted a VERBATIM template copy to **active, NRestarts=0**, ldap mode.
3. **HIGH, deploy.md migrate — FIXED (WU-049, s34).** §4 uses the sourcing recipe (`set -a;
   . …; set +a; exec … migrate up` inside the service user's shell); the only `xargs` mention
   left is the warning naming the old form. Live: the recipe ran VERBATIM → schema v12.
4. **LOW, dotenv log — FIXED (WU-049, s34).** `config.Load` returns `loaded`; main has the
   three honest branches (grep-confirmed); `TestLoadReportsWhetherDotenvWasRead` fresh-green.
   Live: `PORTAL_DOTENV=/typo` → the distinct WARN.

CI green on both fix commits (runs 31423009840, 31529946640). **M4 EXITS; the MVP is
complete** (ROADMAP.md updated s35).
