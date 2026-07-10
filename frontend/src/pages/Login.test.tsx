import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Identity } from '../lib/api';
import { LoginPage } from './Login';

const IDENTITY: Identity = { username: 'dba1', display_name: 'DBA One' };

function stubLogin(respond: () => Promise<Response>) {
  vi.stubGlobal('fetch', vi.fn(respond));
}

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Render the login page with a couple of landing routes to prove where
 * navigate() actually took the user, the same way the real router tree
 * would resolve `/` or a carried "from" path. */
function renderLogin(
  initialEntries: Parameters<typeof MemoryRouter>[0]['initialEntries'],
  onLogin = vi.fn(),
) {
  render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route path="/login" element={<LoginPage onLogin={onLogin} />} />
        <Route path="/" element={<div>Landed home</div>} />
        <Route path="/runs/9" element={<div>Landed on run 9</div>} />
      </Routes>
    </MemoryRouter>,
  );
  return onLogin;
}

test('successful submit posts credentials and navigates home by default', async () => {
  const mock = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(IDENTITY))),
  );
  vi.stubGlobal('fetch', mock);
  const user = userEvent.setup();
  const onLogin = renderLogin(['/login']);

  await user.type(screen.getByLabelText('Username'), 'dba1');
  await user.type(screen.getByLabelText('Password'), 'dba1');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  expect(await screen.findByText('Landed home')).toBeInTheDocument();
  expect(onLogin).toHaveBeenCalledWith(IDENTITY);

  const [path, init] = mock.mock.calls[0];
  expect(String(path)).toBe('/api/auth/login');
  expect(JSON.parse(String((init as RequestInit).body))).toEqual({
    username: 'dba1',
    password: 'dba1',
  });
});

test('after a redirect-to-login, a successful sign-in returns to the intended path', async () => {
  stubLogin(() => Promise.resolve(new Response(JSON.stringify(IDENTITY))));
  const user = userEvent.setup();
  renderLogin([{ pathname: '/login', state: { from: { pathname: '/runs/9', search: '' } } }]);

  await user.type(screen.getByLabelText('Username'), 'dba1');
  await user.type(screen.getByLabelText('Password'), 'dba1');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  expect(await screen.findByText('Landed on run 9')).toBeInTheDocument();
});

test('401 shows "Sign-in failed" and keeps the form for another try', async () => {
  stubLogin(() =>
    Promise.resolve(new Response('{"error":"authentication required"}', { status: 401 })),
  );
  const user = userEvent.setup();
  renderLogin(['/login']);

  await user.type(screen.getByLabelText('Username'), 'dba1');
  await user.type(screen.getByLabelText('Password'), 'wrong');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  expect(await screen.findByRole('alert')).toHaveTextContent('Sign-in failed');
  expect(screen.getByLabelText('Username')).toHaveValue('dba1');
  expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument();
});

test('submit is disabled while the login request is pending', async () => {
  let resolveFetch: (r: Response) => void = () => {};
  stubLogin(
    () =>
      new Promise((resolve) => {
        resolveFetch = resolve;
      }),
  );
  const user = userEvent.setup();
  renderLogin(['/login']);

  await user.type(screen.getByLabelText('Username'), 'dba1');
  await user.type(screen.getByLabelText('Password'), 'dba1');
  await user.click(screen.getByRole('button', { name: 'Sign in' }));

  const pendingButton = screen.getByRole('button', { name: 'Signing in…' });
  expect(pendingButton).toBeDisabled();

  resolveFetch(new Response(JSON.stringify(IDENTITY)));
  expect(await screen.findByText('Landed home')).toBeInTheDocument();
});
