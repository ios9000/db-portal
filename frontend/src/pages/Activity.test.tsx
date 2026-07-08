import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
    requested_by: 'local-dev',
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
    requested_by: 'local-dev',
    submitted_at: '2026-07-07T12:00:00Z',
    started_at: '2026-07-07T12:00:01Z',
    finished_at: '2026-07-07T12:01:31Z',
    artifact: { name: 'billing-test-mock-nonprod-1.dump', size_bytes: 1048576, checksum: 'abc' },
  },
];

/** Stub fetch: /api/operations serves the catalog, /api/runs the given list. */
function stubApi(runs: Run[]) {
  const fetchMock = vi.fn((input: RequestInfo | URL) => {
    const url = String(input);
    if (url.startsWith('/api/operations')) {
      return Promise.resolve(
        new Response(
          JSON.stringify({
            operations: [
              {
                id: 'dump',
                label: 'Backup',
                icon: '💾',
                description: '',
                duration_hint: '~25 min',
                online_hint: 'Database stays online',
              },
            ],
          }),
        ),
      );
    }
    return Promise.resolve(new Response(JSON.stringify({ runs })));
  });
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
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

test('splits live runs into Now running and terminal runs into the history table', async () => {
  stubApi(RUNS);
  renderActivity();

  // The running run lives in the Now running section, not the table.
  const liveSection = await screen.findByRole('region', { name: 'Now running' });
  expect(within(liveSection).getByRole('link', { name: 'RUN-2' })).toHaveAttribute(
    'href',
    '/runs/2',
  );
  expect(within(liveSection).getByText('PROD')).toBeInTheDocument();
  expect(within(liveSection).getByText(/elapsed/)).toBeInTheDocument();

  const table = screen.getByRole('table');
  const rows = within(table).getAllByRole('row');
  expect(rows).toHaveLength(2); // header + the one terminal run
  expect(within(rows[1]).getByRole('link', { name: 'RUN-1' })).toHaveAttribute('href', '/runs/1');
  expect(within(rows[1]).getByText('Success')).toBeInTheDocument();
  expect(within(rows[1]).getByText('local-dev')).toBeInTheDocument(); // requester column
  expect(within(rows[1]).getByText('1m 30s')).toBeInTheDocument();
});

test('status chip refetches with the server-side filter', async () => {
  const fetchMock = stubApi(RUNS);
  renderActivity();
  await screen.findByRole('table');

  await userEvent.click(screen.getByRole('button', { name: 'Failed' }));

  await vi.waitFor(() => {
    const runCalls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(runCalls).toContain('/api/runs?state=failed');
  });
  expect(screen.getByRole('button', { name: 'Failed' })).toHaveAttribute('aria-pressed', 'true');
});

test('filters compose into one query string', async () => {
  const fetchMock = stubApi(RUNS);
  renderActivity();
  await screen.findByRole('table');

  await userEvent.click(screen.getByRole('button', { name: 'PROD' }));
  await userEvent.click(screen.getByRole('button', { name: 'Backup' }));

  await vi.waitFor(() => {
    const runCalls = fetchMock.mock.calls.map((c) => String(c[0]));
    expect(runCalls).toContain('/api/runs?env=prod&operation=dump');
  });
});

test('empty history shows the launch hint, filtered-empty names the filters', async () => {
  stubApi([]);
  renderActivity();
  expect(await screen.findByText(/No runs yet/)).toBeInTheDocument();

  await userEvent.click(screen.getByRole('button', { name: 'Canceled' }));
  expect(await screen.findByText(/No runs match these filters/)).toBeInTheDocument();
});

test('export button disables without rows', async () => {
  stubApi([]);
  renderActivity();
  await screen.findByText(/No runs yet/);
  expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled();
});

test('network failure shows the unreachable message', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new TypeError('fetch failed'))),
  );
  renderActivity();
  expect(await screen.findByRole('alert')).toHaveTextContent('API unreachable');
});
