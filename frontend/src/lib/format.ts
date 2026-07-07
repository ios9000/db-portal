/*
 * Shared display formatting for runs (WU-013): timestamps, durations,
 * artifact sizes. Pure functions, locale-aware where it matters.
 */

/** Compact "Jul 7, 12:31" for run lists and headers. */
export function formatTimestamp(iso: string): string {
  return new Date(iso).toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

/** "12:31:07" wall-clock stamp for log lines. */
export function formatClock(iso: string): string {
  return new Date(iso).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

/** "45s" / "1m 30s" between two instants; "—" until both exist. */
export function formatDuration(startIso: string | null, endIso: string | null): string {
  if (startIso === null || endIso === null) return '—';
  return formatSeconds((new Date(endIso).getTime() - new Date(startIso).getTime()) / 1000);
}

/** Seconds → "45s" / "1m 30s". Elapsed-time display for live runs. */
export function formatSeconds(secs: number): string {
  const s = Math.max(0, Math.round(secs));
  if (s < 60) return `${s}s`;
  return `${Math.floor(s / 60)}m ${s % 60}s`;
}

/** "1.0 MB" / "4.2 GB" human artifact size. */
export function formatBytes(n: number): string {
  if (n >= 2 ** 30) return `${(n / 2 ** 30).toFixed(1)} GB`;
  if (n >= 2 ** 20) return `${(n / 2 ** 20).toFixed(1)} MB`;
  if (n >= 2 ** 10) return `${(n / 2 ** 10).toFixed(1)} KB`;
  return `${n} B`;
}
