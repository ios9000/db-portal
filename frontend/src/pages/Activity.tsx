import { useEffect, useState } from 'react';
import { EnvBadge } from '../components/EnvBadge';
import { RunStatus } from '../components/RunStatus';
import { ApiError, fetchRuns, type Run } from '../lib/api';

const POLL_ACTIVE_MS = 3_000;
const POLL_IDLE_MS = 10_000;

function isTerminal(r: Run): boolean {
  return r.state === 'success' || r.state === 'failed' || r.state === 'canceled';
}

function formatSubmitted(iso: string): string {
  return new Date(iso).toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** Duration is defined only for finished runs (SPEC-012 — live elapsed is WU-013). */
function formatDuration(r: Run): string {
  if (r.started_at === null || r.finished_at === null) return '—';
  const secs = Math.round(
    (new Date(r.finished_at).getTime() - new Date(r.started_at).getTime()) / 1000,
  );
  if (secs < 60) return `${secs}s`;
  return `${Math.floor(secs / 60)}m ${secs % 60}s`;
}

/**
 * Activity: the run list (WU-012 slice — filters, export and approval rows
 * are Screen 6 / WU-014). Polls faster while anything is still moving.
 */
export function Activity() {
  const [runs, setRuns] = useState<Run[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    const poll = async () => {
      let next = POLL_IDLE_MS;
      try {
        const list = await fetchRuns();
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
  }, []);

  return (
    <>
      <h1>Activity</h1>

      {error !== null && (
        <p className="placeholder" role="alert">
          {error}
        </p>
      )}
      {error === null && runs === null && <p className="placeholder">Loading runs…</p>}
      {runs !== null && runs.length === 0 && (
        <p className="placeholder">No runs yet — launch one from My Databases.</p>
      )}

      {runs !== null && runs.length > 0 && (
        <table className="instance-table">
          <thead>
            <tr>
              <th>Status</th>
              <th>Run</th>
              <th>Operation</th>
              <th>Instance</th>
              <th>Env</th>
              <th>Submitted</th>
              <th>Duration</th>
            </tr>
          </thead>
          <tbody>
            {runs.map((r) => (
              <tr key={r.id} className={r.environment === 'prod' ? 'prod-edge' : ''}>
                <td>
                  <RunStatus status={r.state} />
                </td>
                <td className="instance-name">RUN-{r.id}</td>
                <td>{r.operation}</td>
                <td>{r.instance}</td>
                <td>
                  <EnvBadge env={r.environment} />
                </td>
                <td>{formatSubmitted(r.submitted_at)}</td>
                <td>{formatDuration(r)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}
