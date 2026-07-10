import { type FormEvent, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { ApiError, login, type Identity } from '../lib/api';

interface Props {
  /** Called with the freshly authenticated identity; the bootstrap gate in
   * App.tsx owns the actual state, this page just reports the result. */
  onLogin: (identity: Identity) => void;
}

/** Where App.tsx stashes the path a redirect-to-login interrupted, so a
 * successful sign-in can return the operator to it (SPEC-020). */
export interface LoginState {
  from?: { pathname: string; search: string };
}

/**
 * `/login` (SPEC-020 UI): centered card, no nav chrome — rendered outside
 * the authenticated shell entirely. No self-service anything; AD (or the
 * fake/break-glass directory) owns passwords.
 */
export function LoginPage({ onLogin }: Props) {
  const location = useLocation();
  const navigate = useNavigate();

  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const from = (location.state as LoginState | null)?.from;
  const redirectTo = from ? `${from.pathname}${from.search}` : '/';

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setPending(true);
    setError(null);
    try {
      const identity = await login(username, password);
      onLogin(identity);
      navigate(redirectTo, { replace: true });
    } catch (err) {
      // SPEC-020: uniform "Sign-in failed" on bad credentials — no
      // user-exists oracle, no chattier hint about break-glass. Network
      // unreachable is a different, non-security condition and gets the
      // same wording every other page uses for it.
      setError(
        err instanceof ApiError && err.status === 0
          ? 'API unreachable — is the backend running?'
          : 'Sign-in failed',
      );
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="login-screen">
      <form className="login-card" onSubmit={(e) => void handleSubmit(e)}>
        <h1>Sign in</h1>
        <p className="login-subtitle">DB Portal — sign in with your directory account.</p>

        <label className="drawer-field">
          Username
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            autoComplete="username"
            required
          />
        </label>

        <label className="drawer-field">
          Password
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </label>

        {error !== null && (
          <p className="login-error" role="alert">
            {error}
          </p>
        )}

        <button type="submit" className="btn-primary" disabled={pending}>
          {pending ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
