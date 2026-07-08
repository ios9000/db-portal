import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router';
import { afterEach, expect, test, vi } from 'vitest';
import type { Instance, Operation, Run } from '../lib/api';
import { LaunchDrawer } from './LaunchDrawer';

const DUMP: Operation = {
  id: 'dump',
  label: 'Backup',
  icon: '💾',
  description: 'Full backup (pg_dump), verified after completion.',
  duration_hint: '~25 min',
  online_hint: 'Database stays online',
};

function makeInstance(overrides: Partial<Instance>): Instance {
  return {
    name: 'billing-test',
    cluster: 'billing',
    env: 'test',
    platform: 'k8s_patroni',
    pg_version: '16.3',
    size_gb: 12,
    owner: 'team-billing',
    maintenance_window: null,
    ...overrides,
  };
}

const STARTED: Run = {
  id: 42,
  instance: 'billing-prod',
  environment: 'prod',
  operation: 'dump',
  state: 'queued',
  reason: null,
  error: null,
  job_id: 'mock-prod-1',
  requested_by: 'local-dev',
  submitted_at: '2026-07-07T12:31:00Z',
  started_at: null,
  finished_at: null,
  artifact: null,
};

function stubStartRun() {
  const mock = vi.fn(() => Promise.resolve(new Response(JSON.stringify(STARTED), { status: 201 })));
  vi.stubGlobal('fetch', mock);
  return mock;
}

function renderDrawer(instance: Instance) {
  return render(
    <MemoryRouter>
      <LaunchDrawer instance={instance} operation={DUMP} onClose={() => {}} />
    </MemoryRouter>,
  );
}

function launchButton() {
  return screen.getByRole('button', { name: /run backup on/i });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// SPEC-015 behavior 5: non-prod is one click — no ritual, button live.
test('non-prod launch is one click', async () => {
  const fetchMock = stubStartRun();
  renderDrawer(makeInstance({ env: 'test' }));

  expect(screen.getByText(/TEST environment/)).toBeInTheDocument();
  expect(screen.queryByLabelText(/type the instance name/i)).not.toBeInTheDocument();
  expect(launchButton()).toBeEnabled();

  await userEvent.click(launchButton());
  expect(await screen.findByText(/Run #42 started/)).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledOnce();
});

// SPEC-015 behavior 3: prod unlocks only on the exact instance name.
test('prod launch requires the exact typed instance name', async () => {
  const fetchMock = stubStartRun();
  renderDrawer(makeInstance({ name: 'billing-prod', env: 'prod' }));

  expect(screen.getByText(/PRODUCTION environment/)).toBeInTheDocument();
  const confirm = screen.getByLabelText(/type the instance name/i);
  expect(screen.getByText(/\(paste disabled\)/)).toBeInTheDocument();
  expect(launchButton()).toBeDisabled();

  await userEvent.type(confirm, 'billing-pro'); // partial (Screen 4's example)
  expect(launchButton()).toBeDisabled();

  await userEvent.type(confirm, 'd');
  expect(launchButton()).toBeEnabled();

  await userEvent.type(confirm, 'x'); // overshoot re-locks
  expect(launchButton()).toBeDisabled();
  expect(fetchMock).not.toHaveBeenCalled();
});

// SPEC-015 behavior 4: paste into the confirm field is ignored.
test('prod confirm field rejects paste', async () => {
  stubStartRun();
  renderDrawer(makeInstance({ name: 'billing-prod', env: 'prod' }));

  const confirm = screen.getByLabelText<HTMLInputElement>(/type the instance name/i);
  confirm.focus();
  await userEvent.paste('billing-prod');

  expect(confirm.value).toBe('');
  expect(launchButton()).toBeDisabled();
});

// The ritual gates the click, not the API: a confirmed prod launch starts.
test('confirmed prod launch starts the run', async () => {
  const fetchMock = stubStartRun();
  renderDrawer(makeInstance({ name: 'billing-prod', env: 'prod' }));

  await userEvent.type(screen.getByLabelText(/type the instance name/i), 'billing-prod');
  await userEvent.click(launchButton());

  expect(await screen.findByText(/Run #42 started/)).toBeInTheDocument();
  expect(fetchMock).toHaveBeenCalledOnce();
});
