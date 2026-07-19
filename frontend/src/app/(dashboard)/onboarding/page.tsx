"use client";

import { useState } from "react";
import { CheckCircle2, ChevronRight } from "lucide-react";
import { useRouter } from "next/navigation";

export default function OnboardingPage() {
  const router = useRouter();
  const [step, setStep] = useState(1);
  const [isActivating, setIsActivating] = useState(false);

  const handleConnectRepo = async () => {
    // In a real flow, this would redirect to GitHub App installation
    // For now, we simulate success and move to next step
    setTimeout(() => setStep(2), 500);
  };

  const handleActivate = async () => {
    setIsActivating(true);
    // Real implementation would redirect to Stripe Checkout
    // We will just redirect to settings for now
    setTimeout(() => {
      router.push("/settings");
    }, 1000);
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
            <div className={`flex items-center justify-center w-8 h-8 rounded-full text-sm font-bold shrink-0 ${step > 1 ? 'bg-green-500 text-black' : 'bg-white text-black'}`}>
              {step > 1 ? <CheckCircle2 className="w-5 h-5" /> : '1'}
            </div>
            <div className="flex-1">
              <h2 className="text-xl font-medium text-white mb-2">Connect your Repository</h2>
              <p className="text-zinc-400 mb-6">Link your codebase so Kiwi agents can analyze, plan, and submit pull requests.</p>
              {step === 1 && (
                <button 
                  onClick={handleConnectRepo}
                  className="flex items-center gap-2 bg-white text-black hover:bg-zinc-200 px-5 py-2.5 rounded-xl font-medium transition-colors"
                >
                  <svg className="w-5 h-5" viewBox="0 0 24 24">
                    <path fill="currentColor" d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
                  </svg>
                  Connect GitHub
                </button>
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
              <h2 className="text-xl font-medium text-white mb-2">Activate to Run</h2>
              <p className="text-zinc-400 mb-6">
                You can plan and preview tasks for free. To actually <strong>execute</strong> tasks and have the Swarm write code, you need an active plan.
              </p>
              
              {step === 2 && (
                <div className="flex gap-4">
                  <button 
                    onClick={handleActivate}
                    disabled={isActivating}
                    className="flex items-center gap-2 bg-blue-500 text-white hover:bg-blue-600 px-6 py-2.5 rounded-xl font-medium transition-colors disabled:opacity-50"
                  >
                    {isActivating ? 'Activating...' : 'Activate Now'}
                    {!isActivating && <ChevronRight className="w-4 h-4" />}
                  </button>
                  <button 
                    onClick={() => router.push("/")}
                    className="flex items-center gap-2 bg-white/5 text-white hover:bg-white/10 px-6 py-2.5 rounded-xl font-medium transition-colors"
                  >
                    Skip & Preview only
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
