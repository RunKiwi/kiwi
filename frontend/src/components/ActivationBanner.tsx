"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { AlertTriangle } from "lucide-react";
import { usePathname } from "next/navigation";
import { client } from "@/lib/api";

// A persistent banner shown on every dashboard page while the org can't run
// tasks. Running is gated on activation_state server-side (the submit endpoint
// returns 402 when inactive), so this makes the gate visible everywhere instead
// of only surfacing as a cryptic error after a failed dispatch.
export function ActivationBanner() {
  const [state, setState] = useState<string | null>(null);
  const pathname = usePathname();

  useEffect(() => {
    client
      .validate()
      .then((r) => setState(r.activation_state))
      .catch(() => setState(null));
  }, []);

  if (!state || state === "active") return null;

  // Don't stack the banner on the page that already explains activation.
  const onSettings = pathname?.startsWith("/settings");

  return (
    <div className="flex items-center gap-3 px-6 py-2.5 bg-amber-500/10 border-b border-amber-500/20 text-amber-200 text-sm">
      <AlertTriangle className="w-4 h-4 shrink-0 text-amber-400" />
      <span className="flex-1 min-w-0">
        Your organization isn&apos;t activated — you can plan and preview tasks, but
        <span className="font-medium text-amber-100"> running them is disabled</span>.
      </span>
      {!onSettings && (
        <Link
          href="/settings#activation"
          className="font-semibold underline underline-offset-2 hover:text-white shrink-0"
        >
          Activate to run
        </Link>
      )}
    </div>
  );
}
