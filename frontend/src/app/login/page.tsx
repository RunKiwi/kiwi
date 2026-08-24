"use client";

import { Key, ShieldCheck, ArrowRight, ArrowLeft } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, useEffect } from "react";
import { client } from "@/lib/api";
import { auth } from "@/lib/auth";
import { capture, identify, rememberAuthMethod } from "@/lib/analytics";
import { Logo } from "@/components/Logo";

export default function LoginPage() {
  const router = useRouter();
  const [isLoading, setIsLoading] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [error, setError] = useState("");
  const [providers, setProviders] = useState<string[]>([]);
  const [loadingProviders, setLoadingProviders] = useState(true);
  const [showApiKey, setShowApiKey] = useState(false);

  useEffect(() => {
    client
      .getAuthProviders()
      .then((res) => {
        setProviders(res.providers || []);
        if (!res.providers || res.providers.length === 0) {
          setShowApiKey(true);
        }
      })
      .catch(() => {
        setShowApiKey(true);
      })
      .finally(() => {
        setLoadingProviders(false);
      });
  }, []);

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!apiKey.trim()) return;

    setIsLoading(true);
    setError("");
    capture("signup_started", { method: "api_key" });

    try {
      auth.setSession(apiKey, "", "");
      const res = await client.validate();
      auth.setSession(apiKey, res.org_id, res.org_name);
      auth.setUserId(res.user_id);
      capture("signup_completed", { method: "api_key" });
      identify(res.user_id, {
        org_id: res.org_id,
        plan: res.plan,
        activation_state: res.activation_state,
      });
      router.push("/");
    } catch (err) {
      auth.clearSession();
      capture("signup_failed", {
        method: "api_key",
        reason: err instanceof Error ? err.message : "unknown",
      });
      setError("Invalid API key or server unreachable.");
    } finally {
      setIsLoading(false);
    }
  };

  const getBaseUrl = () => {
    return process.env.NEXT_PUBLIC_KIWI_API_URL || "http://localhost:8080";
  };

  return (
    <div className="min-h-screen flex flex-col items-center justify-center p-4 sm:p-6 bg-[#F8F7F4] bg-dot-grid relative overflow-hidden select-none font-sans text-stone-900">
      {/* Ambient background glow */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[500px] h-[500px] bg-kiwi-400/[0.08] rounded-full blur-3xl pointer-events-none" />

      {/* Main Login Card */}
      <div className="w-full max-w-[400px] bg-white border border-sand-200/90 rounded-2xl shadow-popover p-6 sm:p-8 flex flex-col items-center text-center relative z-10 animate-in fade-in zoom-in-95 duration-200">
        {/* Workspace Version Chip */}
        <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-600 bg-sand-100 px-2 py-0.5 rounded-md border border-sand-200/80 mb-3.5">
          PLATFORM ACCESS
        </span>

        {/* 8-bit Kiwi Mascot */}
        <div className="w-14 h-14 rounded-2xl bg-sand-50 border border-sand-200/90 flex items-center justify-center shadow-2xs mb-3.5">
          <Logo variant="full-color" pose="vibing" animated={true} className="w-8 h-8" />
        </div>

        <h1 className="text-xl font-bold tracking-tight text-stone-900 mb-1">
          Log in to Kiwi
        </h1>
        <p className="text-stone-500 text-xs mb-6 max-w-xs leading-relaxed">
          Autonomous agent platform for engineering teams to build, verify, and ship code safely.
        </p>

        {loadingProviders ? (
          <div className="py-6 flex flex-col items-center gap-2">
            <div className="w-5 h-5 border-2 border-sand-200 border-t-kiwi-500 rounded-full animate-spin" />
            <span className="text-[11px] font-mono text-stone-400">Loading auth providers...</span>
          </div>
        ) : !showApiKey ? (
          <div className="w-full flex flex-col gap-3">
            {providers.includes("github") && (
              <a
                href={`${getBaseUrl()}/auth/github/start`}
                onClick={() => {
                  rememberAuthMethod("github");
                  capture("signup_started", { method: "github" });
                }}
                className="w-full flex items-center justify-center gap-2.5 bg-charcoal-900 text-white hover:bg-charcoal-800 transition-all py-2.5 px-4 rounded-xl font-semibold text-xs shadow-sm active:scale-[0.98]"
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24">
                  <path
                    fill="currentColor"
                    d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"
                  />
                </svg>
                <span>Continue with GitHub</span>
              </a>
            )}

            {providers.includes("google") && (
              <a
                href={`${getBaseUrl()}/auth/google/start`}
                onClick={() => {
                  rememberAuthMethod("google");
                  capture("signup_started", { method: "google" });
                }}
                className="w-full flex items-center justify-center gap-2.5 bg-white border border-sand-200 text-stone-800 hover:bg-sand-50 transition-all py-2.5 px-4 rounded-xl font-semibold text-xs shadow-2xs active:scale-[0.98]"
              >
                <svg className="w-4 h-4" viewBox="0 0 24 24">
                  <path
                    fill="currentColor"
                    d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"
                  />
                  <path
                    fill="currentColor"
                    d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"
                  />
                  <path
                    fill="currentColor"
                    d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"
                  />
                  <path
                    fill="currentColor"
                    d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"
                  />
                </svg>
                <span>Continue with Google</span>
              </a>
            )}

            {providers.length > 0 && (
              <div className="flex items-center gap-3 w-full my-1">
                <div className="h-px bg-sand-200/80 flex-1" />
                <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-stone-400">OR</span>
                <div className="h-px bg-sand-200/80 flex-1" />
              </div>
            )}

            <button
              onClick={() => setShowApiKey(true)}
              className="w-full py-2 px-3 rounded-xl border border-sand-200 hover:border-sand-300 hover:bg-sand-50/80 text-stone-700 font-semibold text-xs shadow-2xs transition-all flex items-center justify-center gap-2 cursor-pointer"
            >
              <Key className="w-3.5 h-3.5 text-stone-500" />
              <span>Sign in with API Key</span>
              <ArrowRight className="w-3 h-3 text-stone-400 ml-auto" />
            </button>
          </div>
        ) : (
          <form onSubmit={handleLogin} className="w-full flex flex-col gap-3">
            <div className="relative">
              <Key className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-stone-400" />
              <input
                type="password"
                placeholder="API Key (e.g. kw_...)"
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
                className="w-full bg-sand-50/80 border border-sand-200 rounded-xl py-2.5 pl-9 pr-3 text-stone-900 placeholder-stone-400 text-xs font-mono focus:outline-none focus:border-stone-900 focus:bg-white transition-all shadow-2xs"
              />
            </div>

            {error && (
              <div className="p-2 rounded-lg bg-rose-50 border border-rose-200 text-rose-800 text-[11px] font-mono text-left">
                {error}
              </div>
            )}

            <button
              type="submit"
              disabled={isLoading || !apiKey.trim()}
              className="w-full flex items-center justify-center gap-2 bg-charcoal-900 text-white hover:bg-charcoal-800 transition-all py-2.5 px-4 rounded-xl font-semibold text-xs shadow-sm disabled:opacity-40 cursor-pointer"
            >
              {isLoading ? (
                <div className="w-4 h-4 border-2 border-white/30 border-t-white rounded-full animate-spin" />
              ) : (
                "Continue to Workspace"
              )}
            </button>

            {providers.length > 0 && (
              <button
                type="button"
                onClick={() => setShowApiKey(false)}
                className="mt-2 text-xs font-semibold text-stone-500 hover:text-stone-900 transition-colors flex items-center justify-center gap-1.5 cursor-pointer"
              >
                <ArrowLeft className="w-3.5 h-3.5" />
                <span>Back to OAuth sign in</span>
              </button>
            )}
          </form>
        )}
      </div>

      {/* Security & Infrastructure Footer */}
      <div className="mt-6 flex items-center justify-center gap-2 text-[10px] font-mono text-stone-400">
        <ShieldCheck className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
        <span>Isolated gVisor MicroVMs • Cryptographic Audit Receipts</span>
      </div>
    </div>
  );
}
