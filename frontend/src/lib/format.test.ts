import { expect, test } from 'vitest';
import { formatBytes, formatDuration, formatSeconds } from './format';

test('formatDuration needs both instants', () => {
  expect(formatDuration(null, null)).toBe('—');
  expect(formatDuration('2026-07-07T12:00:00Z', null)).toBe('—');
  expect(formatDuration('2026-07-07T12:00:00Z', '2026-07-07T12:00:45Z')).toBe('45s');
  expect(formatDuration('2026-07-07T12:00:00Z', '2026-07-07T12:01:30Z')).toBe('1m 30s');
});

test('formatSeconds clamps negatives and splits minutes', () => {
  expect(formatSeconds(-3)).toBe('0s');
  expect(formatSeconds(59.4)).toBe('59s');
  expect(formatSeconds(61)).toBe('1m 1s');
});

test('formatBytes picks a readable unit', () => {
  expect(formatBytes(512)).toBe('512 B');
  expect(formatBytes(1048576)).toBe('1.0 MB');
  expect(formatBytes(4.2 * 2 ** 30)).toBe('4.2 GB');
});
