"use client";

import { useState, useEffect } from "react";
import { CheckCircle2, ChevronRight, Loader2, AlertCircle } from "lucide-react";
import { useRouter } from "next/navigation";
import { client, type Integration } from "@/lib/api";

export default function OnboardingPage() {
  const router = useRouter();
  const [step, setStep] = useState(1);
  const [ghToken, setGhToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  // Poll for a GitHub connection only while on step 1 (e.g. the user connected
  // it on another tab); once we advance there's nothing left to detect, so we
  // stop hitting the API.
  useEffect(() => {
    if (step !== 1) return;
    const checkGH = async () => {
      try {
        const res = await client.listIntegrations();
        const gh = res.integrations.find((i: Integration) => i.key === "github");
        if (gh?.connected) setStep(2);
      } catch { /* best-effort */ }
    };
    checkGH();
    const interval = setInterval(checkGH, 3000);
    return () => clearInterval(interval);
  }, [step]);

  const handleConnectRepo = async () => {
    const val = ghToken.trim();
    if (!val) { setErr("Paste a GitHub PAT first."); return; }
    setBusy(true); setErr("");
    try {
      await client.setCredential("GITHUB_TOKEN", "github", val);
      const res = await client.listIntegrations();
      const gh = res.integrations.find((i: Integration) => i.key === "github");
      if (gh?.connected) {
        setStep(2);
      } else {
        setErr("Connected, but waiting for verification...");
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Failed to connect");
    } finally {
      setBusy(false);
    }
  };

  const handleUpgrade = async () => {
    setBusy(true); setErr("");
    try {
      const { url } = await client.createCheckout();
      window.location.href = url;
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Upgrade isn't available yet — try again shortly.");
      setBusy(false);
    }
  };

  return (
    <div className="p-8 max-w-3xl mx-auto min-h-[80vh] flex flex-col justify-center">
      <div className="text-center mb-12">
        <h1 className="text-4xl font-light tracking-tight text-white mb-4">Welcome to Kiwi</h1>
        <p className="text-zinc-400 text-lg">Let&apos;s get your organization set up and ready to run.</p>
      </div>

      <div className="space-y-6">
        {/* Step 1: Connect Repo */}
        <div className={`glass-panel p-6 transition-all duration-300 ${step === 1 ? 'border-white/20 shadow-[0_0_30px_rgba(255,255,255,0.1)] scale-[1.02]' : 'opacity-60 grayscale'}`}>
          <div className="flex items-start gap-4">
            <div className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${step > 1 ? 'bg-[#93C645] text-[#0A1017]' : 'bg-white text-black'}`}>
              {step > 1 ? <CheckCircle2 className="w-5 h-5" /> : '1'}
            </div>
            <div className="flex-1 min-w-0">
              <h2 className="text-xl font-medium text-white mb-2">Connect your Repository</h2>
              <p className="text-zinc-400 mb-6">Link your codebase so Kiwi agents can analyze, plan, and submit pull requests. Provide a GitHub Personal Access Token (repo scope).</p>
              {step === 1 && (
                <div className="flex flex-col gap-3 max-w-md">
                  <div className="flex gap-2">
                    <input 
                      type="password" 
                      value={ghToken} 
                      onChange={e => setGhToken(e.target.value)}
                      placeholder="github_pat_..." 
                      className="flex-1 field text-sm"
                    />
                    <button 
                      onClick={handleConnectRepo}
                      disabled={busy}
                      className="flex items-center justify-center gap-2 btn-primary px-5 py-2.5 transition-colors disabled:opacity-50"
                    >
                      {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                      Connect
                    </button>
                  </div>
                  {err && (
                    <div className="flex items-center gap-2 text-red-400 text-sm">
                      <AlertCircle className="w-4 h-4 shrink-0" /> {err}
                    </div>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>

        {/* Step 2: Activate */}
        <div className={`glass-panel p-6 transition-all duration-300 ${step === 2 ? 'border-blue-500/30 bg-blue-950/20 shadow-[0_0_30px_rgba(59,130,246,0.1)] scale-[1.02]' : 'opacity-60 grayscale'}`}>
          <div className="flex items-start gap-4">
            <div className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${step === 2 ? 'bg-blue-500 text-white' : 'bg-white/20 text-white'}`}>
              2
            </div>
            <div className="flex-1">
              <h2 className="text-xl font-medium text-white mb-2">You&apos;re on the Free plan</h2>
              <p className="text-zinc-400 mb-6">
                Free runs on Kiwi&apos;s <strong>shared fleet</strong> — no card, no setup. Submit a task and the swarm plans it, runs it in an isolated sandbox, and opens a pull request. Add your model key under <strong>Integrations</strong> (you bring your own), and you&apos;re set. Free includes a monthly agent-minute allowance and runs one task at a time. Need a dedicated fleet, higher limits, or your own cloud? <strong>Upgrade to Pro</strong> anytime.
              </p>

              {step === 2 && (
                <div className="flex gap-4">
                  <button
                    onClick={() => router.push("/")}
                    className="flex items-center gap-2 btn-primary px-6 py-2.5 transition-colors"
                  >
                    Start building
                    <ChevronRight className="w-4 h-4" />
                  </button>
                  <button
                    onClick={handleUpgrade}
                    disabled={busy}
                    className="flex items-center gap-2 bg-white/5 hover:bg-white/10 text-zinc-200 px-6 py-2.5 rounded-xl font-medium disabled:opacity-50 transition-colors"
                  >
                    {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                    Upgrade to Pro
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
