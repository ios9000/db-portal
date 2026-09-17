# Deploy runbook — pilot deployment (WU-046)

Turn the `build:release` binary (ADR-010) into a running, reboot-surviving
service on a fresh host, with **auth on**, **prod guardrails on**, and **failure
+ break-glass mail wired**. The portal is a single static binary carrying the
SPA; all state lives in Postgres, so the host itself is stateless and
disposable.

> Scope: the MockEngine pilot (ADR-002 forever-default). Wiring a real Semaphore
> engine (+ its target/object-store creds, ADR-004) is out of scope here — see
> `.env.example` and the SPEC-033/034/035 heads-ups in `docs/agent/STANDING-CONTEXT.md` §2.

## 0. Prerequisites

- A Linux host with **systemd** and outbound access to: the portal Postgres, the
  SMTP relay, and (for `ldap` mode) the AD/LDAP server.
- A **Postgres** database for the portal (empty; the portal owns its schema via
  migrations). A dedicated role with `CREATEDB` is not required for prod — only
  DDL on its own database.
- **TLS termination in front** (nginx/caddy/ELB) forwarding to the portal's
  `PORTAL_HTTP_ADDR`. The portal speaks plain HTTP; TLS is the reverse proxy's job.
- The release artifact, built on a trusted machine (below).

## 1. Build the release artifact (on a build host)

```
npm run build:release
```

Produces (ADR-010 + WU-046):
- `backend/bin/portal` — the static binary (SPA embedded), stamped with the git
  commit + build date (`./backend/bin/portal version` prints them).
- `backend/bin/portal.sha256` — checksum; verify the bytes on the target with
  `sha256sum -c portal.sha256`.

Copy both to the target host (e.g. `scp`).

## 2. Provision the host (once)

```
sudo useradd --system --no-create-home --shell /usr/sbin/nologin dbportal
sudo install -d -o root -g root -m 0755 /opt/dbportal
sudo install -d -o root -g dbportal -m 0750 /etc/dbportal
```

## 3. Install binary, config, unit

```
# binary (verify the checksum first)
sha256sum -c portal.sha256
sudo install -m 0755 portal /opt/dbportal/portal

# config: fill in real values, keep it unreadable to the world (holds secrets)
sudo install -m 0640 -o root -g dbportal infra/portal.env.template /etc/dbportal/portal.env
sudo "${EDITOR:-vi}" /etc/dbportal/portal.env          # set the [REQUIRED] vars

# systemd unit
sudo cp infra/dbportal.service /etc/systemd/system/dbportal.service
sudo systemctl daemon-reload
```

Fill in every `[REQUIRED]` var in `portal.env` (see `infra/portal.env.template`):
`PORTAL_DB_*`, and for a pilot `PORTAL_AUTH_MODE=ldap` with `PORTAL_LDAP_URL`
+ `PORTAL_LDAP_BIND_TEMPLATE`. **If a required var is missing the portal refuses
to boot with a clear error** (`config.Validate`) — it never starts a door nobody
can open. Set `PORTAL_NOTIFY_TO` so failures *and* break-glass use are mailed,
and keep `PORTAL_COOKIE_SECURE=true` (TLS in front).

## 4. Migrate the database

Run migrations once (and on every upgrade that adds them), as the service user
with the service env — **source the file, never word-split it** (an
`env $(cat … | xargs)` pipeline mangles values and chokes on comments;
WU-049 / m4-gate finding 3):

```
sudo -u dbportal bash -c 'set -a; . /etc/dbportal/portal.env; set +a; exec /opt/dbportal/portal migrate up'
sudo -u dbportal bash -c 'set -a; . /etc/dbportal/portal.env; set +a; exec /opt/dbportal/portal migrate status'
```

(`dbportal` group-reads the 0640 file, so no `sudo cat` indirection is needed;
sourcing inside the target user's shell also sidesteps sudo's env_reset.
`migrate` needs only `PORTAL_DB_*`; it does not require the auth vars.)

## 5. Grant the DBA role(s)

In `ldap` mode the portal grants **no roles at boot** (dev/fake modes seed dev
users; SPEC-021 mini-ADR 8). Every mutation route requires the `dba` role, so a
fresh pilot is login-only-read until the pilot DBAs are granted. Grants are
admin INSERTs into the portal DB (a `portal role` CLI is post-MVP) — run once
per DBA, username = the LDAP login name the portal sees:

```sql
INSERT INTO user_role (username, role_id)
SELECT 'jdoe', id FROM role WHERE name = 'dba'
ON CONFLICT DO NOTHING;
```

The `break-glass` account is granted by migration 0006 out of the box — no
INSERT needed for it.

## 6. Import the estate inventory

The fleet starts empty; the MVP import is CLI-only (SPEC-010, CSV — no UI
upload). Run as the service user with the service env (same sourcing form as
migrate):

```
sudo -u dbportal bash -c 'set -a; . /etc/dbportal/portal.env; set +a; exec /opt/dbportal/portal import /path/to/estate.csv'
```

CSV columns (header required):
`instance_name,cluster_name,env,platform,pg_version,size_gb,owner,maintenance_window`
— `platform` is `k8s_patroni` | `vm`; `env` is `dev` | `test` | `prod`;
`maintenance_window` is optional ("Sat 02:00-06:00"). Re-import is idempotent
(natural key `instance_name`) and re-platforms in place; a same-name cluster
with a different platform quarantines the row. Do **not** run `portal seed`
against a pilot — it generates a synthetic staging estate.

## 7. Enable + start (survives reboot)

```
sudo systemctl enable --now dbportal
systemctl status dbportal --no-pager
systemctl is-enabled dbportal      # -> "enabled" = it starts on every boot
```

`enable` links the unit into `multi-user.target`, reached on every boot — that
**is** the reboot-survival guarantee. `--now` also starts it immediately.

## 8. Verify the deploy

```
# health (unauthenticated)
curl -fsS http://127.0.0.1:8080/healthz            # {"status":"ok",...}

# auth is ON: the API rejects unauthenticated calls
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8080/api/instances   # 401

# version/provenance of the running build
/opt/dbportal/portal version                        # db-portal 0.0.1 (commit …, built …)
```

- **Auth on:** every `/api/*` route except health returns 401 without a session.
- **Roles + fleet live:** a §5-granted DBA logs in through the TLS front and
  sees the §6-imported instances; an ungranted login gets read-only 403s on
  mutations.
- **Prod ritual on:** a `POST /api/restore` / `POST /api/runs` against a `prod`
  target without the exact typed instance name is refused 400 (server-side).
- **Notify wired:** a failing run mails `PORTAL_NOTIFY_TO`; verify by watching
  the relay / a test message.
- **Boot log:** confirm `journalctl -u dbportal` shows `auth mode: ldap`, the
  resolved dotenv line ("no dotenv file found; using process env + defaults"),
  and — if `PORTAL_COOKIE_SECURE=false` — the plain-HTTP cookie Warn (it should
  NOT appear for the pilot config).

Reboot survival can be confirmed without a real reboot: `systemctl is-enabled`
is `enabled`, and `sudo systemctl restart dbportal` brings it back healthy (the
boot orphan-sweep reconciles any in-flight run).

## 9. Break-glass (emergency access)

When AD is down, the local `break-glass` account (if `PORTAL_BREAKGLASS_HASH` is
set) logs in. **Every use is alarmed three ways** (SPEC-020, WU-046): an
append-only `auth.break_glass` row, an `slog` Error line, and a **mail alarm to
`PORTAL_NOTIFY_TO`** ("SECURITY: break-glass account used", with the remote +
time — never the password). Treat an unexpected alarm as a possible compromise:
review the auth trail and rotate `PORTAL_BREAKGLASS_HASH`.

## 10. TLS / cookie guidance

The portal serves plain HTTP; terminate TLS in front and forward. Keep
`PORTAL_COOKIE_SECURE=true` so the session cookie is only sent over HTTPS — the
boot logs a Warn if it is false in `ldap` mode. Set `PORTAL_BASE_URL` to the
public `https://…` origin so mail links resolve.

## 11. Object-store retention (real engine only)

The maintenance loop reaps the artifact **registry** (SPEC-044); for a real
object store, add a **bucket lifecycle-expiry** rule matching
`PORTAL_ARTIFACT_RETENTION`, with `safety` artifacts in a lifecycle-**exempt**
key space (ADR-004/ADR-013 — the portal never issues `mc rm`). Under the mock
pilot, `location` is NULL, so there is nothing to expire.

## 12. Upgrade / rollback

- **Upgrade:** build a new artifact, `sha256sum -c`, `systemctl stop dbportal`,
  replace `/opt/dbportal/portal`, run `migrate up` if the release adds
  migrations, `systemctl start dbportal`. Verify §8.
- **Rollback:** migrations are backward-compatible within a release train; to
  roll back the binary, restore the previous `portal` and (only if the newer
  release added migrations you must undo) `migrate down` the delta. Prefer
  rolling forward.
