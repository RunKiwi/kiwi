import { useEffect, useRef } from "react";

export interface PollingOptions {
  enabled?: boolean;
  activeIntervalMs?: number; // Frequency when jobs are running/queued (default 2500ms)
  idleIntervalMs?: number;   // Backoff frequency when all jobs are terminal (default 15000ms)
  isIdle?: boolean;          // Set to true when all jobs are terminal
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

    const intervalMs = isIdle ? idleIntervalMs : activeIntervalMs;

    const scheduleNext = () => {
      if (!cancelled) {
        timerId = setTimeout(tick, intervalMs);
      }
    };

    const tick = async () => {
      if (cancelled) return;

      // Pause execution when document is hidden
      if (typeof document !== "undefined" && document.hidden) {
        return;
      }

      try {
        await callbackRef.current();
      } catch (err) {
        // Prevent unhandled rejection breaking polling cycle
        console.error("[usePolling] callback error:", err);
      } finally {
        scheduleNext();
      }
    };

    const handleVisibilityChange = () => {
      if (typeof document !== "undefined" && !document.hidden && !cancelled) {
        // Tab became visible again — clear any pending backoff and run immediate update
        if (timerId) clearTimeout(timerId);
        tick();
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
