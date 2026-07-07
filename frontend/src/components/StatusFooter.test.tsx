import { render, screen } from '@testing-library/react';
import { afterEach, expect, test, vi } from 'vitest';
import { StatusFooter } from './StatusFooter';

function stubHealthz(response: () => Promise<Response>) {
  vi.stubGlobal('fetch', vi.fn(response));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('shows ok when /healthz answers 200', async () => {
  stubHealthz(() => Promise.resolve(new Response(JSON.stringify({ status: 'ok', db: 'ok' }))));
  render(<StatusFooter />);
  expect(await screen.findByText('API ok · DB ok')).toBeInTheDocument();
});

test('shows degraded when /healthz answers 503', async () => {
  stubHealthz(() =>
    Promise.resolve(
      new Response(JSON.stringify({ status: 'degraded', db: 'down' }), { status: 503 }),
    ),
  );
  render(<StatusFooter />);
  expect(await screen.findByText('API ok · DB down')).toBeInTheDocument();
});

test('shows unreachable when the probe fails at the network level', async () => {
  stubHealthz(() => Promise.reject(new TypeError('fetch failed')));
  render(<StatusFooter />);
  expect(await screen.findByText('API unreachable')).toBeInTheDocument();
});
