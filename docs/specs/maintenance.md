# SPEC-044 · Maintenance & retention jobs

> Status: authoritative for WU-044 (Phase 4, M4). Written just-in-time.
> Precedes: `ARCHITECTURE.md` (no maintenance subsystem existed); records the
> retention policy in `DECISIONS.md` ADR-013. Business precedence unchanged
> (D1–D7). MockEngine stays the default (ADR-002) — every sweep is portal-DB
> work exercised under mock.

## Problem

A portal that runs for months accumulates rows no path ever removes: expired
`session` rows (TTL is enforced only lazily, on the next read of that exact
token — SPEC-020), `standard` dump artifacts long past any usefulness, and an
ever-growing audit trail. The ROADMAP M4 exit list calls for a retention job;
the icebox carries the session-GC and artifact-enforcement debt; and the
m3-gate finding 8 leaves real dump bytes orphaned on the runner volume when a
post-`pg_dump` task fails. This WU adds the periodic-sweep subsystem a
long-running deployment needs, on the same tick-loop lifecycle the scheduler
already uses, and fixes the playbook orphan.

## Decisions (mini-ADRs)

### 1. A new `internal/maintenance` Service, ticked like the scheduler

Sweeps are a distinct concern from schedule firing, so they get their own
`maintenance.Service` with a `Run(ctx)` tick loop, wired in `main` as
`go maint.Run(ctx)` beside `go sched.Run(ctx)`. It sweeps ONCE at boot (reap
what accumulated during downtime) then every `PORTAL_MAINTENANCE_INTERVAL`
(default 1h). Each sweep pass is best-effort and independent — one failing pass
logs and never skips the others — and returns a `Report` so tests assert counts
directly. Folding sweeps into `schedule.Service` was rejected: it would muddy
that type's single responsibility (schedule rows) and couple two lifecycles.

### 2. Audit retention = retain in-DB; cold-storage archival is post-MVP

`audit_event` and `auth_event` are append-only **by database trigger** (0003–
0005: `BEFORE UPDATE OR DELETE`/`TRUNCATE` RAISE, plus REVOKE) — a DELETE
cannot succeed, by design. So a retention job for the audit trail can only mean
archive-then-purge, and shipping to cold storage is an explicit icebox item
("5-year audit shipping to object storage") beyond MVP. The policy (ADR-013):
**the security ledgers are retained in-database for the pilot's lifetime; the
maintenance loop NEVER mutates them** (respecting the trigger and audit
integrity — AC-1's "no silent mutation"). The audit pass is therefore
*observational*: it reports the oldest event's age and logs a notice when it
exceeds `PORTAL_AUDIT_RETENTION` (default 365d) so an operator knows when
cold-storage archival becomes necessary. `run` rows are pinned by the audit FK
(`audit_event.run_id NOT NULL`) and so are equally immortal — deliberately: the
audit trail is the long-term operational record.

### 3. Artifact enforcement = trim the registry; object bytes are the store's TTL

The reap deletes `standard` artifact rows older than `PORTAL_ARTIFACT_RETENTION`
(default 90d) from the `artifact` registry (the "what can be restored" list),
**structurally preserving `safety`** — the WHERE clause matches
`retention_class = 'standard'`, so a safety artifact can never be selected, let
alone deleted. Each deletion is audited: the same statement inserts an
`artifact.reaped` `audit_event` row linked to the artifact's origin run (the
`run.cancel_requested` precedent — audit_event already carries actions beyond
submitted/finished, and INSERT is allowed; only mutation is blocked). `run`'s
own `artifact_*` columns STAY — they record what the run produced (history),
distinct from the restorable registry (SPEC-030).

Object bytes: per ADR-004 the portal never holds object-store credentials and
never issues `mc rm` — so it does NOT delete objects directly. Byte-level TTL is
the object store's own **lifecycle-expiry** policy, configured engine-side
(the deploy runbook, WU-046), with `safety` written to a lifecycle-exempt key
space; the portal enforces the registry and the store enforces the bytes,
kept consistent by matching the retention age. For the pilot (MockEngine,
`location` NULL) there is no object store, so this is a documented operational
requirement, not pilot code. The reap logs at WARN any non-NULL `location` it
removes from the registry, so an orphaned object is never silent.

### 4. Session GC = bulk-delete expired rows

`DELETE FROM session WHERE expires_at < now()`. `session` is not append-only, so
this is a plain delete; the predicate can never match a live session
(`expires_at > now()`), so AC-3's "a live session is never reaped" is
structural. Complements SPEC-020's lazy per-token expiry (which only reaps a
token when it is re-presented). Not audited — a session row is not a security
event; the count is logged.

### 5. Disabled-when-nonpositive, defensively

A retention duration of `0` would mean "reap everything" — catastrophic for the
artifact sweep. So `ArtifactRetention <= 0` DISABLES that pass (logged), and
`Interval <= 0` disables the whole loop. Only a positive, explicit age reaps.

### 6. dump.yml orphan fix (m3-gate finding 8)

Wrap the post-`pg_dump` tasks (`run pg_dump` … `emit result`) in a `block:` with
an `always:` that removes the staging file (`file: state=absent`,
`failed_when: false`). The old best-effort trailing `rm` only ran on success, so
an `mc cp`/stat failure orphaned the real dump bytes on `/artifacts` forever;
`always:` runs on both paths, so a failed upload cleans up too. Playbook-only;
the emit-only-after-verified-upload contract (WU-035) is unchanged.

## Behaviors (acceptance)

1. **B1 (audit, AC-a).** The audit pass reports the oldest `audit_event` age and
   logs when it exceeds `PORTAL_AUDIT_RETENTION`; it deletes/mutates NOTHING —
   `audit_event`/`auth_event` row counts are unchanged after a sweep, and a
   direct DELETE still raises (the trigger holds).
2. **B2 (artifacts, AC-b).** `standard` artifacts older than the retention age
   are removed from the registry AND an `artifact.reaped` audit row is written;
   `safety` artifacts of any age are NEVER removed; artifacts younger than the
   age stay. `ArtifactRetention <= 0` reaps nothing.
3. **B3 (sessions, AC-c).** Expired `session` rows are reaped each sweep; a
   session with `expires_at > now()` is never touched.
4. **B4 (playbook, AC-d).** dump.yml `--syntax-check` passes; a failure after
   `pg_dump` removes the staging file (block/always), a success still removes it.
5. **B5 (lifecycle).** `Run` sweeps at boot then every interval, best-effort;
   the golden flow is unchanged and green.

## Verify

- `-race` tests per sweep: audit no-mutation + age report; artifact
  standard-reaped/safety-preserved/age-boundary/disabled; session
  expired-reaped/live-kept. dump.yml `--syntax-check` EXIT:0 (runner image).
- `npm run check` green (golden flow not-skipped).
