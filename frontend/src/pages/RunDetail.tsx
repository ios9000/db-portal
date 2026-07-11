import { useEffect, useRef, useState } from 'react';
import { Link, useParams } from 'react-router';
import { EnvBadge } from '../components/EnvBadge';
import { EnvBanner } from '../components/EnvBanner';
import { RunStatus } from '../components/RunStatus';
import {
  ApiError,
  cancelRun,
  fetchOperations,
  fetchRun,
  fetchRunChain,
  openRunLogStream,
  resumeChain,
  type Chain,
  type Operation,
  type Run,
  type RunLogLine,
} from '../lib/api';
import {
  formatBytes,
  formatClock,
  formatDuration,
  formatSeconds,
  formatTimestamp,
} from '../lib/format';

const POLL_MS = 3_000;

function isTerminal(r: Run): boolean {
  return r.state === 'success' || r.state === 'failed' || r.state === 'canceled';
}

/** Resume failures render the server's own message (SPEC-021 idiom, mirrors
 * describeActionError in Schedules.tsx) — kept local per WU-032's brief. */
function describeChainError(err: unknown): string {
  if (err instanceof ApiError) {
    return err.status === 0 ? 'API unreachable — is the backend running?' : err.detail;
  }
  return 'Could not resume the chain.';
}

/**
 * Run detail (design brief Screen 5, adapted per SPEC-013: one operation =
 * one stage, no chain pipeline until WU-032). Page data arrives by polling;
 * log lines arrive only over the SSE stream.
 */
export function RunDetail() {
  const params = useParams();
  const runId = Number(params.id);

  const [run, setRun] = useState<Run | null>(null);
  const [error, setError] = useState<'notfound' | 'unavailable' | null>(null);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [lines, setLines] = useState<RunLogLine[]>([]);
  const [logsGone, setLogsGone] = useState(false);
  const [follow, setFollow] = useState(true);
  const [aborting, setAborting] = useState(false);
  const [abortError, setAbortError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  // Bumped when the SSE `end` event fires, so the poll effect re-fetches
  // the final state immediately instead of waiting out the interval.
  const [pollEpoch, setPollEpoch] = useState(0);
  const paneRef = useRef<HTMLDivElement>(null);

  // Chain strip state (WU-032). chainRef mirrors `chain` so a later poll
  // failure can tell "never had a chain" (404 is the normal standalone-run
  // case) apart from "had one, then it 404'd" (this attempt was superseded
  // by a resume — SPEC-032 mini-ADR 2), without depending on stale closures.
  const [chain, setChain] = useState<Chain | null>(null);
  const [chainSuperseded, setChainSuperseded] = useState(false);
  // Bumped after a successful resume so the poll effect restarts even
  // though runId/run.state didn't change — the new chain is `running`, so
  // polling then continues on its own.
  const [chainPollEpoch, setChainPollEpoch] = useState(0);
  const chainRef = useRef<Chain | null>(null);

  useEffect(() => {
    chainRef.current = null;
    setChain(null);
    setChainSuperseded(false);
  }, [runId]);

  useEffect(() => {
    if (!Number.isInteger(runId)) {
      setError('notfound');
      return;
    }
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = async () => {
      try {
        const r = await fetchRun(runId);
        if (cancelled) return;
        setRun(r);
        setError(null);
        if (!isTerminal(r)) timer = setTimeout(() => void poll(), POLL_MS);
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 404) {
          setError('notfound');
          return; // a missing run will not appear by retrying
        }
        setError('unavailable');
        timer = setTimeout(() => void poll(), POLL_MS);
      }
    };
    void poll();

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [runId, pollEpoch]);

  useEffect(() => {
    fetchOperations().then(setOperations, () => setOperations([]));
  }, []);

  // Open the log stream once the run is known to exist. The server replays
  // history then follows, so a single subscription covers the whole run.
  const hasRun = run !== null;
  useEffect(() => {
    if (!hasRun) return;
    return openRunLogStream(runId, {
      onOpen: () => setLines([]), // reconnect replays from the start
      onLine: (l) => setLines((prev) => [...prev, l]),
      onEnd: () => setPollEpoch((e) => e + 1),
      onUnavailable: () => setLogsGone(true),
    });
  }, [runId, hasRun]);

  // The chain strip (WU-032): fetched once the run has loaded, and again
  // whenever run.state changes (a step finishing means the chain may have
  // advanced). 404 — unknown run, or a run that isn't a chain step — is the
  // NORMAL case for a standalone run, not an error; any other failure also
  // just means no strip (auxiliary data must never block the page). While
  // the chain is `running`, keep polling on the page's own cadence so
  // sibling-step progress appears; stop once halted/success. A poll that
  // 404s after we'd already shown a chain means this attempt was superseded
  // by a resume — keep the last-known steps and note it, rather than
  // clearing the strip.
  useEffect(() => {
    if (!hasRun) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = async () => {
      try {
        const c = await fetchRunChain(runId);
        if (cancelled) return;
        chainRef.current = c;
        setChain(c);
        setChainSuperseded(false);
        if (c.state === 'running') timer = setTimeout(() => void poll(), POLL_MS);
      } catch (err) {
        if (cancelled) return;
        // Only a 404 after a chain was shown means superseded (SPEC-032
        // mini-ADR 2) — a network blip must not fake that note. Any other
        // failure just stops polling, keeping the last-known strip.
        if (err instanceof ApiError && err.status === 404 && chainRef.current !== null) {
          setChainSuperseded(true);
        }
      }
    };
    void poll();

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [runId, hasRun, run?.state, chainPollEpoch]);

  const live = run !== null && !isTerminal(run);
  useEffect(() => {
    if (!live) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [live]);

  useEffect(() => {
    const pane = paneRef.current;
    if (follow && pane !== null) pane.scrollTop = pane.scrollHeight;
  }, [lines, follow]);

  if (error === 'notfound') {
    return (
      <>
        <Link className="breadcrumb" to="/activity">
          ← Activity
        </Link>
        <h1>Run not found</h1>
        <p className="placeholder">No such run.</p>
      </>
    );
  }
  if (run === null) {
    return (
      <p className="placeholder" role={error === 'unavailable' ? 'alert' : undefined}>
        {error === 'unavailable' ? 'API unreachable — is the backend running?' : 'Loading run…'}
      </p>
    );
  }

  const op = operations.find((o) => o.id === run.operation);
  const opLabel = op?.label ?? run.operation;

  const abort = async () => {
    setAborting(true);
    setAbortError(null);
    try {
      await cancelRun(runId);
      // Stay locked on "Aborting…" — the poll observes `canceled`.
    } catch {
      setAborting(false);
      setAbortError('Could not abort the run.');
    }
  };

  return (
    <>
      <Link className="breadcrumb" to="/activity">
        ← Activity
      </Link>

      <EnvBanner env={run.environment} />

      <header className="run-header">
        <h1>
          RUN-{run.id} · {opLabel} · {run.instance}
        </h1>
        <EnvBadge env={run.environment} />
        <span className="spacer" />
        {live && (
          <button
            type="button"
            className="btn-secondary"
            onClick={() => void abort()}
            disabled={aborting}
          >
            ✖ {aborting ? 'Aborting…' : 'Abort'}
          </button>
        )}
      </header>
      <p className="run-meta">
        Submitted {formatTimestamp(run.submitted_at)}
        {live && run.started_at !== null && (
          <> · elapsed {formatSeconds((now - new Date(run.started_at).getTime()) / 1000)}</>
        )}
        {!live && run.finished_at !== null && (
          <> · took {formatDuration(run.started_at, run.finished_at)}</>
        )}
        {run.reason !== null && <> · {run.reason}</>}
      </p>
      {abortError !== null && (
        <p className="drawer-error" role="alert">
          {abortError}
        </p>
      )}

      {chain !== null && (
        <ChainStrip
          chain={chain}
          superseded={chainSuperseded}
          runId={runId}
          operations={operations}
          onResumed={(updated) => {
            chainRef.current = updated;
            setChain(updated);
            setChainSuperseded(false);
            setChainPollEpoch((e) => e + 1);
          }}
        />
      )}

      <section className="stage-panel" aria-label="Stage">
        <span className="stage-icon" aria-hidden="true">
          {op?.icon ?? '⚙'}
        </span>
        <div className="stage-body">
          <span className="stage-title">{opLabel}</span>
          {live && op !== undefined && <span className="stage-hint">✓ {op.online_hint}</span>}
        </div>
        <RunStatus status={run.state} />
      </section>

      {run.state === 'failed' && (
        <div className="outcome-card outcome-failed" role="alert">
          <strong>{opLabel} did not complete.</strong>
          <p>Your database is online and unchanged.{run.error !== null && <> — {run.error}</>}</p>
          <p className="outcome-ref">The DB team can reference RUN-{run.id}.</p>
        </div>
      )}
      {run.state === 'canceled' && (
        <div className="outcome-card outcome-canceled">
          <strong>{opLabel} was canceled.</strong>
          <p>Your database is online and unchanged.</p>
        </div>
      )}
      {run.artifact !== null && (
        <div className="artifact-strip">
          💾 {run.artifact.name} · {formatBytes(run.artifact.size_bytes)} · sha256{' '}
          {run.artifact.checksum.slice(0, 12)}…
        </div>
      )}

      <section className="log-section" aria-label="Logs">
        <div className="log-toolbar">
          <span className="log-title">Logs</span>
          <span className="spacer" />
          <button
            type="button"
            className="pill"
            aria-pressed={follow}
            onClick={() => setFollow((f) => !f)}
          >
            Follow {follow ? '✓' : ''}
          </button>
        </div>
        {logsGone ? (
          <p className="placeholder">
            Logs are no longer available for this run (they do not survive a portal restart).
          </p>
        ) : (
          <div className="log-pane" ref={paneRef}>
            {lines.map((l, i) => (
              <div key={i} className="log-line">
                <span className="log-ts">{formatClock(l.ts)}</span>
                {l.line}
              </div>
            ))}
            {lines.length === 0 && <div className="log-line log-waiting">Waiting for output…</div>}
          </div>
        )}
      </section>
    </>
  );
}

interface ChainStripProps {
  chain: Chain;
  superseded: boolean;
  runId: number;
  operations: Operation[];
  onResumed: (updated: Chain) => void;
}

/**
 * Chain progress strip (WU-032, SPEC-032): the ordered steps of the chain
 * this run belongs to. Sibling steps (a different run than the one this
 * page is showing) link off to their own run pages; the current step and
 * still-pending steps are plain text. Local to this page, same pattern as
 * NewScheduleForm in Schedules.tsx.
 */
function ChainStrip({ chain, superseded, runId, operations, onResumed }: ChainStripProps) {
  const [resuming, setResuming] = useState(false);
  const [resumeError, setResumeError] = useState<string | null>(null);

  const currentStep = chain.steps.find((s) => s.run_id === runId);

  const resume = async () => {
    setResuming(true);
    setResumeError(null);
    try {
      const updated = await resumeChain(chain.id);
      onResumed(updated);
    } catch (err) {
      setResumeError(describeChainError(err));
    } finally {
      setResuming(false);
    }
  };

  return (
    <section className="chain-strip" aria-label="Chain">
      <div className="chain-strip-header">
        <span className="chain-strip-kind">{chain.kind}</span>
        {currentStep !== undefined && !superseded && (
          <span className="chain-strip-counter">
            Step {currentStep.seq} of {chain.steps.length}
          </span>
        )}
      </div>
      <ol className="chain-strip-steps">
        {chain.steps.map((step, i) => {
          const isCurrent = step.run_id === runId;
          const op = operations.find((o) => o.id === step.operation);
          const label = op?.label ?? step.operation;
          return (
            <li
              key={step.seq}
              className={isCurrent ? 'chain-step chain-step-current' : 'chain-step'}
            >
              {i > 0 && (
                <span className="chain-step-arrow" aria-hidden="true">
                  →
                </span>
              )}
              <span className="chain-step-label">
                {step.seq}.{' '}
                {step.run_id !== null && !isCurrent ? (
                  <Link to={`/runs/${step.run_id}`}>{label}</Link>
                ) : (
                  label
                )}
              </span>{' '}
              {step.status === 'pending' ? (
                <span className="chain-step-pending">Pending</span>
              ) : (
                <RunStatus status={step.status} />
              )}
            </li>
          );
        })}
      </ol>
      {superseded && (
        <p className="chain-strip-note">
          This chain was resumed — this attempt was superseded by a new run.
        </p>
      )}
      {chain.state === 'halted' && (
        <div className="chain-strip-actions">
          <button
            type="button"
            className="btn-secondary"
            onClick={() => void resume()}
            disabled={resuming}
          >
            {resuming ? 'Resuming…' : 'Resume'}
          </button>
          {resumeError !== null && (
            <p className="drawer-error" role="alert">
              {resumeError}
            </p>
          )}
        </div>
      )}
    </section>
  );
}
