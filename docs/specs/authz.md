# SPEC-021 · AuthZ: DBA role, route guards, real actor

> WU-021. Builds on SPEC-020 (sessions put `authn.Identity` in the request
> context); closes the Phase-1 deferral ledger: SPEC-013 `run.cancel_requested`,
> SPEC-014 requester filter, SPEC-015 server-side prod ritual, m1-gate item 15
> (body cap). D2: DBA role only; D3: app-admin role deferred entirely.

## Mini-ADRs

1. **Roles live in the portal DB, not in directory groups.** `role` +
   `user_role` (keyed by directory username). The directory answers *who you
   are*; the portal answers *what you may do*. LDAP-group→role mapping is a
   post-MVP convenience — the portal-DB grant is the auditable source of
   truth either way, so it comes first. Migration seeds the `dba` role and
   ONE standing grant: `break-glass` — an emergency account that cannot act
   is not an emergency account.

2. **Guard split: mutations need the `dba` role; reads need only a session.**
   POST /api/runs and POST /api/runs/{id}/cancel sit behind `requireRole`.
   Read endpoints stay session-gated ("role-gated-lite"): the D2 threat is
   *actions* on databases, not fleet visibility, and D3's trajectory (app
   admins, no prod access) will want read-only sessions eventually.
   /api/auth/logout stays role-free — any session may end itself.

3. **Denials are auth_event rows, action `authz.denied`.** audit_event is
   run-centric (NOT NULL run_id/instance_id — hardened invariants); a denial
   has no run. auth_event is the actor-centric ledger and already
   append-only. `detail` = `"METHOD /path"` only — never body content
   (secrets rule). A store/DB error during the check is a 500, NOT a 403,
   and writes no denial row — "denied" must mean denied.

4. **The audit actor is an explicit parameter, not context magic.**
   `runs.Service.Start(ctx, StartRequest{Actor, ...})`,
   `Cancel(ctx, actor, id)`. The actor is too load-bearing for the trail to
   ride implicitly in ctx (a missed middleware would silently audit "").
   Handlers extract it via `authn.From(ctx)` (guaranteed by requireSession).
   WU-022's scheduler passes `schedule:<owner>` through the same explicit
   door. `run.finished` INHERITS the submitted row's actor (finalize already
   copies its stamps): actor means *on whose behalf*, not *which component
   wrote the row* — watcher/sweep finalizations carry the requester. The
   `local-dev` constant is deleted.

5. **Cancel writes `run.cancel_requested`, intent-first.** Name kept from
   SPEC-013's deferral (over the backlog's `run.canceled` shorthand):
   cancel is asynchronous and can race a success finish, so the row records
   the *accepted request* (final_status NULL, job_id stamped); the terminal
   outcome stays on `run.finished`. Written after local validation passes
   and BEFORE Adapter.Cancel — Start's record-intent-first pattern: the
   trail shows the attempt even if the engine then refuses. A canceled run
   therefore has THREE audit rows (supersedes SPEC-013 behavior 5's
   "exactly 2"). audit_event.action has no CHECK constraint; the new value
   needs no migration there.

6. **Prod ritual enforced server-side.** POST /api/runs gains `confirm`;
   `Start` returns `ErrProdUnconfirmed` (→ 400, distinct message) when the
   instance is prod and `Confirm != instance name` (exact, case-sensitive —
   same predicate as the drawer). Non-prod ignores `confirm`. No trail on a
   failed ritual: an authenticated DBA fat-fingering a name is friction
   doing its job, not a security event (role denials are). WU-022: the
   scheduler sets Confirm programmatically when firing prod schedules; the
   human ritual for scheduled prod work happens at schedule creation.

7. **Body caps (m1-gate item 15).** http.MaxBytesReader: 64 KiB on
   POST /api/runs (→ 413), 4 KiB on /api/auth/login. `reason` capped at 500
   chars server-side (→ 400) — the DB column is unbounded text; the cap is
   policy, not storage.

8. **Role provisioning follows PORTAL_AUTH_MODE.** Migration seeds only the
   role + the break-glass grant. At boot: `fake` mode ensures grants for
   dba1/dba2, `off` ensures local-dev (idempotent INSERT … ON CONFLICT DO
   NOTHING) — dev/demo work out of the box. `ldap` mode grants nothing:
   admins INSERT via psql (documented below); a grant UI/CLI is post-MVP.

## Data (migration 0006)

- `role`: `id PK`, `name text NOT NULL UNIQUE`. Seed: `('dba')`.
- `user_role`: `username text NOT NULL`, `role_id FK -> role NOT NULL`,
  `granted_at timestamptz NOT NULL DEFAULT now()`,
  `PRIMARY KEY (username, role_id)`. Seed: break-glass ↦ dba.
- `auth_event.action` CHECK extended with `'authz.denied'` (drop + re-add
  constraint; down restores the old list — dev-only caveat: fails if
  authz.denied rows exist, acceptable for a down migration).

Manual grant (ldap mode):
`INSERT INTO user_role (username, role_id) SELECT 'jane.doe', id FROM role WHERE name = 'dba';`

## Interfaces

- `authz.Store` (new package, pool-backed):
  `HasRole(ctx, username, role) (bool, error)`;
  `Grant(ctx, role string, usernames ...string) error` (idempotent);
  `Require(ctx, username, role, remote, detail string) error` — nil when
  held; records the denial and returns `ErrDenied` when not.
- `server.Deps` gains `Roles RoleGuard` (`Require` only — the middleware
  seam). `requireRole(guard, role)` middleware wraps the mutating subgroup.
- `runs.StartRequest{Actor, Instance, Operation, Reason, Confirm string,
  EngineParams map[string]string}`; `Start(ctx, StartRequest)`;
  `Cancel(ctx context.Context, actor string, id int64)`.
- `POST /api/runs` body: `{instance, operation, reason?, confirm?}` →
  400 `prod launch requires typing the instance name` on a failed ritual;
  400 `reason too long (max 500 characters)`; 413 on oversized body;
  403 `dba role required` (JSON, from middleware) for non-DBA.
- `GET /api/runs?requested_by=<username>` → `ListFilter.RequestedBy`
  (EXISTS against the run.submitted audit row; unknown value = empty list).

## Behavior (testable)

1. Session without dba: POST /api/runs → 403 JSON; ONE auth_event row
   (actor=username, action=authz.denied, detail="POST /api/runs"); no run
   row. Same for cancel. Reads still 200.
2. DBA on prod instance: no/wrong confirm → 400, zero rows written; exact
   name → 201 and BOTH audit rows carry the session username as actor.
3. Cancel by DBA on a live run: `run.cancel_requested` row (canceling
   actor, job_id set, final_status NULL) precedes the engine call; the
   finished row says canceled; three rows total.
4. Watcher and orphan-sweep finalizations inherit the submitted actor —
   `local-dev` appears nowhere.
5. `requested_by` filter: exact match only, composes (AND) with the other
   filters, unknown username → `"runs": []`.
6. Body > 64 KiB → 413; reason of 501 chars → 400; both leave no rows.
7. Grant is idempotent; HasRole(unknown user) = false; a failing role store
   → 500 (not 403) and no denial row.
8. Migration 0006 up seeds role+grant; down leaves 0005 intact.
9. Golden flow: dba1 (auto-granted in fake mode) launches; run JSON
   requested_by == "dba1".

## UI slice contract

- `api.ts`: `startRun(instance, operation, reason, confirm?)`;
  `RunFilter.requestedBy` → `requested_by` query param.
- LaunchDrawer: already collects the typed name — send it as `confirm` on
  prod; server 400/403 messages surface in the drawer's existing error slot.
- Activity: requester filter rides URL param `by` (pattern of state/env/op);
  clicking a Requester cell sets it; an active filter shows a clearable
  chip; counts toward `filtersActive`; CSV export inherits it for free
  (exports the on-screen rows).

## Out of scope / deferred

- Role admin UI or CLI; LDAP-group→role mapping → post-MVP.
- Role-gated reads, per-env permissions, app-admin role → D3, post-MVP.
- Denial rate alarming / lockout policy → icebox (with WU-020's).
- Server-side CSV export, date filters → icebox (SPEC-014).
