import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { RunStatus, type RunStatusValue } from './RunStatus';

const CASES: Array<{ status: RunStatusValue; label: string }> = [
  { status: 'queued', label: 'Queued' },
  { status: 'running', label: 'Running' },
  { status: 'success', label: 'Success' },
  { status: 'failed', label: 'Failed' },
  { status: 'halted', label: 'Halted' },
];

test.each(CASES)('renders $status as labeled chip', ({ status, label }) => {
  render(<RunStatus status={status} />);
  const chip = screen.getByText(label);
  expect(chip).toBeInTheDocument();
  expect(chip).toHaveClass('run-status', `run-status-${status}`);
});
