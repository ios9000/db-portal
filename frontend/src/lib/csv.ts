/*
 * CSV export of the Activity view (SPEC-014 mini-ADR 7): exactly the
 * columns the table shows — no error text, no reason, no params.
 */

import type { Run } from './api';

const COLUMNS = [
  'run_id',
  'state',
  'operation',
  'instance',
  'environment',
  'requested_by',
  'submitted_at',
  'started_at',
  'finished_at',
] as const;

function field(v: string | number | null): string {
  const s = v === null ? '' : String(v);
  return /[",\n\r]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
}

export function runsToCsv(runs: Run[]): string {
  const lines = [COLUMNS.join(',')];
  for (const r of runs) {
    lines.push(
      [
        r.id,
        r.state,
        r.operation,
        r.instance,
        r.environment,
        r.requested_by,
        r.submitted_at,
        r.started_at,
        r.finished_at,
      ]
        .map(field)
        .join(','),
    );
  }
  return `${lines.join('\r\n')}\r\n`;
}
