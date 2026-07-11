import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Instance, Operation, Schedule } from '../lib/api';
import { formatTimestamp } from '../lib/format';
import { Schedules } from './Schedules';

const INSTANCES: Instance[] = [
  {
    name: 'billing-test',
    cluster: 'billing',
    env: 'test',
    platform: 'k8s_patroni',
    pg_version: '16.3',
    size_gb: 38,
    owner: 'billing-team',
    maintenance_window: null,
    window_state: null,
    last_backup_at: null,
  },
  {
    name: 'billing-prod',
    cluster: 'billing',
    env: 'prod',
    platform: 'k8s_patroni',
    pg_version: '16.3',
    size_gb: 412,
    owner: 'billing-team',
    maintenance_window: null,
    window_state: null,
    last_backup_at: null,
  },
];

const DUMP_OP: Operation = {
  id: 'dump',
  label: 'Backup',
  icon: '💾',
  description: 'Full backup (pg_dump), verified after completion.',
  duration_hint: '~25 min',
  online_hint: 'Database stays online',
};

const SCHEDULE_FIRED: Schedule = {
  id: 3,
  instance: 'billing-test',
  env: 'test',
  operation: 'dump',
  cron_spec: '30 2 * * *',
  reason: 'nightly',
  enabled: true,
  created_by: 'dba1',
  created_at: '2026-07-01T00:00:00Z',
  next_fire_at: '2026-07-11T02:30:00Z',
  last_fired_at: '2026-07-10T02:30:00Z',
  last_run_id: 42,
  last_fire_status: 'fired',
};

const SCHEDULE_DISABLED: Schedule = {
  id: 5,
  instance: 'billing-prod',
  env: 'prod',
  operation: 'dump',
  cron_spec: '0 * * * *',
  reason: null,
  enabled: false,
  created_by: 'dba2',
  created_at: '2026-07-01T00:00:00Z',
  next_fire_at: null,
  last_fired_at: null,
  last_run_id: null,
  last_fire_status: null,
};

interface StubOpts {
  schedules?: Schedule[];
  createResponse?: Schedule;
  createStatus?: number;
  createError?: string;
}

/** Stub fetch for the page's whole API surface: schedule list, the
 * instance/operation catalog for the create form, and create/toggle/delete. */
function stubApi({
  schedules = [SCHEDULE_FIRED, SCHEDULE_DISABLED],
  createResponse,
  createStatus = 201,
  createError,
}: StubOpts = {}) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://test');
    const method = init?.method ?? 'GET';

    if (url.pathname === '/api/schedules' && method === 'GET') {
      return Promise.resolve(new Response(JSON.stringify({ schedules })));
    }
    if (url.pathname === '/api/instances') {
      return Promise.resolve(new Response(JSON.stringify({ instances: INSTANCES })));
    }
    if (url.pathname === '/api/operations') {
      return Promise.resolve(new Response(JSON.stringify({ operations: [DUMP_OP] })));
    }
    if (url.pathname === '/api/schedules' && method === 'POST') {
      if (createError !== undefined) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: createError }), { status: createStatus }),
        );
      }
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
      const created: Schedule = createResponse ?? {
        id: 99,
        instance: body.instance as string,
        env: INSTANCES.find((i) => i.name === body.instance)?.env ?? 'test',
        operation: body.operation as string,
        cron_spec: body.cron_spec as string,
        reason: (body.reason as string) || null,
        enabled: true,
        created_by: 'dba1',
        created_at: '2026-07-10T12:00:00Z',
        next_fire_at: '2026-07-11T03:00:00Z',
        last_fired_at: null,
        last_run_id: null,
        last_fire_status: null,
      };
      return Promise.resolve(new Response(JSON.stringify(created), { status: createStatus }));
    }
    const idMatch = /^\/api\/schedules\/(\d+)$/.exec(url.pathname);
    if (idMatch && method === 'PATCH') {
      const id = Number(idMatch[1]);
      const body = JSON.parse(String(init?.body)) as { enabled: boolean };
      const existing = schedules.find((s) => s.id === id)!;
      return Promise.resolve(
        new Response(JSON.stringify({ ...existing, enabled: body.enabled }), { status: 200 }),
      );
    }
    if (idMatch && method === 'DELETE') {
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    return Promise.resolve(new Response('not found', { status: 404 }));
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Schedules />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('renders schedule rows: instance + env badge, cron spec, next fire, run link', async () => {
  stubApi();
  renderPage();

  const table = await screen.findByRole('table');
  const rows = within(table).getAllByRole('row');
  expect(rows).toHaveLength(3); // header + 2 schedules

  const firedRow = rows[1];
  expect(within(firedRow).getByText('billing-test')).toBeInTheDocument();
  expect(within(firedRow).getByText('TEST')).toBeInTheDocument();
  expect(within(firedRow).getByText('30 2 * * *')).toBeInTheDocument();
  expect(
    within(firedRow).getByText(formatTimestamp(SCHEDULE_FIRED.next_fire_at!)),
  ).toBeInTheDocument();
  expect(within(firedRow).getByRole('link', { name: 'RUN-42' })).toHaveAttribute(
    'href',
    '/runs/42',
  );

  const disabledRow = rows[2];
  expect(within(disabledRow).getByText('billing-prod')).toBeInTheDocument();
  expect(within(disabledRow).getByText('PROD')).toBeInTheDocument();
  const cells = within(disabledRow).getAllByRole('cell');
  expect(cells[3]).toHaveTextContent('—'); // null next_fire_at (disabled schedule)
  expect(cells[4]).toHaveTextContent('—'); // never fired
});

test('empty state shows the friendly hint', async () => {
  stubApi({ schedules: [] });
  renderPage();
  expect(await screen.findByText(/No schedules yet/)).toBeInTheDocument();
});

test('network failure shows the unreachable message', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new TypeError('fetch failed'))),
  );
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('API unreachable');
});

test('create flow: fill the form, submit, and the new row appears', async () => {
  const mock = stubApi();
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole('table');

  await user.click(screen.getByRole('button', { name: 'New schedule' }));
  const dialog = await screen.findByRole('dialog', { name: 'New schedule' });

  await user.type(within(dialog).getByLabelText(/Cron spec/), '0 3 * * *');
  await user.type(within(dialog).getByLabelText(/Reason/), 'nightly job');
  await user.click(within(dialog).getByRole('button', { name: 'Create schedule' }));

  await screen.findByText('0 3 * * *'); // new row's cron spec renders
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument(); // form closed on success

  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(post).toBeDefined();
  expect(JSON.parse(String((post![1] as RequestInit).body))).toEqual({
    instance: 'billing-test', // default: first instance in the catalog
    operation: 'dump', // default: first operation in the catalog
    cron_spec: '0 3 * * *',
    reason: 'nightly job',
  });
});

test('prod instance selected: submit stays disabled until the exact typed name, paste blocked', async () => {
  stubApi();
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole('table');

  await user.click(screen.getByRole('button', { name: 'New schedule' }));
  const dialog = await screen.findByRole('dialog', { name: 'New schedule' });

  await user.selectOptions(within(dialog).getByLabelText('Instance'), 'billing-prod');
  await user.type(within(dialog).getByLabelText(/Cron spec/), '0 3 * * *');

  expect(within(dialog).getByText(/PRODUCTION environment/)).toBeInTheDocument();
  const confirmInput = within(dialog).getByLabelText<HTMLInputElement>(/type the instance name/i);
  const submitBtn = within(dialog).getByRole('button', { name: 'Create schedule' });
  expect(submitBtn).toBeDisabled();

  confirmInput.focus();
  await user.paste('billing-prod');
  expect(confirmInput.value).toBe('');
  expect(submitBtn).toBeDisabled();

  await user.type(confirmInput, 'billing-prod');
  expect(submitBtn).toBeEnabled();
});

test('server 400 renders its message in the form error slot', async () => {
  stubApi({ createError: 'invalid cron spec: bad token', createStatus: 400 });
  const user = userEvent.setup();
  renderPage();
  await screen.findByRole('table');

  await user.click(screen.getByRole('button', { name: 'New schedule' }));
  const dialog = await screen.findByRole('dialog', { name: 'New schedule' });
  await user.type(within(dialog).getByLabelText(/Cron spec/), 'garbage');
  await user.click(within(dialog).getByRole('button', { name: 'Create schedule' }));

  expect(await within(dialog).findByRole('alert')).toHaveTextContent(
    'invalid cron spec: bad token',
  );
});

test('toggling enabled calls setScheduleEnabled and updates the row from the response', async () => {
  const mock = stubApi();
  const user = userEvent.setup();
  renderPage();
  const table = await screen.findByRole('table');
  const rows = within(table).getAllByRole('row');
  const firedRow = rows[1]; // billing-test, currently enabled

  await user.click(within(firedRow).getByRole('button', { name: 'Disable' }));

  await within(firedRow).findByRole('button', { name: 'Enable' }); // flipped
  const patch = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'PATCH');
  expect(patch).toBeDefined();
  expect(String(patch![0])).toBe('/api/schedules/3');
  expect(JSON.parse(String((patch![1] as RequestInit).body))).toEqual({ enabled: false });
});

test('delete requires a second click to confirm, then removes the row', async () => {
  const mock = stubApi();
  const user = userEvent.setup();
  renderPage();
  const table = await screen.findByRole('table');
  const rows = within(table).getAllByRole('row');
  const firedRow = rows[1];

  await user.click(within(firedRow).getByRole('button', { name: 'Delete' }));
  expect(within(firedRow).getByRole('button', { name: 'Confirm delete?' })).toBeInTheDocument();

  await user.click(within(firedRow).getByRole('button', { name: 'Confirm delete?' }));

  await vi.waitFor(() => {
    expect(screen.queryByText('billing-test')).not.toBeInTheDocument();
  });
  const del = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'DELETE');
  expect(del).toBeDefined();
  expect(String(del![0])).toBe('/api/schedules/3');
});
