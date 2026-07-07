import { useEffect, useState } from 'react';
import { fetchHealthz } from '../lib/api';
import { APP_VERSION } from '../lib/version';

const PROBE_INTERVAL_MS = 30_000;

type Health = 'loading' | 'ok' | 'degraded' | 'unreachable';

const TEXT: Record<Health, string> = {
  loading: 'Checking…',
  ok: 'API ok · DB ok',
  degraded: 'API ok · DB down',
  unreachable: 'API unreachable',
};

/** Footer probing /healthz on mount and every 30s. */
export function StatusFooter() {
  const [health, setHealth] = useState<Health>('loading');

  useEffect(() => {
    let cancelled = false;
    const probe = async () => {
      let next: Health;
      try {
        const h = await fetchHealthz();
        next = h.status === 'ok' ? 'ok' : 'degraded';
      } catch {
        next = 'unreachable';
      }
      if (!cancelled) setHealth(next);
    };
    void probe();
    const id = setInterval(() => void probe(), PROBE_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return (
    <footer className="status-footer">
      <span className={`health-dot ${health === 'loading' ? '' : health}`} aria-hidden="true" />
      <span role="status">{TEXT[health]}</span>
      <span className="spacer" />
      <span>db-portal {APP_VERSION}</span>
    </footer>
  );
}
