import { useState } from 'react';
import { Link } from 'react-router';
import { ApiError, startRun, type Instance, type Operation, type Run } from '../lib/api';
import { EnvBadge } from './EnvBadge';
import { EnvBanner } from './EnvBanner';

interface Props {
  instance: Instance;
  operation: Operation;
  onClose: () => void;
}

/**
 * Launch drawer (design brief Screens 3–4, adapted to the MVP catalog):
 * env banner, target restated with its env badge, catalog info chips,
 * plain-language description, optional reason, and a primary button labeled
 * with the consequence — never "OK" or "Submit". Prod adds the typed-name
 * ritual (SPEC-015, guardrail layer 2): the button unlocks only when the
 * operator has typed the exact instance name, paste disabled. Non-prod
 * stays one click. Scheduling arrives with WU-022.
 */
export function LaunchDrawer({ instance, operation, onClose }: Props) {
  const [reason, setReason] = useState('');
  const [confirmName, setConfirmName] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [started, setStarted] = useState<Run | null>(null);
  const [error, setError] = useState<string | null>(null);

  const isProd = instance.env === 'prod';
  const confirmed = !isProd || confirmName === instance.name;

  // Overlay (backdrop) click is a soft dismiss — it must not eat a launch in
  // flight or a just-fired prod confirmation. Explicit Cancel/Close buttons
  // bypass this and always work.
  const dismissOverlay = () => {
    if (submitting || started !== null) return;
    onClose();
  };

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
    <div className="drawer-overlay" onClick={dismissOverlay}>
      <aside
        className="drawer"
        role="dialog"
        aria-label={`Run ${operation.label}`}
        onClick={(e) => e.stopPropagation()}
      >
        <EnvBanner env={instance.env} />

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

            {isProd && (
              <label className="drawer-field">
                To confirm, type the instance name{' '}
                <span className="optional">(paste disabled)</span>
                <input
                  type="text"
                  value={confirmName}
                  onChange={(e) => setConfirmName(e.target.value)}
                  onPaste={(e) => e.preventDefault()}
                  onDrop={(e) => e.preventDefault()}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={instance.name}
                />
              </label>
            )}

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
                disabled={submitting || !confirmed}
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
              <Link className="btn-primary" to={`/runs/${started.id}`} onClick={onClose}>
                View run
              </Link>
            </footer>
          </>
        )}
      </aside>
    </div>
  );
}
