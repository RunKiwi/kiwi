/**
 * Timestamp, duration, and magnitude formatting for run views.
 *
 * The job tile used to render `toLocaleTimeString()` — and only as a fallback
 * when the job had no repo, which is the uncommon case. So a tile normally
 * carried no time at all, and when it did, "3:42:11 PM" was ambiguous the
 * moment the job was more than a day old.
 */

/** Full timestamp for a `title` attribute — the exact value, always available on hover. */
export function exactTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, { dateStyle: "full", timeStyle: "medium" });
}

/**
 * Short, unambiguous timestamp for a dense list.
 *
 * Scales the precision to the age: a run from this morning wants the clock
 * time, one from last month wants the date. Showing both always would cost
 * horizontal space the tile does not have.
 */
export function shortTime(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";

  const time = d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return time;

  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return `Yesterday ${time}`;

  // Inside a week, the weekday reads faster than a numeric date.
  const ageDays = (now.getTime() - d.getTime()) / 86_400_000;
  if (ageDays >= 0 && ageDays < 7) {
    return `${d.toLocaleDateString(undefined, { weekday: "short" })} ${time}`;
  }

  const sameYear = d.getFullYear() === now.getFullYear();
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  });
}

/**
 * Elapsed wall clock, in the largest unit that keeps it readable.
 *
 * Runs here span a second to tens of minutes, so "1h 4m" and "312ms" both have
 * to look right. Sub-second matters because a cached phase completing instantly
 * is meaningful — rounding it to "0s" would hide that.
 */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)}ms`;

  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;

  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return seconds === 0 ? `${minutes}m` : `${minutes}m ${seconds}s`;

  const hours = Math.floor(minutes / 60);
  const rem = minutes % 60;
  return rem === 0 ? `${hours}h` : `${hours}h ${rem}m`;
}

/** Elapsed time between two timestamps, or "" if either is missing/invalid. */
export function durationBetween(startIso?: string, endIso?: string): string {
  if (!startIso || !endIso) return "";
  const a = Date.parse(startIso);
  const b = Date.parse(endIso);
  if (Number.isNaN(a) || Number.isNaN(b) || b < a) return "";
  return formatDuration(b - a);
}

/**
 * Cost, with enough precision to stay honest at both ends.
 *
 * A session round can cost a fraction of a cent, and rounding that to "$0.00"
 * reads as free rather than as small. Anything at or above a cent gets the
 * familiar two decimals.
 */
export function formatCost(usd: number): string {
  if (!Number.isFinite(usd) || usd < 0) return "";
  if (usd === 0) return "$0.00";
  if (usd < 0.01) return `$${usd.toFixed(4)}`;
  return `$${usd.toFixed(2)}`;
}

/** Token counts, abbreviated — exact digits past a few thousand are noise. */
export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "";
  if (n < 1000) return String(Math.round(n));
  if (n < 1_000_000) {
    const k = n / 1000;
    return `${k < 10 ? k.toFixed(1) : Math.round(k)}k`;
  }
  return `${(n / 1_000_000).toFixed(1)}M`;
}
