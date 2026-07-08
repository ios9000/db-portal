import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { EnvBadge } from '../components/EnvBadge';
import { RunStatus } from '../components/RunStatus';
import {
  ApiError,
  fetchOperations,
  fetchRuns,
  type InstanceEnv,
  type Operation,
  type Run,
  type RunState,
} from '../lib/api';
import { runsToCsv } from '../lib/csv';
import { formatDuration, formatSeconds, formatTimestamp } from '../lib/format';

const POLL_ACTIVE_MS = 3_000;
const POLL_IDLE_MS = 10_000;

const STATUS_PILLS: { label: string; state?: RunState }[] = [
  { label: 'All' },
  { label: 'Queued', state: 'queued' },
  { label: 'Running', state: 'running' },
  { label: 'Success', state: 'success' },
  { label: 'Failed', state: 'failed' },
  { label: 'Canceled', state: 'canceled' },
];

const ENV_PILLS: { label: string; env?: InstanceEnv }[] = [
  { label: 'All' },
  { label: 'DEV', env: 'dev' },
  { label: 'TEST', env: 'test' },
  { label: 'PROD', env: 'prod' },
];

function isTerminal(r: Run): boolean {
  return r.state === 'success' || r.state === 'failed' || r.state === 'canceled';
}

/** Download the current view as CSV (SPEC-014 mini-ADR 7). */
function exportCsv(runs: Run[]) {
  const url = URL.createObjectURL(new Blob([runsToCsv(runs)], { type: 'text/csv' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = `db-portal-activity-${new Date().toISOString().slice(0, 10)}.csv`;
  a.click();
  URL.revokeObjectURL(url);
}

/**
 * Activity (design brief Screen 6, WU-014 — approvals adapted out):
 * filter chips over server-side filters, a live "Now running" section,
 * the history table with the audit trail's requester, CSV export.
 * Polls faster while anything is still moving.
 */
export function Activity() {
  const [searchParams, setSearchParams] = useSearchParams();
  const state = searchParams.get('state') ?? undefined;
  const env = searchParams.get('env') ?? undefined;
  const op = searchParams.get('op') ?? undefined;

  const [runs, setRuns] = useState<Run[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [now, setNow] = useState(() => Date.now());

  // Operation chips come from the catalog; without it the group just
  // doesn't render — the list must not depend on it.
  useEffect(() => {
    let cancelled = false;
    fetchOperations()
      .then((ops) => {
        if (!cancelled) setOperations(ops);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = async () => {
      let next = POLL_IDLE_MS;
      try {
        const list = await fetchRuns({ state, env, operation: op });
        if (cancelled) return;
        setRuns(list);
        setError(null);
        if (list.some((r) => !isTerminal(r))) next = POLL_ACTIVE_MS;
      } catch (err) {
        if (cancelled) return;
        setError(
          err instanceof ApiError && err.status === 0
            ? 'API unreachable — is the backend running?'
            : 'Could not load runs.',
        );
      }
      timer = setTimeout(() => void poll(), next);
    };
    void poll();

    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [state, env, op]);

  const live = (runs ?? []).filter((r) => !isTerminal(r));
  const history = (runs ?? []).filter(isTerminal);

  // 1s elapsed tick, only while something is actually running.
  useEffect(() => {
    if (live.length === 0) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [live.length]);

  const setParam = (key: string, value?: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next, { replace: true });
  };

  const filtersActive = Boolean(state ?? env ?? op);

  return (
    <>
      <h1>Activity</h1>

      <div className="toolbar">
        <div className="pill-group" role="group" aria-label="Status filter">
          {STATUS_PILLS.map(({ label, state: pillState }) => (
            <button
              key={label}
              type="button"
              className="pill"
              aria-pressed={state === pillState}
              onClick={() => setParam('state', pillState)}
            >
              {label}
            </button>
          ))}
        </div>
        <div className="pill-group" role="group" aria-label="Environment filter">
          {ENV_PILLS.map(({ label, env: pillEnv }) => (
            <button
              key={label}
              type="button"
              className="pill"
              aria-pressed={env === pillEnv}
              onClick={() => setParam('env', pillEnv)}
            >
              {label}
            </button>
          ))}
        </div>
        {operations.length > 0 && (
          <div className="pill-group" role="group" aria-label="Operation filter">
            <button
              type="button"
              className="pill"
              aria-pressed={op === undefined}
              onClick={() => setParam('op', undefined)}
            >
              All
            </button>
            {operations.map((o) => (
              <button
                key={o.id}
                type="button"
                className="pill"
                aria-pressed={op === o.id}
                onClick={() => setParam('op', o.id)}
              >
                {o.label}
              </button>
            ))}
          </div>
        )}
        <span className="spacer" />
        <button
          type="button"
          className="btn-secondary btn-small"
          disabled={runs === null || runs.length === 0}
          onClick={() => exportCsv(runs ?? [])}
        >
          Export CSV
        </button>
      </div>

      {error !== null && (
        <p className="placeholder" role="alert">
          {error}
        </p>
      )}
      {error === null && runs === null && <p className="placeholder">Loading runs…</p>}
      {runs !== null && runs.length === 0 && (
        <p className="placeholder">
          {filtersActive
            ? 'No runs match these filters.'
            : 'No runs yet — launch one from My Databases.'}
        </p>
      )}

      {live.length > 0 && (
        <section className="now-running" aria-label="Now running">
          <h2>Now running</h2>
          {live.map((r) => (
            <div key={r.id} className="live-run">
              <div className="live-run-meta">
                <RunStatus status={r.state} />
                <Link className="instance-name" to={`/runs/${r.id}`}>
                  RUN-{r.id}
                </Link>
                <span>{r.operation}</span>
                <span>{r.instance}</span>
                <EnvBadge env={r.environment} />
                <span className="live-run-elapsed">
                  {r.started_at !== null
                    ? `elapsed ${formatSeconds((now - new Date(r.started_at).getTime()) / 1000)}`
                    : 'waiting for the engine…'}
                </span>
              </div>
              <div className="progress-indeterminate" aria-hidden="true" />
            </div>
          ))}
        </section>
      )}

      {history.length > 0 && (
        <table className="instance-table">
          <thead>
            <tr>
              <th>Status</th>
              <th>Run</th>
              <th>Operation</th>
              <th>Instance</th>
              <th>Env</th>
              <th>Requester</th>
              <th>Submitted</th>
              <th>Duration</th>
            </tr>
          </thead>
          <tbody>
            {history.map((r) => (
              <tr key={r.id} className={r.environment === 'prod' ? 'prod-edge' : ''}>
                <td>
                  <RunStatus status={r.state} />
                </td>
                <td className="instance-name">
                  <Link to={`/runs/${r.id}`}>RUN-{r.id}</Link>
                </td>
                <td>{r.operation}</td>
                <td>{r.instance}</td>
                <td>
                  <EnvBadge env={r.environment} />
                </td>
                <td>{r.requested_by}</td>
                <td>{formatTimestamp(r.submitted_at)}</td>
                <td>{formatDuration(r.started_at, r.finished_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
