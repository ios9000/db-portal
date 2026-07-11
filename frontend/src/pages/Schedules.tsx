import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { EnvBadge } from '../components/EnvBadge';
import { EnvBanner } from '../components/EnvBanner';
import {
  ApiError,
  createSchedule,
  deleteSchedule,
  fetchInstances,
  fetchOperations,
  fetchSchedules,
  setScheduleEnabled,
  type Instance,
  type Operation,
  type Schedule,
  type ScheduleFireStatus,
} from '../lib/api';
import { formatTimestamp } from '../lib/format';

const FIRE_STATUS_LABELS: Record<ScheduleFireStatus, string> = {
  fired: 'fired',
  skipped_overlap: 'skipped (overlap)',
  error: 'error',
};

/** Server errors on an action (toggle/delete/create) render the server's own
 * message (SPEC-021 idiom, mirrors LaunchDrawer); a network failure gets the
 * friendly unreachable line instead of the raw fetch error text. */
function describeActionError(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    return err.status === 0 ? 'API unreachable — is the backend running?' : err.detail;
  }
  return fallback;
}

/** "fired" / "skipped (overlap)" / "error", with a link to the run when one
 * exists; "—" if the schedule has never fired. */
function LastFire({ schedule }: { schedule: Schedule }) {
  if (schedule.last_fired_at === null || schedule.last_fire_status === null) {
    return <>—</>;
  }
  return (
    <>
      {formatTimestamp(schedule.last_fired_at)} — {FIRE_STATUS_LABELS[schedule.last_fire_status]}
      {schedule.last_run_id !== null && (
        <>
          {' '}
          (<Link to={`/runs/${schedule.last_run_id}`}>RUN-{schedule.last_run_id}</Link>)
        </>
      )}
    </>
  );
}

/**
 * Schedules (WU-022 slice b): recurring dumps over the scheduler backend.
 * List idiom matches Activity/MyDatabases (loading/error/empty), plus a
 * create panel styled like LaunchDrawer including its prod typed-name
 * ritual. `next_fire_at` is server-computed and jittered — this page only
 * ever displays it, never recomputes a cron expression client-side.
 */
export function Schedules() {
  const [schedules, setSchedules] = useState<Schedule[] | null>(null);
  const [listError, setListError] = useState<string | null>(null);
  const [rowError, setRowError] = useState<string | null>(null);
  const [confirmDeleteId, setConfirmDeleteId] = useState<number | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [operations, setOperations] = useState<Operation[]>([]);

  useEffect(() => {
    let cancelled = false;
    setSchedules(null);
    setListError(null);
    fetchSchedules()
      .then((list) => {
        if (!cancelled) setSchedules(list);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setListError(
          err instanceof ApiError && err.status === 0
            ? 'API unreachable — is the backend running?'
            : 'Schedules unavailable.',
        );
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Catalog data for the create form. Loaded independently of the schedule
  // list — a catalog failure just leaves "New schedule" disabled, it must
  // not block the list itself from rendering.
  useEffect(() => {
    let cancelled = false;
    Promise.all([fetchInstances(), fetchOperations()])
      .then(([i, o]) => {
        if (cancelled) return;
        setInstances(i);
        setOperations(o);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  const toggle = async (schedule: Schedule) => {
    setRowError(null);
    setConfirmDeleteId(null);
    try {
      const updated = await setScheduleEnabled(schedule.id, !schedule.enabled);
      setSchedules((prev) => (prev ?? []).map((row) => (row.id === updated.id ? updated : row)));
    } catch (err) {
      setRowError(describeActionError(err, 'Could not update the schedule.'));
    }
  };

  const doDelete = async (id: number) => {
    setRowError(null);
    try {
      await deleteSchedule(id);
      setSchedules((prev) => (prev ?? []).filter((row) => row.id !== id));
    } catch (err) {
      setRowError(describeActionError(err, 'Could not delete the schedule.'));
    } finally {
      setConfirmDeleteId(null);
    }
  };

  const handleDeleteClick = (id: number) => {
    if (confirmDeleteId === id) {
      void doDelete(id);
    } else {
      setRowError(null);
      setConfirmDeleteId(id);
    }
  };

  const handleCreated = (created: Schedule) => {
    setSchedules((prev) => [created, ...(prev ?? [])]);
  };

  const catalogReady = instances.length > 0 && operations.length > 0;

  return (
    <>
      <h1>Schedules</h1>

      <div className="toolbar">
        <span className="spacer" />
        <button
          type="button"
          className="btn-primary"
          disabled={!catalogReady}
          onClick={() => setShowForm(true)}
        >
          New schedule
        </button>
      </div>

      {listError !== null && (
        <p className="placeholder" role="alert">
          {listError}
        </p>
      )}
      {listError === null && schedules === null && (
        <p className="placeholder">Loading schedules…</p>
      )}
      {schedules !== null && schedules.length === 0 && (
        <p className="placeholder">No schedules yet — create one to dump on a rhythm.</p>
      )}

      {rowError !== null && (
        <p className="drawer-error" role="alert">
          {rowError}
        </p>
      )}

      {schedules !== null && schedules.length > 0 && (
        <table className="instance-table">
          <thead>
            <tr>
              <th>Instance</th>
              <th>Operation</th>
              <th>Schedule</th>
              <th>Next fire</th>
              <th>Last fire</th>
              <th>Owner</th>
              <th>
                <span className="visually-hidden">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {schedules.map((s) => (
              <tr key={s.id} className={s.env === 'prod' ? 'prod-edge' : ''}>
                <td>
                  <span className="instance-name">{s.instance}</span> <EnvBadge env={s.env} />
                </td>
                <td>{s.operation}</td>
                <td>
                  <span className="cron-spec">{s.cron_spec}</span>
                </td>
                <td>{s.next_fire_at === null ? '—' : formatTimestamp(s.next_fire_at)}</td>
                <td>
                  <LastFire schedule={s} />
                </td>
                <td>{s.created_by}</td>
                <td>
                  <button
                    type="button"
                    className="btn-secondary btn-small"
                    onClick={() => void toggle(s)}
                  >
                    {s.enabled ? 'Disable' : 'Enable'}
                  </button>{' '}
                  <button
                    type="button"
                    className="btn-secondary btn-small"
                    onClick={() => handleDeleteClick(s.id)}
                  >
                    {confirmDeleteId === s.id ? 'Confirm delete?' : 'Delete'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {showForm && catalogReady && (
        <NewScheduleForm
          instances={instances}
          operations={operations}
          onClose={() => setShowForm(false)}
          onCreated={handleCreated}
        />
      )}
    </>
  );
}

interface NewScheduleFormProps {
  instances: Instance[];
  operations: Operation[];
  onClose: () => void;
  onCreated: (schedule: Schedule) => void;
}

/**
 * Create panel, styled like LaunchDrawer (design brief Screen 3 grammar)
 * but local to this page rather than reusing that component, since the
 * target is a select rather than a pre-chosen instance. Carries the same
 * prod typed-name ritual (SPEC-015/021): paste disabled, submit locked
 * until the typed value exactly matches the selected instance's name.
 */
function NewScheduleForm({ instances, operations, onClose, onCreated }: NewScheduleFormProps) {
  const [instanceName, setInstanceName] = useState(instances[0]?.name ?? '');
  const [operationId, setOperationId] = useState(operations[0]?.id ?? '');
  const [cronSpec, setCronSpec] = useState('');
  const [reason, setReason] = useState('');
  const [confirmName, setConfirmName] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const instance = instances.find((i) => i.name === instanceName);
  const isProd = instance?.env === 'prod';
  const confirmed = !isProd || confirmName === instance?.name;

  const dismissOverlay = () => {
    if (submitting) return;
    onClose();
  };

  const submit = async () => {
    setSubmitting(true);
    setError(null);
    try {
      const created = await createSchedule(
        instanceName,
        operationId,
        cronSpec.trim(),
        reason.trim(),
        isProd ? confirmName : undefined,
      );
      onCreated(created);
      onClose();
    } catch (err) {
      setError(describeActionError(err, 'The schedule could not be created.'));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="drawer-overlay" onClick={dismissOverlay}>
      <aside
        className="drawer"
        role="dialog"
        aria-label="New schedule"
        onClick={(e) => e.stopPropagation()}
      >
        {isProd && instance && <EnvBanner env={instance.env} />}

        <header className="drawer-header">
          <h2>New schedule</h2>
        </header>

        {instance && (
          <div className="drawer-target">
            <span className="drawer-target-name">{instance.name}</span>
            <EnvBadge env={instance.env} />
          </div>
        )}

        <label className="drawer-field">
          Instance
          <select value={instanceName} onChange={(e) => setInstanceName(e.target.value)}>
            {instances.map((i) => (
              <option key={i.name} value={i.name}>
                {i.name} ({i.env})
              </option>
            ))}
          </select>
        </label>

        <label className="drawer-field">
          Operation
          <select value={operationId} onChange={(e) => setOperationId(e.target.value)}>
            {operations.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
        </label>

        <label className="drawer-field">
          Cron spec
          <input
            type="text"
            value={cronSpec}
            onChange={(e) => setCronSpec(e.target.value)}
            placeholder="30 2 * * *"
            required
          />
          <span className="field-hint">
            Five-field cron (or @daily/@hourly), evaluated in server time.
          </span>
        </label>

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

        {isProd && instance && (
          <label className="drawer-field">
            To confirm, type the instance name <span className="optional">(paste disabled)</span>
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
            onClick={() => void submit()}
            disabled={submitting || cronSpec.trim() === '' || !confirmed}
          >
            {submitting ? 'Creating…' : 'Create schedule'}
          </button>
        </footer>
      </aside>
    </div>
  );
}
