export type RunStatusValue = 'queued' | 'running' | 'success' | 'failed' | 'halted' | 'canceled';

const LABELS: Record<RunStatusValue, string> = {
  queued: 'Queued',
  running: 'Running',
  success: 'Success',
  failed: 'Failed',
  halted: 'Halted',
  canceled: 'Canceled',
};

/**
 * Run status chip: colored dot + text label (redundant encoding, never
 * color alone). `halted` is the chain-failure state (D5: halt, notify,
 * resume from failed step).
 */
export function RunStatus({ status }: { status: RunStatusValue }) {
  return (
    <span className={`run-status run-status-${status}`}>
      <span className="dot" aria-hidden="true" />
      {LABELS[status]}
    </span>
  );
}
