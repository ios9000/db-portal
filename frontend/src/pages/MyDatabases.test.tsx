import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Instance } from '../lib/api';
import { MyDatabases } from './MyDatabases';

const SAMPLE: Instance[] = [
  {
    name: 'billing-prod',
    cluster: 'billing',
    env: 'prod',
    platform: 'k8s_patroni',
    pg_version: '16.3',
    size_gb: 412,
    owner: 'billing-team',
    maintenance_window: 'Sat 02:00-06:00',
  },
  {
    name: 'billing-test',
    cluster: 'billing',
    env: 'test',
    platform: 'k8s_patroni',
    pg_version: '16.3',
    size_gb: 38,
    owner: 'billing-team',
    maintenance_window: null,
  },
  {
    name: 'analytics-dev',
    cluster: 'analytics',
    env: 'dev',
    platform: 'k8s_patroni',
    pg_version: '17.1',
    size_gb: null,
    owner: 'analytics-team',
    maintenance_window: null,
  },
];

const DUMP_OP = {
  id: 'dump',
  label: 'Backup',
  icon: '💾',
  description: 'Full backup (pg_dump), verified after completion.',
  duration_hint: '~25 min',
  online_hint: 'Database stays online',
};

/** Stub fetch for the page's API surface: instances, catalog, run launch. */
function stubInstances(instances: Instance[] = SAMPLE) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://test');
    if (url.pathname === '/api/operations') {
      return Promise.resolve(new Response(JSON.stringify({ operations: [DUMP_OP] })));
    }
    if (url.pathname === '/api/runs' && init?.method === 'POST') {
      const body = JSON.parse(String(init.body)) as { instance: string };
      return Promise.resolve(
        new Response(JSON.stringify({ id: 42, instance: body.instance, state: 'queued' }), {
          status: 201,
        }),
      );
    }
    if (url.pathname !== '/api/instances') {
      return Promise.resolve(new Response('not found', { status: 404 }));
    }
    const env = url.searchParams.get('env');
    const filtered = env ? instances.filter((i) => i.env === env) : instances;
    return Promise.resolve(new Response(JSON.stringify({ instances: filtered })));
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/databases']}>
      <MyDatabases />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('renders instance cards with env badge and placeholder backup line', async () => {
  stubInstances();
  renderPage();

  const card = (await screen.findByText('billing-prod')).closest('article');
  expect(card).not.toBeNull();
  expect(within(card!).getByText('PROD')).toBeInTheDocument();
  expect(within(card!).getByText('PostgreSQL 16.3 · 412 GB')).toBeInTheDocument();
  expect(within(card!).getByText('Last backup: —')).toBeInTheDocument();
  expect(card).toHaveClass('prod-edge');

  // null size_gb: the size segment is simply absent
  const devCard = screen.getByText('analytics-dev').closest('article');
  expect(within(devCard!).getByText('PostgreSQL 17.1')).toBeInTheDocument();
  expect(devCard).not.toHaveClass('prod-edge');
});

test('toggles to the fleet table (no bulk-action checkboxes)', async () => {
  stubInstances();
  const user = userEvent.setup();
  renderPage();
  await screen.findByText('billing-prod');

  await user.click(screen.getByRole('button', { name: 'Table' }));

  const table = screen.getByRole('table');
  for (const header of ['Instance', 'Env', 'PG version', 'Last backup', 'Last vacuum', 'Bloat %']) {
    expect(within(table).getByText(header)).toBeInTheDocument();
  }
  expect(within(table).getAllByRole('row')).toHaveLength(4); // header + 3 instances
  expect(within(table).queryByRole('checkbox')).not.toBeInTheDocument();
  // unknown-data columns render em-dashes, never fake values
  expect(within(table).getAllByText('—').length).toBeGreaterThanOrEqual(6);
});

test('env pills refetch with the server-side filter', async () => {
  const mock = stubInstances();
  const user = userEvent.setup();
  renderPage();
  await screen.findByText('billing-prod');

  await user.click(screen.getByRole('button', { name: 'PROD' }));

  expect(await screen.findByText('billing-prod')).toBeInTheDocument();
  expect(screen.queryByText('billing-test')).not.toBeInTheDocument();
  const urls = mock.mock.calls.map((c) => String(c[0]));
  expect(urls).toContain('/api/instances?env=prod');
});

test('search filters client-side by name, cluster and owner', async () => {
  stubInstances();
  const user = userEvent.setup();
  renderPage();
  await screen.findByText('billing-prod');

  await user.type(screen.getByRole('searchbox', { name: 'Search instances' }), 'analytics');
  expect(screen.getByText('analytics-dev')).toBeInTheDocument();
  expect(screen.queryByText('billing-prod')).not.toBeInTheDocument();

  await user.clear(screen.getByRole('searchbox', { name: 'Search instances' }));
  await user.type(screen.getByRole('searchbox', { name: 'Search instances' }), 'zzz');
  expect(await screen.findByText(/No instances match/)).toBeInTheDocument();
});

test('empty inventory shows the import hint', async () => {
  stubInstances([]);
  renderPage();
  expect(await screen.findByText(/No instances in the inventory yet/)).toBeInTheDocument();
  expect(screen.getByText(/portal import/)).toBeInTheDocument();
});

test('network failure shows the unreachable message', async () => {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new TypeError('fetch failed'))),
  );
  renderPage();
  expect(await screen.findByRole('alert')).toHaveTextContent('API unreachable');
});

// WU-012 hero flow, UI side: Backup button → Screen 3 drawer → consequence-
// labeled launch → started confirmation.
test('Backup opens the launch drawer and starts a run', async () => {
  const mock = stubInstances();
  const user = userEvent.setup();
  renderPage();
  await screen.findByText('billing-test');

  const card = screen.getByText('billing-test').closest('article')!;
  await user.click(within(card).getByRole('button', { name: /Backup/ }));

  const drawer = await screen.findByRole('dialog', { name: 'Run Backup' });
  expect(within(drawer).getByText('billing-test')).toBeInTheDocument();
  expect(within(drawer).getByText('TEST')).toBeInTheDocument();
  expect(within(drawer).getByText('~25 min')).toBeInTheDocument();
  expect(within(drawer).getByText('Database stays online')).toBeInTheDocument();

  await user.type(within(drawer).getByRole('textbox'), 'CHG-77');
  await user.click(within(drawer).getByRole('button', { name: 'Run backup on billing-test' }));

  expect(await within(drawer).findByRole('status')).toHaveTextContent('Run #42 started');
  expect(within(drawer).getByRole('link', { name: 'View in Activity' })).toBeInTheDocument();

  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(post).toBeDefined();
  expect(JSON.parse(String((post![1] as RequestInit).body))).toEqual({
    instance: 'billing-test',
    operation: 'dump',
    reason: 'CHG-77',
  });
});

test('drawer cancel closes without posting', async () => {
  const mock = stubInstances();
  const user = userEvent.setup();
  renderPage();
  await screen.findByText('billing-test');

  const card = screen.getByText('billing-test').closest('article')!;
  await user.click(within(card).getByRole('button', { name: /Backup/ }));
  await user.click(screen.getByRole('button', { name: 'Cancel' }));

  expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  expect(mock.mock.calls.some((c) => (c[1] as RequestInit | undefined)?.method === 'POST')).toBe(
    false,
  );
});
