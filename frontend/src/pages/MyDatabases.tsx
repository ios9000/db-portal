import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router';
import { EnvBadge } from '../components/EnvBadge';
import { LaunchDrawer } from '../components/LaunchDrawer';
import {
  ApiError,
  fetchInstances,
  fetchOperations,
  type Instance,
  type InstanceEnv,
  type Operation,
} from '../lib/api';
import { formatTimestamp } from '../lib/format';

type View = 'cards' | 'table';

const ENV_PILLS: { label: string; env?: InstanceEnv }[] = [
  { label: 'All' },
  { label: 'DEV', env: 'dev' },
  { label: 'TEST', env: 'test' },
  { label: 'PROD', env: 'prod' },
];

/**
 * My Databases (design brief Screens 1–2, WU-011 scope): cards ⇄ table over
 * the imported inventory. Last backup = the newest successful dump run
 * (WU-011R). Health, last vacuum and bloat have no data source until probes
 * exist (M3+) and render as "—" / unknown.
 */
export function MyDatabases() {
  const [searchParams, setSearchParams] = useSearchParams();
  const envParam = searchParams.get('env');
  const env: InstanceEnv | undefined =
    envParam === 'dev' || envParam === 'test' || envParam === 'prod' ? envParam : undefined;
  const view: View = searchParams.get('view') === 'table' ? 'table' : 'cards';

  const [search, setSearch] = useState('');
  const [instances, setInstances] = useState<Instance[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [operations, setOperations] = useState<Operation[]>([]);
  const [launching, setLaunching] = useState<Instance | null>(null);

  // Catalog for the launch buttons/drawer. On failure the buttons simply
  // don't render — browsing the inventory must not depend on the catalog.
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
    setInstances(null);
    setError(null);
    fetchInstances(env)
      .then((list) => {
        if (!cancelled) setInstances(list);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(
          err instanceof ApiError && err.status === 0
            ? 'API unreachable — is the backend running?'
            : 'Could not load instances.',
        );
      });
    return () => {
      cancelled = true;
    };
  }, [env]);

  const setParam = (key: string, value?: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next, { replace: true });
  };

  const dumpOp = operations.find((o) => o.id === 'dump');

  const q = search.trim().toLowerCase();
  const visible = (instances ?? []).filter(
    (i) =>
      q === '' ||
      i.name.toLowerCase().includes(q) ||
      i.cluster.toLowerCase().includes(q) ||
      i.owner.toLowerCase().includes(q),
  );

  return (
    <>
      <h1>My Databases</h1>

      <div className="toolbar">
        <input
          type="search"
          placeholder="Search instances…"
          aria-label="Search instances"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
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
        <span className="spacer" />
        <div className="pill-group" role="group" aria-label="View">
          <button
            type="button"
            className="pill"
            aria-pressed={view === 'cards'}
            onClick={() => setParam('view', undefined)}
          >
            Cards
          </button>
          <button
            type="button"
            className="pill"
            aria-pressed={view === 'table'}
            onClick={() => setParam('view', 'table')}
          >
            Table
          </button>
        </div>
      </div>

      {error !== null && (
        <p className="placeholder" role="alert">
          {error}
        </p>
      )}
      {error === null && instances === null && <p className="placeholder">Loading instances…</p>}
      {instances !== null && instances.length === 0 && (
        <p className="placeholder">
          No instances{env ? ` in ${env.toUpperCase()}` : ' in the inventory yet'}.
          {!env && ' Import them with: portal import <file.csv>'}
        </p>
      )}
      {instances !== null && instances.length > 0 && visible.length === 0 && (
        <p className="placeholder">No instances match “{search.trim()}”.</p>
      )}

      {visible.length > 0 &&
        (view === 'cards' ? (
          <CardGrid instances={visible} dump={dumpOp} onLaunch={setLaunching} />
        ) : (
          <FleetTable instances={visible} dump={dumpOp} onLaunch={setLaunching} />
        ))}

      {launching !== null && dumpOp !== undefined && (
        <LaunchDrawer instance={launching} operation={dumpOp} onClose={() => setLaunching(null)} />
      )}
    </>
  );
}

/** Neutral health indicator: no monitoring data source exists yet (M3+). */
function HealthDot() {
  return <span className="health-dot unknown" title="Health: not yet monitored" />;
}

function formatSize(sizeGB: number | null): string {
  return sizeGB === null ? '—' : `${sizeGB} GB`;
}

function formatLastBackup(iso: string | null): string {
  return iso === null ? '—' : formatTimestamp(iso);
}

interface LaunchProps {
  dump: Operation | undefined;
  onLaunch: (instance: Instance) => void;
}

function CardGrid({ instances, dump, onLaunch }: { instances: Instance[] } & LaunchProps) {
  return (
    <div className="instance-grid">
      {instances.map((i) => (
        <article key={i.name} className={`instance-card${i.env === 'prod' ? ' prod-edge' : ''}`}>
          <header>
            <HealthDot />
            <span className="instance-name">{i.name}</span>
            <EnvBadge env={i.env} />
          </header>
          <p className="instance-meta">
            PostgreSQL {i.pg_version}
            {i.size_gb !== null && <> · {formatSize(i.size_gb)}</>}
          </p>
          <p className="instance-meta">Last backup: {formatLastBackup(i.last_backup_at)}</p>
          {dump !== undefined && (
            <div className="card-actions">
              <button type="button" className="btn-secondary" onClick={() => onLaunch(i)}>
                <span aria-hidden="true">{dump.icon}</span> {dump.label}
              </button>
            </div>
          )}
        </article>
      ))}
    </div>
  );
}

function FleetTable({ instances, dump, onLaunch }: { instances: Instance[] } & LaunchProps) {
  return (
    <table className="instance-table">
      <thead>
        <tr>
          <th>
            <span className="visually-hidden">Health</span>
          </th>
          <th>Instance</th>
          <th>Env</th>
          <th>PG version</th>
          <th>Size</th>
          <th>Owner</th>
          <th>Last backup</th>
          <th>Last vacuum</th>
          <th>Bloat %</th>
          {dump !== undefined && (
            <th>
              <span className="visually-hidden">Actions</span>
            </th>
          )}
        </tr>
      </thead>
      <tbody>
        {instances.map((i) => (
          <tr key={i.name} className={i.env === 'prod' ? 'prod-edge' : ''}>
            <td>
              <HealthDot />
            </td>
            <td className="instance-name">{i.name}</td>
            <td>
              <EnvBadge env={i.env} />
            </td>
            <td>{i.pg_version}</td>
            <td>{formatSize(i.size_gb)}</td>
            <td>{i.owner}</td>
            <td>{formatLastBackup(i.last_backup_at)}</td>
            <td>—</td>
            <td>—</td>
            {dump !== undefined && (
              <td>
                <button
                  type="button"
                  className="btn-secondary btn-small"
                  onClick={() => onLaunch(i)}
                >
                  {dump.label}
                </button>
              </td>
            )}
          </tr>
        ))}
      </tbody>
    </table>
  );
}
