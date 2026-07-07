import { useState } from 'react';
import { Link } from 'react-router';
import { ApiError, startRun, type Instance, type Operation, type Run } from '../lib/api';
import { EnvBadge } from './EnvBadge';

interface Props {
  instance: Instance;
  operation: Operation;
  onClose: () => void;
}

/**
 * Launch drawer (design brief Screen 3, adapted to the MVP catalog): target
 * restated with its env badge, catalog info chips, plain-language
 * description, optional reason, and a primary button labeled with the
 * consequence — never "OK" or "Submit". The typed-name prod ritual arrives
 * with WU-015; scheduling with WU-022.
 */
export function LaunchDrawer({ instance, operation, onClose }: Props) {
  const [reason, setReason] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [started, setStarted] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  const launch = async () => {
    setSubmitting(true);
    setError(null);
    try {
      setStarted(await startRun(instance.name, operation.id, reason.trim()));
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 0
          ? 'API unreachable — is the backend running?'
          : 'The run could not be started.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="drawer-overlay" onClick={onClose}>
      <aside
        className="drawer"
        role="dialog"
        aria-label={`Run ${operation.label}`}
        onClick={(e) => e.stopPropagation()}
      >
        <header className="drawer-header">
          <span aria-hidden="true">{operation.icon}</span>
          <h2>Run {operation.label}</h2>
        </header>

        <div className="drawer-target">
          <span className="drawer-target-name">{instance.name}</span>
          <EnvBadge env={instance.env} />
        </div>

        {started === null ? (
          <>
            <div className="chip-strip">
              <span className="chip">{operation.duration_hint}</span>
              <span className="chip">{operation.online_hint}</span>
            </div>
            <p className="drawer-description">{operation.description}</p>

            <label className="drawer-field">
              Reason / ticket # <span className="optional">(optional)</span>
              <input
                type="text"
                value={reason}
                onChange={(e) => setReason(e.target.value)}
                placeholder="CHG-1234"
              />
            </label>

            {error !== null && (
              <p className="drawer-error" role="alert">
                {error}
              </p>
            )}

            <footer className="drawer-footer">
              <button type="button" className="btn-text" onClick={onClose} disabled={submitting}>
                Cancel
              </button>
              <button
                type="button"
                className="btn-primary"
                onClick={() => void launch()}
                disabled={submitting}
              >
                {submitting
                  ? 'Starting…'
                  : `Run ${operation.label.toLowerCase()} on ${instance.name}`}
              </button>
            </footer>
          </>
        ) : (
          <>
            <p className="drawer-started" role="status">
              ✓ Run #{started.id} started — you can close this page.
            </p>
            <footer className="drawer-footer">
              <button type="button" className="btn-text" onClick={onClose}>
                Close
              </button>
              <Link className="btn-primary" to="/activity" onClick={onClose}>
                View in Activity
              </Link>
            </footer>
          </>
        )}
      </aside>
    </div>
  );
}
