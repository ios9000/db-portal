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

/**
 * Error bodies are `{"error": "message"}` (the backend's writeJSONError,
 * WU-021) — unwrap that envelope so callers can show the server's message
 * directly. Non-JSON/unshaped bodies fall back to the raw text as before.
 */
async function errorDetail(res: Response): Promise<string> {
  let text: string;
  try {
    text = await res.text();
  } catch {
    return res.statusText;
  }
  if (!text) return res.statusText;
  try {
    const body: unknown = JSON.parse(text);
    if (
      body &&
      typeof body === 'object' &&
      typeof (body as { error?: unknown }).error === 'string'
    ) {
      return (body as { error: string }).error;
    }
  } catch {
    // not JSON — fall through to the raw text
  }
  return text;
}

/**
 * Parse a 2xx body as JSON. A malformed body is still a failure of the
 * module's contract ("every failure is an ApiError") — a raw SyntaxError
 * must never escape past this point.
 */
async function parseJSON<T>(res: Response): Promise<T> {
  try {
    return (await res.json()) as T;
  } catch {
    throw new ApiError(res.status, 'response body was not valid JSON');
  }
}

/**
 * Single-slot subscription for "the session just died" (SPEC-020). The
 * shell registers one handler on mount; getJSON/postJSON/postVoid fire it
 * whenever a call comes back 401, UNLESS the caller opts out via
 * `signal401: false`. Login (bad credentials) and the bootstrap
 * `/api/auth/me` probe are expected to see 401s during normal operation —
 * without the opt-out they'd immediately re-trigger the very redirect
 * they're already handling themselves.
 */
type FetchOpts = { signal401?: boolean };

let unauthorizedHandler: (() => void) | null = null;

export function onUnauthorized(handler: () => void): void {
  unauthorizedHandler = handler;
}

function signalIfUnauthorized(status: number, opts: FetchOpts | undefined): void {
  if (status === 401 && opts?.signal401 !== false) {
    unauthorizedHandler?.();
  }
}

export async function getJSON<T>(path: string, opts?: FetchOpts): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { Accept: 'application/json' } });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    signalIfUnauthorized(res.status, opts);
    throw new ApiError(res.status, await errorDetail(res));
  }
  return parseJSON<T>(res);
}

export async function postJSON<T>(path: string, body: unknown, opts?: FetchOpts): Promise<T> {
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
    signalIfUnauthorized(res.status, opts);
    throw new ApiError(res.status, await errorDetail(res));
  }
  return parseJSON<T>(res);
}

/**
 * POST expecting no meaningful response body (204). Distinct from
 * postJSON: parsing an empty 204 body as JSON would trip the
 * malformed-body guard in parseJSON for a perfectly valid response.
 */
async function postVoid(path: string, body: unknown, opts?: FetchOpts): Promise<void> {
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
    signalIfUnauthorized(res.status, opts);
    throw new ApiError(res.status, await errorDetail(res));
  }
}

/** PATCH with a JSON body, expecting a JSON response (WU-022: schedule toggle). */
async function patchJSON<T>(path: string, body: unknown, opts?: FetchOpts): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(body),
    });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    signalIfUnauthorized(res.status, opts);
    throw new ApiError(res.status, await errorDetail(res));
  }
  return parseJSON<T>(res);
}

/**
 * DELETE expecting no meaningful response body (204, WU-022: schedule
 * delete). No request body and no Content-Type header — there's nothing
 * to encode.
 */
async function deleteVoid(path: string, opts?: FetchOpts): Promise<void> {
  let res: Response;
  try {
    res = await fetch(path, { method: 'DELETE', headers: { Accept: 'application/json' } });
  } catch (err) {
    throw new ApiError(0, err instanceof Error ? err.message : String(err));
  }
  if (!res.ok) {
    signalIfUnauthorized(res.status, opts);
    throw new ApiError(res.status, await errorDetail(res));
  }
}

/** Signed-in user (SPEC-020). The cookie is httpOnly — this is the only
 * shape the SPA ever learns identity in, via login/me. */
export interface Identity {
  username: string;
  display_name: string;
}

/** POST /api/auth/login. 401 (bad credentials) is an expected outcome the
 * login form handles inline — opt out of the generic 401 signal. */
export async function login(username: string, password: string): Promise<Identity> {
  return postJSON<Identity>('/api/auth/login', { username, password }, { signal401: false });
}

/** POST /api/auth/logout (204). A 401 here just means the session was
 * already gone — routes through the generic signal like any other call. */
export async function logout(): Promise<void> {
  await postVoid('/api/auth/logout', {});
}

/** GET /api/auth/me — the app's bootstrap identity probe. A 401 here means
 * "not signed in", handled directly by the caller, so it opts out of the
 * generic signal too. */
export async function fetchMe(): Promise<Identity> {
  return getJSON<Identity>('/api/auth/me', { signal401: false });
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
  /** Where "now" sits relative to the maintenance window (SPEC-023):
   * server-computed — the client displays it and never parses window text.
   * null = no window, or text the server couldn't parse. */
  window_state: 'inside' | 'outside' | null;
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
  requestedBy?: string;
}

export async function fetchRuns(filter: RunFilter = {}): Promise<Run[]> {
  const params = new URLSearchParams();
  if (filter.state) params.set('state', filter.state);
  if (filter.env) params.set('env', filter.env);
  if (filter.operation) params.set('operation', filter.operation);
  if (filter.requestedBy) params.set('requested_by', filter.requestedBy);
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

export type ChainState = 'running' | 'halted' | 'success';

/** One chain step (SPEC-032 mini-ADR 4): status is derived server-side —
 * `pending` while `run_id` is null, else the linked run's own state. */
export interface ChainStep {
  seq: number;
  operation: string;
  run_id: number | null;
  status: 'pending' | RunState;
}

/** A chain as served by GET /api/runs/{id}/chain and POST
 * /api/chains/{id}/resume (WU-032). Steps arrive ordered by seq. */
export interface Chain {
  id: number;
  kind: string;
  instance: string;
  env: InstanceEnv;
  state: ChainState;
  created_by: string;
  reason: string | null;
  created_at: string;
  halted_at: string | null;
  finished_at: string | null;
  steps: ChainStep[];
}

/** The chain a run is a step of, for the RunDetail strip. 404 (`ApiError`
 * status 404) for both an unknown run and a run that isn't a chain step —
 * either way there's no strip to show (SPEC-032). */
export async function fetchRunChain(id: number): Promise<Chain> {
  return getJSON<Chain>(`/api/runs/${id}/chain`);
}

/**
 * Resume a halted chain (dba-gated, no body). The failed step re-fires as a
 * new run attributed to the resumer (SPEC-032 mini-ADR 1). 409 = the chain
 * isn't halted (single-flight resume).
 */
export async function resumeChain(id: number): Promise<Chain> {
  return postJSON<Chain>(`/api/chains/${id}/resume`, {});
}

/** One registered artifact row (SPEC-030): the restore drawer's per-instance
 * source feed. Distinct from RunArtifact, the run read model's own "what
 * this run produced" sub-object — this one carries the registry id, its
 * origin run, and the retention class that governs its future pruning. */
export interface RegisteredArtifact {
  id: number;
  run_id: number;
  name: string;
  size_bytes: number;
  checksum: string;
  retention_class: 'standard' | 'safety';
  created_at: string;
}

/** List an instance's registered artifacts, newest first (SPEC-030
 * mini-ADR 4) — the restore drawer's source-artifact picker. */
export async function fetchArtifacts(instance: string): Promise<RegisteredArtifact[]> {
  const body = await getJSON<{ artifacts: RegisteredArtifact[] }>(
    `/api/artifacts?instance=${encodeURIComponent(instance)}`,
  );
  return body.artifacts;
}

/**
 * Assemble and start a restore chain (SPEC-031 mini-ADR 4): verify → safety
 * dump → restore, onto an explicit target. `confirm` carries the prod
 * typed-name ritual on the TARGET (same idiom as startRun) — the server
 * ignores it on non-prod, so it's omitted from the body rather than sent
 * empty. Returns the created chain, so the caller can navigate to its steps.
 */
export async function startRestore(args: {
  artifactId: number;
  target: string;
  confirm?: string;
  reason: string;
}): Promise<Chain> {
  return postJSON<Chain>('/api/restore', {
    artifact_id: args.artifactId,
    target: args.target,
    reason: args.reason,
    ...(args.confirm ? { confirm: args.confirm } : {}),
  });
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

// Retry tuning for a stream that dies before `end` (m1-gate item 6): base
// delay doubles each attempt, capped, and gives up after a fixed number of
// tries. EventSource cannot see HTTP status codes, so a genuine 410 (portal
// restart, logs gone) looks identical to a transient 5xx/network drop here —
// both simply keep failing every reconnect, so "retry until give-up" is the
// one design that serves both cases correctly.
const RETRY_BASE_MS = 500;
const RETRY_MAX_MS = 8_000;
const RETRY_MAX_ATTEMPTS = 5;

/**
 * Open the live log stream for a run (SPEC-013: replay, then follow, then
 * one `end` event). Returns a close function — always call it on cleanup.
 *
 * A CLOSED readyState on `error` means EventSource has given up reconnecting
 * on its own (it only auto-retries from CONNECTING). Before `end` arrives,
 * that is treated as transient: this wrapper opens a fresh EventSource with
 * exponential backoff instead of surfacing `onUnavailable` immediately, so
 * lines already delivered to the caller stay put (the caller's onOpen — not
 * this retry — is what resets its buffer, per the replay-on-reconnect
 * contract). Only once retries are exhausted does the stream give up for
 * real. An error after `end` (or once the caller's cleanup runs) is ignored.
 */
export function openRunLogStream(id: number, h: RunLogStreamHandlers): () => void {
  let es: EventSource;
  let attempt = 0;
  let ended = false;
  let stopped = false;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;

  const connect = () => {
    es = new EventSource(`/api/runs/${id}/logs`);
    es.onopen = () => {
      attempt = 0; // a live connection means the failure streak is over
      h.onOpen();
    };
    es.addEventListener('log', (e: MessageEvent<string>) => {
      h.onLine(JSON.parse(e.data) as RunLogLine);
    });
    es.addEventListener('end', () => {
      ended = true;
      es.close();
      h.onEnd();
    });
    es.onerror = () => {
      if (stopped || ended) return; // stream already done — clean close, no retry
      if (es.readyState !== EventSource.CLOSED) return; // browser is retrying on its own
      es.close();
      if (attempt >= RETRY_MAX_ATTEMPTS) {
        h.onUnavailable();
        return;
      }
      const delay = Math.min(RETRY_BASE_MS * 2 ** attempt, RETRY_MAX_MS);
      attempt += 1;
      retryTimer = setTimeout(connect, delay);
    };
  };
  connect();

  return () => {
    stopped = true;
    clearTimeout(retryTimer);
    es.close();
  };
}

/**
 * Launch an operation. The consequence-labeled button calls this. `confirm`
 * carries the prod typed-name ritual (SPEC-015/021); the server ignores it
 * on non-prod, so it's omitted from the body rather than sent empty.
 */
export async function startRun(
  instance: string,
  operation: string,
  reason: string,
  confirm?: string,
): Promise<Run> {
  return postJSON<Run>('/api/runs', {
    instance,
    operation,
    reason,
    ...(confirm ? { confirm } : {}),
  });
}

export type ScheduleFireStatus = 'fired' | 'skipped_overlap' | 'error';

/** One recurring schedule as served by GET /api/schedules (WU-022). Times
 * are RFC 3339; `next_fire_at` is already jittered server-side — display
 * it, never recompute it client-side. */
export interface Schedule {
  id: number;
  instance: string;
  env: InstanceEnv;
  operation: string;
  cron_spec: string;
  reason: string | null;
  enabled: boolean;
  created_by: string;
  created_at: string;
  next_fire_at: string | null;
  last_fired_at: string | null;
  last_run_id: number | null;
  last_fire_status: ScheduleFireStatus | null;
}

export async function fetchSchedules(): Promise<Schedule[]> {
  return (await getJSON<{ schedules: Schedule[] }>('/api/schedules')).schedules;
}

/**
 * Create a recurring schedule. `confirm` carries the prod typed-name ritual
 * (same as startRun) — the server ignores it on non-prod, so it's omitted
 * from the body rather than sent empty.
 */
export async function createSchedule(
  instance: string,
  operation: string,
  cronSpec: string,
  reason: string,
  confirm?: string,
): Promise<Schedule> {
  return postJSON<Schedule>('/api/schedules', {
    instance,
    operation,
    cron_spec: cronSpec,
    reason,
    ...(confirm ? { confirm } : {}),
  });
}

export async function setScheduleEnabled(id: number, enabled: boolean): Promise<Schedule> {
  return patchJSON<Schedule>(`/api/schedules/${id}`, { enabled });
}

export async function deleteSchedule(id: number): Promise<void> {
  await deleteVoid(`/api/schedules/${id}`);
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
