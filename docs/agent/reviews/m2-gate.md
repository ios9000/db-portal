# M2-gate review — findings record (2026-07-10, s12)

Method: the M1-lesson light shape (see the cost-sensitivity agreement): workflow
`wf_7ee53a3d-c48` ran **5 independent Sonnet 5 reviewers**, one per dimension
(security-authn, security-authz, concurrency-scheduler, correctness-backend,
frontend+spec-conformance), each briefed with the Phase-2 specs, the ground rules
that must hold, and the deliberate-deferral list. **No verifier agents** — every
finding below was verified inline by the architect against the cited code before
it entered this record. Cost: 510,401 subagent tokens / 195 tool calls / ~8.7 min
(M1's original design was ~50 agents; this was 5).

Result: 11 findings raised, **11 confirmed, 0 refuted** (two re-graded in
severity during inline verification, noted below). No criticals. Verdict:
**GATE PASSES with fix WUs** — same protocol as M1: WU-024/WU-025 land before
any Phase-3 WU starts.

## Confirmed — HIGH

1. **Scheduled prod ritual survives instance env reclassification** —
   `backend/internal/schedule/executor.go:119` + `backend/internal/inventory/import.go:167`.
   The executor fires with `Confirm: d.instance`, which is *by construction* equal
   to the instance name, so `runs.Start`'s ritual check can never fail for a
   scheduled fire — regardless of the instance's CURRENT env. Inventory re-import
   legitimately updates `env` in place (UPDATE keyed by name, schedules untouched).
   A schedule created on a test instance keeps firing, ritual-free, after the
   instance is promoted to prod — silently defeating "prod ritual is enforced
   server-side for BOTH launch and schedule creation" (SPEC-021 mini-ADR 6 /
   SPEC-022 mini-ADR 8). Verified: import's upsert does update env; no code path
   revisits schedules on env change; no test covers promotion. Fix: persist the
   creation-time `confirm` string on the schedule row and pass it verbatim at
   fire time — a promoted instance then fails the ritual loudly
   (`last_fire_status='error'`) until a human re-confirms by re-creating.
   → **WU-024**

## Confirmed — MEDIUM

2. **Disable/Delete racing an in-flight fire corrupts `next_fire_at`** —
   `backend/internal/schedule/executor.go:145` (stamp). fireDue snapshots due rows,
   then fires; `SetEnabled(false)`/`Delete` can interleave. The stamp UPDATE has no
   `enabled` guard, so a concurrent disable gets its `next_fire_at = NULL`
   overwritten with a real time — breaking the "NULL iff disabled" invariant
   (display-only harm: the due query still keys on `enabled`; re-enable recomputes).
   The fire itself landing seconds after a Disable click is accepted (same class as
   cancel racing a success finish); the delete race is a harmless 0-row stamp.
   *Re-graded HIGH → MEDIUM during inline verification* (no fire can result from
   the corrupted value; invariant + display drift only). Fix: stamp
   `next_fire_at = CASE WHEN enabled THEN $5 ELSE NULL END` + re-check enabled at
   fire() entry to shrink the fire-after-disable window. → **WU-024**

3. **Overlap protection is per-schedule, not per-instance** —
   `backend/internal/schedule/executor.go:95`. The overlap probe checks only the
   firing schedule's own `last_run_id`, while SPEC-022 mini-ADR 4's *rationale* is
   instance load ("overlapping dumps of the same instance are a load hazard") and
   the spec explicitly blesses sibling schedules on one instance. Two siblings due
   in the same tick dump the same prod instance concurrently. Implementation
   matches the spec as written — this is a spec-vs-rationale design gap, graded
   MEDIUM (needs two schedules on one instance with coinciding fire times). Fix:
   widen the probe to "any live run on this instance" (which also stops a
   scheduled fire from piling onto a live button-press run) + amend mini-ADR 4;
   full cross-resource locking stays M4 as planned. → **WU-024**

4. **LDAP username casing fragments identity** — `backend/internal/authn/ldap.go:65`.
   `Identity{Username: username}` echoes the typed casing; AD binds are
   case-insensitive but `user_role` lookups and every actor stamp are
   case-sensitive. "Dana.Baker" authenticates fine, then gets a wrong 403 (grant
   is under "dana.baker") — or, with double grants, one human becomes two actors
   on the append-only trail. Fix: canonicalize (lowercase) the username once at
   the authn seam, before session mint / audit / authz. → **WU-025**

5. **One stuck fire wedges the whole executor** —
   `backend/internal/schedule/executor.go:68`. Fires run sequentially on the
   process ctx with no per-fire timeout; a hung DB call (or a hung engine adapter
   once WU-033 replaces the mock) freezes every future schedule AND blocks
   graceful shutdown. Latent today (MockEngine can't hang), real at M3. Fix:
   per-fire `context.WithTimeout` — composes safely with Start's existing
   stranded-job repair. → **WU-024**

6. **Bootstrap treats any /api/auth/me error as signed-out** —
   `frontend/src/App.tsx:23`. A 500/network failure during bootstrap renders the
   login page instead of an error state — an authenticated DBA on a degraded
   backend is silently shown a sign-in form (that also won't work), instead of
   the "unavailable + retry" idiom every other page uses. SPEC-020 says 401 →
   login. Fix: branch on status 401 vs everything else. → **WU-025**

7. **Sign-out lies on failure** — `frontend/src/components/Shell.tsx:30`.
   `logout()` failures are swallowed and `onSignOut()` always fires; the cookie
   is httpOnly so the client cannot clear it — after a failed logout the UI
   shows signed-out while the session cookie + DB row stay live (reload = back
   in). On a shared bastion workstation that's a real exposure. Fix: proceed
   locally only on 401 (session already gone); otherwise surface "could not
   confirm sign-out" and stay signed in. → **WU-025**

8. **SetEnabled(true) on an enabled schedule re-rolls jitter** —
   `backend/internal/schedule/schedule.go:191`. No current-state check: a
   redundant PATCH recomputes `next_fire_at` (fresh jitter draw, ±60s) — and if
   it lands in the due-to-fire window (next_fire_at just passed, tick pending),
   the recompute moves the fire a whole period out (@daily → a day). Fix:
   short-circuit when the requested state equals the current state. → **WU-024**

## Confirmed — LOW

9. **Schedules page: create can race the initial list fetch** —
   `frontend/src/pages/Schedules.tsx:141`. "New schedule" is gated on the
   catalog, not the list; a created row can be overwritten by the still-in-flight
   initial GET's response. *Re-graded MEDIUM → LOW* (needs a slow list fetch +
   a user who beats it; harm is display-until-refresh). Fix: also gate the
   button on `schedules !== null`. → **WU-025**

10. **PORTAL_COOKIE_SECURE=false boots silently in ldap mode** —
    `backend/internal/config/config.go:47`. The analogous PORTAL_LDAP_INSECURE
    gets a loud boot warning; a prod-shaped deployment that forgets the Secure
    cookie flag gets nothing. Fix: warn at boot when AuthMode=="ldap" &&
    !CookieSecure. → **WU-025**

11. **PATCH /api/schedules/{id} answers 400 for an oversized body** —
    `backend/internal/server/schedules_http.go:115`. Missing the
    `*http.MaxBytesError` → 413 branch its sibling handlers have. → **WU-024**

## Notes

- Finding precision 11/11 vs M1's 12/15 — the difference is the deferral list:
  every "finding" M1-style reviewers would have burned on (no session GC, no
  lockout, CSRF posture, schedule-change ledger) was pre-listed as deliberate.
- Nothing touched the append-only trails, the denied-means-denied rule, the
  explicit-actor rule, or warn-never-block — the four ground rules the gate
  most needed to hold.
