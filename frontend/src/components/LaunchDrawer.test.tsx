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
    last_backup_at: null,
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

function renderDrawer(instance: Instance, onClose: () => void = () => {}) {
  return render(
    <MemoryRouter>
      <LaunchDrawer instance={instance} operation={DUMP} onClose={onClose} />
    </MemoryRouter>,
  );
}

/** The backdrop is the dialog's parent — clicks there simulate an overlay click. */
function overlay() {
  return screen.getByRole('dialog').parentElement!;
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

// m1-gate item 7: an overlay click firing mid-launch must not eat the
// operator's confirmation/run-id feedback.
test('overlay click during an in-flight launch keeps the drawer open', async () => {
  let resolvePost!: (r: Response) => void;
  const pending = new Promise<Response>((resolve) => {
    resolvePost = resolve;
  });
  vi.stubGlobal(
    'fetch',
    vi.fn(() => pending),
  );
  const onClose = vi.fn();
  const user = userEvent.setup();
  renderDrawer(makeInstance({ env: 'test' }), onClose);

  await user.click(launchButton());
  expect(await screen.findByRole('button', { name: /Starting…/ })).toBeDisabled();

  await user.click(overlay());
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  expect(onClose).not.toHaveBeenCalled();

  // Let the in-flight request settle so it doesn't leak into other tests.
  resolvePost(new Response(JSON.stringify(STARTED), { status: 201 }));
  expect(await screen.findByText(/Run #42 started/)).toBeInTheDocument();
});

// m1-gate item 7: the started state (run id + "View run") must survive an
// overlay click too; explicit Close still works.
test('overlay click on the started state keeps it open; explicit Close still works', async () => {
  stubStartRun();
  const onClose = vi.fn();
  const user = userEvent.setup();
  renderDrawer(makeInstance({ env: 'test' }), onClose);

  await user.click(launchButton());
  expect(await screen.findByText(/Run #42 started/)).toBeInTheDocument();

  await user.click(overlay());
  expect(screen.getByRole('dialog')).toBeInTheDocument();
  expect(onClose).not.toHaveBeenCalled();

  await user.click(screen.getByRole('button', { name: 'Close' }));
  expect(onClose).toHaveBeenCalledOnce();
});
