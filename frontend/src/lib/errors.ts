export interface ActionableError {
  message: string;
  actionLabel?: string;
  actionHref?: string;
  code?: string;
}

/**
 * Maps raw server error codes, API error tracebacks, or HTTP status messages into
 * user-friendly error objects with actionable next steps and direct links.
 */
export function parseActionableError(rawError: unknown): ActionableError {
  const errString =
    typeof rawError === "string"
      ? rawError
      : rawError instanceof Error
      ? rawError.message
      : String(rawError || "An unexpected error occurred");

  const lower = errString.toLowerCase();

  // 1. Payment required / 402 / inactive org
  if (
    lower.includes("402") ||
    lower.includes("inactive") ||
    lower.includes("activate to run") ||
    lower.includes("payment required") ||
    lower.includes("org is inactive")
  ) {
    return {
      message: "Your organization is inactive. You can preview tasks, but you must activate to run tasks.",
      actionLabel: "Activate in Settings",
      actionHref: "/settings#activation",
      code: "INACTIVE_ORG",
    };
  }

  // 2. Provider credential errors (Anthropic / Codex / OpenAI / GitHub)
  if (
    lower.includes("anthropic") ||
    lower.includes("openai") ||
    lower.includes("codex") ||
    lower.includes("api key") ||
    lower.includes("invalid key") ||
    lower.includes("unauthorized") ||
    lower.includes("401") ||
    lower.includes("integration") ||
    lower.includes("provider key")
  ) {
    return {
      message: errString,
      actionLabel: "Manage Integrations",
      actionHref: "/integrations",
      code: "CREDENTIAL_ERROR",
    };
  }

  // 3. Compute cap / concurrency cap
  if (
    lower.includes("compute cap") ||
    lower.includes("compute_cap") ||
    lower.includes("out of agent-minutes") ||
    lower.includes("agent minutes") ||
    lower.includes("concurrency_cap") ||
    lower.includes("concurrency cap") ||
    lower.includes("concurrent-task limit")
  ) {
    return {
      message: errString,
      actionLabel: "Upgrade Plan",
      actionHref: "/settings#plan",
      code: "CAP_EXCEEDED",
    };
  }

  // 4. Default fallback
  return {
    message: errString,
  };
}
