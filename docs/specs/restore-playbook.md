# SPEC-036 · Restore playbook (real) + M3 rehearsal

> Groomed 2026-07-14 for WU-036 (Phase 3, the LAST M3 WU — then the M3 gate).
> Authority chain: D4 (catalog = Backup + Restore) + D5 (chains) → ARCHITECTURE
> §3 (Restore) + §7 (do-not-discover-twice) → SPEC-031
> (docs/specs/restore.md: the `verify → safety_dump → restore` chain recipe,
> the launchable gate, the "unconditional safety dump" invariant) → this file.
> Rides SPEC-034 (docs/specs/dump-playbook.md: the playbook shape + the
> engine-side libpq/`mc` credential pattern, ADR-004) and SPEC-035
> (docs/specs/artifact-storage.md: `location = s3://<bucket>/<name>`, the object
> key IS the artifact name). Engine story unchanged: MockEngine is the forever
> default for dev + ALL tests (ADR-002); the real playbooks run only under the
> opt-in Semaphore adapter, live-verified on the VM.

## Scope

Close the loop the product exists for: a REAL restore of a REAL dump onto the
compose target, driven by the SAME portal chain that WU-031 built on MockEngine
— the mock→semaphore swap behind the UNCHANGED `engine.Adapter` seam, exactly as
WU-034 did for dump. WU-031 shipped the assembler, the recipe, the `'safety'`
plumbing, the guardrails, and the drawer against MockEngine; WU-036 supplies the
three steps' real playbooks and the one Go change that lets the artifact lineage
reach them. Delivers:

1. `playbooks/verify.yml` — the chain's step 1: fetch the source artifact from
   object storage and sha256-verify it against the expected checksum, touching
   the target NOT AT ALL. A mismatch fails the task → the chain halts at step 1
   → the safety dump and the restore never fire (SPEC-031 mini-ADR 1).
2. `playbooks/restore.yml` — the chain's step 3: fetch the artifact, RE-verify
   its sha256 (defense in depth — a tampered object yields zero target writes
   even if it slipped past step 1), then `pg_restore` with a fixed vetted flag
   set. No new step-2 playbook: `safety_dump` reuses the existing
   `playbooks/dump.yml` (catalog Template `dump`), which already stores a
   `'safety'`-classed artifact.
3. `infra/semaphore-bootstrap.sh` gains a `verify` template and a `restore`
   template, both pinned to the existing `pgtarget-env` Environment (id 3) —
   verify needs the `MC_*` object-store creds; restore needs those plus the
   `PG*` libpq creds. Prints their ids.
4. **One Go change (`internal/engine/semaphore.go`): `StartJob` forwards the
   artifact lineage to Semaphore as Ansible extra-vars.** WU-034's `StartJob`
   discards params (dump needs none — it dumps whatever `PGDATABASE` points at);
   restore/verify need to learn WHICH object to fetch. A fail-closed allowlist
   (`artifact_name`, `checksum`) crosses the seam as the task `environment`
   field. Nothing else changes; MockEngine and the dump/smoke bodies are
   untouched.
5. `docs/demo-m3.md` — the human twin of the M3 exit (the demo-m1.md pattern):
   seed → portal dump → destroy a table → portal restore (verify → safety_dump →
   restore) → data verified back + `'safety'` artifact registered; the
   checksum-tamper halt+resume beat; the mock-vs-semaphore same-code beat.
6. O-4 (dump/restore options matrix) annotated RESOLVED in DECISIONS.md (dump
   flags landed in WU-034; restore flags land here — O-4 covers both).

**Explicitly NOT here:** Patroni-aware restore sequencing (pause/detach →
restore → reinit replicas — ARCHITECTURE §7, the named hard problem, iceboxed;
dev target is a plain single Postgres); PITR / partial restore (D4 = full
restore); DROP/CREATE DATABASE restore (mini-ADR 2); exposing `location` on the
artifacts API (SPEC-030 holds — it stays engine-side); retention ENFORCEMENT of
`'safety'` artifacts (M4). No migration, no UI change (WU-031's Restore drawer
already assembles the chain; the steps just run real playbooks now), no chain /
catalog / restore-recipe change.

## Mini-ADRs (agent decisions, revisitable)

1. **The artifact lineage crosses the seam as Semaphore extra-vars, via a
   fail-closed allowlist — NOT the whole params map.** `restore.Steps`
   (SPEC-031) puts `{artifact_id, checksum, artifact_name}` on the verify and
   restore step params, and `runs.Start` adds `{instance}`. Of these the real
   playbooks consume exactly two: `artifact_name` (the object key to fetch) and
   `checksum` (the sha256 to assert). `StartJob` forwards ONLY those two as the
   task `environment` (Ansible `--extra-vars`), so `{{ artifact_name }}` and
   `{{ checksum }}` resolve in the playbook. `instance` (portal routing — the
   target is selected engine-side by the template→Environment binding, not a
   playbook var) and `artifact_id` (portal bookkeeping) never leave the portal.
   Allowlist, not denylist: a param added later reaches the real engine only
   when someone lists it — the fail-closed idiom the registry and the template
   mapping already use. The forwarded values are not secrets (a generated dump
   name + a hex hash), so extra-vars carry nothing leakable. A params-free op
   (dump/safety_dump/smoke) posts the exact body WU-033/034 posted — no
   `environment` key — so their behavior is byte-identical.

2. **`pg_restore --clean --if-exists --no-owner --no-privileges
   --single-transaction` into the LIVE target database — not DROP/CREATE
   DATABASE (O-4, the restore half).** Rationale, flag by flag:
   `--clean --if-exists` emits `DROP … IF EXISTS` then recreate for each dumped
   object, restoring INTO the existing database — no need to drop the database
   (you cannot drop the one you're connected to; that path needs a maintenance
   DB + terminating every other connection, heavier and riskier for a
   single-target dev restore). `--no-owner --no-privileges` mirrors the dump
   flags (SPEC-034): the runner role is not the objects' owner, so ownership /
   ACL restore would fail. `--single-transaction` wraps the whole restore in ONE
   transaction (implies `--exit-on-error`): a mid-restore failure rolls back and
   the target is left EXACTLY as it was — the "no half-restored target"
   property, the restore analogue of verify's "zero target writes". The dump is
   `-Fc` (custom format, SPEC-034), which `pg_restore` reads directly and orders
   drops reverse-dependency (view → index → table), so `--clean` inside one
   transaction is safe for the seeded schema. Zero user-facing options (O-4): the
   flag set is fixed in the playbook, same posture as dump. Rejected for MVP:
   DROP/CREATE DATABASE (above); `--jobs` parallel restore (incompatible with
   `--single-transaction`; a nightly-scale concern, not a correctness one).

3. **The playbook reconstructs the `mc` fetch path from the engine-side bucket +
   the `artifact_name` param — `location` never crosses the seam.** SPEC-035
   mini-ADR 2 fixed that the object key IS the artifact name and anticipated a
   `s3://` → `<alias>/` rewrite for WU-036. We reach the same path WITHOUT
   threading `location` through the portal: the bucket is `DBPORTAL_BUCKET`
   (engine-side env, already in `pgtarget-env`), the name is the `artifact_name`
   extra-var, so the fetch path is `{{ mc_alias }}/{{ bucket }}/{{
   artifact_name }}` — the exact path `dump.yml` uploaded to. This keeps
   `location` fully engine-opaque (SPEC-030: it is never exposed above the seam,
   and here it need not even be passed down). MVP assumes the single configured
   bucket (true today); a multi-bucket future would thread `location` through
   the step params, a deferred change.

4. **verify has ZERO target contact; restore RE-verifies before touching the
   target.** `verify.yml` never names a PG connection — it fetches the object
   (`mc cat`) and pipes to `sha256sum`, asserts equality with `{{ checksum }}`,
   and stops. So a checksum mismatch (AC-2's tamper) halts the chain at step 1
   with the target never dumped and never restored (the only target run is the
   read-only failed verify, per SPEC-031 mini-ADR 1). `restore.yml` re-runs the
   same sha256 assertion on the freshly fetched bytes BEFORE any `pg_restore`
   invocation, so even a tamper that somehow passed step 1 (a race, a bug)
   produces zero target writes. Defense in depth, and cheap (the object is
   already local for the restore).

5. **verify/restore emit NO `DBPORTAL_RESULT` line — the task terminal status IS
   their machine-readable outcome.** SPEC-031 is explicit that verify and
   restore register no artifact; only `dump`/`safety_dump` produce one. The
   adapter attaches an artifact solely from a `DBPORTAL_RESULT` sentinel line
   (SPEC-034), and `parseResultLine` yields nil when none is present — so a
   restore/verify success finalizes with no registry row, unchanged. The
   playbooks emit a HUMAN summary line (`RESTORE OK: …` / `VERIFY OK: …`,
   deliberately NOT the `DBPORTAL_RESULT=` sentinel) for the log and the demo;
   the machine-readable success/fail is the Semaphore PLAY RECAP → task status
   the adapter already maps. So "machine-readable result line" for a restore is
   the task status, not a sentinel — sentinels are only for artifact-producing
   ops. `safety_dump` (step 2) DOES emit the sentinel — it runs `dump.yml`
   unchanged and registers its `'safety'` artifact.

6. **A `DBPORTAL_ARTIFACT_NAME`/`_CHECKSUM` preflight assert makes a wiring
   mistake fail fast and locally.** Both new playbooks assert `artifact_name`
   and `checksum` are defined and non-empty before doing anything (the
   `dump.yml` PGHOST-preflight pattern). If the extra-vars forwarding is
   mis-wired, the failure is an honest preflight error with a clear message —
   BEFORE any object fetch or target contact — not a cryptic `mc`/`pg_restore`
   crash. This is also the belt-and-braces for the one open question below.

## Interfaces

- `playbooks/verify.yml` (Template `verify`, PlaybookTag `verify`): preflight
  assert on `artifact_name`/`checksum`; `mc cat {{ mc_alias }}/{{ bucket }}/{{
  artifact_name }}` piped to `sha256sum`; assert the computed hash == `{{
  checksum }}`; emit `VERIFY OK: <name> <sha>`. Names no PG connection. Fetch
  creds from `MC_HOST_dbportal` (engine-side, ADR-004).
- `playbooks/restore.yml` (Template `restore`, PlaybookTag `restore`): same
  preflight; `mc cp …` the object to a staging path; RE-stat sha256 and assert ==
  `{{ checksum }}` (mini-ADR 4); `pg_restore --clean --if-exists --no-owner
  --no-privileges --single-transaction --dbname={{ lookup('env','PGDATABASE') }}
  <staging>` (mini-ADR 2); emit `RESTORE OK: <name> -> <db>`; best-effort staging
  `rm` (`failed_when: false`). PG* + MC_* both engine-side.
- `infra/semaphore-bootstrap.sh`: after the `dump` template, ensure a `verify`
  template (playbook `verify.yml`, env `pgtarget-env`) and a `restore` template
  (playbook `restore.yml`, env `pgtarget-env`), idempotent like the others;
  print `SEMAPHORE_VERIFY_TEMPLATE_ID` / `SEMAPHORE_RESTORE_TEMPLATE_ID` and the
  `verify:<id>,restore:<id>` map hint.
- `internal/engine/semaphore.go`: `StartJob(ctx, template, params)` now consumes
  `params` — builds the task body `{template_id, environment?}` where
  `environment` is the JSON of the allowlisted params (`artifact_name`,
  `checksum`) when present, absent otherwise. New unexported `forwardVars`
  (allowlist) + `extraVars(params) string`. No signature change; MockEngine,
  `runs`, chain, catalog, and the artifact/result-line parsing are all
  untouched.
- **Go: nothing else.** No migration, no catalog change (verify/restore/dump
  templates already exist as catalog data, WU-031), no chain/recipe change, no
  UI change.

## Behavior (testable)

1. **No Go regression:** `npm run check` green with ZERO Semaphore/minio
   dependence. MockEngine restore beat (golden flow Beat 10) is untouched — the
   mock ignores `params` in `StartJob` exactly as before, so verify/safety_dump/
   restore still drive on the mock. `TestSemaphoreStartJob` (dump/smoke, no
   forwardable params) still posts a body with NO `environment` key.
2. **Extra-vars forwarding (unit, stub Semaphore):** `StartJob` for a template
   with `{artifact_name, checksum, instance, artifact_id}` params posts
   `environment` = the JSON of `{artifact_name, checksum}` ONLY — `instance` and
   `artifact_id` are absent; a params-free `StartJob` posts no `environment`
   key at all.
3. **Live rehearsal (AC-1, VM, isolated portal — [[live-drill-isolation]]):**
   the full demo-m3.md flow passes on the release binary, human pace ≤ 15 min;
   every M3 exit criterion ticks in the doc. Portal dump on pgtarget → success +
   `'standard'` artifact in minio; `DROP TABLE widget` on pgtarget; portal
   restore of that artifact onto pgtarget → a running `kind=restore` chain →
   success; `widget` is back with its 4 rows; the `safety_dump` step registered
   ONE `'safety'` artifact for pgtarget.
4. **Checksum tamper halts at verify (AC-2):** corrupt the stored object (or
   pass a wrong checksum), restore → the chain halts at step 1 (verify failed),
   NO `safety_dump` run and NO `restore` run exist on the target, `widget` still
   absent (target untouched), ONE chain halt mail; fix the object + resume →
   verify passes, safety_dump + restore fire → success.
5. **Safety artifact is itself restorable (AC-3):** the `'safety'` artifact the
   rehearsal registered `pg_restore --list`s cleanly (the seed schema + data) —
   a real, independently-restorable dump, not a metadata stub.

## Guardrails & audit

Everything above the engine seam is WU-031/032 code, unchanged: all four
guardrail layers arrive per step through `runs.Start`; the prod ritual is
enforced once at chain creation on the TARGET and fired verbatim on every step
incl. resume; the audit trail is the per-step run rows plus the chain row; the
halt mail is the chain's (D7 content: who/what/where/status + link). pgtarget is
env=dev → ClassNonProd → the nonprod Semaphore adapter; prod stays mock in dev
(guardrail 3, structural). No secret enters the repo, portal DB, params, audit
rows, or logs: PG* + MC_* are Environment-injected engine-side (ADR-004); the
forwarded extra-vars are a name + a hash, not secrets; the step params already
carry only ids + checksums (SPEC-032). Poll remains the finalization authority.

## Live drill recipe (VM, isolated portal — [[live-drill-isolation]])

Persistent `.env` stays `PORTAL_ENGINE_NONPROD=mock`; the drill runs an
EPHEMERAL isolated portal (scratch DB + spare port :8099 +
`PORTAL_ENGINE_NONPROD=semaphore` + `PORTAL_SEMAPHORE_TEMPLATES=verify:<v>,
restore:<r>,dump:<d>,smoke:1`), driven over HTTP, torn down after. Bring the
stack up (`docker compose -f infra/compose.yaml --env-file .env up -d --build
--wait`), re-run the bootstrap (creates the verify + restore templates, prints
their ids), then run demo-m3.md's beats against :8099. Reset pgtarget between
runs by re-restoring or `docker compose restart pgtarget` (re-seeds only on a
fresh volume — use SQL to reset rows). Grep the DB dump + task logs for every
secret → absent. Demo :8080 untouched.

## Out of scope / deferred

- Patroni-aware restore sequencing → ARCHITECTURE §7 (iceboxed; the named M3
  hard problem — prototype post-MVP).
- PITR / partial / point-in-object restore → out of MVP (D4 = full restore).
- DROP/CREATE DATABASE restore + connection termination → post-MVP (mini-ADR 2).
- Multi-bucket / threading `location` through step params → deferred (mini-ADR 3).
- `'safety'` retention ENFORCEMENT (prune job) → icebox (M4).
- Parallel `--jobs` restore for nightly-scale dumps → M4 load-test territory.

## Open questions

- **Does Semaphore forward the task `environment` field to `ansible-playbook`
  as `--extra-vars` (so `{{ artifact_name }}` resolves), or as process env?**
  The interface above assumes extra-vars (mini-ADR 1); the preflight assert
  (mini-ADR 6) makes a wrong assumption fail fast and locally in the drill, with
  ZERO target writes. Fallback if it lands in process env: have the playbooks
  read `lookup('env','artifact_name')` instead — a one-line change per playbook,
  the interface and the Go allowlist unchanged. Pinned in the live drill (run
  the verify step FIRST as the extra-vars smoke test). Owner: agent.
</invoke>
