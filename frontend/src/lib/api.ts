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
  submitted_at: string;
  started_at: string | null;
  finished_at: string | null;
  artifact: RunArtifact | null;
}

export async function fetchRuns(): Promise<Run[]> {
  return (await getJSON<{ runs: Run[] }>('/api/runs')).runs;
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
