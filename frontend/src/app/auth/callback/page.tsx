"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { client } from "@/lib/api";
import { auth } from "@/lib/auth";
import { capture, identify, recallAuthMethod } from "@/lib/analytics";
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
    <div className="flex min-h-screen items-center justify-center p-4">
      <div className="glass-panel w-full max-w-md p-8 flex flex-col items-center text-center gap-6">
        <div className="w-14 h-14 rounded-2xl bg-white flex items-center justify-center">
          <Logo className="w-9 h-9 text-black" />
        </div>
        {error ? (
          <>
            <p className="text-red-400 text-sm">{error}</p>
            <button
              onClick={() => router.replace("/login")}
              className="text-sm text-zinc-400 hover:text-white transition-colors"
            >
              Back to sign in
            </button>
          </>
        ) : (
          <>
            <div className="w-6 h-6 border-2 border-white/20 border-t-white rounded-full animate-spin" />
            <p className="text-zinc-400 text-sm">Completing sign-in…</p>
          </>
        )}
      </div>
    </div>
  );
}
