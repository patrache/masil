/** Current wall-clock time in epoch milliseconds. */
export function now(): number {
  return Date.now();
}

/** RFC 3339 UTC with milliseconds, e.g. "2026-11-05T02:03:04.567Z". */
export function iso(ms: number): string {
  return new Date(ms).toISOString();
}

export function isoOrNull(ms: number | null | undefined): string | null {
  return ms === null || ms === undefined ? null : iso(ms);
}
