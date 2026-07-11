import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import {
  ApiError,
  fetchArtifacts,
  fetchInstances,
  startRestore,
  type Chain,
  type Instance,
  type RegisteredArtifact,
} from '../lib/api';
import { formatBytes } from '../lib/format';
import { EnvBadge } from './EnvBadge';
import { EnvBanner } from './EnvBanner';

interface Props {
  source: Instance;
  onClose: () => void;
}

/**
 * Restore drawer (SPEC-031 UI slice), mirroring LaunchDrawer's grammar: env
 * banner, target restated, optional reason, the prod typed-name ritual, a
 * primary button labeled with the consequence, and a success state with a
 * link instead of auto-navigation. Two things make this drawer different
 * from a launch:
 *
 *  - the entry instance is the SOURCE (its artifact registry is what's
 *    browsed here) — the TARGET is a separate, explicit choice, defaulted
 *    to the source only when the source is non-prod (mini-ADR 5 + D2), so
 *    a prod target is never pre-filled;
 *  - the safety dump is shown as a fixed, unconditional step in the plan
 *    preview — never a checkbox, never skippable.
 */
export function RestoreDrawer({ source, onClose }: Props) {
  const [artifacts, setArtifacts] = useState<RegisteredArtifact[] | null>(null);
  const [artifactsError, setArtifactsError] = useState<string | null>(null);
  const [selectedArtifactId, setSelectedArtifactId] = useState<number | null>(null);

  const [instances, setInstances] = useState<Instance[] | null>(null);
  const [instancesError, setInstancesError] = useState<string | null>(null);
  const [targetName, setTargetName] = useState('');

  const [reason, setReason] = useState('');
  const [confirmName, setConfirmName] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [started, setStarted] = useState<Chain | null>(null);
  const [error, setError] = useState<string | null>(null);

  // The source's registry — its own env filter is irrelevant here, this is
  // always the one instance's full backup list (SPEC-030 mini-ADR 4).
  useEffect(() => {
    let cancelled = false;
    fetchArtifacts(source.name)
      .then((list) => {
        if (cancelled) return;
        setArtifacts(list);
        if (list.length > 0) setSelectedArtifactId(list[0].id); // newest first
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setArtifactsError(
          err instanceof ApiError && err.status === 0
            ? 'API unreachable — is the backend running?'
            : 'Could not load backups.',
        );
      });
    return () => {
      cancelled = true;
    };
  }, [source.name]);

  // The full fleet for the target selector — deliberately NOT the source's
  // env, and unaffected by any My Databases env filter. Default rule
  // (mini-ADR 5): non-prod source defaults to itself (rollback is the
  // common case); a prod source defaults to no selection.
  useEffect(() => {
    let cancelled = false;
    fetchInstances()
      .then((list) => {
        if (cancelled) return;
        setInstances(list);
        setTargetName(source.env !== 'prod' ? source.name : '');
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setInstancesError(
          err instanceof ApiError && err.status === 0
            ? 'API unreachable — is the backend running?'
            : 'Could not load instances.',
        );
      });
    return () => {
      cancelled = true;
    };
  }, [source.env, source.name]);

  const selectedArtifact = artifacts?.find((a) => a.id === selectedArtifactId);
  const target = instances?.find((i) => i.name === targetName);
  const isProdTarget = target?.env === 'prod';
  const confirmed = !isProdTarget || confirmName === target?.name;
  const canSubmit = selectedArtifact !== undefined && target !== undefined && confirmed;

  // Overlay (backdrop) click is a soft dismiss — mirrors LaunchDrawer: must
  // not eat a restore in flight or a just-fired prod confirmation.
  const dismissOverlay = () => {
    if (submitting || started !== null) return;
    onClose();
  };

  const submit = async () => {
    if (selectedArtifact === undefined || target === undefined) return;
    setSubmitting(true);
    setError(null);
    try {
      setStarted(
        await startRestore({
          artifactId: selectedArtifact.id,
          target: target.name,
          confirm: isProdTarget ? confirmName : undefined,
          reason: reason.trim(),
        }),
      );
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 0
          ? 'API unreachable — is the backend running?'
          : err instanceof ApiError
            ? err.detail
            : 'The restore could not be started.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  // The chain's first step with a run_id (SPEC-032): the response is fired
  // at creation time, before the driver's first tick, so step 1 may not
  // have a run yet — fall back to Activity rather than a dead link.
  const firstStepRunId = started?.steps.find((s) => s.run_id !== null)?.run_id ?? null;
  const viewRestoreTo = firstStepRunId !== null ? `/runs/${firstStepRunId}` : '/activity';

  const targetNameForPlan = target?.name ?? 'the target';

  return (
    <div className="drawer-overlay" onClick={dismissOverlay}>
      <aside
        className="drawer"
        role="dialog"
        aria-label="Restore backup"
        onClick={(e) => e.stopPropagation()}
      >
        {target !== undefined && <EnvBanner env={target.env} />}

        <header className="drawer-header">
          <span aria-hidden="true">⏮</span>
          <h2>Restore from {source.name}</h2>
        </header>

        <div className="drawer-target">
          <span className="drawer-target-name">{source.name}</span>
          <EnvBadge env={source.env} />
        </div>

        {started === null ? (
          <>
            {artifacts === null && artifactsError === null && (
              <p className="placeholder">Loading backups…</p>
            )}
            {artifactsError !== null && (
              <p className="drawer-error" role="alert">
                {artifactsError}
              </p>
            )}
            {artifacts !== null && artifacts.length === 0 && (
              <p className="placeholder">
                No backups registered for {source.name} yet — run a Backup first.
              </p>
            )}

            {artifacts !== null && artifacts.length > 0 && (
              <>
                <fieldset className="drawer-field artifact-picker">
                  <legend>Backup to restore</legend>
                  {artifacts.map((a) => (
                    <label key={a.id} className="artifact-option">
                      <input
                        type="radio"
                        name="artifact"
                        checked={selectedArtifactId === a.id}
                        onChange={() => setSelectedArtifactId(a.id)}
                      />
                      <span className="artifact-option-name">{a.name}</span>
                      <span className="artifact-option-meta">
                        sha256 {a.checksum.slice(0, 12)}… · {formatBytes(a.size_bytes)}
                      </span>
                      <span className="chip">{a.retention_class}</span>
                    </label>
                  ))}
                </fieldset>

                <label className="drawer-field">
                  Restore onto
                  <select
                    value={targetName}
                    onChange={(e) => setTargetName(e.target.value)}
                    disabled={instances === null}
                  >
                    <option value="">— choose a target —</option>
                    {(instances ?? []).map((i) => (
                      <option key={i.name} value={i.name}>
                        {i.name} ({i.env})
                      </option>
                    ))}
                  </select>
                </label>
                {instancesError !== null && (
                  <p className="drawer-error" role="alert">
                    {instancesError}
                  </p>
                )}

                <div className="restore-plan">
                  <p className="restore-plan-title">This restore will:</p>
                  <ol className="restore-plan-steps">
                    <li>Verify the backup</li>
                    <li>Safety backup of {targetNameForPlan} (automatic — always taken)</li>
                    <li>Restore onto {targetNameForPlan}</li>
                  </ol>
                  <p className="field-hint">
                    The safety backup cannot be skipped — it's what this workflow exists to
                    guarantee.
                  </p>
                </div>

                <label className="drawer-field">
                  Reason / ticket # <span className="optional">(optional)</span>
                  <input
                    type="text"
                    value={reason}
                    onChange={(e) => setReason(e.target.value)}
                    placeholder="CHG-1234"
                    maxLength={500}
                  />
                </label>

                {isProdTarget && target !== undefined && (
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
                      placeholder={target.name}
                    />
                  </label>
                )}

                {error !== null && (
                  <p className="drawer-error" role="alert">
                    {error}
                  </p>
                )}

                <footer className="drawer-footer">
                  <button
                    type="button"
                    className="btn-text"
                    onClick={onClose}
                    disabled={submitting}
                  >
                    Cancel
                  </button>
                  <button
                    type="button"
                    className="btn-primary"
                    onClick={() => void submit()}
                    disabled={submitting || !canSubmit}
                  >
                    {submitting
                      ? 'Starting…'
                      : target !== undefined
                        ? `Restore onto ${target.name}`
                        : 'Restore'}
                  </button>
                </footer>
              </>
            )}

            {artifacts !== null && artifacts.length === 0 && (
              <footer className="drawer-footer">
                <button type="button" className="btn-text" onClick={onClose}>
                  Cancel
                </button>
              </footer>
            )}
          </>
        ) : (
          <>
            <p className="drawer-started" role="status">
              ✓ Restore started on {target?.name}.
            </p>
            <ol className="restore-plan-steps">
              <li>Verify the backup</li>
              <li>Safety backup of {targetNameForPlan} (automatic — always taken)</li>
              <li>Restore onto {targetNameForPlan}</li>
            </ol>
            <footer className="drawer-footer">
              <button type="button" className="btn-text" onClick={onClose}>
                Close
              </button>
              <Link className="btn-primary" to={viewRestoreTo} onClick={onClose}>
                View restore
              </Link>
            </footer>
          </>
        )}
      </aside>
    </div>
  );
}
