# SPEC-050 — Local Playbook Execution Engine (`engine/local`)

> Module spec for the ADR-014 pivot: the portal executes Ansible playbooks itself,
> in-process, behind the UNCHANGED `engine.Adapter` seam. Written at M5 grooming
> (s37, user-directed — ahead of the usual just-in-time point); each mini-ADR is
> re-confirmed by its implementing WU (WU-050…055). Precedence: ADR-014 wins where
> older docs conflict.

## Scope

One new adapter package `backend/internal/engine/local` plus the machinery that feeds
it: dynamic inventory from the fleet model, a validated extra_vars contract, and a
manifest-driven playbook library. Everything upstream of the seam — instance lock,
self-target ban, prod ritual, Patroni fire-time block, launchable gate, append-only
audit, artifact registry, chains, scheduler, notifications — is **inherited unchanged
by construction**, because the engine still sits behind `runs.Service.Start` and the
`Adapter` interface (mini-ADR 8). MockEngine remains the default for dev and all tests
(ADR-002).

**Non-goals (deliberate):** distributed/remote runners; a web playbook editor or upload
API (delivery v1 is git/filesystem — ADR-014); per-playbook authorization (DBA-only
stands, D2); Windows targets; `ansible-runner` structured events (recorded option, not
required runtime).

> **WU-050 (s38) re-confirmed mini-ADRs 1–4** with three recorded adjustments:
> (1) the adapter lives in **`package engine` as `local.go`** (not a sub-package) —
> consistent with the mock/semaphore precedent and sharing the result-line parser
> (`resultFromLine`, WU-040 guard) directly; (2) mini-ADR 1 gained a **post-exit
> group SIGKILL reap**: after the child exits (any path, not just cancel), the
> whole process group is killed so a backgrounded straggler can never outlive its
> job (proven by test — a playbook that exits 0 leaving a sleeper behind);
> (3) `PORTAL_ENGINE_PROD=semaphore` is **refused at wiring** (see config table).

## Mini-ADR 1 — Process model: one supervised `ansible-playbook` child per job

- Direct `exec` of `PORTAL_ANSIBLE_BIN` (default `ansible-playbook` from PATH) with a
  **fixed argv** — never a shell, never string interpolation. Command injection is
  closed by construction: user input reaches the child only through the extra_vars
  FILE (mini-ADR 6) and the inventory FILE (mini-ADR 5).
- The child runs in its **own process group** (`Setpgid`). Cancel = SIGINT to the
  group (Ansible's graceful interrupt) → `PORTAL_ENGINE_CANCEL_GRACE` (default 10s) →
  SIGKILL the group. Per-job timeout from the manifest, ceilinged by
  `PORTAL_ENGINE_TIMEOUT_CAP`; timeout follows the same kill discipline and finalizes
  `failed` with an honest "timed out after …" Error. `Pdeathsig=SIGKILL` (Linux) so a
  dying portal never leaks a runner.
- **Concurrency cap** `PORTAL_ENGINE_MAX_CONCURRENT` (default 8): jobs beyond the cap
  hold `StateQueued` in an in-adapter FIFO. The instance lock already serializes
  per-instance; this caps aggregate load on the portal host (ARCHITECTURE §6 planned
  25–50 concurrent at estate scale — the cap is the local-host reality check, and the
  scheduler's jitter spreads the rest).
- **Per-job private workdir** `PORTAL_ENGINE_WORKDIR/<job-id>` (0700, service user):
  generated inventory + extravars files live here and nowhere else; removed on
  terminal state (kept under a debug knob). The library root is read-only to the job.

## Mini-ADR 2 — JobID + state: in-memory, restart-forgets (the mock's proven shape)

`JobID = local-<boot-nonce>-<seq>` — the nonce keeps IDs from aliasing across restarts,
exactly the seam contract ("a stale JobID must fail ErrUnknownJob, never resolve to a
newer job"). The job table is in-memory only, like MockEngine: a portal restart forgets
live jobs → `Status` answers ErrUnknownJob → the EXISTING boot orphan sweep finalizes
the run failed and frees the lock. No new crash-recovery machinery; `Pdeathsig` ensures
the orphaned process died with the portal.

## Mini-ADR 3 — Exit status parsing

State machine: spawn → `StateRunning` (Started stamped) → exit → terminal.
Exit 0 → `success` (+ artifact if a result line was seen). Killed-by-Cancel →
`canceled`. Anything else → `failed`, Error = mapped ansible-playbook code
(1 generic error · 2 failed task(s) · 4 unreachable host(s) · 5 bad options ·
99 user interrupted · 250 unexpected) + the last stderr lines. The `PLAY RECAP`
stays in the log stream for humans. **The `DBPORTAL_RESULT=<base64 json
{name,size_bytes,sha256,location}>` line contract carries over verbatim** from the
Semaphore adapter (same regex, same nil-on-malformed posture, WU-040 field guard) —
playbooks stay portable across engines and the artifact-registry path is untouched.

## Mini-ADR 4 — Log streaming

stdout+stderr merged into one pipe → line scanner → per-job ring buffer + subscriber
channels, giving the seam's replay-then-follow semantics (the mock already models
this; StreamLogs may be called any number of times, incl. after terminal). Child env
sets `ANSIBLE_FORCE_COLOR=0`, `PYTHONUNBUFFERED=1` so lines arrive clean and promptly.
**Caps:** per-line length cap and a per-job total cap (default 10 MiB) — past the cap
the stream carries a single truncation marker and the tail; a runaway playbook must
not OOM the portal. (Log persistence stays out of scope — SPEC-013 mini-ADR 3.)

## Mini-ADR 5 — Dynamic inventory from the fleet model

- **Schema (WU-051, migration 0013):** `instance` gains a nullable connection tuple —
  `host text`, `port int`. CSV import gains optional trailing columns `host,port`
  (absent → NULL; existing fixtures/imports unchanged — backward compatible;
  re-import updates in place under the existing natural-key idempotency).
- **Render:** per job, a JSON inventory file (unambiguous quoting; INI rejected) in
  the job workdir, 0600. **Least privilege: the inventory contains exactly the hosts
  the operation declares** — a `target` group holding the one target instance
  (`ansible_host`, `dbportal_port`, plus `dbportal_instance/env/platform/cluster`
  hostvars); a `cluster` group only when the manifest declares `targets: cluster`.
  Never the whole fleet.
- **No credentials in the inventory, ever.** Connection auth is host-side: the service
  user's SSH keys / `~/.pgpass` / vault files (ADR-014 secrets posture). The file
  holds addresses and facts only.
- **Fail closed:** a target with no connection tuple → `StartJob` errors
  ("instance has no connection info; re-import with host,port") → the existing
  ErrEngine path finalizes the run failed with that message. No run ever silently
  targets localhost by default.
- *WU-051 delivery notes:* the tuple = the HOST (a bare host is legal — a NULL
  port renders `dbportal_port: 5432`, the libpq default; a port without a host
  quarantines at import). Resolution crosses a new `engine.InventorySource`
  interface (defined in `engine`, implemented by `inventory.Store`, wired in
  main) so the seam, the params map, and `params_digest` stay byte-identical;
  the read happens fresh at StartJob. Env hostvar is `dbportal_env` (this
  section's shorthand), distinct from the extra-vars `dbportal_environment`
  (mini-ADR 6, WU-052). The file is `inventory.json`, passed as `--inventory`
  (replaces Ansible's default inventory sources); Ansible's stock yaml plugin
  parses it (.json is in its default extension list).

## Mini-ADR 6 — extra_vars contract

- Params → `extravars.json` in the job workdir (0600), passed as
  `--extra-vars @<file>`. **Never argv** — argv leaks via `ps`/`/proc`.
- **Reserved namespace `dbportal_*`, engine-injected:** `dbportal_instance`,
  `dbportal_environment`, `dbportal_operation`, plus the operation's engine params
  (artifact refs: `dbportal_artifact_name`, `dbportal_checksum`). A client-supplied
  param may not use the prefix (rejected at validation).
- **Client params are schema-validated at Start** against the manifest (mini-ADR 7):
  unknown param → reject; missing required → reject; type/enum mismatch → reject —
  the current per-op fail-closed allowlist, generalized. `params_digest` auditing is
  unchanged (digest over the merged map, already in Start).
- **Child env is scrubbed to an allowlist** (PATH, HOME, LANG, ANSIBLE_*,
  explicitly-configured extras): the portal's own env (DB creds, session config)
  must never reach a playbook.

## Mini-ADR 7 — Playbook manifests: the delivery-platform contract

- Library root per env class: `PORTAL_ENGINE_LIBRARY` (dev: repo `playbooks/`;
  deploy: shipped with the package). One playbook = one directory:
  `manifest.yml` + entrypoint + roles/files.
- **Manifest fields:** `id` (catalog id, unique), `label`, `icon`, `description`,
  `duration_hint`, `online_hint`, `entrypoint` (relative path — must resolve INSIDE
  the library root, path-traversal-guarded), `launchable` (bool — non-launchable ops
  remain chain-only), `retention_class` (`""|standard|safety`), `targets`
  (`instance|cluster`), `timeout`, `params` (list of {name, type: string|int|bool|enum,
  required, default, enum values}), `guardrails.patroni_restore_block` (bool — opts a
  restore-like playbook into the existing fire-time Patroni refusal).
- **Loader:** parse + validate at server boot, fail closed on any malformed manifest,
  duplicate id, or escaping entrypoint (the `config.Validate` pattern — refuse to
  boot with a clear message; `migrate/import/seed` unaffected). Optional
  `--syntax-check` per entrypoint at load (on by default, config toggle).
- **The catalog becomes manifest-backed:** the static Go catalog's four built-ins
  (dump / verify / safety_dump / restore) become the first four manifests — parity
  proven by the existing catalog tests. `/api/operations` additionally serves each
  op's params schema so the UI can render the launch form dynamically (WU-054).
  MockEngine keeps working: `template` = manifest id.
- **Adding an operation = dropping playbook + manifest into the library and
  restarting.** No Go change. That drill is the M5 exit criterion.

## Mini-ADR 8 — Integration invariants (unchanged by construction)

The engine change is invisible to: instance lock (acquire in Start's tx / release on
finalize), self-target ban, prod ritual, Patroni fire-time re-validation (WU-048),
launchable gate, `run.submitted`/`run.finished` audit + `params_digest`, artifact
registration + retention classes, chains (halt/notify/resume), scheduler, StepRunFilter
mail semantics. Each M5 WU's AC list asserts this by running the EXISTING suites
untouched; the golden flow stays on MockEngine.

## Config surface (all new knobs default-safe)

| var | default | meaning |
|---|---|---|
| `PORTAL_ENGINE_NONPROD` | `mock` | `mock` \| `local` (`semaphore` until WU-056 removes it) |
| `PORTAL_ENGINE_PROD` | `mock` | NEW — `mock` \| `local`. `semaphore` is REFUSED at wiring (WU-050): the Semaphore config block is single-instance and env classes must never share engine credentials (guardrail 3) — moot at WU-056 anyway |
| `PORTAL_ANSIBLE_BIN` | `ansible-playbook` | executable path |
| `PORTAL_ENGINE_LIBRARY` | — | playbook library root ([REQUIRED] for `local`) |
| `PORTAL_ENGINE_WORKDIR` | — | per-job private dirs ([REQUIRED] for `local`) |
| `PORTAL_ENGINE_MAX_CONCURRENT` | `8` | aggregate running-job cap (FIFO queue above) |
| `PORTAL_ENGINE_CANCEL_GRACE` | `10s` | SIGINT→SIGKILL grace |
| `PORTAL_ENGINE_TIMEOUT_CAP` | `2h` | ceiling on manifest timeouts |

`config.Validate` grows a `local`-mode arm (library + workdir required, ansible binary
found) — same fail-closed posture as the ldap arm.

## Testing strategy

- **Unit/CI: a stub `ansible-playbook`** (test-built helper binary set via
  `PORTAL_ANSIBLE_BIN`) that emits scripted lines, exit codes, `DBPORTAL_RESULT`
  lines, and sleeps — so the ENTIRE adapter (cancel kills the group, timeout, queue
  cap, exit map, result parse, restart→ErrUnknownJob, env scrubbing, workdir cleanup)
  is testable in `go test -race` with no Ansible installed. CI needs nothing new.
- **MockEngine stays the suite default** (ADR-002); the golden flow is untouched.
- **One skip-gated real-Ansible itest** on the VM (the semaphore-itest pattern):
  real `ansible-playbook` against localhost/compose `pgtarget`, skipped where the
  binary is absent.
- **Parity drill (WU-055):** the WU-034/036 rehearsal re-run on `engine=local` —
  real `pg_dump`, tamper-halt, resume, restore — before Semaphore is removed.

## Open questions (tracked, not blocking WU-050)

- **Artifact bytes destination** for local dumps: object store stays optional
  (playbooks may still `mc cp`); a local artifacts root (`file://` locations) is the
  compact default — but byte retention for a local root needs an owner (the
  maintenance loop reaps only the registry, ADR-013). Decide at WU-055 with the
  ported dump playbook on the table.
- **Per-class credential isolation depth** (separate runner user per class?) — M5
  gate dimension; hardening path recorded in ADR-014.
- **`ansible-runner` structured events** as an opt-in manifest flag, if line-parsing
  proves too coarse for playbook developers.
- **Cancel semantics per operation** (killing Ansible doesn't roll back a server-side
  operation) — pre-existing icebox item, unchanged by the pivot, more visible now.
