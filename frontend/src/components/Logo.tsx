/**
 * Kiwi 8-bit chunky chibi mark. Colored via `currentColor`, so set the color
 * with a Tailwind `text-*` class or inline style.
 * Source: https://github.com/RunKiwi/kiwi-brand (svg/logos/kiwi-logo-monochrome.svg)
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      className={className}
      fill="currentColor"
      shapeRendering="crispEdges"
      style={{ imageRendering: "pixelated" }}
      aria-hidden="true"
    >
      <rect x="5" y="2" width="5" height="1" opacity={0.8} />
      <rect x="3" y="3" width="8" height="1" opacity={0.9} />
      <rect x="2" y="4" width="5" height="1" opacity={1.0} />
      <rect x="9" y="4" width="2" height="1" opacity={1.0} />
      <rect x="2" y="5" width="9" height="1" opacity={1.0} />
      <rect x="1" y="6" width="10" height="1" opacity={0.95} />
      <rect x="1" y="7" width="10" height="1" opacity={0.95} />
      <rect x="2" y="8" width="9" height="1" opacity={0.85} />
      <rect x="2" y="9" width="9" height="1" opacity={0.8} />
      <rect x="3" y="10" width="7" height="1" opacity={0.7} />
      <rect x="4" y="11" width="5" height="1" opacity={0.6} />
      <rect x="11" y="5" width="3" height="2" opacity={0.95} />
      <rect x="14" y="6" width="1" height="1" opacity={0.8} />
      <rect x="4" y="12" width="2" height="2" opacity={0.9} />
      <rect x="3" y="13" width="3" height="1" opacity={0.9} />
      <rect x="8" y="12" width="2" height="2" opacity={0.9} />
      <rect x="7" y="13" width="3" height="1" opacity={0.9} />
      <rect x="7" y="4" width="1" height="1" fill="#FFFFFF" opacity={0.95} />
    </svg>
  );
}
