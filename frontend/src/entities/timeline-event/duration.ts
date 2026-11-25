/** 84,000 → "1m 24s"; 372,000 → "6m"; 6,240,000 → "1h 44m" — durations
 *  stay short; zero parts drop. */
export function fmtDuration(ms: number): string {
  if (!ms || ms < 0) return '';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rest = s % 60;
  if (m < 60) return rest ? `${m}m ${rest}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const restM = m % 60;
  return restM ? `${h}h ${restM}m` : `${h}h`;
}
