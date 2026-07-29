export interface ActionableError {
  message: string;
  actionLabel?: string;
  actionHref?: string;
  code?: string;
}

/** What the caller knows about the org, where it changes the right advice. */
export interface ErrorContext {
  /** "free" | "pro" | … — a Free org never goes through paid activation. */
  plan?: string;
}

/**
 * Status codes are matched on word boundaries, never as bare substrings. Job ids
 * are `job_` + hex and routinely appear inside error text, so a naive
 * `includes("402")` reports "your organization is inactive" for any unrelated
 * failure on a job whose id happens to contain those digits.
 */
const has = (s: string, ...needles: (string | RegExp)[]) =>
  needles.some(n => (typeof n === "string" ? s.includes(n) : n.test(s)));

/**
 * Turns a raw server error into something a user can act on. Anything we do not
 * recognise falls through with the server's own words intact — an error we
 * cannot explain is still more useful than one we have paraphrased away.
 */
export function parseActionableError(rawError: unknown, ctx: ErrorContext = {}): ActionableError {
  const errString =
    typeof rawError === "string"
      ? rawError
      : rawError instanceof Error
      ? rawError.message
      : String(rawError || "An unexpected error occurred");

  const lower = errString.toLowerCase();

  // Suspended is checked before activation: both are activation_state values,
  // but a suspended org is not one activation away from running.
  if (has(lower, "suspended")) {
    return {
      message: "This organization is suspended, so tasks cannot run.",
      actionLabel: "See details",
      actionHref: "/settings#activation",
      code: "SUSPENDED",
    };
  }

  // Paid-plan activation. A Free org runs on the shared fleet without ever
  // activating, so telling one to "activate" sends it somewhere with nothing
  // to do — fall through to the server's own message instead.
  if (has(lower, /\b402\b/, "payment required", "activate to run", "org is inactive", "organization is inactive")) {
    if (ctx.plan === "free") {
      return { message: errString };
    }
    return {
      message: "This organization is inactive. You can plan tasks, but activating is required to run them.",
      actionLabel: "Activate in Settings",
      actionHref: "/settings#activation",
      code: "INACTIVE_ORG",
    };
  }

  // Out of agent-minutes: a hard stop needing either a new month or a bigger
  // plan. Distinct from the concurrency cap below, which clears on its own.
  if (has(lower, "compute_cap", "compute cap", "out of agent-minutes", "agent-minutes", "agent minutes")) {
    return {
      message: "This organization is out of agent-minutes for the month, so new tasks will not start.",
      actionLabel: "Review plan and usage",
      actionHref: "/settings#plan",
      code: "COMPUTE_CAP",
    };
  }

  // Concurrency is a queue, not a wall. Sending someone to the pricing page for
  // a limit that clears by itself in a minute is the wrong instruction.
  if (has(lower, "concurrency_cap", "concurrency cap", "concurrent-task limit", "concurrent job")) {
    return {
      message: "This task is queued behind others — it starts as soon as a slot frees up.",
      code: "CONCURRENCY_CAP",
    };
  }

  // No runner / runner offline / provisioning failure all mean the same thing to
  // the user: nothing is going to pick this up right now.
  if (
    has(
      lower,
      "no_runner",
      "no runner",
      "runner_offline",
      "runner offline",
      "no daemon",
      "provision_failed",
      "provisioning failed",
      "runner failed to start",
    )
  ) {
    return {
      message: "No runner is available to pick up this work.",
      actionLabel: "Check fleets",
      actionHref: "/fleet",
      code: "NO_RUNNER",
    };
  }

  // Repository access — a token scope problem far more often than a typo.
  if (
    has(
      lower,
      "repository not found",
      "repo not found",
      "not accessible",
      "could not clone",
      "failed to clone",
    )
  ) {
    return {
      message: "That repository could not be reached. Check the URL, and that the connected GitHub token can read it.",
      actionLabel: "Manage integrations",
      actionHref: "/integrations",
      code: "REPO_UNREACHABLE",
    };
  }

  // Provider credentials. Deliberately narrow: matching a bare provider name
  // would capture any error that merely mentions the provider, including a
  // transient 500 that has nothing to do with the key.
  if (
    has(
      lower,
      /\b401\b/,
      "unauthorized",
      "invalid api key",
      "invalid key",
      "api key",
      "rejected this credential",
      "no provider key",
      "provider key",
      "missing credential",
    )
  ) {
    return {
      message: errString,
      actionLabel: "Manage integrations",
      actionHref: "/integrations",
      code: "CREDENTIAL_ERROR",
    };
  }

  // Unrecognised: hand back exactly what the server said.
  return { message: errString };
}
