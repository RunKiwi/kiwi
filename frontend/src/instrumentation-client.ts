/**
 * Client-side analytics bootstrap. Runs after the document loads and before
 * React hydration (Next's `instrumentation-client` convention).
 *
 * Analytics is opt-in and off by default: with `NEXT_PUBLIC_POSTHOG_KEY`
 * unset the dynamic import below never runs, so `posthog-js` is never
 * downloaded, never initialized, and makes no network call. It is code-split
 * into a chunk nothing else references (see the note in `lib/analytics.ts` on
 * why it cannot be eliminated outright). That matters because this repo is
 * public and self-hosted deployments must not phone home to our PostHog.
 *
 * See `lib/analytics.ts` for the event contract and why it is allow-listed.
 */

import { ANALYTICS_HOST, ANALYTICS_KEY, pagePath, setAnalyticsClient } from "@/lib/analytics";

const KEY = ANALYTICS_KEY;
const HOST = ANALYTICS_HOST;

if (KEY) {
  // Dynamic so an unconfigured build carries none of this code, and awaited
  // nowhere so instrumentation stays under Next's 16ms init budget.
  import("posthog-js")
    .then(({ default: posthog }) => {
      posthog.init(KEY, {
        api_host: HOST,
        // PostHog's dated defaults bundle. Every option we care about is set
        // explicitly below and an explicit option wins, so this only affects
        // settings we have not named.
        defaults: "2026-05-30",
        // Don't create a person profile for every anonymous visitor. Only
        // identified users — the ones the funnel is about — get one, which
        // keeps the billable event volume tied to real signups.
        person_profiles: "identified_only",
        // Every one of these defaults is wrong for this product.
        //
        // `autocapture` sends the text and attributes of whatever element was
        // clicked. On this dashboard that is repository names, task
        // descriptions and job ids — the data the entire product promises to
        // contain. Off, permanently: we send the explicit events in
        // lib/analytics.ts and nothing else.
        autocapture: false,
        // Session replay records the DOM, including the task composer and the
        // credential fields.
        disable_session_recording: true,
        // We send page views ourselves, through `pagePath`, so the URL is
        // stripped before it leaves the browser.
        capture_pageview: false,
        capture_pageleave: false,
        // PostHog attaches the raw location to *every* event, not only page
        // views, so stripping it at the call site is not enough. This is the
        // one chokepoint all properties pass through.
        sanitize_properties: (props) => {
          for (const k of ["$current_url", "$referrer", "$pathname", "$initial_current_url"]) {
            const v = props[k];
            if (typeof v === "string" && v) props[k] = pagePath(v);
          }
          return props;
        },
      });
      setAnalyticsClient(posthog);
      posthog.capture("$pageview", { $current_url: pagePath(window.location.pathname) });
    })
    .catch(() => {
      // A blocked or failed analytics load must never affect the dashboard.
    });
}

/**
 * App-router navigations are client-side, so they produce no document load and
 * no page view of their own.
 */
export function onRouterTransitionStart(url: string): void {
  if (!KEY) return;
  import("posthog-js")
    .then(({ default: posthog }) => {
      posthog.capture("$pageview", { $current_url: pagePath(url) });
    })
    .catch(() => {});
}
