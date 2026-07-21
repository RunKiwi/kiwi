"use client";

import { useEffect, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { client } from "@/lib/api";

// A persistent banner shown on every dashboard page while the org is SUSPENDED —
// task submission is blocked server-side (the planner endpoint returns 403).
// Note: free orgs are created "inactive" and still run on the shared fleet, so
// "inactive" is NOT a blocking state; only "suspended" is surfaced here.
export function ActivationBanner() {
  const [state, setState] = useState<string | null>(null);

  useEffect(() => {
    client
      .validate()
      .then((r) => setState(r.activation_state))
      .catch(() => setState(null));
  }, []);

  if (state !== "suspended") return null;

  return (
    <div className="flex items-center gap-3 px-6 py-2.5 bg-red-500/10 border-b border-red-500/20 text-red-200 text-sm">
      <AlertTriangle className="w-4 h-4 shrink-0 text-red-400" />
      <span className="flex-1 min-w-0">
        Your organization is <span className="font-medium text-red-100">suspended</span> —
        running tasks is disabled. This can follow repeated abuse signals or
        exhausting your plan&apos;s limits.
      </span>
      <a
        href="mailto:support@runkiwi.com?subject=Suspended%20organization"
        className="font-semibold underline underline-offset-2 hover:text-white shrink-0"
      >
        Contact support
      </a>
    </div>
  );
}
