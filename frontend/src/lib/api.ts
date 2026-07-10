/*
 * Typed API client. All backend access goes through here so error
 * handling is uniform: every failure surfaces as an ApiError.
 * status 0 means the server was unreachable (network error).
 */

export class ApiError extends Error {
  readonly status: number;
  readonly detail: string;

  constructor(status: number, detail: string) {
    super(status === 0 ? `API unreachable: ${detail}` : `API error ${status}: ${detail}`);
    this.name = 'ApiError';
    this.status = status;
    this.detail = detail;
  }
}

async function errorDetail(res: Response): Promise<string> {
  try {
    return (await res.text()) || res.statusText;
  } catch {
    return res.statusText;
  }
}

export async function getJSON<T>(path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: 'application/json' } });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    throw new ApiError(res.status, await errorDetail(res));
  }
  return (await res.json()) as T;
}

export async function postJSON<T>(path: string, body: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    throw new ApiError(res.status, await errorDetail(res));
  }
  return (await res.json()) as T;
}

export type InstanceEnv = 'dev' | 'test' | 'prod';

/** One inventory row as served by GET /api/instances (WU-011). */
export interface Instance {
  name: string;
  cluster: string;
  env: InstanceEnv;
  platform: 'k8s_patroni' | 'vm';
  pg_version: string;
  size_gb: number | null;
  owner: string;
  maintenance_window: string | null;
  /** Newest successful dump run's finish time (WU-011R); null if never dumped. */
  last_backup_at: string | null;
}

/** List instances, optionally narrowed to one environment (server-side). */
export async function fetchInstances(env?: InstanceEnv): Promise<Instance[]> {
  const path = env ? `/api/instances?env=${env}` : '/api/instances';
  const body = await getJSON<{ instances: Instance[] }>(path);
  return body.instances;
}

/** One catalog entry as served by GET /api/operations (WU-012). */
export interface Operation {
  id: string;
  label: string;
  icon: string;
  description: string;
  duration_hint: string;
  online_hint: string;
}

export async function fetchOperations(): Promise<Operation[]> {
  return (await getJSON<{ operations: Operation[] }>('/api/operations')).operations;
}

export type RunState = 'queued' | 'running' | 'success' | 'failed' | 'canceled';

export interface RunArtifact {
  name: string;
  size_bytes: number;
  checksum: string;
}

/** One run as served by /api/runs (WU-012). Timestamps are RFC 3339. */
export interface Run {
  id: number;
  instance: string;
  environment: InstanceEnv;
  operation: string;
  state: RunState;
  reason: string | null;
  error: string | null;
  job_id: string | null;
  /** Actor from the audit trail ('local-dev' until WU-020/021). */
  requested_by: string;
  submitted_at: string;
  started_at: string | null;
  finished_at: string | null;
  artifact: RunArtifact | null;
}

/**
 * Server-side run filters (SPEC-014): fields AND together, an unknown
 * value matches nothing (never an error). Values ride URL search params,
 * so they stay plain strings.
 */
export interface RunFilter {
  state?: string;
  env?: string;
  operation?: string;
}

export async function fetchRuns(filter: RunFilter = {}): Promise<Run[]> {
  const params = new URLSearchParams();
  if (filter.state) params.set('state', filter.state);
  if (filter.env) params.set('env', filter.env);
  if (filter.operation) params.set('operation', filter.operation);
  const qs = params.toString();
  return (await getJSON<{ runs: Run[] }>(qs ? `/api/runs?${qs}` : '/api/runs')).runs;
}

export async function fetchRun(id: number): Promise<Run> {
  return getJSON<Run>(`/api/runs/${id}`);
}

/**
 * Ask the engine to stop a run (WU-013). A resolved promise means the
 * cancel was accepted (202) — the run reaches `canceled` asynchronously,
 * observed via polling. 409 = not cancelable (already finished).
 */
export async function cancelRun(id: number): Promise<void> {
  await postJSON<{ status: string }>(`/api/runs/${id}/cancel`, {});
}

/** One `log` event from GET /api/runs/{id}/logs (SSE, WU-013). */
export interface RunLogLine {
  ts: string;
  line: string;
}

export interface RunLogStreamHandlers {
  /** Fires on every (re)connect. The server replays the full history each
   * time, so reset any line buffer here. */
  onOpen: () => void;
  onLine: (line: RunLogLine) => void;
  /** The run finished and the stream is complete; the source is closed. */
  onEnd: () => void;
  /** Terminal refusal (unknown run, logs gone after a portal restart):
   * the source is closed and will not retry. */
  onUnavailable: () => void;
}

/**
 * Open the live log stream for a run (SPEC-013: replay, then follow, then
 * one `end` event). Returns a close function — always call it on cleanup.
 * Network drops are retried by EventSource itself (see onOpen).
 */
export function openRunLogStream(id: number, h: RunLogStreamHandlers): () => void {
  const es = new EventSource(`/api/runs/${id}/logs`);
  es.onopen = () => h.onOpen();
  es.addEventListener('log', (e: MessageEvent<string>) => {
    h.onLine(JSON.parse(e.data) as RunLogLine);
  });
  es.addEventListener('end', () => {
    es.close();
    h.onEnd();
  });
  es.onerror = () => {
    // A CLOSED source means the server refused the stream (404/410) —
    // EventSource only auto-retries from the CONNECTING state.
    if (es.readyState === EventSource.CLOSED) {
      h.onUnavailable();
    }
  };
  return () => es.close();
}

/** Launch an operation. The consequence-labeled button calls this. */
export async function startRun(instance: string, operation: string, reason: string): Promise<Run> {
  return postJSON<Run>('/api/runs', { instance, operation, reason });
}

export interface Healthz {
  status: 'ok' | 'degraded';
  db: 'ok' | 'down';
}

/**
 * Probe /healthz. Unlike getJSON, a 503 here is a valid answer (degraded,
 * DB down) — only an unparseable/unexpected response or a network failure
 * becomes an ApiError.
 */
export async function fetchHealthz(): Promise<Healthz> {
  let res: Response;
  try {
    res = await fetch('/healthz', { headers: { Accept: 'application/json' } });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (res.status !== 200 && res.status !== 503) {
    throw new ApiError(res.status, await errorDetail(res));
  }
  try {
    return (await res.json()) as Healthz;
  } catch {
    throw new ApiError(res.status, 'healthz returned a non-JSON body');
  }
}
