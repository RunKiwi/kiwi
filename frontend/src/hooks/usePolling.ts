import { useEffect, useRef } from "react";

export interface PollingOptions {
  enabled?: boolean;
  activeIntervalMs?: number; // Frequency when jobs are running/queued (default 2500ms)
  idleIntervalMs?: number;   // Backoff frequency when all jobs are terminal (default 15000ms)
  isIdle?: boolean;          // Set to true when all jobs are terminal
}

/**
 * The delay before the next poll. Extracted so the backoff rule is testable
 * without standing up a renderer.
 */
export function pollIntervalFor(
  isIdle: boolean,
  opts: { activeIntervalMs?: number; idleIntervalMs?: number } = {},
): number {
  const { activeIntervalMs = 2500, idleIntervalMs = 15000 } = opts;
  return isIdle ? idleIntervalMs : activeIntervalMs;
}

/**
 * Custom hook providing robust polling discipline:
 * - Pauses execution when page/tab is hidden via Page Visibility API.
 * - Resumes immediately when tab becomes visible.
 * - Dynamically backs off interval when all jobs are in terminal state.
 * - Ensures clean timer cancellation on unmount.
 */
export function usePolling(
  callback: () => Promise<void> | void,
  options: PollingOptions = {}
) {
  const {
    enabled = true,
    activeIntervalMs = 2500,
    idleIntervalMs = 15000,
    isIdle = false,
  } = options;

  const callbackRef = useRef(callback);

  useEffect(() => {
    callbackRef.current = callback;
  }, [callback]);

  useEffect(() => {
    if (!enabled) return;

    let timerId: ReturnType<typeof setTimeout> | null = null;
    let cancelled = false;
    // Guards against two timer chains existing at once. Without it, a
    // visibilitychange arriving while a tick is still awaiting its callback
    // starts a second chain: the in-flight tick reschedules from its finally
    // block and so does the new one, permanently doubling the poll rate — and
    // every further tab switch adds another.
    let inFlight = false;

    const intervalMs = pollIntervalFor(isIdle, { activeIntervalMs, idleIntervalMs });

    const scheduleNext = () => {
      if (cancelled) return;
      if (timerId) clearTimeout(timerId);
      timerId = setTimeout(tick, intervalMs);
    };

    const tick = async () => {
      if (cancelled || inFlight) return;

      // A hidden tab skips the request but still reschedules, so the chain
      // cannot die in the window between checking document.hidden and the
      // visibilitychange event that would otherwise have to revive it. An
      // idle timer costs nothing; browsers throttle background timers anyway.
      if (typeof document !== "undefined" && document.hidden) {
        scheduleNext();
        return;
      }

      inFlight = true;
      try {
        await callbackRef.current();
      } catch {
        // A failed poll is usually a transient network blip. Swallow it rather
        // than logging on every tick — at this cadence that floods the console
        // while the backend is briefly unreachable.
      } finally {
        inFlight = false;
        scheduleNext();
      }
    };

    const handleVisibilityChange = () => {
      if (typeof document !== "undefined" && !document.hidden && !cancelled) {
        // Back in view — refresh straight away rather than waiting out the
        // remainder of the current interval.
        scheduleNext();
        void tick();
      }
    };

    if (typeof document !== "undefined") {
      document.addEventListener("visibilitychange", handleVisibilityChange);
    }

    scheduleNext();

    return () => {
      cancelled = true;
      if (timerId) clearTimeout(timerId);
      if (typeof document !== "undefined") {
        document.removeEventListener("visibilitychange", handleVisibilityChange);
      }
    };
  }, [enabled, activeIntervalMs, idleIntervalMs, isIdle]);
}
