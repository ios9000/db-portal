import { useCallback, useEffect, useState } from 'react';
import { Navigate, Route, Routes, useLocation } from 'react-router';
import { Shell } from './components/Shell';
import { ApiError, fetchMe, type Identity, onUnauthorized } from './lib/api';
import { LoginPage } from './pages/Login';

/**
 * Root bootstrap gate (SPEC-020). `identity` states:
 *   undefined — the /api/auth/me probe hasn't answered yet (brief loading).
 *   null      — signed out: bootstrap 401, an explicit sign-out, or a
 *               mid-session 401 relayed by api.ts's onUnauthorized signal.
 *   Identity  — signed in; the authenticated shell renders.
 * Only a 401 means signed out (M2-gate finding 6): any other probe failure
 * — a degraded backend, a network blip — shows a retry screen instead of
 * silently presenting the sign-in form to someone who may well still hold
 * a valid session.
 * `/login` itself lives outside the shell (no nav chrome) and only ever
 * shows while signed out; a signed-in visit to it bounces to `/`.
 */
function App() {
  const [identity, setIdentity] = useState<Identity | null | undefined>(undefined);
  const [bootFailed, setBootFailed] = useState(false);
  const location = useLocation();

  const probe = useCallback(() => {
    setBootFailed(false);
    setIdentity(undefined);
    fetchMe()
      .then(setIdentity)
      .catch((err: unknown) => {
        if (err instanceof ApiError && err.status === 401) {
          setIdentity(null);
        } else {
          setBootFailed(true);
        }
      });
  }, []);

  useEffect(probe, [probe]);

  // The one place the whole app reacts to "the session just died" instead
  // of every page checking for it individually.
  useEffect(() => {
    onUnauthorized(() => setIdentity(null));
  }, []);

  const handleSignOut = useCallback(() => setIdentity(null), []);

  if (bootFailed) {
    return (
      <div className="app-loading">
        <p className="placeholder" role="alert">
          The portal is unreachable — your session may still be active.
        </p>
        <button type="button" className="btn-secondary" onClick={probe}>
          Retry
        </button>
      </div>
    );
  }

  if (identity === undefined) {
    return (
      <div className="app-loading">
        <p className="placeholder">Loading…</p>
      </div>
    );
  }

  return (
    <Routes>
      <Route
        path="/login"
        element={
          identity === null ? <LoginPage onLogin={setIdentity} /> : <Navigate to="/" replace />
        }
      />
      <Route
        path="*"
        element={
          identity === null ? (
            <Navigate
              to="/login"
              replace
              state={{ from: { pathname: location.pathname, search: location.search } }}
            />
          ) : (
            <Shell identity={identity} onSignOut={handleSignOut} />
          )
        }
      />
    </Routes>
  );
}

export default App;
