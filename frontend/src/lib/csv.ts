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

// OWASP CSV injection: a field opening with one of these is a formula in
// Excel/Sheets/LibreOffice. Neutralize by prefixing a single quote before
// the existing quoting/escaping logic runs. ASCII hyphen-minus only — the
// em dash '—' (U+2014) must pass through untouched.
const FORMULA_LEADERS = new Set(['=', '+', '-', '@', '\t']);

export function field(v: string | number | null): string {
  let s = v === null ? '' : String(v);
  if (s.length > 0 && FORMULA_LEADERS.has(s[0])) {
    s = `'${s}`;
  }
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
