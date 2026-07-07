import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import { EnvBadge, type Env } from './EnvBadge';

// Every variant must carry its text label — color is never the only encoding.
const CASES: Array<{ env: Env; label: string }> = [
  { env: 'prod', label: 'PROD' },
  { env: 'test', label: 'TEST' },
  { env: 'dev', label: 'DEV' },
];

test.each(CASES)('renders $env as labeled badge', ({ env, label }) => {
  render(<EnvBadge env={env} />);
  const badge = screen.getByText(label);
  expect(badge).toBeInTheDocument();
  expect(badge).toHaveClass('env-badge', `env-${env}`);
});
