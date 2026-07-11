import { useState } from 'react';
import { Navigate, NavLink, Route, Routes } from 'react-router';
import { ApiError, type Identity, logout } from '../lib/api';
import { Activity } from '../pages/Activity';
import { MyDatabases } from '../pages/MyDatabases';
import { RunDetail } from '../pages/RunDetail';
import { Schedules } from '../pages/Schedules';
import { StatusFooter } from './StatusFooter';

interface Props {
  identity: Identity;
  /** Drop the app back to signed-out state. Called only when the server
   * CONFIRMED the session is gone — a 2xx logout or a 401 (already gone).
   * The cookie is httpOnly, so the client cannot revoke anything itself;
   * pretending to sign out on other failures would leave a live session
   * behind a signed-out screen (M2-gate finding 7). */
  onSignOut: () => void;
}

/**
 * The authenticated app: nav chrome + routes (formerly all of App.tsx,
 * split out in WU-020 so the bootstrap gate in App.tsx can render `/login`
 * with no chrome at all).
 */
export function Shell({ identity, onSignOut }: Props) {
  const [signingOut, setSigningOut] = useState(false);
  const [signOutError, setSignOutError] = useState(false);

  const handleSignOut = async () => {
    setSigningOut(true);
    setSignOutError(false);
    try {
      await logout();
      onSignOut();
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onSignOut(); // session already gone server-side
      } else {
        setSignOutError(true); // the session row is still alive — say so
      }
    } finally {
      setSigningOut(false);
    }
  };

  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">DB Portal</span>
        <nav aria-label="Primary">
          <NavLink to="/databases">My Databases</NavLink>
          <NavLink to="/activity">Activity</NavLink>
          <NavLink to="/schedules">Schedules</NavLink>
        </nav>
        <span className="spacer" />
        <div className="topbar-user">
          {signOutError && (
            <span className="signout-error" role="alert">
              Could not confirm sign-out — try again.
            </span>
          )}
          <span className="identity">{identity.display_name}</span>
          <button
            type="button"
            className="btn-text"
            onClick={() => void handleSignOut()}
            disabled={signingOut}
          >
            Sign out
          </button>
        </div>
      </header>
      <main className="content">
        <Routes>
          <Route index element={<Navigate to="/databases" replace />} />
          <Route path="/databases" element={<MyDatabases />} />
          <Route path="/activity" element={<Activity />} />
          <Route path="/runs/:id" element={<RunDetail />} />
          <Route path="/schedules" element={<Schedules />} />
          <Route
            path="*"
            element={
              <>
                <h1>Not found</h1>
                <p className="placeholder">No such page.</p>
              </>
            }
          />
        </Routes>
      </main>
      <StatusFooter />
    </div>
  );
}
