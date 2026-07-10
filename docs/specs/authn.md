# SPEC-020 · AuthN — LDAP bind against AD, server sessions

> Groomed 2026-07-10 for WU-020. Authority chain: DECISIONS.md (D2/D3) →
> ARCHITECTURE.md §2 Identity → this file. AuthZ/roles are explicitly NOT
> here (WU-021): any authenticated user passes every route this WU.

## Scope

Login page + server-side sessions in front of the whole API: AD LDAP bind
(the portal NEVER stores or logs AD passwords), a fake in-process directory
for dev/CI (same seam pattern as MockEngine), ONE break-glass local account
whose every use writes an alarmed audit event, and the golden-flow e2e
authenticating for real. Explicitly NOT here: roles/route guards + real
audit actor (WU-021 — runs keep `actor='local-dev'`), SSO (post-MVP),
account lockout/rate-limit policy beyond a failure delay (icebox), email
alarm on break-glass use (icebox — the audit row + error log land now).

## Mini-ADRs (agent decisions, revisitable)

1. **Sessions = portal-DB table + opaque cookie token; hash stored.**
   256-bit random token in an `httpOnly` cookie; the DB stores only
   `sha256(token)` (ground rule: secrets never enter the portal DB —
   a leaked table must not yield usable cookies). DB-backed (not
   in-memory) so sessions survive the restarts that already bit MockEngine
   state; server-side rows (not JWT/signed cookies) so logout and
   break-glass hygiene are real revocations, with no signing-key
   management. Absolute TTL (default 12h, config), no sliding renewal in
   MVP; `last_seen_at` updated for observability only.
2. **Directory seam.** `internal/authn.Directory` interface —
   `Bind(ctx, username, password) (Identity, error)` — with two
   implementations: `LDAP` (production) and `Fake` (in-process map,
   dev/CI). Mirrors the engine.Adapter/MockEngine pattern: only tests and
   fake mode reference Fake concretely; the service and handlers see the
   interface.
3. **go-ldap/v3, direct template bind.** Dep = `github.com/go-ldap/ldap/v3`
   (de-facto Go LDAP client). MVP bind strategy = ONE round trip with the
   user's own credentials against `PORTAL_LDAP_BIND_TEMPLATE` (e.g.
   `%s@corp.example.com` for AD UPNs, or a full DN template). Chosen over
   search+bind because search needs a stored service-account credential —
   template bind keeps the portal holding ZERO directory secrets. Revisit
   (add search+bind) only if the estate's UPNs don't map from usernames.
   TLS required: `ldaps://` URL or StartTLS; `PORTAL_LDAP_INSECURE=true`
   (dev only) is the sole opt-out, and main logs a warning when set.
4. **Auth mode is a tri-state that fails closed.** `PORTAL_AUTH_MODE` =
   `ldap` (DEFAULT) | `fake` | `off`. Unset/unknown → `ldap`; an
   unconfigured LDAP URL then means every login fails and every API call
   stays 401 — misconfiguration locks the door, never opens it ("bypass
   flag off = no bypass"). `fake` = login flow against the in-process
   directory (fixed dev users, fixture-style credentials — they are
   fixtures, not secrets, same standing as mock_fail_at). `off` = full
   bypass for demos: every request is authenticated as `local-dev`,
   no login page needed; main logs a loud warning at startup.
5. **Break-glass = one local account, checked before the directory.**
   Username `break-glass`; bcrypt hash in `PORTAL_BREAKGLASS_HASH`
   (empty = account disabled — the default). Works in `ldap` AND `fake`
   modes (its whole point is AD being down). Every successful break-glass
   login writes `auth.break_glass` to the auth trail AND slog Error —
   it must be impossible to use quietly. bcrypt via
   `golang.org/x/crypto/bcrypt` (no cost tuning in MVP).
6. **Auth events get their own append-only table, not audit_event.**
   `audit_event` is run-centric with NOT NULL run_id/instance_id — the
   invariants WU-016/017 just hardened. Relaxing them for auth rows would
   weaken the run trail; instead migration 0005 adds `auth_event`
   (actor, action, remote, detail) with the SAME immutability pattern as
   0003/0004: BEFORE UPDATE/DELETE row trigger + BEFORE TRUNCATE statement
   trigger raising "auth_event is append-only", plus REVOKE. Actions:
   `auth.login`, `auth.login_failed`, `auth.logout`, `auth.break_glass`.
   `auth.login_failed` records the attempted username (identifier, not a
   secret) and remote — NEVER the password. A fixed ~300ms delay on any
   failed login blunts online guessing; real lockout policy is icebox.
7. **CSRF posture: SameSite=Lax + JSON-only mutations.** The session
   cookie is `httpOnly, SameSite=Lax, Path=/` (+ `Secure` when
   `PORTAL_COOKIE_SECURE=true` — default off, dev is plain HTTP). Lax
   blocks cross-site POSTs; every mutating endpoint already requires an
   `application/json` body, which a cross-site form can't produce. A CSRF
   token header is deliberately deferred until something breaks this
   posture (e.g. a GET with side effects — none exist).
8. **Golden flow authenticates for real.** The e2e wires the Fake
   directory into the real router, asserts an unauthenticated request →
   401 first, then logs in via POST /api/auth/login with a cookie jar and
   runs every existing beat through the authenticated client — the canary
   now covers the auth path end to end. No test-only bypass in the e2e:
   `off` mode exists for demos, not for dodging the canary.

## Data (migration 0005, goose embed)

- `session`: `id PK`, `token_hash text NOT NULL UNIQUE` (sha256 hex),
  `username text NOT NULL`, `display_name text NOT NULL`,
  `created_at timestamptz NOT NULL DEFAULT now()`,
  `expires_at timestamptz NOT NULL`, `last_seen_at timestamptz NOT NULL
  DEFAULT now()`. Expired rows are deleted lazily on validation misses
  (a sweep job is icebox — volume is trivial at MVP scale).
- `auth_event`: `id PK`, `ts timestamptz NOT NULL DEFAULT now()`,
  `actor text NOT NULL`, `action text NOT NULL CHECK (action IN
  ('auth.login','auth.login_failed','auth.logout','auth.break_glass'))`,
  `remote text NOT NULL DEFAULT ''`, `detail text NULL`.
  Trigger `auth_event_immutable`: BEFORE UPDATE OR DELETE (row) and
  BEFORE TRUNCATE (statement) → RAISE EXCEPTION; plus
  `REVOKE UPDATE, DELETE, TRUNCATE ON auth_event FROM portal`.
- Down: drop both tables (+ trigger function), restore grants.

## Interfaces

- `POST /api/auth/login` `{username, password}` → 200 `{username,
  display_name}` + `Set-Cookie: portal_session=…`; 401 bad credentials
  (uniform body — no user-exists oracle); 400 malformed body. Exempt from
  the session requirement. Writes `auth.login` / `auth.break_glass` /
  `auth.login_failed`.
- `POST /api/auth/logout` → 204, session row deleted, cookie cleared.
  Requires a session (401 otherwise); writes `auth.logout`.
- `GET /api/auth/me` → 200 `{username, display_name}` or 401. The SPA's
  bootstrap probe. Exempt from nothing — this IS the session check.
- Middleware `requireSession` wraps the whole `/api` subtree EXCEPT
  `/api/auth/login`; `/healthz` and the SPA/static tree stay public (the
  login page must load). 401 body: `{"error":"authentication required"}`.
  In `off` mode the middleware stamps `local-dev` and passes.
- `internal/authn`: `Directory` (mini-ADR 2), `Identity{Username,
  DisplayName}`, `Service` (owns bind → break-glass check → session CRUD
  → auth_event writes; the ONLY thing handlers talk to).
  `server.Deps` gains `Auth server.Authenticator` (handler-facing
  interface: Login/Logout/Validate) — same seam style as InstanceReader.
- Request identity travels via context (`authn.From(ctx)`) for WU-021 to
  pick up; runs keep `actor='local-dev'` until then.

## Config (config.Load + .env.example; no secrets committed)

`PORTAL_AUTH_MODE` (ldap|fake|off, default **ldap**) ·
`PORTAL_LDAP_URL` (ldaps://host:636 or ldap://host:389+StartTLS) ·
`PORTAL_LDAP_BIND_TEMPLATE` (one `%s`) ·
`PORTAL_LDAP_INSECURE` (default false; true logs a warning) ·
`PORTAL_BREAKGLASS_HASH` (bcrypt; empty = disabled) ·
`PORTAL_SESSION_TTL` (default 12h) · `PORTAL_COOKIE_SECURE` (default false).

## Behavior (testable)

1. Unauthenticated `GET /api/instances` (and every /api route) → 401 JSON;
   `/healthz` → 200; `GET /` and `/login` serve the SPA.
2. Fake-mode login with seeded creds → 200 + cookie; the same client then
   uses the API normally; `auth.login` row written with remote.
3. Bad password (fake or LDAP) → 401 uniform body, `auth.login_failed` row
   (username + remote, no password anywhere — assert log capture too),
   response delayed ~300ms.
4. Logout → 204; the old cookie is 401 afterwards (row gone, not just
   cookie cleared). `auth.logout` row.
5. Expired session (TTL elapsed) → 401; row lazily deleted.
6. Break-glass with correct password while the directory refuses/errors →
   200 + session; `auth.break_glass` row + slog Error. Empty
   `PORTAL_BREAKGLASS_HASH` → break-glass always 401.
7. Mode `off` → no login needed, API serves, identity = `local-dev`.
   Mode unset → behaves as `ldap`; with no LDAP URL every login is 401
   and the API stays locked (fail closed).
8. auth_event immutability: UPDATE/DELETE/TRUNCATE all raise (same test
   pattern as audit_event).
9. LDAP path: bind template renders the username; TLS enforced unless
   the insecure flag is set (unit-test the dial options; a live AD is
   out of test scope — the Directory seam isolates it exactly like the
   real Ansible engine).
10. Golden flow: 401 before login, then the full hero flow authenticated
    via cookie jar (mini-ADR 8).

## UI (WU-020 slice)

- `/login` route: centered card, username + password, error line on 401
  ("Check your credentials — or use break-glass if AD is down" is TOO
  chatty; plain "Sign-in failed" only), submit disabled while pending.
  No self-service anything (AD owns passwords).
- App bootstrap: probe `/api/auth/me`; 401 anywhere (bootstrap or any
  later API call) → redirect to `/login` preserving the intended path;
  after login, return to it.
- Shell shows the signed-in identity (display_name) + a Sign out button
  (calls logout, lands on /login).
- api.ts: 401s surface as ApiError(401) — a tiny subscription point lets
  the shell react centrally instead of per-page checks.

## Out of scope / deferred

- Roles, route guards, real audit actor, `?requested_by=` filter → WU-021.
- Email alarm on break-glass use → icebox (audit row + error log now).
- Account lockout / adaptive rate limiting → icebox (fixed delay now).
- Session sweep job (expired-row GC beyond lazy delete) → icebox.
- SSO / OIDC → post-MVP (ARCHITECTURE §2).

## Fixture / demo

Fake mode: log in as a seeded dev user → badge in the shell → run the M1
demo authenticated → sign out → API 401s. Break-glass demo: set a bcrypt
hash in .env, stop nothing, log in as `break-glass` → psql shows the
alarmed `auth.break_glass` row; UPDATE on it → "auth_event is append-only".
