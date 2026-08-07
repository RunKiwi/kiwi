"use client";

import { ThinkingOrb } from "thinking-orbs";
import type { OrbState } from "@/lib/orbState";

/**
 * The app is fetching something, as distinct from an agent working on something.
 *
 * The default is `shaping` — a dotted outline cycling circle → triangle →
 * square — which no agent phase maps to. That separation is deliberate: every
 * other state in the app is derived from a real phase (`lib/orbState.ts`), so
 * an orb met on its own while a page boots should not be mistakable for one
 * reporting that a run is reading files or waiting on a critic. A loading
 * screen implying an agent is thinking would be a lie in exactly the register
 * this product sells.
 *
 * `state` exists because that reasoning is weaker for a targeted fetch than for
 * a cold page load. The drawer passes `connecting`, which is the same verb the
 * sign-in callback uses and is literally what a request for one job's details
 * is doing. It is safe there specifically because the drawer's header orb
 * derives from `currentJob`, and this renders only while `currentJob` is still
 * null — the two are mutually exclusive by construction, so a reader never sees
 * two `connecting` orbs at once and has to wonder which is which.
 *
 * Note where this is *not* used: the spend page renders a skeleton panel, which
 * is the better pattern when the shape of the incoming content is known — a
 * spinner there would be a regression, not an upgrade.
 */
export function LoadingState({
  label,
  className = "",
  state = "shaping",
}: {
  /** What is being waited on. Shown, and the only thing announced. */
  label: string;
  /** Height/spacing for the context — a Suspense fallback wants more than a drawer body. */
  className?: string;
  /** Override only with a reason; see the note above. */
  state?: OrbState;
}) {
  return (
    <div
      className={`flex flex-col items-center justify-center gap-4 py-16 ${className}`}
      role="status"
      aria-live="polite"
    >
      {/* Hidden from assistive tech: the orb ships its own role="img" and label,
          which would otherwise be announced alongside the text saying the same
          thing. The text is the accessible name here. */}
      <ThinkingOrb state={state} size={64} aria-hidden />
      <p className="text-sm text-zinc-500">{label}</p>
    </div>
  );
}
