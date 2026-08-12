/**
 * A short, honest title for a job.
 *
 * The task text is whatever the submitter pasted, and people paste issue
 * reports: several hundred words of Problem/Proposed fix, rendered verbatim
 * into the drawer's <h2>. One task pushed the run it was describing entirely
 * below the fold.
 *
 * Nothing here calls a model. A title generated at submit time would mean the
 * Control Plane making a provider call with the org's decrypted key — the
 * containment gap that removing Control-Plane planning just closed. Instead
 * this uses two things that already exist: the sentence the task leads with,
 * and, once round 0 lands, the objective the Architect already wrote.
 */

const MAX_TITLE = 100;

/** The subset of a progress event this needs. */
export type TitleSource = {
  phase: string;
  outcome: string;
  detail?: string;
};

/**
 * Reduce a task description to the sentence it leads with.
 *
 * The order matters: a markdown heading wins outright, then a line break, then
 * a `##` section marker, and only then sentence splitting — because the earlier
 * signals are explicit statements of "the title ends here" and the last one is
 * a guess.
 */
export function deriveTitle(task: string): string {
  let s = (task ?? "").trim();
  if (!s) return "";

  // An explicit heading is the submitter naming their own title.
  const heading = s.match(/^#{1,3}\s+(.+)$/m);
  if (heading && s.startsWith("#")) {
    s = heading[1].trim();
  } else {
    // First line, then the text before the first `##` section. Issue reports
    // pasted as a single line put the whole body after that marker.
    s = s.split("\n")[0].trim();
    s = s.split(/\s#{2,}\s/)[0].trim();
  }

  // Sentence split, but only on a full stop that ends a word and is followed by
  // a capitalised next word. Splitting on every "." truncates "Go to 1.25" and
  // "e.g. empty payloads" mid-thought, which is worse than a slightly long title.
  const sentence = s.match(/^(.+?[a-z0-9)\]`"'])\.\s+[A-Z]/);
  if (sentence) s = sentence[1];

  // A trailing full stop reads as a fragment once it is a title.
  s = s.replace(/\.$/, "").trim();

  if (s.length > MAX_TITLE) s = s.slice(0, MAX_TITLE).trimEnd() + "…";
  return s;
}

/**
 * The title to show for a job, and whether the original is worth expanding to.
 *
 * `fromArchitect` is exposed so the caller can say where the title came from.
 * A model-written summary standing in for what the user typed should be
 * labelled as one — silently replacing their words with a paraphrase is how a
 * user stops trusting that the page is showing them their own task.
 */
export function jobTitle(
  task: string,
  events: readonly TitleSource[] | undefined,
): { title: string; fromArchitect: boolean; truncated: boolean } {
  const original = (task ?? "").trim();

  // The Architect's opening spec. `sessionPhase` maps both "plan" and "review"
  // onto the "critic" phase, so the outcome is what separates them: only the
  // plan is "proposed". Matching on phase alone would retitle the job with a
  // review's rationale every round.
  const plan = (events ?? []).find(
    e => e.phase === "critic" && e.outcome === "proposed" && (e.detail ?? "").trim() !== "",
  );

  if (plan) {
    const objective = deriveTitle(plan.detail!);
    if (objective) {
      return { title: objective, fromArchitect: true, truncated: original !== objective };
    }
  }

  const derived = deriveTitle(original);
  if (!derived) return { title: "Job Details", fromArchitect: false, truncated: false };
  return { title: derived, fromArchitect: false, truncated: derived !== original };
}
