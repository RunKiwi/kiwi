"use client";

import { GitBranch } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function LoginPage() {
  const router = useRouter();
  const [isLoading, setIsLoading] = useState(false);

  const handleGithubLogin = () => {
    setIsLoading(true);
    // Simulate OAuth redirect and token exchange
    setTimeout(() => {
      // In a real app, this would redirect to GitHub, then a callback route
      // would exchange the code for a JWT via kiwi-api and store it.
      router.push("/");
    }, 1500);
  };

  return (
    <div className="flex min-h-screen items-center justify-center p-4 relative overflow-hidden">
      {/* Background decoration */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[800px] bg-white/[0.02] rounded-full blur-3xl mix-blend-screen pointer-events-none" />
      
      <div className="glass-panel w-full max-w-md p-8 relative z-10 flex flex-col items-center text-center">
        <div className="w-16 h-16 rounded-2xl bg-white shadow-[0_0_30px_rgba(255,255,255,0.4)] flex items-center justify-center mb-8">
          <span className="text-black font-bold text-4xl leading-none">K</span>
        </div>
        
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Welcome to Kiwi</h1>
        <p className="text-zinc-400 mb-8">Sign in to control your BYOC Swarm</p>

        <button 
          onClick={handleGithubLogin}
          disabled={isLoading}
          className="w-full flex items-center justify-center gap-3 bg-white text-black hover:bg-zinc-200 transition-colors py-3 px-4 rounded-xl font-medium disabled:opacity-70"
        >
          {isLoading ? (
            <div className="w-5 h-5 border-2 border-black/20 border-t-black rounded-full animate-spin" />
          ) : (
            <>
              <GitBranch className="w-5 h-5" />
              Continue with GitHub
            </>
          )}
        </button>

        <p className="mt-8 text-xs text-zinc-500">
          By signing in, you agree to our Terms of Service and Privacy Policy.
        </p>
      </div>
    </div>
  );
}
