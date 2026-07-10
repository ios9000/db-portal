import { useState } from 'react';
import { Navigate, NavLink, Route, Routes } from 'react-router';
import { type Identity, logout } from '../lib/api';
import { Activity } from '../pages/Activity';
import { MyDatabases } from '../pages/MyDatabases';
import { RunDetail } from '../pages/RunDetail';
import { Schedules } from '../pages/Schedules';
import { StatusFooter } from './StatusFooter';

interface Props {
  identity: Identity;
  /** Drop the app back to signed-out state. Called after logout() settles
   * either way — a 401 here just means the session was already gone, and
   * there's nothing else the button can meaningfully do about it. */
  onSignOut: () => void;
}

/**
 * The authenticated app: nav chrome + routes (formerly all of App.tsx,
 * split out in WU-020 so the bootstrap gate in App.tsx can render `/login`
 * with no chrome at all).
 */
export function Shell({ identity, onSignOut }: Props) {
  const [signingOut, setSigningOut] = useState(false);

  const handleSignOut = async () => {
    setSigningOut(true);
    try {
      await logout();
    } catch {
      // session already gone server-side — sign out locally regardless
    } finally {
      onSignOut();
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
