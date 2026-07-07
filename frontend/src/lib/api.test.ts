import { afterEach, expect, test, vi } from 'vitest';
import { ApiError, fetchHealthz, getJSON } from './api';

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
