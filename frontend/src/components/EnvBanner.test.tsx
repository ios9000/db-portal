import { render, screen } from '@testing-library/react';
import { expect, test } from 'vitest';
import type { Env } from './EnvBadge';
import { EnvBanner } from './EnvBanner';

// Guardrail layer 1 (SPEC-015 behaviors 1–2): the environment is named in
// text on every banner — color is never the only encoding.
const CASES: Array<{ env: Env; text: RegExp }> = [
  { env: 'prod', text: /PRODUCTION environment/ },
  { env: 'test', text: /TEST environment/ },
  { env: 'dev', text: /DEV environment/ },
];

test.each(CASES)('renders $env as labeled banner', ({ env, text }) => {
  render(<EnvBanner env={env} />);
  const banner = screen.getByText(text);
  expect(banner).toBeInTheDocument();
  expect(banner).toHaveClass('env-banner', `env-${env}`);
});

test('prod banner warns about real users', () => {
  render(<EnvBanner env="prod" />);
  expect(screen.getByText(/affect real users/)).toBeInTheDocument();
});
