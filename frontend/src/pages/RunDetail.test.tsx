import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Operation, Run } from '../lib/api';
import { RunDetail } from './RunDetail';

const OPS: Operation[] = [
  {
    id: 'dump',
    label: 'Backup',
    icon: '💾',
    description: 'Full backup (pg_dump), verified after completion.',
    duration_hint: '~25 min',
    online_hint: 'Database stays online',
  },
];

function makeRun(overrides: Partial<Run>): Run {
  return {
    id: 7,
    instance: 'billing-test',
    environment: 'test',
    operation: 'dump',
    state: 'running',
    reason: null,
    error: null,
    job_id: 'mock-nonprod-1',
    requested_by: 'local-dev',
    submitted_at: '2026-07-07T12:31:00Z',
    started_at: '2026-07-07T12:31:01Z',
    finished_at: null,
    artifact: null,
    ...overrides,
  };
}

/** Minimal EventSource double: tests drive open/log/end/error by hand. */
class FakeEventSource {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSED = 2;
  static instances: FakeEventSource[] = [];

  url: string;
  readyState = 0;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }

  close() {
    this.readyState = FakeEventSource.CLOSED;
  }

  emit(type: string, data: string) {
    for (const fn of this.listeners.get(type) ?? []) {
      fn(new MessageEvent<string>(type, { data }));
    }
  }

  /** Server refused the stream (404/410): closed, error, no retry. */
  refuse() {
    this.readyState = FakeEventSource.CLOSED;
    this.onerror?.();
  }
}

function stubApi(run: Run): ReturnType<typeof vi.fn> {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.endsWith('/api/operations')) {
      return Promise.resolve(new Response(JSON.stringify({ operations: OPS })));
    }
    if (init?.method === 'POST') {
      return Promise.resolve(
        new Response(JSON.stringify({ status: 'canceling' }), { status: 202 }),
      );
    }
    if (url.endsWith(`/api/runs/${run.id}`)) {
      return Promise.resolve(new Response(JSON.stringify(run)));
    }
    return Promise.resolve(new Response(JSON.stringify({ error: 'no such run' }), { status: 404 }));
  });
  vi.stubGlobal('fetch', mock);
  return mock;
}

function renderRun(id: number | string) {
  return render(
    <MemoryRouter initialEntries={[`/runs/${id}`]}>
      <Routes>
        <Route path="/runs/:id" element={<RunDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

async function findStream(): Promise<FakeEventSource> {
  await vi.waitFor(() => {
    expect(FakeEventSource.instances).toHaveLength(1);
  });
  return FakeEventSource.instances[0];
}

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal('EventSource', FakeEventSource);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

test('live run: header, stage panel, streamed log lines, follow on', async () => {
  stubApi(makeRun({}));
  renderRun(7);

  expect(
    await screen.findByRole('heading', { name: /RUN-7 · Backup · billing-test/ }),
  ).toBeInTheDocument();
  expect(screen.getByText('TEST')).toBeInTheDocument();
  // Guardrail layer 1 (SPEC-015 behavior 1): the run context names its env.
  expect(screen.getByText(/TEST environment/)).toHaveClass('env-banner', 'env-test');
  expect(await screen.findByText('Running')).toBeInTheDocument();
  expect(screen.getByText(/Database stays online/)).toBeInTheDocument();

  const es = await findStream();
  expect(es.url).toBe('/api/runs/7/logs');
  act(() => {
    es.onopen?.();
    es.emit(
      'log',
      '{"ts":"2026-07-07T12:31:05Z","line":"TASK [preflight : ping billing-test] ok"}',
    );
    es.emit(
      'log',
      '{"ts":"2026-07-07T12:31:06Z","line":"TASK [dump : run pg_dump --format=custom] ok"}',
    );
  });
  expect(screen.getByText(/preflight : ping billing-test/)).toBeInTheDocument();
  expect(screen.getByText(/run pg_dump --format=custom/)).toBeInTheDocument();

  expect(screen.getByRole('button', { name: /Follow/ })).toHaveAttribute('aria-pressed', 'true');
});

test('success run: artifact strip, no abort, stream closed on end', async () => {
  stubApi(
    makeRun({
      state: 'success',
      finished_at: '2026-07-07T12:33:01Z',
      artifact: {
        name: 'billing-test-mock-nonprod-1.dump',
        size_bytes: 1048576,
        checksum: 'deadbeefcafe0123',
      },
    }),
  );
  renderRun(7);

  expect(await screen.findByText(/billing-test-mock-nonprod-1\.dump/)).toBeInTheDocument();
  expect(screen.getByText(/1\.0 MB/)).toBeInTheDocument();
  expect(screen.getByText(/sha256 deadbeefcafe…/)).toBeInTheDocument();
  expect(screen.getByText(/took 2m 0s/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: /Abort/ })).not.toBeInTheDocument();

  const es = await findStream();
  act(() => {
    es.onopen?.();
    es.emit('log', '{"ts":"2026-07-07T12:31:05Z","line":"PLAY RECAP ok=5 failed=0"}');
    es.emit('end', '{"state":"success"}');
  });
  expect(screen.getByText(/PLAY RECAP/)).toBeInTheDocument();
  expect(es.readyState).toBe(FakeEventSource.CLOSED);
});

test('failed run shows the calm failure card with the error', async () => {
  stubApi(
    makeRun({
      state: 'failed',
      error: 'step 2 (dump : run pg_dump --format=custom) failed',
      finished_at: '2026-07-07T12:32:00Z',
    }),
  );
  renderRun(7);

  const card = await screen.findByRole('alert');
  expect(card).toHaveTextContent('Backup did not complete.');
  expect(card).toHaveTextContent('Your database is online and unchanged.');
  expect(card).toHaveTextContent('step 2 (dump : run pg_dump --format=custom) failed');
  expect(card).toHaveTextContent('RUN-7');
});

test('abort posts the cancel and locks the button', async () => {
  const mock = stubApi(makeRun({}));
  const user = userEvent.setup();
  renderRun(7);

  await user.click(await screen.findByRole('button', { name: /Abort/ }));
  expect(await screen.findByRole('button', { name: /Aborting…/ })).toBeDisabled();

  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(post).toBeDefined();
  expect(String(post![0])).toBe('/api/runs/7/cancel');
});

test('refused stream shows the logs-gone note, outcome still served', async () => {
  stubApi(
    makeRun({
      state: 'failed',
      error: 'engine job lost (portal restart)',
      finished_at: '2026-07-07T12:32:00Z',
    }),
  );
  renderRun(7);

  const es = await findStream();
  act(() => {
    es.refuse();
  });
  expect(await screen.findByText(/Logs are no longer available/)).toBeInTheDocument();
  expect(screen.getByRole('alert')).toHaveTextContent('engine job lost (portal restart)');
});

test('unknown run renders the not-found state without retry loops', async () => {
  stubApi(makeRun({}));
  renderRun(999);

  expect(await screen.findByText('No such run.')).toBeInTheDocument();
  expect(FakeEventSource.instances).toHaveLength(0);
});
