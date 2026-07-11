import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import App from './App';

const IDENTITY = { username: 'dba1', display_name: 'DBA One' };

/**
 * Stub the shell's whole API surface by path: bootstrap /api/auth/me, the
 * default /databases route's instance list + operation catalog, /healthz
 * for the footer, and /api/auth/logout for the sign-out button. Each is
 * overridable per test (extends the by-path stub pattern already used in
 * MyDatabases.test.tsx / StatusFooter.test.tsx).
 */
function stubApp(
  overrides: {
    me?: () => Promise<Response>;
    instances?: () => Promise<Response>;
    logout?: () => Promise<Response>;
  } = {},
) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://test');
    if (url.pathname === '/api/auth/me') {
      return (overrides.me ?? (() => Promise.resolve(new Response(JSON.stringify(IDENTITY)))))();
    }
    if (url.pathname === '/api/auth/logout' && init?.method === 'POST') {
      return (overrides.logout ?? (() => Promise.resolve(new Response(null, { status: 204 }))))();
    }
    if (url.pathname === '/healthz') {
      return Promise.resolve(new Response(JSON.stringify({ status: 'ok', db: 'ok' })));
    }
    if (url.pathname === '/api/operations') {
      return Promise.resolve(new Response(JSON.stringify({ operations: [] })));
    }
    if (url.pathname === '/api/instances') {
      return (
        overrides.instances ??
        (() => Promise.resolve(new Response(JSON.stringify({ instances: [] }))))
      )();
    }
    return Promise.resolve(new Response('not found', { status: 404 }));
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('a successful bootstrap renders the shell, nav, identity and healthz status', async () => {
  stubApp();

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  // brief loading window before /api/auth/me answers
  expect(screen.getByText('Loading…')).toBeInTheDocument();

  const nav = await screen.findByRole('navigation', { name: 'Primary' });
  expect(nav).toBeInTheDocument();
  for (const label of ['My Databases', 'Activity', 'Schedules']) {
    expect(screen.getByRole('link', { name: label })).toBeInTheDocument();
  }

  // index route redirects to /databases — this is a second, effect-driven
  // render past the one that first mounted the nav, so it needs its own wait.
  expect(await screen.findByRole('heading', { name: 'My Databases' })).toBeInTheDocument();

  // signed-in identity shown in the shell
  expect(screen.getByText('DBA One')).toBeInTheDocument();

  // footer probed /healthz and rendered the result
  expect(await screen.findByText('API ok · DB ok')).toBeInTheDocument();
});

test('a 401 from the bootstrap probe redirects to /login, no chrome', async () => {
  stubApp({
    me: () => Promise.resolve(new Response('{"error":"authentication required"}', { status: 401 })),
  });

  render(
    <MemoryRouter initialEntries={['/activity']}>
      <App />
    </MemoryRouter>,
  );

  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
  expect(screen.queryByRole('navigation', { name: 'Primary' })).not.toBeInTheDocument();
});

test('a 401 mid-session drops back to /login via the onUnauthorized subscription', async () => {
  stubApp({
    instances: () =>
      Promise.resolve(new Response('{"error":"authentication required"}', { status: 401 })),
  });

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  // shell mounts first (bootstrap succeeded)...
  await screen.findByRole('navigation', { name: 'Primary' });
  // ...then My Databases' own /api/instances call comes back 401 and the
  // generic signal (not a per-page check) drops the app to signed-out.
  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
});

test('sign out calls the logout endpoint and lands on /login', async () => {
  const mock = stubApp();
  const user = userEvent.setup();

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  await screen.findByRole('navigation', { name: 'Primary' });
  await user.click(screen.getByRole('button', { name: 'Sign out' }));

  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
  expect(mock.mock.calls.some((c) => String(c[0]).includes('/api/auth/logout'))).toBe(true);
});

test('visiting /login while already signed in bounces to /', async () => {
  stubApp();

  render(
    <MemoryRouter initialEntries={['/login']}>
      <App />
    </MemoryRouter>,
  );

  expect(await screen.findByRole('heading', { name: 'My Databases' })).toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Sign in' })).not.toBeInTheDocument();
});

// M2-gate finding 6: only a 401 means signed out. A degraded backend gets
// a retry screen, never the sign-in form.
test('a non-401 bootstrap failure shows retry, not the sign-in form; retry recovers', async () => {
  let probes = 0;
  stubApp({
    me: () => {
      probes += 1;
      return probes === 1
        ? Promise.resolve(new Response('{"error":"sessions unavailable"}', { status: 500 }))
        : Promise.resolve(new Response(JSON.stringify(IDENTITY)));
    },
  });
  const user = userEvent.setup();

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  const alert = await screen.findByRole('alert');
  expect(alert.textContent).toContain('unreachable');
  expect(screen.queryByRole('heading', { name: 'Sign in' })).not.toBeInTheDocument();

  await user.click(screen.getByRole('button', { name: 'Retry' }));
  expect(await screen.findByRole('navigation', { name: 'Primary' })).toBeInTheDocument();
});

// M2-gate finding 7: a failed sign-out must not pretend — the httpOnly
// cookie is still valid, so the session stays visibly signed in.
test('a failed sign-out keeps the session and says so', async () => {
  stubApp({
    logout: () => Promise.resolve(new Response('{"error":"boom"}', { status: 500 })),
  });
  const user = userEvent.setup();

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  await screen.findByRole('navigation', { name: 'Primary' });
  await user.click(screen.getByRole('button', { name: 'Sign out' }));

  const alert = await screen.findByRole('alert');
  expect(alert.textContent).toContain('Could not confirm sign-out');
  expect(screen.getByText('DBA One')).toBeInTheDocument();
  expect(screen.queryByRole('heading', { name: 'Sign in' })).not.toBeInTheDocument();
});

// The one failure that IS a completed sign-out: the session was already
// gone server-side (401) — signing out locally is then honest.
test('sign-out on an already-dead session still lands on /login', async () => {
  stubApp({
    logout: () =>
      Promise.resolve(new Response('{"error":"authentication required"}', { status: 401 })),
  });
  const user = userEvent.setup();

  render(
    <MemoryRouter initialEntries={['/']}>
      <App />
    </MemoryRouter>,
  );

  await screen.findByRole('navigation', { name: 'Primary' });
  await user.click(screen.getByRole('button', { name: 'Sign out' }));

  expect(await screen.findByRole('heading', { name: 'Sign in' })).toBeInTheDocument();
});
