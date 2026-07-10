import { useCallback, useEffect, useState } from 'react';
import { Navigate, Route, Routes, useLocation } from 'react-router';
import { Shell } from './components/Shell';
import { fetchMe, type Identity, onUnauthorized } from './lib/api';
import { LoginPage } from './pages/Login';

/**
 * Root bootstrap gate (SPEC-020). `identity` states:
 *   undefined — the /api/auth/me probe hasn't answered yet (brief loading).
 *   null      — signed out: bootstrap 401, an explicit sign-out, or a
 *               mid-session 401 relayed by api.ts's onUnauthorized signal.
 *   Identity  — signed in; the authenticated shell renders.
 * `/login` itself lives outside the shell (no nav chrome) and only ever
 * shows while signed out; a signed-in visit to it bounces to `/`.
 */
function App() {
  const [identity, setIdentity] = useState<Identity | null | undefined>(undefined);
  const location = useLocation();

  useEffect(() => {
    fetchMe()
      .then(setIdentity)
      .catch(() => setIdentity(null));
  }, []);

  // The one place the whole app reacts to "the session just died" instead
  // of every page checking for it individually.
  useEffect(() => {
    onUnauthorized(() => setIdentity(null));
  }, []);

  const handleSignOut = useCallback(() => setIdentity(null), []);

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
