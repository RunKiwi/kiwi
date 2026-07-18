"use client";

import { Key } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { client } from "@/lib/api";
import { auth } from "@/lib/auth";
import { Logo } from "@/components/Logo";

export default function LoginPage() {
  const router = useRouter();
  const [isLoading, setIsLoading] = useState(false);
  const [apiKey, setApiKey] = useState("");
  const [error, setError] = useState("");

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!apiKey.trim()) return;
    
    setIsLoading(true);
    setError("");

    try {
      // Temporarily store it so the client uses it for validate()
      auth.setSession(apiKey, "", "");
      
      const res = await client.validate();
      auth.setSession(apiKey, res.org_id, res.org_name);
      router.push("/");
    } catch {
      auth.clearSession();
      setError("Invalid API key or server unreachable.");
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center p-4 relative overflow-hidden">
      {/* Background decoration */}
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[800px] bg-white/[0.02] rounded-full blur-3xl mix-blend-screen pointer-events-none" />
      
      <div className="glass-panel w-full max-w-md p-8 relative z-10 flex flex-col items-center text-center">
        <div className="w-16 h-16 rounded-2xl bg-white shadow-[0_0_30px_rgba(255,255,255,0.4)] flex items-center justify-center mb-8">
          <Logo className="w-10 h-10 text-black" />
        </div>
        
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Welcome to Kiwi</h1>
        <p className="text-zinc-400 mb-8">Sign in with your Org API Key to control your BYOC Swarm</p>

        <form onSubmit={handleLogin} className="w-full flex flex-col gap-4">
          <div className="relative">
            <Key className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-zinc-500" />
            <input
              type="password"
              placeholder="API Key (e.g. kw_...)"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              className="w-full bg-black/50 border border-white/10 rounded-xl py-3 pl-10 pr-4 text-white placeholder-zinc-500 focus:outline-none focus:border-white/30 transition-colors"
            />
          </div>
          
          {error && <p className="text-red-400 text-sm text-left">{error}</p>}

          <button 
            type="submit"
            disabled={isLoading || !apiKey.trim()}
            className="w-full flex items-center justify-center gap-3 bg-white text-black hover:bg-zinc-200 transition-colors py-3 px-4 rounded-xl font-medium disabled:opacity-70"
          >
            {isLoading ? (
              <div className="w-5 h-5 border-2 border-black/20 border-t-black rounded-full animate-spin" />
            ) : (
              "Continue"
            )}
          </button>
        </form>

        <p className="mt-8 text-xs text-zinc-500">
          GitHub OAuth is planned but not yet available. Please use your API key.
        </p>
      </div>
    </div>
  );
}
