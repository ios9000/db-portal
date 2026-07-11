import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Chain, Instance, RegisteredArtifact } from '../lib/api';
import { RestoreDrawer } from './RestoreDrawer';

const SOURCE_NONPROD: Instance = {
  name: 'billing-test',
  cluster: 'billing',
  env: 'test',
  platform: 'k8s_patroni',
  pg_version: '16.3',
  size_gb: 38,
  owner: 'billing-team',
  maintenance_window: null,
  window_state: null,
  last_backup_at: '2026-07-10T04:30:00Z',
};

const SOURCE_PROD: Instance = {
  ...SOURCE_NONPROD,
  name: 'billing-prod',
  env: 'prod',
};

const FLEET: Instance[] = [
  SOURCE_NONPROD,
  SOURCE_PROD,
  {
    name: 'analytics-dev',
    cluster: 'analytics',
    env: 'dev',
    platform: 'k8s_patroni',
    pg_version: '17.1',
    size_gb: null,
    owner: 'analytics-team',
    maintenance_window: null,
    window_state: null,
    last_backup_at: null,
  },
];

const ARTIFACT_NEW: RegisteredArtifact = {
  id: 20,
  run_id: 9,
  name: 'billing-test-2026-07-10.dump',
  size_bytes: 44 * 2 ** 20,
  checksum: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2',
  retention_class: 'standard',
  created_at: '2026-07-10T04:30:00Z',
};

const ARTIFACT_OLD: RegisteredArtifact = {
  id: 11,
  run_id: 3,
  name: 'billing-test-2026-07-03.dump',
  size_bytes: 40 * 2 ** 20,
  checksum: 'f6e5d4c3b2a1f6e5d4c3b2a1f6e5d4c3b2a1f6e5',
  retention_class: 'standard',
  created_at: '2026-07-03T04:30:00Z',
};

function makeChain(overrides: Partial<Chain> = {}): Chain {
  return {
    id: 55,
    kind: 'restore',
    instance: 'billing-test',
    env: 'test',
    state: 'running',
    created_by: 'dba1',
    reason: null,
    created_at: '2026-07-11T12:00:00Z',
    halted_at: null,
    finished_at: null,
    steps: [
      { seq: 1, operation: 'verify', run_id: 101, status: 'running' },
      { seq: 2, operation: 'safety_dump', run_id: null, status: 'pending' },
      { seq: 3, operation: 'restore', run_id: null, status: 'pending' },
    ],
    ...overrides,
  };
}

interface StubOpts {
  artifacts?: RegisteredArtifact[];
  instances?: Instance[];
  restoreResponse?: Chain;
  restoreStatus?: number;
  restoreError?: string;
}

/** Stub fetch for the drawer's whole API surface: the source's artifact
 * registry, the full fleet (target selector), and POST /api/restore. */
function stubApi({
  artifacts = [ARTIFACT_NEW, ARTIFACT_OLD],
  instances = FLEET,
  restoreResponse,
  restoreStatus = 201,
  restoreError,
}: StubOpts = {}) {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://test');
    const method = init?.method ?? 'GET';

    if (url.pathname === '/api/artifacts' && method === 'GET') {
      return Promise.resolve(new Response(JSON.stringify({ artifacts })));
    }
    if (url.pathname === '/api/instances' && method === 'GET') {
      return Promise.resolve(new Response(JSON.stringify({ instances })));
    }
    if (url.pathname === '/api/restore' && method === 'POST') {
      if (restoreError !== undefined) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: restoreError }), { status: restoreStatus }),
        );
      }
      const created = restoreResponse ?? makeChain();
      return Promise.resolve(new Response(JSON.stringify(created), { status: restoreStatus }));
    }
    return Promise.resolve(new Response('not found', { status: 404 }));
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

function renderDrawer(source: Instance = SOURCE_NONPROD, onClose: () => void = vi.fn()) {
  return render(
    <MemoryRouter>
      <RestoreDrawer source={source} onClose={onClose} />
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('lists the source artifacts newest-first, defaulting to the newest', async () => {
  stubApi();
  renderDrawer();

  const newest = await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  const older = screen.getByRole('radio', { name: /billing-test-2026-07-03/ });
  expect(newest).toBeChecked();
  expect(older).not.toBeChecked();
  // checksum (short) + size + retention class all render per artifact
  expect(screen.getByText(/a1b2c3d4e5f6/)).toBeInTheDocument();
  expect(screen.getByText('44.0 MB', { exact: false })).toBeInTheDocument();
});

test('empty artifact list shows the friendly hint and renders no submit control', async () => {
  stubApi({ artifacts: [] });
  renderDrawer();

  expect(
    await screen.findByText('No backups registered for billing-test yet — run a Backup first.'),
  ).toBeInTheDocument();
  expect(screen.queryByRole('radio')).not.toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Restore/ })).not.toBeInTheDocument();
});

test('non-prod source defaults the target to itself: no confirm ritual, submit enabled', async () => {
  stubApi();
  renderDrawer(SOURCE_NONPROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  const targetSelect = await screen.findByLabelText<HTMLSelectElement>('Restore onto');
  await waitFor(() => expect(targetSelect.value).toBe('billing-test'));
  expect(screen.queryByLabelText(/type the instance name/i)).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Restore onto billing-test' })).toBeEnabled();
});

test('prod source defaults to no target: submit disabled until one is chosen', async () => {
  stubApi();
  const user = userEvent.setup();
  renderDrawer(SOURCE_PROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  const targetSelect = await screen.findByLabelText<HTMLSelectElement>('Restore onto');
  expect(targetSelect.value).toBe('');
  expect(screen.getByRole('button', { name: 'Restore' })).toBeDisabled();

  await user.selectOptions(targetSelect, 'analytics-dev');
  expect(screen.getByRole('button', { name: 'Restore onto analytics-dev' })).toBeEnabled();
});

test('choosing a prod target reveals the typed-name ritual, paste blocked', async () => {
  stubApi();
  const user = userEvent.setup();
  renderDrawer(SOURCE_NONPROD);

  const targetSelect = await screen.findByLabelText<HTMLSelectElement>('Restore onto');
  await user.selectOptions(targetSelect, 'billing-prod');

  expect(screen.getByText(/PRODUCTION environment/)).toBeInTheDocument();
  const confirmInput = await screen.findByLabelText<HTMLInputElement>(/type the instance name/i);
  const submitBtn = screen.getByRole('button', { name: 'Restore onto billing-prod' });
  expect(submitBtn).toBeDisabled();

  confirmInput.focus();
  await user.paste('billing-prod');
  expect(confirmInput.value).toBe('');
  expect(submitBtn).toBeDisabled();

  await user.type(confirmInput, 'billing-prod');
  expect(submitBtn).toBeEnabled();
});

test('the safety dump step is fixed and non-optional — never a checkbox', async () => {
  stubApi();
  renderDrawer(SOURCE_NONPROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  expect(
    screen.getByText(/Safety backup of billing-test \(automatic — always taken\)/),
  ).toBeInTheDocument();
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
});

test('submit calls startRestore with the expected args and shows a View restore link', async () => {
  const mock = stubApi();
  const user = userEvent.setup();
  renderDrawer(SOURCE_NONPROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  await user.type(screen.getByRole('textbox', { name: /Reason/ }), '  CHG-99  ');
  await user.click(screen.getByRole('button', { name: 'Restore onto billing-test' }));

  expect(await screen.findByRole('status')).toHaveTextContent('Restore started on billing-test');
  expect(screen.getByRole('link', { name: 'View restore' })).toHaveAttribute('href', '/runs/101');

  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(post).toBeDefined();
  expect(JSON.parse(String((post![1] as RequestInit).body))).toEqual({
    artifact_id: 20,
    target: 'billing-test',
    reason: 'CHG-99',
  });
});

test('prod-target submit sends confirm and the typed name', async () => {
  const mock = stubApi();
  const user = userEvent.setup();
  renderDrawer(SOURCE_NONPROD);

  const targetSelect = await screen.findByLabelText<HTMLSelectElement>('Restore onto');
  await user.selectOptions(targetSelect, 'billing-prod');
  const confirmInput = await screen.findByLabelText<HTMLInputElement>(/type the instance name/i);
  await user.type(confirmInput, 'billing-prod');
  await user.click(screen.getByRole('button', { name: 'Restore onto billing-prod' }));

  expect(await screen.findByRole('status')).toHaveTextContent('Restore started on billing-prod');
  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(JSON.parse(String((post![1] as RequestInit).body))).toEqual({
    artifact_id: 20,
    target: 'billing-prod',
    confirm: 'billing-prod',
    reason: '',
  });
});

test('a chain with no step run_id yet links View restore to /activity', async () => {
  stubApi({
    restoreResponse: makeChain({
      steps: [
        { seq: 1, operation: 'verify', run_id: null, status: 'pending' },
        { seq: 2, operation: 'safety_dump', run_id: null, status: 'pending' },
        { seq: 3, operation: 'restore', run_id: null, status: 'pending' },
      ],
    }),
  });
  const user = userEvent.setup();
  renderDrawer(SOURCE_NONPROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  await user.click(screen.getByRole('button', { name: 'Restore onto billing-test' }));

  expect(await screen.findByRole('status')).toHaveTextContent('Restore started');
  expect(screen.getByRole('link', { name: 'View restore' })).toHaveAttribute('href', '/activity');
});

test('server error renders its detail', async () => {
  stubApi({
    restoreError: 'prod restore requires typing the target instance name',
    restoreStatus: 400,
  });
  const user = userEvent.setup();
  renderDrawer(SOURCE_NONPROD);

  await screen.findByRole('radio', { name: /billing-test-2026-07-10/ });
  await user.click(screen.getByRole('button', { name: 'Restore onto billing-test' }));

  expect(await screen.findByRole('alert')).toHaveTextContent(
    'prod restore requires typing the target instance name',
  );
});
