"use client";

import { Key } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState, useEffect } from "react";
import { client } from "@/lib/api";
import { auth } from "@/lib/auth";
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
    client.getAuthProviders()
      .then(res => {
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

    try {
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

  const getBaseUrl = () => {
    return process.env.NEXT_PUBLIC_KIWI_API_URL || "http://localhost:8080";
  };

  return (
    <div className="flex min-h-screen items-center justify-center p-4 relative overflow-hidden">
      <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[800px] bg-white/[0.02] rounded-full blur-3xl mix-blend-screen pointer-events-none" />
      
      <div className="glass-panel w-full max-w-md p-8 relative z-10 flex flex-col items-center text-center">
        <div className="w-16 h-16 rounded-2xl bg-white shadow-[0_0_30px_rgba(255,255,255,0.4)] flex items-center justify-center mb-8">
          <Logo className="w-10 h-10 text-black" />
        </div>
        
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Welcome to Kiwi</h1>
        <p className="text-zinc-400 mb-8">Sign in to control your agent swarm</p>

        {loadingProviders ? (
          <div className="w-5 h-5 border-2 border-white/20 border-t-white rounded-full animate-spin mb-8" />
        ) : !showApiKey ? (
          <div className="w-full flex flex-col gap-4">
            {providers.includes("github") && (
              <a 
                href={`${getBaseUrl()}/auth/github/start`}
                className="w-full flex items-center justify-center gap-3 bg-white text-black hover:bg-zinc-200 transition-colors py-3 px-4 rounded-xl font-medium"
              >
                <svg className="w-5 h-5" viewBox="0 0 24 24">
                  <path fill="currentColor" d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0024 12c0-6.63-5.37-12-12-12z"/>
                </svg>
                Continue with GitHub
              </a>
            )}
            {providers.includes("google") && (
              <a 
                href={`${getBaseUrl()}/auth/google/start`}
                className="w-full flex items-center justify-center gap-3 bg-white/10 text-white hover:bg-white/20 transition-colors py-3 px-4 rounded-xl font-medium"
              >
                <svg className="w-5 h-5" viewBox="0 0 24 24">
                  <path fill="currentColor" d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z" />
                  <path fill="currentColor" d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z" />
                  <path fill="currentColor" d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z" />
                  <path fill="currentColor" d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z" />
                </svg>
                Continue with Google
              </a>
            )}

            {providers.length > 0 && (
              <div className="mt-4 flex items-center justify-center gap-2">
                <div className="h-px bg-white/10 w-full" />
                <span className="text-xs text-zinc-500 uppercase">or</span>
                <div className="h-px bg-white/10 w-full" />
              </div>
            )}

            <button
              onClick={() => setShowApiKey(true)}
              className="text-sm text-zinc-400 hover:text-white transition-colors"
            >
              Sign in with API Key
            </button>
          </div>
        ) : (
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

            <button
              type="button"
              onClick={() => setShowApiKey(false)}
              className="mt-4 text-sm text-zinc-400 hover:text-white transition-colors"
            >
              Back to OAuth
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
