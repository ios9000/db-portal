import { afterEach, expect, test, vi } from 'vitest';
import { ApiError, fetchHealthz, fetchRuns, getJSON, postJSON, startRun, type Run } from './api';

function stubFetch(impl: (input: RequestInfo | URL) => Promise<Response>) {
  vi.stubGlobal('fetch', vi.fn(impl));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('getJSON returns the parsed body on 200', async () => {
  stubFetch(() => Promise.resolve(new Response(JSON.stringify({ hello: 'world' }))));
  await expect(getJSON<{ hello: string }>('/api/x')).resolves.toEqual({ hello: 'world' });
});

test('getJSON throws a typed ApiError with status and detail on HTTP errors', async () => {
  stubFetch(() => Promise.resolve(new Response('boom', { status: 500 })));
  const err = await getJSON('/api/x').catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(500);
  expect((err as ApiError).detail).toBe('boom');
});

test('getJSON maps network failure to ApiError with status 0', async () => {
  stubFetch(() => Promise.reject(new TypeError('fetch failed')));
  const err = await getJSON('/api/x').catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(0);
  expect((err as ApiError).message).toContain('unreachable');
});

test('fetchHealthz treats 503 as a valid degraded answer', async () => {
  stubFetch(() =>
    Promise.resolve(
      new Response(JSON.stringify({ status: 'degraded', db: 'down' }), { status: 503 }),
    ),
  );
  await expect(fetchHealthz()).resolves.toEqual({ status: 'degraded', db: 'down' });
});

test('fetchHealthz throws ApiError on unexpected statuses', async () => {
  stubFetch(() => Promise.resolve(new Response('gateway', { status: 502 })));
  const err = await fetchHealthz().catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(502);
});

// m1-gate item 12: a malformed 2xx body must not let a raw SyntaxError
// escape — every failure is an ApiError, no exceptions.
test('getJSON throws ApiError when a 2xx body is not valid JSON', async () => {
  stubFetch(() => Promise.resolve(new Response('not json{', { status: 200 })));
  const err = await getJSON('/api/x').catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(200);
});

test('postJSON throws ApiError when a 2xx body is not valid JSON', async () => {
  stubFetch(() => Promise.resolve(new Response('not json{', { status: 201 })));
  const err = await postJSON('/api/x', {}).catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(201);
});

// WU-021: the backend's writeJSONError envelope is {"error": "message"} —
// unwrap it so callers can render the server's message directly instead of
// the raw JSON text.
test('error detail unwraps a {error} JSON body to the plain message', async () => {
  stubFetch(() =>
    Promise.resolve(new Response(JSON.stringify({ error: 'dba role required' }), { status: 403 })),
  );
  const err = await getJSON('/api/x').catch((e: unknown) => e);
  expect(err).toBeInstanceOf(ApiError);
  expect((err as ApiError).status).toBe(403);
  expect((err as ApiError).detail).toBe('dba role required');
});

const RUN: Run = {
  id: 1,
  instance: 'billing-test',
  environment: 'test',
  operation: 'dump',
  state: 'queued',
  reason: null,
  error: null,
  job_id: null,
  requested_by: 'dba1',
  submitted_at: '2026-07-10T00:00:00Z',
  started_at: null,
  finished_at: null,
  artifact: null,
};

function stubPostFetch(body: unknown) {
  const mock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(body), { status: 201 })),
  );
  vi.stubGlobal('fetch', mock);
  return mock;
}

function captureBody(mock: ReturnType<typeof stubPostFetch>): Record<string, unknown> {
  const init = mock.mock.calls[0][1]!;
  return JSON.parse(init.body as string) as Record<string, unknown>;
}

test('startRun omits confirm from the body when not provided', async () => {
  const mock = stubPostFetch(RUN);

  await startRun('billing-test', 'dump', 'CHG-1');

  expect(captureBody(mock)).toEqual({
    instance: 'billing-test',
    operation: 'dump',
    reason: 'CHG-1',
  });
});

test('startRun includes confirm in the body when provided', async () => {
  const mock = stubPostFetch(RUN);

  await startRun('billing-prod', 'dump', '', 'billing-prod');

  expect(captureBody(mock)).toEqual({
    instance: 'billing-prod',
    operation: 'dump',
    reason: '',
    confirm: 'billing-prod',
  });
});

test('fetchRuns sends requested_by when the filter is set', async () => {
  const mock = vi.fn((_input: RequestInfo | URL) =>
    Promise.resolve(new Response(JSON.stringify({ runs: [] }))),
  );
  vi.stubGlobal('fetch', mock);

  await fetchRuns({ requestedBy: 'dba1' });

  expect(String(mock.mock.calls[0][0])).toBe('/api/runs?requested_by=dba1');
});
