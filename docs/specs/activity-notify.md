# SPEC-014 · Activity view + failure email

> Groomed 2026-07-07 for WU-014. Authority chain: DECISIONS.md (D5, D7) →
> ARCHITECTURE.md §2 (Notifications: "Failure/halt/window-warn to DBA list.
> Content: who/what/where/status + run link. Never params or log excerpts.")
> → this file. Design brief Screen 6 drives the Activity rework (adapted:
> no approval rows, no "warnings" state — neither exists in MVP).

## Scope

Two halves of "the run didn't succeed and somebody should know":
(a) a failure email through mailpit, hooked into the single finalization
seam in `internal/runs`; (b) the Screen 6 Activity upgrade — "Now running"
section, filter chips, requester column, CSV export. Explicitly NOT here:
approval rows (post-MVP), `succeeded with warnings` (no such run state),
real recipients/identity (actor stays `local-dev` until WU-020/021),
window-warn emails (WU-023), `🔔 Notify me` per-run toggle (post-MVP; the
DBA-list mail is unconditional).

## Mini-ADRs (agent decisions, revisitable)

1. **Notify from `finalize()`, on any not-success terminal state.**
   finalize() is the single point every ending passes through (watcher
   terminal, engine refusal at submit, cancel, orphan sweep) — one hook
   covers all failure shapes, which is why SPEC-012 rejected lazy
   sync-on-read. ARCHITECTURE §2 names *failure*; `canceled` is included
   deliberately (an aborted operation is operationally noteworthy, and it
   gives the demo/verify a kill path that needs no test-only param
   injection). `success` is silent. Revisit when WU-022 schedules bring
   unattended runs (success digests?).
2. **Email is best-effort and never blocks finalization.** The send runs
   after commit, in a goroutine tracked by the service WaitGroup (tests can
   drain it); SMTP errors are logged, not returned. The audit trail is the
   authority; email is a courtesy copy. A dead mailpit must not fail a run.
3. **Content = who/what/where/status + link, and NOTHING else.** Not the
   operator reason (free text), not the run's error message (engine error
   strings can embed command output — a leak channel per D7), not params,
   not log excerpts. The link leads to the portal, which owns detail and
   (post WU-021) authorization.
4. **Notifier is an interface field on runs.Service** (`nil` = disabled,
   same pattern as PollInterval); `internal/notify.Mailer` implements it
   over plain SMTP (dial timeout, no auth, no TLS — that's mailpit; a real
   relay is a config concern for the ops deployment, not MVP). Empty
   `PORTAL_NOTIFY_TO` disables notifications; main logs which.
5. **List filters are server-side and composable.** `state`, `env`,
   `operation` join the existing `instance` param on GET /api/runs, ANDed.
   Unknown values are filters that match nothing (SPEC-012 behavior 8
   semantics), never 400/404. `requested_by` joined them in WU-021
   (SPEC-021). Still deferred: date range + pagination (limit 50 makes a
   range picker theater; icebox until a history WU raises the cap),
   server-side export.
6. **Requester comes from the audit trail.** Run JSON gains
   `requested_by` = the `run.submitted` event's actor — the read model
   surfaces audit data instead of duplicating actor onto the run row.
7. **Export = client-side CSV of the current filtered view** (≤50 rows,
   exactly what the table shows: id, state, operation, instance,
   environment, requested_by, submitted_at, started_at, finished_at — no
   error text, no params). A full audit export beyond the UI cap is
   icebox'd with the pagination work. Fields opening with `=`, `+`, `-`,
   `@` or TAB are neutralized with a leading `'` (OWASP CSV injection,
   WU-019 — latent until WU-020 puts real usernames in requested_by);
   a filter change drops the on-screen list until the new fetch lands,
   so the export can never contain another filter's rows.

## Config (config.Load + .env.example; no secrets committed)

- `PORTAL_SMTP_HOST` (default `127.0.0.1`) / `PORTAL_SMTP_PORT` (default
  `1025`) — mailpit in compose.
- `PORTAL_SMTP_FROM` (default `portal@db-portal.local`).
- `PORTAL_NOTIFY_TO` — comma-separated DBA list; **empty = notifications
  off** (the dev default in .env.example points at a mailpit-caught list).
- `PORTAL_BASE_URL` (default `http://localhost:8080`) — prefix for the
  `/runs/{id}` link in mail.

## Interfaces

- `runs.Notifier` iface: `RunEnded(ctx, run Run) error`; `Service.Notifier`
  field, nil-safe. Fires only for `failed` / `canceled`.
- `notify.Mailer{Addr, From, To, BaseURL}` implements it. Subject:
  `[db-portal] RUN-<id> <state> — <operation> on <instance> (<env>)`.
  Plain-text body: status line, operation, instance+env, requested by,
  run link. Nothing else (mini-ADR 3).
- `GET /api/runs?instance=&state=&env=&operation=` — all optional, ANDed;
  run JSON gains `requested_by` (string, from the submitted audit event).
- `runs.Service.List(ctx, ListFilter{Instance, State, Environment,
  Operation})` replaces the single-string signature.

## Behavior (testable)

1. A run that finalizes `failed` (mock_fail_at) produces exactly one
   notification carrying run id, instance, env, operation, actor, state —
   and the mail body/subject contain **no** reason text, error text, or
   params.
2. A canceled run notifies; a successful run does NOT.
3. Engine refusal at submit and orphan sweep both notify (they finalize
   `failed` through the same seam).
4. A failing notifier (SMTP down) leaves the run finalized and audited
   exactly as before — error logged only.
5. Mailer speaks real SMTP: an in-test SMTP server receives the message
   with the configured From/To, the subject names RUN-<id> + state, the
   body contains `<base>/runs/<id>`.
6. List filters: `state=failed` returns only failed runs; `env=`,
   `operation=` compose with `instance=`; unknown values → empty list.
7. `requested_by` = `local-dev` on run JSON (list + detail).
8. Audit immutability still holds: UPDATE on an audit_event row raises
   (re-asserted in the live verify — the trigger is WU-012 code).

## UI (Screen 6 minus approvals)

- Toolbar of filter chips (URL search params, MyDatabases pattern):
  Status (All/Queued/Running/Success/Failed/Canceled), Env
  (All/dev/test/prod), Operation (All + catalog entries). Server-side via
  fetchRuns params; poll keeps honoring active filters. `Export CSV`
  button right-aligned — downloads the current view (mini-ADR 7).
- **Now running** section above the history: non-terminal runs as live
  rows — RunStatus chip, RUN-id link, operation, instance + EnvBadge,
  live elapsed (1s tick, RunDetail pattern), indeterminate progress bar.
  Section hidden when empty.
- History table = terminal runs; adds Requester column (`requested_by`).
  Empty states: no runs at all vs nothing-matches-filters get distinct
  copy.
- Poll cadence unchanged: 3s while anything is live, 10s idle.

## Out of scope / deferred

- Approval rows, warnings state → post-MVP (no approvals, no warn state).
- ~~`user` filter chip + real requester identities~~ → landed in WU-021
  (SPEC-021: `requested_by` param; `by` URL param in Activity).
- Date-range filter, pagination past 50, server-side audit export → icebox.
- Window-warn email → WU-023. Success digests → scheduler era (WU-022).
- Per-run notify toggle (`🔔`) → post-MVP polish.

## Fixture / demo

Dev loop: `docker compose` up (PG + mailpit) → launch Backup on
`billing-test`, Abort it mid-run → mailpit UI (:8025) shows
`[db-portal] RUN-n canceled — dump on billing-test (test)` with the run
link; body has who/what/where/status only. Restart the portal mid-run →
orphan-sweep mail arrives unattended. `psql`: UPDATE audit_event → raises.
Activity: filter Status=Failed shows only the swept run; Export CSV
downloads the filtered rows.
