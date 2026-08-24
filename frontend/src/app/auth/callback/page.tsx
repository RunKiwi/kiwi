"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { client } from "@/lib/api";
import { auth } from "@/lib/auth";
import { capture, identify, recallAuthMethod } from "@/lib/analytics";
import { ThinkingOrb } from "thinking-orbs";
import { Logo } from "@/components/Logo";

export default function AuthCallbackPage() {
  const router = useRouter();
  const [error, setError] = useState("");

  useEffect(() => {
    // The backend redirects here with the API token in the URL fragment
    // (/auth/callback#token=kw_...). A fragment is never sent to a server, so
    // the token stays out of access logs and Referer headers.
    const hash =
      typeof window !== "undefined" ? window.location.hash.replace(/^#/, "") : "";
    const token = new URLSearchParams(hash).get("token");

    // The work runs inside an async closure so any setState happens in a
    // callback, not synchronously in the effect body (react-hooks/set-state-in-effect).
    (async () => {
      // Which provider was chosen is stashed before the redirect, so the two
      // halves of the signup funnel can be joined by method.
      const method = recallAuthMethod();
      if (!token) {
        capture("signup_failed", { method, reason: "no_token_returned" });
        setError("No sign-in token was returned. Please try again.");
        return;
      }
      try {
        // Store the token first so client.validate() sends it, then enrich the
        // session with the org details it returns.
        auth.setSession(token, "", "");
        const res = await client.validate();
        auth.setSession(token, res.org_id, res.org_name);
        auth.setUserId(res.user_id);
        capture("signup_completed", { method });
        identify(res.user_id, {
          org_id: res.org_id,
          plan: res.plan,
          activation_state: res.activation_state,
        });
        router.replace("/");
      } catch (err) {
        auth.clearSession();
        capture("signup_failed", {
          method,
          reason: err instanceof Error ? err.message : "validate_failed",
        });
        setError("Sign-in failed. Please try again.");
      }
    })();
  }, [router]);

  return (
    <div className="flex min-h-screen items-center justify-center p-4 sm:p-6 bg-[#F8F7F4] bg-dot-grid font-sans text-stone-900 select-none relative overflow-hidden">
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[500px] h-[500px] bg-kiwi-400/[0.08] rounded-full blur-3xl pointer-events-none" />

      <div className="bg-white border border-sand-200/90 rounded-2xl shadow-popover w-full max-w-[380px] p-6 sm:p-8 flex flex-col items-center text-center gap-5 relative z-10 animate-in fade-in zoom-in-95 duration-200">
        <div className="w-14 h-14 rounded-2xl bg-sand-50 border border-sand-200/90 flex items-center justify-center shadow-2xs">
          <Logo variant="full-color" pose="vibing" animated={true} className="w-8 h-8" />
        </div>
        {error ? (
          <div className="space-y-3 w-full">
            <div className="p-2.5 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 text-xs font-mono">
              {error}
            </div>
            <button
              onClick={() => router.replace("/login")}
              className="w-full py-2 px-3 rounded-xl bg-charcoal-900 text-white hover:bg-charcoal-800 font-semibold text-xs transition-all shadow-sm cursor-pointer"
            >
              Back to sign in
            </button>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3">
            {/* "connecting" is the literal act here — the session is being
                established against the control plane. */}
            <ThinkingOrb state="connecting" size={64} aria-label="Completing sign-in" />
            <div>
              <p className="text-xs font-bold text-stone-900">Establishing Session</p>
              <p className="text-stone-500 text-[11px] font-mono mt-0.5">Connecting to control plane...</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
