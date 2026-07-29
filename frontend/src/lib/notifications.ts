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

  const isSuccess = status === "SUCCEEDED";
  const title = isSuccess ? `Job Completed Successfully 🎉` : `Job Failed ❌`;
  const body = taskGoal ? `Task: "${taskGoal.slice(0, 60)}${taskGoal.length > 60 ? "…" : ""}"` : `Job ${jobId.slice(0, 8)} is ${status.toLowerCase()}.`;

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
  } catch (err) {
    console.error("Failed to send notification:", err);
  }
}
