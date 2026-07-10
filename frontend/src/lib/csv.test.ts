import { expect, test } from 'vitest';
import type { Run } from './api';
import { field, runsToCsv } from './csv';

const run: Run = {
  id: 42,
  instance: 'billing-test',
  environment: 'test',
  operation: 'dump',
  state: 'failed',
  reason: 'CHG-9 "quoted, with comma"',
  error: 'pg_dump: FATAL',
  job_id: 'mock-nonprod-1',
  requested_by: 'local-dev',
  submitted_at: '2026-07-07T12:00:00Z',
  started_at: '2026-07-07T12:00:01Z',
  finished_at: null,
  artifact: null,
};

test('exports exactly the table columns, CRLF-terminated, nulls empty', () => {
  const csv = runsToCsv([run]);
  const lines = csv.split('\r\n');
  expect(lines[0]).toBe(
    'run_id,state,operation,instance,environment,requested_by,submitted_at,started_at,finished_at',
  );
  expect(lines[1]).toBe(
    '42,failed,dump,billing-test,test,local-dev,2026-07-07T12:00:00Z,2026-07-07T12:00:01Z,',
  );
  expect(csv.endsWith('\r\n')).toBe(true);
});

test('never exports reason or error text (SPEC-014 mini-ADR 7)', () => {
  const csv = runsToCsv([run]);
  expect(csv).not.toContain('CHG-9');
  expect(csv).not.toContain('pg_dump');
});

test('quotes fields containing separators or quotes', () => {
  const tricky = { ...run, instance: 'weird,"name' };
  const csv = runsToCsv([tricky]);
  expect(csv).toContain('"weird,""name"');
});

// OWASP CSV injection (m1-gate item 11): a leading =+-@ or TAB is a formula
// trigger in spreadsheet apps that open the export.
test('neutralizes leading formula-injection characters', () => {
  expect(field('=SUM(A1)')).toBe("'=SUM(A1)");
  expect(field('+1+1')).toBe("'+1+1");
  expect(field('-1+1')).toBe("'-1+1");
  expect(field('@SUM(A1)')).toBe("'@SUM(A1)");
  expect(field('\tSUM(A1)')).toBe("'\tSUM(A1)");
});

test('does not neutralize values that merely contain, but do not start with, those characters', () => {
  expect(field('CHG-9')).toBe('CHG-9');
  expect(field('a=b')).toBe('a=b');
  expect(field('team+billing')).toBe('team+billing');
});

test('does not mangle the em dash (not ASCII hyphen-minus)', () => {
  expect(field('—dashboard')).toBe('—dashboard');
});

test('neutralization composes with existing quoting for fields needing both', () => {
  // Leading '=' plus an embedded comma: neutralize first, then quote.
  expect(field('=A1,B1')).toBe('"\'=A1,B1"');
});

test('normal values and null are unaffected by neutralization', () => {
  expect(field('billing-test')).toBe('billing-test');
  expect(field(42)).toBe('42');
  expect(field(null)).toBe('');
});
