# M1-gate review — findings record (2026-07-09, s08)

Method: STRATEGY §6 multi-agent gate. Workflow `wf_3ba143f2-9c0` (5 independent
reviewers: backend-correctness, concurrency, frontend-correctness, security,
spec-conformance; 2 adversarial verifiers per finding) was interrupted twice by
connection loss; rather than re-running, the run journal was harvested (all 5 reviews
complete = 24 raw findings, 36/48 verdicts done) and the remaining 10 findings were
verified inline against the code in the main session. Every confirmed finding below was
either (a) upheld by 2+ independent verifier agents or (b) inline-verified at the cited
lines. Dispositions point at BACKLOG entries.

## Confirmed — HIGH

1. **Start() strands a live engine job when the job_id UPDATE fails** —
   `backend/internal/runs/service.go`. After StartJob succeeds, if recording job_id on
   the run row fails, the run sticks `queued` forever: no watcher, no finalize, no
   `run.finished` audit event, while the engine job runs live. Found independently by
   two reviewers; 8/8 verifier votes real. → **WU-016**
2. **finalize is unguarded and non-idempotent** — `backend/internal/runs/service.go`.
   A second finalizer (watcher vs cancel vs sweep) writes a duplicate `run.finished`
   audit event and can overwrite a terminal state. 4/4 votes real. → **WU-016**

## Confirmed — MEDIUM

3. **SSE `end` event carries a stale non-terminal state** —
   `backend/internal/server/runs_http.go:169-175` vs SPEC-013 "final run state".
   Engine closes the channel before the watcher's next poll mirrors the row; reviewer
   reproduced live (`end {"state":"running"}` on a finished run); `rs.Get` error yields
   `{"state":""}`. Bundled UI self-heals (ignores end.state); spec-conforming clients
   don't. → **WU-016**
4. **audit_event append-only enforcement does not cover TRUNCATE** —
   `backend/internal/db/migrations/0003_runs_audit.sql:52-56`. Row-level
   UPDATE/DELETE trigger never fires on TRUNCATE; TRUNCATE is neither revoked nor
   statement-trigger-guarded — the portal role can silently erase the entire trail,
   contradicting the migration's own "even for the table owner" comment. → **WU-017**
5. **audit_event omits job_id** — ARCHITECTURE §5 (`docs/ARCHITECTURE.md:89-90`) lists
   `job_id` in the audit record and SPEC-012 mini-ADR 1 claims "every §5 field";
   the table has no such column and run.job_id is app-mutable, so the run↔engine-job
   linkage has no forensic anchor. → **WU-017**
6. **Transient SSE failure treated as permanent "logs gone"** — frontend
   `lib/api.ts` / RunDetail: a 5xx on the stream discards already-received lines and
   shows the 410 message. 4/4 votes real. → **WU-019**
7. **LaunchDrawer overlay click closes the drawer mid-launch** — a prod launch
   confirmation can vanish while/after the run fires. 3/3 votes real. → **WU-019**
8. **size_gb parse/canonicalization mismatch (two symptoms, one root cause)** —
   `backend/internal/inventory/csv.go` keeps the raw string; PG canonicalizes.
   (a) forms Go accepts but PG rejects (hex floats) abort the whole import instead of
   quarantining the row; (b) forms PG normalizes ('1e2'→'100', '.5'→'0.5', '0120')
   break idempotency: identical file re-imports as "updated" forever, updated_at
   churns — reproduced live by the reviewer. → **WU-018**

## Confirmed — LOW

9. **SweepOrphans aborts on first error, never retried after boot** — service.go.
   → **WU-016**
10. **Activity stale-filter race** — previous filter's runs render under newly pressed
    chips; CSV export can emit the mismatched list. → **WU-019**
11. **CSV formula injection (latent)** — `frontend/src/lib/csv.ts:20` never neutralizes
    leading `=+-@\t`. Safe today (every field server-constrained, requested_by
    hardcoded); dissolves at WU-020 when real principals arrive. Found by two
    reviewers. → **WU-019** (must land before WU-020 ships real usernames)
12. **getJSON/postJSON let a 2xx malformed-JSON body escape as raw SyntaxError** —
    `frontend/src/lib/api.ts:37,54`, breaking the module's "every failure is an
    ApiError" contract. → **WU-019**
13. **exportCsv revokes the blob URL synchronously after click** —
    `frontend/src/pages/Activity.tsx:47`; Safari can cancel the download. → **WU-019**
14. **CI installs golangci-lint via unpinned `curl | sh` from HEAD** —
    `.github/workflows/check.yml:18`; supply-chain exposure + silent version drift vs
    the VM's pinned 2.12.2. → **icebox**
15. **POST /api/runs decodes an unbounded body** —
    `backend/internal/server/runs_http.go:48`; no MaxBytesReader, `reason` unbounded
    and stored verbatim. → **WU-021 ledger** (lands with the route guards)
16. **LocateDotenv walks up to the filesystem root** —
    `backend/internal/config/config.go:76-86`; can silently adopt a foreign `.env`;
    nothing logs which file loaded. → **icebox**
17. **SPEC-012 flow line inverted vs code** — spec said "instance lookup → catalog
    lookup"; code checks catalog first (service.go:90-104). Observable only when both
    are unknown (400 vs 404). → **doc-fixed in this commit** (spec now matches code;
    in-memory catalog check before a DB query is the sensible order).

## Refuted (adversarially killed — no action)

- MockEngine unbounded jobs/log growth (0/4 votes: bounded by MVP usage & process
  lifetime expectations; tests pin eviction-free contract deliberately).
- Watcher treats transient Status errors as job-lost and finalizes failed (0/4:
  claimed path unreachable as described).
- Graceful shutdown always stalls 10 s while SSE follows a live run (1/3, majority
  refuted: shutdown path detaches subscribers).
