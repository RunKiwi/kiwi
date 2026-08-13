/**
 * Seconds since a phase began, or null when there is nothing to measure.
 *
 * This is a different question from LiveRun's own `staleness()`: staleness
 * asks whether the feed is still arriving (time since the daemon last
 * reported anything); this asks how long the CURRENT phase has taken, which
 * keeps advancing even while the feed is healthy and reporting the same
 * phase every three seconds. A four-minute Architect plan and a
 * just-started one otherwise render identically.
 */
export function elapsedSince(phaseSince?: string): number | null {
  if (!phaseSince) return null;
  const t = Date.parse(phaseSince);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.round((Date.now() - t) / 1000));
}
