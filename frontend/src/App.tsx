import { Navigate, NavLink, Route, Routes } from 'react-router';
import { StatusFooter } from './components/StatusFooter';
import { Activity } from './pages/Activity';
import { MyDatabases } from './pages/MyDatabases';
import { RunDetail } from './pages/RunDetail';
import { Schedules } from './pages/Schedules';

function App() {
  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">DB Portal</span>
        <nav aria-label="Primary">
          <NavLink to="/databases">My Databases</NavLink>
          <NavLink to="/activity">Activity</NavLink>
          <NavLink to="/schedules">Schedules</NavLink>
        </nav>
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

export default App;
