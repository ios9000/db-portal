import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { act } from 'react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { afterEach, beforeEach, expect, test, vi } from 'vitest';
import type { Chain, Operation, Run } from '../lib/api';
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
  {
    id: 'restore',
    label: 'Restore',
    icon: '♻',
    description: 'Restore from a stored artifact.',
    duration_hint: '~15 min',
    online_hint: 'Database stays online',
  },
  {
    id: 'verify',
    label: 'Verify',
    icon: '✓',
    description: 'Verify a restored database.',
    duration_hint: '~5 min',
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

  /**
   * A failed connection: closed + error, exactly what a real EventSource
   * looks like whether the cause is a genuine 404/410 or a transient
   * network drop (EventSource can't tell the difference itself). The
   * wrapper in api.ts decides whether that means "retry" or "give up".
   */
  refuse() {
    this.readyState = FakeEventSource.CLOSED;
    this.onerror?.();
  }
}

/** WU-032 chain fixture: three sequential dump/restore/verify steps, seq 2
 * (this run) mid-chain — run_id 6 for the sibling before it, null (pending)
 * for the one after. */
function makeChain(overrides: Partial<Chain> = {}): Chain {
  return {
    id: 1,
    kind: 'test-chain',
    instance: 'billing-test',
    env: 'test',
    state: 'running',
    created_by: 'dba1',
    reason: null,
    created_at: '2026-07-07T12:30:00Z',
    halted_at: null,
    finished_at: null,
    steps: [
      { seq: 1, operation: 'dump', run_id: 6, status: 'success' },
      { seq: 2, operation: 'restore', run_id: 7, status: 'running' },
      { seq: 3, operation: 'verify', run_id: null, status: 'pending' },
    ],
    ...overrides,
  };
}

/** A chain halted on this run's own (failed) step. */
function makeHaltedChain(): Chain {
  return makeChain({
    state: 'halted',
    halted_at: '2026-07-07T12:32:00Z',
    steps: [
      { seq: 1, operation: 'dump', run_id: 6, status: 'success' },
      { seq: 2, operation: 'restore', run_id: 7, status: 'failed' },
      { seq: 3, operation: 'verify', run_id: null, status: 'pending' },
    ],
  });
}

type ResumeResult = Chain | { status: number; error: string };

/**
 * Extends the run/log fixture with the two WU-032 chain endpoints.
 * `chain` is called on every GET .../chain — a function (not a static
 * value) so a test can change its answer between polls (e.g. superseded:
 * running, then 404). Omitted/null means "not a chain step" (404), which
 * is also the default every pre-WU-032 test relies on.
 */
function stubApi(
  run: Run,
  opts: { chain?: () => Chain | null; resume?: () => ResumeResult } = {},
): ReturnType<typeof vi.fn> {
  const mock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    if (url.endsWith('/api/operations')) {
      return Promise.resolve(new Response(JSON.stringify({ operations: OPS })));
    }
    if (method === 'POST' && /\/api\/chains\/\d+\/resume$/.test(url)) {
      const result = opts.resume?.() ?? { status: 404, error: 'no such chain' };
      if ('status' in result) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: result.error }), { status: result.status }),
        );
      }
      return Promise.resolve(new Response(JSON.stringify(result)));
    }
    if (method === 'POST') {
      return Promise.resolve(
        new Response(JSON.stringify({ status: 'canceling' }), { status: 202 }),
      );
    }
    if (url.endsWith(`/api/runs/${run.id}/chain`)) {
      const c = opts.chain?.() ?? null;
      if (c === null) {
        return Promise.resolve(
          new Response(JSON.stringify({ error: 'run is not part of a chain' }), { status: 404 }),
        );
      }
      return Promise.resolve(new Response(JSON.stringify(c)));
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

// m1-gate item 6: a transient stream failure must not discard what's already
// on screen — it retries with backoff and keeps the lines until it either
// recovers or truly gives up.
test('transient stream error keeps existing lines, retries, and streams normally on reconnect', async () => {
  stubApi(makeRun({}));
  renderRun(7);

  const es1 = await findStream();
  act(() => {
    es1.onopen?.();
    es1.emit('log', '{"ts":"2026-07-07T12:31:05Z","line":"TASK [preflight] ok"}');
  });
  expect(screen.getByText(/TASK \[preflight\]/)).toBeInTheDocument();

  vi.useFakeTimers();
  try {
    act(() => {
      es1.refuse(); // transient failure, well before any `end`
    });
    // Nothing is discarded while backoff is pending.
    expect(screen.getByText(/TASK \[preflight\]/)).toBeInTheDocument();
    expect(screen.queryByText(/Logs are no longer available/)).not.toBeInTheDocument();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000); // past the ~500ms base backoff
    });
    expect(FakeEventSource.instances).toHaveLength(2);
    const es2 = FakeEventSource.instances[1];
    expect(es2.url).toBe('/api/runs/7/logs');

    act(() => {
      es2.onopen?.(); // reconnect replays history (mini-ADR 2) -> buffer resets
      es2.emit('log', '{"ts":"2026-07-07T12:31:07Z","line":"TASK [dump] ok"}');
    });
    expect(screen.getByText(/TASK \[dump\]/)).toBeInTheDocument();
  } finally {
    vi.useRealTimers();
  }
});

// A stream that keeps failing (including a genuine 410 after a portal
// restart, indistinguishable to EventSource) exhausts its retry budget and
// only then falls back to the logs-gone state; run outcome stays served.
test('stream that keeps failing exhausts retries before showing the logs-gone state', async () => {
  stubApi(
    makeRun({
      state: 'failed',
      error: 'engine job lost (portal restart)',
      finished_at: '2026-07-07T12:32:00Z',
    }),
  );
  renderRun(7);

  let current = await findStream();
  vi.useFakeTimers();
  try {
    for (let i = 0; i < 5; i++) {
      act(() => {
        current.refuse();
      });
      // Not exhausted yet: no logs-gone state, retry gets scheduled.
      expect(screen.queryByText(/Logs are no longer available/)).not.toBeInTheDocument();
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10_000); // past the ~8s backoff cap
      });
      expect(FakeEventSource.instances).toHaveLength(i + 2);
      current = FakeEventSource.instances[i + 1];
    }
    // 6th failure (initial connect + 5 retries, all failed): give up for real.
    act(() => {
      current.refuse();
    });
    expect(screen.getByText(/Logs are no longer available/)).toBeInTheDocument();
    expect(FakeEventSource.instances).toHaveLength(6); // no further retry scheduled
    expect(screen.getByRole('alert')).toHaveTextContent('engine job lost (portal restart)');
  } finally {
    vi.useRealTimers();
  }
});

// An error that arrives after `end` (or after the stream is otherwise done)
// is the existing clean-close case, not a failure to retry.
test('an error after `end` does not retry', async () => {
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

  const es = await findStream();
  act(() => {
    es.onopen?.();
    es.emit('log', '{"ts":"2026-07-07T12:31:05Z","line":"PLAY RECAP ok=5 failed=0"}');
    es.emit('end', '{"state":"success"}');
  });
  expect(es.readyState).toBe(FakeEventSource.CLOSED);

  act(() => {
    es.onerror?.(); // stray error after the stream already finished cleanly
  });
  expect(FakeEventSource.instances).toHaveLength(1); // no retry connection opened
  expect(screen.queryByText(/Logs are no longer available/)).not.toBeInTheDocument();
  expect(screen.getByText(/PLAY RECAP/)).toBeInTheDocument();
});

test('unknown run renders the not-found state without retry loops', async () => {
  stubApi(makeRun({}));
  renderRun(999);

  expect(await screen.findByText('No such run.')).toBeInTheDocument();
  expect(FakeEventSource.instances).toHaveLength(0);
});

// ---- WU-032: chain strip ----

test('a run that is not a chain step renders normally with no chain strip', async () => {
  stubApi(makeRun({}));
  renderRun(7);

  expect(
    await screen.findByRole('heading', { name: /RUN-7 · Backup · billing-test/ }),
  ).toBeInTheDocument();
  expect(await screen.findByText('Running')).toBeInTheDocument(); // the page renders normally
  expect(screen.queryByRole('region', { name: 'Chain' })).not.toBeInTheDocument();
});

test('a chain step run shows the strip: all steps, sibling link, counter', async () => {
  stubApi(makeRun({ operation: 'restore', state: 'running' }), { chain: () => makeChain() });
  renderRun(7);

  const strip = await screen.findByRole('region', { name: 'Chain' });
  expect(within(strip).getByText('Step 2 of 3')).toBeInTheDocument();

  // seq 1 (run 6, not this page's run 7): a link off to its own run page.
  const sibling = within(strip).getByRole('link', { name: 'Backup' });
  expect(sibling).toHaveAttribute('href', '/runs/6');

  // seq 2 (run 7, this page's run): the current step, plain text — not a link.
  expect(within(strip).queryByRole('link', { name: 'Restore' })).not.toBeInTheDocument();
  expect(within(strip).getByText(/Restore/)).toBeInTheDocument();

  // seq 3 (no run yet): pending, plain text — not a link.
  expect(within(strip).queryByRole('link', { name: 'Verify' })).not.toBeInTheDocument();
  expect(within(strip).getByText(/Verify/)).toBeInTheDocument();

  // Run-backed steps carry the RunStatus chip; the unfired one is Pending.
  expect(within(strip).getByText('Success')).toBeInTheDocument();
  expect(within(strip).getByText('Running')).toBeInTheDocument();
  expect(within(strip).getByText('Pending')).toBeInTheDocument();
});

test('halted chain: Resume calls resumeChain and applies the returned (running) chain', async () => {
  const halted = makeHaltedChain();
  const resumed = makeChain({ state: 'running', halted_at: null });
  let didResume = false;
  const mock = stubApi(
    makeRun({
      operation: 'restore',
      state: 'failed',
      error: 'boom',
      finished_at: '2026-07-07T12:32:00Z',
    }),
    {
      chain: () => (didResume ? resumed : halted),
      resume: () => {
        didResume = true;
        return resumed;
      },
    },
  );
  const user = userEvent.setup();
  renderRun(7);

  const strip = await screen.findByRole('region', { name: 'Chain' });
  await user.click(within(strip).getByRole('button', { name: 'Resume' }));

  await vi.waitFor(() => {
    expect(within(strip).queryByRole('button', { name: /Resume/ })).not.toBeInTheDocument();
  });

  const post = mock.mock.calls.find((c) => (c[1] as RequestInit | undefined)?.method === 'POST');
  expect(post).toBeDefined();
  expect(String(post![0])).toBe('/api/chains/1/resume');
});

test('resume failure (409) shows the server detail as an alert and keeps the halted chain', async () => {
  const halted = makeHaltedChain();
  stubApi(
    makeRun({
      operation: 'restore',
      state: 'failed',
      error: 'boom',
      finished_at: '2026-07-07T12:32:00Z',
    }),
    { chain: () => halted, resume: () => ({ status: 409, error: 'chain is not halted' }) },
  );
  const user = userEvent.setup();
  renderRun(7);

  const strip = await screen.findByRole('region', { name: 'Chain' });
  await user.click(within(strip).getByRole('button', { name: 'Resume' }));

  expect(await within(strip).findByRole('alert')).toHaveTextContent('chain is not halted');
  expect(within(strip).getByRole('button', { name: 'Resume' })).toBeInTheDocument();
});

test('a running chain that 404s on a later poll shows the superseded note and stops polling', async () => {
  let polls = 0;
  stubApi(makeRun({ operation: 'restore', state: 'running' }), {
    chain: () => {
      polls += 1;
      return polls === 1 ? makeChain({ state: 'running' }) : null;
    },
  });

  vi.useFakeTimers();
  try {
    renderRun(7);

    const strip = await vi.waitFor(() => {
      const el = screen.getByRole('region', { name: 'Chain' });
      expect(within(el).getByText('Step 2 of 3')).toBeInTheDocument();
      return el;
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(3_000); // the page's POLL_MS cadence
    });

    expect(
      within(strip).getByText('This chain was resumed — this attempt was superseded by a new run.'),
    ).toBeInTheDocument();
    expect(within(strip).queryByText('Step 2 of 3')).not.toBeInTheDocument();
    expect(polls).toBe(2); // no further poll went out once superseded
  } finally {
    vi.useRealTimers();
  }
});

test('a non-halted (success) chain shows no Resume button', async () => {
  stubApi(
    makeRun({ operation: 'restore', state: 'success', finished_at: '2026-07-07T12:33:00Z' }),
    {
      chain: () =>
        makeChain({
          state: 'success',
          finished_at: '2026-07-07T12:33:05Z',
          steps: [
            { seq: 1, operation: 'dump', run_id: 6, status: 'success' },
            { seq: 2, operation: 'restore', run_id: 7, status: 'success' },
            { seq: 3, operation: 'verify', run_id: 8, status: 'success' },
          ],
        }),
    },
  );
  renderRun(7);

  const strip = await screen.findByRole('region', { name: 'Chain' });
  expect(within(strip).queryByRole('button', { name: /Resume/ })).not.toBeInTheDocument();
});
