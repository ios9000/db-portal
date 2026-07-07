import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Run } from '../lib/api';
import { Activity } from './Activity';

const RUNS: Run[] = [
  {
    id: 2,
    instance: 'billing-prod',
    environment: 'prod',
    operation: 'dump',
    state: 'running',
    reason: null,
    error: null,
    job_id: 'mock-prod-2',
    submitted_at: '2026-07-07T12:31:00Z',
    started_at: '2026-07-07T12:31:01Z',
    finished_at: null,
    artifact: null,
  },
  {
    id: 1,
    instance: 'billing-test',
    environment: 'test',
    operation: 'dump',
    state: 'success',
    reason: 'CHG-1',
    error: null,
    job_id: 'mock-nonprod-1',
    submitted_at: '2026-07-07T12:00:00Z',
    started_at: '2026-07-07T12:00:01Z',
    finished_at: '2026-07-07T12:01:31Z',
    artifact: { name: 'billing-test-mock-nonprod-1.dump', size_bytes: 1048576, checksum: 'abc' },
  },
];

function stubRuns(runs: Run[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(JSON.stringify({ runs })))),
  );
}

function renderActivity() {
  return render(
    <MemoryRouter>
      <Activity />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('renders the run list with status chips, env badges and durations', async () => {
  stubRuns(RUNS);
  renderActivity();

  const table = await screen.findByRole('table');
  const rows = within(table).getAllByRole('row');
  expect(rows).toHaveLength(3); // header + 2 runs

  // Newest (running) run first, as served.
  expect(within(rows[1]).getByText('RUN-2')).toBeInTheDocument();
  expect(within(rows[1]).getByText('Running')).toBeInTheDocument();
  expect(within(rows[1]).getByText('PROD')).toBeInTheDocument();
  expect(within(rows[1]).getByText('—')).toBeInTheDocument(); // no duration while live

  expect(within(rows[2]).getByText('RUN-1')).toBeInTheDocument();
  expect(within(rows[2]).getByText('Success')).toBeInTheDocument();
  expect(within(rows[2]).getByText('1m 30s')).toBeInTheDocument();
});

test('empty history shows the launch hint', async () => {
  stubRuns([]);
  renderActivity();
  expect(await screen.findByText(/No runs yet/)).toBeInTheDocument();
});

test('network failure shows the unreachable message', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new TypeError('fetch failed'))),
  );
  renderActivity();
  expect(await screen.findByRole('alert')).toHaveTextContent('API unreachable');
});
