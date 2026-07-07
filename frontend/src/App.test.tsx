import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import App from './App';

afterEach(() => {
  vi.unstubAllGlobals();
});

test('shell renders nav, redirects / to My Databases, and shows healthz status', async () => {
  // The shell fires two fetches on load: the footer's /healthz probe and
  // MyDatabases' instance list (WU-011) — answer each by path.
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL) =>
      Promise.resolve(
        String(input).includes('/healthz')
          ? new Response(JSON.stringify({ status: 'ok', db: 'ok' }))
          : new Response(JSON.stringify({ instances: [] })),
      ),
    ),
  );

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  const nav = screen.getByRole('navigation', { name: 'Primary' });
  expect(nav).toBeInTheDocument();
  for (const label of ['My Databases', 'Activity', 'Schedules']) {
    expect(screen.getByRole('link', { name: label })).toBeInTheDocument();
  }

  // index route redirects to /databases
  expect(screen.getByRole('heading', { name: 'My Databases' })).toBeInTheDocument();

  // footer probed /healthz and rendered the result
  expect(await screen.findByText('API ok · DB ok')).toBeInTheDocument();
});
