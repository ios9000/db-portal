import { expect, test } from 'vitest';
import type { Run } from './api';
import { runsToCsv } from './csv';

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
