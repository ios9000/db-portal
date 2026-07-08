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
  openRunLogStream,
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
