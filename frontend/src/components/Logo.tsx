import { useId } from "react";

/**
 * Kiwi bird mark. Colored via `currentColor`, so set the color with a Tailwind
 * `text-*` class or inline style. The eye is a mask hole, so the mark reads in a
 * single color on any ground. v0 silhouette — refine later.
 */
export function Logo({ className }: { className?: string }) {
  // Unique per instance so multiple logos on a page don't share a mask id.
  const maskId = `kiwi-eye-${useId().replace(/:/g, "")}`;
  return (
    <svg viewBox="0 0 128 128" className={className} fill="currentColor" aria-hidden="true">
      <mask id={maskId}>
        <rect width="128" height="128" fill="#fff" />
        <circle cx="54" cy="52" r="4.2" fill="#000" />
      </mask>
      <g mask={`url(#${maskId})`}>
        <path d="M46,40 C58,28 82,26 96,42 C112,52 110,66 104,74 C98,90 80,100 60,98 C46,96 36,86 34,74 C31,64 34,50 46,40 Z" />
        <path d="M36,60 C25,68 16,80 8,94 C19,85 30,79 40,72 Z" />
        <path d="M60,96 L60,112" stroke="currentColor" strokeWidth="6" strokeLinecap="round" fill="none" />
        <path d="M76,98 L76,112" stroke="currentColor" strokeWidth="6" strokeLinecap="round" fill="none" />
      </g>
    </svg>
  );
}
