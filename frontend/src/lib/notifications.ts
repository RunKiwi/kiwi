export function getNotificationPermission(): NotificationPermission | "unsupported" {
  if (typeof window !== "undefined" && "Notification" in window) {
    return Notification.permission;
  }
  return "unsupported";
}

export async function requestNotificationPermission(): Promise<boolean> {
  if (typeof window !== "undefined" && "Notification" in window) {
    const perm = await Notification.requestPermission();
    if (perm === "granted") {
      setNotificationEnabled(true);
      return true;
    }
  }
  setNotificationEnabled(false);
  return false;
}

export function isNotificationEnabled(): boolean {
  if (typeof localStorage === "undefined") return false;
  const isGranted = typeof Notification !== "undefined" && Notification.permission === "granted";
  return isGranted && localStorage.getItem("kiwi_notifications_enabled") === "true";
}

export function setNotificationEnabled(enabled: boolean) {
  if (typeof localStorage !== "undefined") {
    localStorage.setItem("kiwi_notifications_enabled", enabled ? "true" : "false");
  }
}

export function sendJobCompletionNotification(jobId: string, status: "SUCCEEDED" | "FAILED", taskGoal?: string) {
  if (!isNotificationEnabled()) return;

  // Sentence case, no emoji — the rest of the interface uses neither, and a
  // failure is not an occasion for decoration.
  const title = status === "SUCCEEDED" ? "Job succeeded" : "Job failed";
  const body = taskGoal
    ? `${taskGoal.slice(0, 80)}${taskGoal.length > 80 ? "…" : ""}`
    : `Job ${jobId.slice(0, 10)}`;

  try {
    const notification = new Notification(title, {
      body,
      tag: `job-${jobId}`,
    });

    notification.onclick = () => {
      if (typeof window !== "undefined") {
        window.focus();
        window.location.href = `/?job=${encodeURIComponent(jobId)}`;
      }
    };
  } catch {
    // Some browsers refuse construction outside a service worker. Nothing the
    // user can act on, and the job outcome is already on screen.
  }
}
