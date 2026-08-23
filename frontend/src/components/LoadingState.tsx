"use client";

import { ThinkingOrb } from "@/components/ThinkingOrb";
import type { OrbState } from "@/lib/orbState";

export function LoadingState({
  label,
  className = "",
  state = "shaping",
  size = 64,
}: {
  /** What is being waited on. Shown, and the only thing announced. */
  label: string;
  /** Height/spacing for the context — a Suspense fallback wants more than a drawer body. */
  className?: string;
  /** Override only with a reason; see the note above. */
  state?: OrbState;
  /** Size of the orb in px. */
  size?: number;
}) {
  return (
    <div
      className={`flex flex-col items-center justify-center gap-4 py-16 ${className}`}
      role="status"
      aria-live="polite"
    >
      <ThinkingOrb state={state} size={size} theme="kiwi" glow={true} aria-hidden />
      <p className="text-xs font-mono text-stone-500 font-medium">{label}</p>
    </div>
  );
}
