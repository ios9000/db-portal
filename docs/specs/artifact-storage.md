# SPEC-035 · Object storage for dump artifacts (minio)

> Groomed 2026-07-12 for WU-035 (Phase 3). Authority chain: O-1 (DECISIONS
> §Open: S3-compatible, minio in dev) → ADR-004 (object-store creds engine-side
> ONLY; portal DB / logs / params carry no secrets) → ADR-002 (one Adapter,
> MockEngine is the forever default for dev + ALL tests; Semaphore is opt-in)
> → SPEC-034 (docs/specs/dump-playbook.md: the dump playbook + the
> `DBPORTAL_RESULT` result-line contract + `engine.Artifact.Location`) → this
> file. Rides SPEC-012 (runs.Service ↔ Adapter) and SPEC-030
> (docs/specs/artifacts.md: the `artifact` registry row, `location` column).

## Scope

Dump artifact **bytes** get a real home. Today (WU-034) `location` is a
filesystem path on a shared compose volume; that volume is single-host,
unmanaged, and not the storage story the product ships. This WU puts the bytes
in object storage (minio in dev, any S3-compatible store in prod) and records
the object URL as `location`. The shared volume becomes **staging only**. No
portal code above the engine seam changes; in fact **no Go code changes at
all** — the result-line contract already carries `location` (SPEC-034
mini-ADR 4/6), so only its *value* changes (a path → an `s3://…` URL) and the
value is opaque to the portal. Delivers:

1. Compose `minio` (S3-compatible object store) + a `createbuckets` one-shot
   that creates the artifact bucket idempotently; a `miniodata` named volume.
2. The custom Semaphore runner image gains the `mc` client (build layer, like
   `postgresql16-client` — the non-root runner cannot install at playbook
   time; SPEC-034 mini-ADR 2).
3. `playbooks/dump.yml`: after the sha256/size stat, **upload** the `.dump` to
   minio via `mc`, verify the stored object, and record `location =
   s3://<bucket>/<name>`. An upload (or verify) failure **fails the run** — a
   dump that isn't stored must not register an artifact claiming otherwise.
4. `infra/semaphore-bootstrap.sh` extends the `pgtarget-env` Environment with
   the object-store connection (engine-side creds, ADR-004): the `MC_HOST_*`
   credentialed alias URL + a writable `MC_CONFIG_DIR` + the bucket name. The
   playbook names no credential — `mc` reads the alias from its process
   environment exactly as `pg_dump` reads `PG*` (SPEC-034 mini-ADR 3).
5. `.env(.example)`: `MINIO_ROOT_USER/PASSWORD`, `DBPORTAL_BUCKET`, and the
   engine-side `MINIO_*` the bootstrap folds into the Environment.
6. O-1 resolved in DECISIONS.md (the Open item annotated, not deleted).

**Explicitly NOT here:** the real restore playbook fetching from `location` +
the M3 rehearsal (WU-036); the portal ever reading/proxying object bytes (it
never does — mini-ADR 1); retention/lifecycle policy on the bucket (M4);
prod-class object store / real S3 endpoint wiring (dev minio only; the
`location` URL and `mc` alias are the only things that change in prod);
exposing `location` on the artifacts API (SPEC-030 holds — it stays engine-side).

## Mini-ADRs (agent decisions, revisitable)

1. **The portal never touches object bytes — it stores and passes `location`
   strings.** The upload happens entirely engine-side, inside the playbook; the
   portal receives only the `location` string in the result line and persists
   it (SPEC-034 mini-ADR 6). It never uploads, downloads, proxies, or streams
   artifact bytes, and it holds no object-store credential. This keeps the
   ADR-004 secrets boundary intact (creds live only in the Semaphore
   Environment) and keeps the portal a control plane, not a data plane. WU-036's
   restore reads `location` and fetches engine-side the same way.

2. **`location` is a canonical `s3://<bucket>/<key>` URL; the object key is the
   artifact name.** `s3://dbportal-artifacts/appdb-20260712T…-….dump` is
   storage-addressing, portable across engines, and does not embed the
   engine-local `mc` alias name (which is config, not identity). WU-036 maps it
   back to an `mc` path with a trivial `s3://` → `<alias>/` rewrite. The bucket
   is `DBPORTAL_BUCKET` (default `dbportal-artifacts`).

3. **Upload failure FAILS the run (and a verify step makes "stored" a hard
   gate).** The playbook uploads with `mc cp` (non-zero exit aborts the play),
   then `mc stat --json` the object and asserts its size equals the dumped
   file's size. Only *after* both succeed does it emit the `DBPORTAL_RESULT`
   line. So a failed/partial upload → task error → `StateFailed` → run failed,
   ZERO artifact row, notify mail — the SPEC-034 failure path, now covering
   storage. The size assert is a cheap integrity gate that doesn't download the
   whole object; the recorded sha256 (of the exact bytes `mc cp` copied) is the
   object's hash by construction, proven mc-side in the live drill (AC).

4. **`mc` is baked into the runner image; the object-store alias is injected
   engine-side as `MC_HOST_<alias>`, never named in the playbook.** The runner
   runs as uid 1001 (non-root) so a playbook-time install is impossible — `mc`
   is added at build time (SPEC-034 mini-ADR 2 pattern). The credentialed alias
   URL `MC_HOST_dbportal=http://<key>:<secret>@minio:9000` lives ONLY in the
   Semaphore `pgtarget-env` Environment; `mc` auto-reads it from the process
   environment (mc's documented secret-safe mechanism — no `mc alias set` with
   creds in argv, nothing in the playbook, nothing to leak). `mc` masks the
   credential when it echoes a host, so task output carries no secret; the drill
   greps to confirm. `MC_CONFIG_DIR=/tmp/.mc` gives the non-root runner a
   writable config dir. Bucket name is not a secret (plain env / playbook var).

5. **The shared `artifacts` volume becomes staging only; the staged file is
   best-effort removed after a verified upload.** Object storage is now the
   source of truth for `location`; the local `.dump` is a transient staging
   copy pg_dump must write before `mc` can upload it. A best-effort `rm` after
   the result line (`failed_when: false`) keeps the volume from growing without
   ever failing a run that *did* store its artifact. The volume stays in compose
   (pg_dump needs a scratch path); WU-036 fetches from minio, not the volume.

## Interfaces

- `infra/compose.yaml`: add `minio` (server, 127.0.0.1:9000 API + :9001
  console, `miniodata` volume) and `createbuckets` (minio/mc one-shot: wait for
  minio, `mc mb --ignore-existing` the bucket, exit 0); new named volume
  `miniodata`. No change to the `semaphore`/`pgtarget`/`postgres`/`mailpit`
  services except that `semaphore` now reaches `minio:9000` on the compose net.
- `infra/semaphore.Dockerfile`: add the pinned `mc` client binary
  (`RELEASE.2025-08-13T08-35-41Z`) to the existing runner image; no other change.
- `infra/semaphore-bootstrap.sh`: the `pgtarget-env` Environment JSON gains
  `MC_HOST_dbportal` (credentialed alias, built with `jq` so the secret is
  escaped and never expanded into a log line), `MC_CONFIG_DIR`, and
  `DBPORTAL_BUCKET`. Built/updated in place exactly like the PG* vars.
- `playbooks/dump.yml`: after the non-empty assert, add upload + verify + emit
  (location = `s3://<bucket>/<name>`) + best-effort staging cleanup. The result
  JSON shape is unchanged (`{name,size_bytes,sha256,location}`); only `location`
  now holds an `s3://` URL.
- `.env(.example)`: `MINIO_ROOT_USER`, `MINIO_ROOT_PASSWORD`, `DBPORTAL_BUCKET`
  (compose + engine-side), and the engine-side `MINIO_ENDPOINT`/`MINIO_ACCESS_KEY`/
  `MINIO_SECRET_KEY` the bootstrap folds into `MC_HOST_dbportal`. SHAPE ONLY in
  `.env.example`; real dev values in gitignored `.env`.
- **Go: none.** `engine.Artifact.Location`, `semaphore.go` result parsing, and
  `finalize`'s `location` write are all from WU-034 and unchanged. GET
  /api/artifacts still does NOT expose `location` (SPEC-030).

## Behavior (testable)

1. **No Go behavior change (regression):** `npm run check` is green with ZERO
   Semaphore/minio dependence; MockEngine dump beats still register an artifact
   with `location` NULL (mock leaves Location empty; the golden flow is
   untouched). The existing `semaphore_test.go` parse cases and
   `TestArtifactLocationPersisted` already cover an arbitrary `location` string
   — an `s3://…` value round-trips identically.
2. **Live drill (AC evidence, VM, isolated portal):** portal dump on pgtarget
   through real Semaphore → an object in the minio bucket; the registry
   `location` = the `s3://…` URL; the recorded checksum == the object's actual
   sha256 (`mc cat … | sha256sum`, mc-side). No secret (minio keys, PG password,
   Semaphore token, webhook secret) in the portal DB dump, run params, audit
   rows, or task logs.
3. **Injected upload failure (AC evidence):** with minio stopped (or the bucket
   removed), a portal dump → pg_dump succeeds locally but `mc cp` fails → run
   FAILED, honest error, ZERO artifact row, notify mail "RUN-N failed — dump on
   pgtarget (dev)". A dump that wasn't stored registers nothing.

## Guardrails & audit

Everything above the engine seam is unchanged, so all four guardrail layers,
the prod ritual, authz, windows, and the audit trail apply exactly as before
(ADR-002). pgtarget is env=dev → ClassNonProd → the nonprod Semaphore adapter;
prod stays mock in dev (guardrail 3, structural). No secret enters the repo,
portal DB, params, audit rows, or logs — object-store creds are
Environment-injected engine-side (ADR-004, mini-ADR 4). Poll remains the
finalization authority; the artifact tx (SPEC-030) is untouched — `location`
just carries an `s3://` URL now.

## Live drill recipe (VM, isolated portal — [[live-drill-isolation]])

Persistent `.env` stays `PORTAL_ENGINE_NONPROD=mock`; the drill runs an
EPHEMERAL isolated portal (scratch DB + spare port + `PORTAL_ENGINE_NONPROD=
semaphore` + `dump:<id>,smoke:1`), driven over HTTP, torn down after. Bring the
stack up (`docker compose … up -d --build --wait`), re-run the bootstrap (folds
the `MINIO_*` into the Environment, prints the dump template id), then: dump on
pgtarget → success + object in minio + registry `location` = `s3://…`; `mc cat`
the object | `sha256sum` == recorded checksum; stop minio → dump → failed + no
artifact + mail; grep the DB dump + logs for every secret → absent. Demo :8080
untouched.

## Out of scope / deferred

- Real restore playbook fetching from `location` + M3 rehearsal → WU-036.
- Retention/lifecycle on the bucket (expire old dumps) → M4 / icebox.
- Prod-class object store + real S3 endpoint/region/TLS → post-MVP (only the
  `location` URL and the `mc` alias change).
- `npm run up` passing `--env-file .env` (the compose GOTCHA) → BACKLOG idea.
- Exposing `location` on the artifacts API → intentionally never (SPEC-030).

## Open questions

- **`mc` output masking** (does `mc cp`/`mc stat` ever echo the credentialed
  `MC_HOST` alias unmasked on error?) is pinned in the live drill against the
  running service; the interface above is stable regardless. Fallback if `mc`
  leaks the alias on some error path: add `no_log: true` to that task and keep
  a separate non-secret verify step for the visible log. Owner: agent.
