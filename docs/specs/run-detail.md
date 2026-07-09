# SPEC-013 · Run detail + live logs

> Groomed 2026-07-07 for WU-013. Authority chain: DECISIONS.md → ARCHITECTURE.md
> §3 (hero workflow) → this file. Design brief Screen 5 drives the page (adapted:
> single-step dump, no chain pipeline until WU-032); WU-005 fixed the StreamLogs
> contract; SPEC-012 fixed the run model and deferred cancel + live logs here.

## Scope

The run detail page: from Activity (or the launch drawer's started state) into
one run — header with target + env, live state, a log pane streaming the
engine's output (replay then follow), follow mode, final status, artifact
strip, and an Abort button backed by a cancel endpoint. Explicitly NOT here:
chain/stage pipeline UI (WU-032 — single operation renders as a single stage),
failure email (WU-014), typed-name prod ritual + env banner (WU-015), log
search + download (post-MVP polish), log persistence (see mini-ADR 3),
approval flows (post-MVP).

## Mini-ADRs (agent decisions, revisitable)

1. **SSE over WebSocket for log streaming.** Logs flow one way
   (server → client); the only client→server actions (abort, follow toggle)
   are a REST call and a purely local UI state. SSE works over plain HTTP/1.1
   — no new Go dependency (WebSocket needs one), no upgrade dance, native
   `EventSource` in the browser with built-in reconnect, streams through the
   Vite dev proxy and the single binary (WU-006) unchanged. Revisit if a
   bidirectional need appears (interactive consoles) or when WU-033's real
   engine forces a different transport.
2. **Stream ends with an explicit `end` event.** `EventSource` auto-reconnects
   whenever a 200 stream closes — without a terminal marker, a finished run
   would replay its logs in a loop forever. The server sends
   `event: end` + final run state after the log channel closes; the client
   closes the connection on receiving it. Mid-run network drops still get
   EventSource's free reconnect (the client resets its buffer on `open`,
   because StreamLogs replays history on every call).
3. **Logs are NOT persisted; unavailable logs are a graceful state.**
   Engine job state (and MockEngine memory) dies with the process; after a
   restart, a swept run's logs are gone. Persisting log lines in the portal
   DB is real cost for a nice-to-have (ARCHITECTURE keeps run *outcomes* in
   the portal; the engine owns execution detail — Semaphore keeps its own
   task output for WU-033 to link/fetch). The endpoint answers 410 Gone and
   the UI renders "logs no longer available"; run outcome, error and artifact
   metadata remain fully served from the run row. Revisit with WU-033.
4. **Cancel endpoint ships in this WU.** SPEC-012 deferred it here to pair
   with the Abort button. POST /api/runs/{id}/cancel forwards to
   Adapter.Cancel via the same registry path; the transition to `canceled`
   stays asynchronous — the existing watcher observes it and finalizes
   (run row + `run.finished` audit event with final_status `canceled`), so
   cancel introduces NO new finalization path. No `run.cancel_requested`
   audit action yet: actor is still the 'local-dev' placeholder, so the row
   would add no information; WU-021 (real identity) adds it.

## Interfaces

- `GET /api/runs/{id}/logs` → `text/event-stream`.
  Events, in order: zero or more `event: log` with
  `data: {"ts":"<RFC3339Nano>","line":"<text>"}` (full replay first, then
  live follow — that ordering is the WU-005 adapter contract, not the
  handler's job); then exactly one `event: end` with
  `data: {"state":"<final run state>"}`, then the stream closes. The run
  row lags the engine by up to one watcher poll, so the handler waits
  (bounded, 2 s) for the row to reach a terminal state before emitting
  `end`; past the deadline it sends the last observed state (WU-016,
  m1-gate item 3).
  A `: keepalive` comment every 15s defeats idle-connection proxies.
  Errors (JSON body, no stream): 404 unknown run id; 410 logs unavailable
  (no engine job was ever started, or the engine no longer knows the job —
  post-restart orphans). Client disconnect cancels the engine subscription.
- `POST /api/runs/{id}/cancel` (no body) → 202 `{"status":"canceling"}`.
  Cancel is asynchronous and idempotent at the engine; observe the outcome
  via run polling. Errors: 404 unknown run; 409 not cancelable (run already
  terminal, or its engine job is lost); 502 engine refused the cancel.
- `internal/runs` additions (Service stays the only Registry caller):
  `StreamLogs(ctx, id) (<-chan engine.LogLine, error)` — ErrNotFound /
  ErrNoLogs; channel closes on job end or ctx cancel.
  `Cancel(ctx, id) error` — ErrNotFound / ErrNotCancelable / ErrEngine.

## Behavior (testable)

1. Streaming a finished run replays every log line then closes: collected
   lines contain the PLAY header and the RECAP line in order.
2. Streaming a live run follows to completion: lines emitted after the
   stream opened arrive; channel closes when the job finishes.
3. SSE endpoint emits `log` events then a final `end` event carrying the
   run's terminal state; Content-Type is `text/event-stream`.
4. Unknown run id → 404. Run whose job the engine lost (or that never got a
   job_id — engine refused at submit) → 410; the run JSON itself still
   serves normally.
5. Cancel a running job → 202; the watcher finalizes the run `canceled`,
   error "canceled by operator", and the `run.finished` audit event carries
   final_status `canceled` (exactly 2 audit events total — cancel adds none).
6. Cancel a terminal run → 409, run row untouched.
7. Client disconnect (ctx cancel) detaches the engine subscription without
   affecting the job.

## UI (WU-013 slice — Screen 5 adapted to single-step runs)

- Route `/runs/:id`, linked from each Activity row (run id cell) and from
  the launch drawer's started state ("View run"). Unknown id → in-page
  "no such run" state.
- Header: "← Activity" breadcrumb; title `RUN-<id> · <op label> · <instance>`
  with EnvBadge; meta line: submitted time · live elapsed while running
  (1s tick), final duration once terminal; RunStatus chip. "Started by" waits
  for WU-021 (actor not exposed on the run JSON). `⏸ Pause` from the brief
  has no engine semantics — omitted. `✖ Abort` outlined button while
  non-terminal → cancel endpoint → button locks to "Aborting…" until the
  poll observes the terminal state.
- Stage panel (single-step adaptation of the 3-node pipeline): operation
  icon + label, RunStatus chip, catalog reassurance line ("Database stays
  online") while running.
- Outcome cards (calm, plain language — brief's failed variant): failed →
  "<op label> did not complete. Your database is online and unchanged."
  plus the run's error text and ref RUN-<id>; canceled → neutral card.
  Success → artifact strip: 💾 name · human size · `sha256 <prefix>…`.
- Log pane: dark monospace, HH:MM:SS-stamped lines; `Follow` pill
  (aria-pressed) auto-scrolls on new lines, default on; buffer resets on
  EventSource `open` (replay-on-reconnect, mini-ADR 2); stream closed on
  `end`, then one final run re-fetch. 410 → "logs no longer available"
  placeholder; the page still shows outcome + artifact.
- Run polling: 3s while non-terminal, stops once terminal (page data);
  logs arrive only via SSE, never via polling.

## Out of scope / deferred

- Chain pipeline nodes, resume-from-failed-step → WU-032.
- Log search, log download, `🔔 Notify me` toggle → post-MVP polish
  (notify pairs with WU-014's email path).
- Log persistence / fetching historical logs from the engine → WU-033.
- `run.cancel_requested` audit action → WU-021 (needs a real actor).
- Cancel confirmation ritual on prod → WU-015 owns prod rituals.

## Fixture / demo

Dev loop: launch Backup on `billing-test` from My Databases → drawer "View
run" → watch the log pane fill live, RECAP, artifact strip appears. Launch
another and hit Abort mid-run → status flips to Canceled, audit shows
final_status=canceled. Restart the portal mid-run → orphan sweep fails the
run → its detail page shows outcome + "logs no longer available".
