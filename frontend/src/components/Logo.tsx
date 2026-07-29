/**
 * The Kiwi mark: a pear-bodied bird whose beak is nearly as long as it is, on a
 * 128-unit grid.
 *
 * The beak is the whole idea. A kiwi is the one bird identifiable by its beak
 * alone, so the silhouette leans on it rather than on plumage or a head shape
 * that would disappear at small sizes.
 *
 * Two constraints shaped the geometry, both learned from the mark this replaces:
 *
 * 1. Legs are omitted. On a 128 grid an 8-unit leg is 1.1px once the mark is
 *    rendered at the ~18px it ships at in a browser tab, and the gap between two
 *    of them is narrower still — they merge into a smudge that costs legibility
 *    and buys nothing.
 * 2. The eye is a counter punched out of the body with fill-rule="evenodd",
 *    not a <mask> and not an overpainted dot in the background colour. One path
 *    then renders correctly in a single colour on any ground, light or dark,
 *    and needs no per-instance mask id.
 */
export const KIWI_MARK_BODY =
  "M6 88 C 21 75, 35 60, 48 48 C 63 30, 92 23, 108 40 C 124 56, 124 82, 106 94 C 92 103, 73 104, 61 97 C 52 92, 48 83, 47 73 C 33 79, 17 85, 6 88 Z";

/** The eye, as a subpath. Radius 7.5 keeps it ~2px — still a hole — at 18px. */
export const KIWI_MARK_EYE =
  "M 63 50 m -7.5 0 a 7.5 7.5 0 1 0 15 0 a 7.5 7.5 0 1 0 -15 0 Z";

/** Body and eye in one path. Requires fill-rule="evenodd" to punch the eye. */
export const KIWI_MARK_PATH = `${KIWI_MARK_BODY} ${KIWI_MARK_EYE}`;

/**
 * Coloured via `currentColor`, so set it with a Tailwind `text-*` class or an
 * inline style.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 128 128"
      className={className}
      fill="currentColor"
      fillRule="evenodd"
      aria-hidden="true"
    >
      <path d={KIWI_MARK_PATH} />
    </svg>
  );
}
