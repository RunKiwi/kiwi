/**
 * Product analytics: an explicit, allow-listed event stream.
 *
 * Kiwi's whole claim is that customer code and credentials stay contained, so
 * the dashboard must not ship them to a third-party analytics vendor in the
 * name of measurement. Two rules follow, and both are enforced here rather
 * than left to whoever adds the next `capture` call:
 *
 *  - **Explicit events only.** Autocapture and session recording are off (see
 *    `instrumentation-client.ts`). Autocapture sends the text of whatever
 *    element was clicked, which on this dashboard is repository names, task
 *    descriptions and job ids — exactly the data we tell customers we contain.
 *  - **Typed properties.** Every event declares its payload in `EventProps`
 *    below, so a task description or a repo URL cannot be added to one by
 *    reflex. It does not typecheck.
 *
 * Analytics is **opt-in**. With `NEXT_PUBLIC_POSTHOG_KEY` unset — local dev,
 * `make local`, and every self-hosted deployment — no client is ever
 * installed, `posthog-js` is never even imported, and each call below costs
 * one null check.
 */

/**
 * Analytics configuration, in one place so the enabled/disabled decision has a
 * single source.
 *
 * Note what this does *not* buy: Turbopack compiles `NEXT_PUBLIC_*` to a
 * runtime `process.env` shim lookup rather than an inline literal, so the
 * `if (KEY)` guard in `instrumentation-client.ts` is not dead-code eliminated
 * and `posthog-js` still lands in the build output. What holds instead — and
 * what was verified against a real build — is that it is code-split into its
 * own chunk that no entry point or prerendered page references, so a browser
 * loading an unconfigured deployment never requests those 230KB and never
 * initializes anything. The value is still baked at build time; it must be
 * passed as a build arg, not a runtime env var.
 */
export const ANALYTICS_KEY = process.env.NEXT_PUBLIC_POSTHOG_KEY ?? "";
export const ANALYTICS_HOST = process.env.NEXT_PUBLIC_POSTHOG_HOST || "https://us.i.posthog.com";

/** How someone reached an authenticated session. */
export type AuthMethod = "github" | "google" | "api_key";

/**
 * Where in the product an event happened. The same credential can be saved
 * from two places, and "onboarding saves fail but Integrations works" is a
 * conclusion you can only reach if the events say which one.
 */
export type Surface = "onboarding" | "integrations" | "github_app";

/**
 * The activation funnel, in order. Anything not listed here cannot be sent.
 *
 * Deliberately absent: repository URLs, task text, branch names, credential
 * values, and anything else that describes *what* a customer is building. The
 * model ids are ours, not theirs, so they stay.
 */
export type EventProps = {
  signup_started: { method: AuthMethod };
  signup_completed: { method: AuthMethod };
  signup_failed: { method: AuthMethod; reason: string };
  repo_connected: { surface: Surface };
  model_key_added: { provider: string; surface: Surface };
  onboarding_step_skipped: { step: 1 | 2 | 3 };
  task_submitted: {
    mode: "file_loop" | "session";
    worker_model: string;
    planner_model?: string;
    max_workers: number;
    has_test_cmd: boolean;
    from_starter: boolean;
  };
  task_submit_failed: { reason: string };
  /** The activation moment: a run this org started produced a pull request. */
  pr_opened: { job_id: string; pr_count: number };
};

export type EventName = keyof EventProps;

/** Traits attached to the identified user. No email, no name — see the note in `identify`. */
export interface Identity {
  org_id: string;
  plan: string;
  activation_state: string;
}

/**
 * The slice of `posthog-js` this module uses. Declaring it locally keeps
 * `posthog-js` out of every importer's bundle and lets the tests drive a fake.
 */
export interface AnalyticsClient {
  capture(event: string, props?: Record<string, unknown>): void;
  identify(distinctId: string, props?: Record<string, unknown>): void;
  reset(): void;
}

let client: AnalyticsClient | null = null;

/**
 * Installed once by `instrumentation-client.ts` when a PostHog key is
 * configured. Until then — and forever, on an unconfigured build — every
 * function below is a no-op.
 */
export function setAnalyticsClient(next: AnalyticsClient | null): void {
  client = next;
}

/** Exposed for tests; production code has no reason to ask. */
export function isEnabled(): boolean {
  return client !== null;
}

/**
 * The last line of defence on payload size and content. Every string property
 * is truncated, because the one field we cannot fully constrain by type is a
 * `reason` carrying a backend error message, and those interpolate values we
 * do not control.
 */
const MAX_STRING_LEN = 200;

export function sanitize(props: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(props)) {
    if (v === undefined) continue;
    out[k] = typeof v === "string" && v.length > MAX_STRING_LEN
      ? v.slice(0, MAX_STRING_LEN) + "…"
      : v;
  }
  return out;
}

/** Send one funnel event. Silently does nothing when analytics is not configured. */
export function capture<K extends EventName>(name: K, props: EventProps[K]): void {
  client?.capture(name, sanitize(props));
}

/**
 * Tie subsequent events to a user.
 *
 * `userId` is the control plane's opaque user id, and the traits are plan and
 * activation state. No email or display name is sent: they add nothing to a
 * funnel and would make this a place customer identities are stored.
 */
export function identify(userId: string, traits: Identity): void {
  if (!userId) return;
  client?.identify(userId, sanitize({ ...traits }));
}

/** Drop the identity association. Called on sign-out so a shared browser doesn't merge two people. */
export function resetIdentity(): void {
  client?.reset();
}

/**
 * A URL safe to report as a page view.
 *
 * Query strings and fragments are dropped whole rather than filtered: the auth
 * callback carries the session token in its fragment (`/auth/callback#token=…`),
 * and an allow-list that has to be kept in step with every future route is the
 * kind of thing that is correct until someone adds a `?repo=` parameter.
 */
export function pagePath(url: string): string {
  const noFragment = url.split("#")[0];
  return noFragment.split("?")[0] || "/";
}

/** Minimal `localStorage` shape, so `markOnce` is testable without a DOM. */
export interface KeyValueStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

const AUTH_METHOD_KEY = "kiwi_auth_method";

/**
 * Carry the chosen sign-in method across the OAuth round trip.
 *
 * `signup_started` fires on the login page and `signup_completed` fires on
 * `/auth/callback`, with a full redirect to the provider in between. Without
 * this the two halves of the funnel cannot be joined by method, and "GitHub
 * converts, Google doesn't" is unanswerable.
 */
export function rememberAuthMethod(method: AuthMethod, store: KeyValueStore | null = defaultSessionStore()): void {
  try {
    store?.setItem(AUTH_METHOD_KEY, method);
  } catch {
    // Non-fatal: attribution degrades to the default below.
  }
}

/** The method stashed by {@link rememberAuthMethod}, or `github` when the trail is cold. */
export function recallAuthMethod(store: KeyValueStore | null = defaultSessionStore()): AuthMethod {
  try {
    const v = store?.getItem(AUTH_METHOD_KEY);
    if (v === "github" || v === "google" || v === "api_key") return v;
  } catch {
    // Fall through to the default.
  }
  return "github";
}

function defaultSessionStore(): KeyValueStore | null {
  try {
    return typeof window !== "undefined" ? window.sessionStorage : null;
  } catch {
    return null;
  }
}

const SEEN_KEY = "kiwi_analytics_seen";
const SEEN_LIMIT = 200;

function defaultStore(): KeyValueStore | null {
  try {
    return typeof window !== "undefined" ? window.localStorage : null;
  } catch {
    // Storage can throw outright when cookies are blocked.
    return null;
  }
}

/**
 * True the first time it sees `key`, false forever after.
 *
 * `pr_opened` is derived from a polled list rather than pushed by the backend,
 * so without this the same pull request is reported every few seconds for as
 * long as the tab is open, and again on every reload. The record is kept in
 * `localStorage` and trimmed to the most recent {@link SEEN_LIMIT} keys — an
 * unbounded list would grow for the life of the browser profile.
 *
 * Falling back to `true` when storage is unavailable is deliberate: an event
 * counted twice is a worse outcome than one missed, but a *silent* funnel is
 * worse than both, and this only happens where `localStorage` throws.
 */
export function markOnce(key: string, store: KeyValueStore | null = defaultStore()): boolean {
  if (!store) return true;
  try {
    const raw = store.getItem(SEEN_KEY);
    const seen: string[] = raw ? JSON.parse(raw) : [];
    if (seen.includes(key)) return false;
    seen.push(key);
    store.setItem(SEEN_KEY, JSON.stringify(seen.slice(-SEEN_LIMIT)));
    return true;
  } catch {
    // Corrupt or unwritable storage: report the event rather than lose it.
    return true;
  }
}
