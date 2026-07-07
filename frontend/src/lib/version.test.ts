import { expect, test } from 'vitest';
import { APP_VERSION } from './version';

test('version placeholder', () => {
  expect(APP_VERSION).toBe('0.0.1');
});
