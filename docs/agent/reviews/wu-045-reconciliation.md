# WU-045 — Docs-vs-reality reconciliation report

Date: 2026-07-20 (session s31). Method: walk the load-bearing claims in
SPEC/DECISIONS/ARCHITECTURE/ROADMAP/STATE/CLAUDE.md and the doc comments that
narrate the code, verify each against the tree, and either **resolve** the drift
in this WU or **file** it against the WU that owns it. This is the ROADMAP M4
"docs-vs-reality reconciliation" exit item.

## Drifts found and resolved (this WU)

| # | Claim / location | Reality | Fix |
|---|---|---|---|
| R1 | `frontend/src/index.css` header: "Exact hex values **pending the brief extract on this machine**" | The design brief has been on the VM at `docs/specs/design-brief.md` since 2026-07-07; it pins subtle borders `#E5E7EB` but the token was `#e2e8f0` | Rewrote the comment to cite the brief + record the reconciliation; aligned `--border` → `#e5e7eb` (the brief's one exact hex). The env/status palettes are named *semantically* in the brief, so the exact hexes stay as a documented WCAG-AA set. |
| R2 | `docs/demo-m1.md` header: "live-verified … **2026-07-08** (JOURNAL s07)" | The hero-flow beats were re-walked live through JOURNAL s13, and the automated twin (`golden_flow_test.go`) re-verifies every gate | Refreshed to name the automated twin as the standing proof; first live-verified s07, last re-walked s13, header reconciled WU-045. |
| R3 | `config.LocateDotenv` walked up to the **filesystem root** and nothing logged which `.env` loaded (M1-gate item 16, foreign-`.env` footgun) | A binary run from an arbitrary dir could silently adopt a stray parent `.env` | Bounded the walk to the repo root (nearest ancestor with `.git`); added a `PORTAL_DOTENV` explicit-path/disable override; `main` now logs the resolved path (or "no dotenv file"). 4 new tests, incl. the foreign-`.env` case. |
| R4 | `.env.example` omitted the WU-041…044 knobs | `config.go` carries `PORTAL_LOCK_TTL`, `PORTAL_PROTECTED_INSTANCES`, `PORTAL_MAINTENANCE_INTERVAL`, `PORTAL_ARTIFACT_RETENTION`, `PORTAL_AUDIT_RETENTION` | Added all five to `.env.example` (shape + defaults + the disable/observational semantics). The full *deploy* template stays WU-046 scope. |
| R5 | `docs/ARCHITECTURE.md` §7 listed Locking as a future "(M4)" item and Patroni-restore sequencing as "(M3, prototype first)" | Both delivered by WU-042/ADR-012 (`instance_lock`; `ErrPatroniRestore` block) | Annotated both entries with their resolution (lock proven under load in WU-043; full Patroni sequencing remains post-MVP). |
| R6 | SPEC-043 mini-ADR 1 + `runs/load_test.go`: the load harness "**skips in CI** (no Postgres)" | WU-045 adds a Postgres service to `check.yml`, so it now runs in CI too | Annotated the supersession in both. Verified the assertions are runner-speed-robust: parallelism is only asserted `>= 2`, never a hard peak, and every other assertion is deterministic (exactly-once counts, zero orphans/locks). |
| R7 | ROADMAP M3 exit was unmarked (carried STATE housekeeping) | M3 exit criteria met at s22 (WU-036); exit twin `demo-m3.md`; gate `m3-gate.md` | Added an "**Exited** (s22, WU-036)" annotation to the M3 section; re-surfaced the security-vetting-package gate. |

## Checked and matching (no drift)

- **Migrations:** `0001…0012` on disk == STATE's "through 0012". `0011_job_id_unique`, `0012_instance_locks` present as WU-040/WU-042 claim.
- **Toolchain:** VM `golangci-lint` = **2.12.2** == CLAUDE.md == the new CI pin; VM Go = **1.26.4** == CLAUDE.md.
- **ADRs:** ADR-011 (golden-flow-in-`go test`), ADR-012 (concurrency), ADR-013 (retention) all present and dated in DECISIONS.md.
- **Config knobs:** every documented env var exists in `config.go` with the documented default (`LockTTL` 30m, `MaintenanceInterval` 1h, `ArtifactRetention` 2160h, `AuditRetention` 8760h, `EngineNonProd` mock, `AuthMode` ldap-fails-closed).
- **Guardrails:** non-launchable restore ops gated at `runs.Start`; `instance_lock` acquired in Start's tx; protected set seeded with `DBName` — all present as ARCHITECTURE/SPEC-031/SPEC-042 describe.

## Filed (owned by another WU — no action here)

- **Prod `.env` deploy template** (every required var by shape, no secrets) + **object-store bucket lifecycle-expiry** rule → **WU-046** (already in its context brief). `.env.example` now covers the dev-compose shape; the pilot template is WU-046.
- **Semaphore template ids** (`dump:3 / verify:4 / restore:5 / smoke:1`) are assigned by Semaphore in creation order at bootstrap time — a runtime fact recorded in STATE + `demo-m3.md`, not a static-file claim. No drift to resolve.

## CI hardening (same WU)

- Postgres 16 service added to `check.yml` (health-gated); DB-backed tests + the golden-flow e2e + the load/stampede harness now **run** in CI instead of skipping (ADR-011 gap closed).
- `golangci-lint` installer pinned to **v2.12.2** (script + binary), not `curl | sh` from HEAD (M1-gate item 14 — supply-chain + lint drift).
- Actions bumped to the node24 majors (`checkout@v7`, `setup-node@v7`, `setup-go@v7`); `setup-go` cache pointed at `backend/go.sum` (was a silent miss).

Cold-start evidence (fresh clone → install → migrate → check → build:release → run) is pasted in JOURNAL s31.
