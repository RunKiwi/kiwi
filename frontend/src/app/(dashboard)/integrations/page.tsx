"use client";

import { useEffect, useState } from "react";
import { client, type Integration } from "@/lib/api";
import { Boxes, MessageSquare, KeyRound, GitBranch, Sparkles, CheckCircle2, Loader2, AlertCircle } from "lucide-react";

// UI catalog: which integrations we surface and how to connect them. `credName`
// is the credential the backend stores; `kind` classifies it.
const CATALOG: Record<string, {
  title: string; blurb: string; credName: string; kind: string;
  placeholder: string; icon: React.ComponentType<{ className?: string }>;
}> = {
  github: { title: "GitHub", blurb: "List repos in the task form and push branches / open PRs.", credName: "GITHUB_TOKEN", kind: "github", placeholder: "github_pat_… (repo scope)", icon: Boxes },
  slack:  { title: "Slack",  blurb: "Notify a channel when jobs finish.", credName: "SLACK_TOKEN", kind: "slack", placeholder: "xoxb-… or a webhook URL", icon: MessageSquare },
  git:    { title: "Git push token", blurb: "Token the daemon uses to push branches.", credName: "GIT_TOKEN", kind: "git", placeholder: "github_pat_…", icon: GitBranch },
  anthropic: { title: "Anthropic", blurb: "API key for Claude models.", credName: "ANTHROPIC_API_KEY", kind: "llm", placeholder: "sk-ant-…", icon: Sparkles },
  gemini: { title: "Gemini", blurb: "API key for Google Gemini models.", credName: "GEMINI_API_KEY", kind: "llm", placeholder: "AIza…", icon: KeyRound },
};
const ORDER = ["github", "slack", "anthropic", "gemini", "git"];

export default function IntegrationsPage() {
  const [status, setStatus] = useState<Record<string, boolean>>({});
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<Record<string, string>>({});

  const load = () => client.listIntegrations()
    .then(r => setStatus(Object.fromEntries(r.integrations.map((i: Integration) => [i.key, i.connected]))))
    .catch(() => {});
  useEffect(() => { load(); }, []);

  const connect = async (key: string) => {
    const meta = CATALOG[key];
    const val = (values[key] || "").trim();
    if (!val) { setMsg(m => ({ ...m, [key]: "Paste a token first." })); return; }
    setBusy(key); setMsg(m => ({ ...m, [key]: "" }));
    try {
      await client.setCredential(meta.credName, meta.kind, val);
      setValues(v => ({ ...v, [key]: "" }));
      setMsg(m => ({ ...m, [key]: "Connected." }));
      await load();
    } catch (e) {
      setMsg(m => ({ ...m, [key]: e instanceof Error ? e.message : "Failed" }));
    } finally { setBusy(null); }
  };

  return (
    <div className="p-8 max-w-4xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Integrations</h1>
        <p className="text-zinc-400">Connect your tools. Tokens are encrypted at rest and sealed to the runtime — never shown again.</p>
      </div>

      <div className="flex flex-col gap-4">
        {ORDER.map(key => {
          const meta = CATALOG[key];
          const Icon = meta.icon;
          const connected = status[key];
          return (
            <div key={key} className="glass-panel border border-white/10 rounded-2xl p-5">
              <div className="flex items-start justify-between gap-4">
                <div className="flex items-start gap-3 min-w-0">
                  <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center shrink-0">
                    <Icon className="w-5 h-5 text-zinc-200" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="font-medium">{meta.title}</h3>
                      {connected && <span className="flex items-center gap-1 text-[11px] text-green-400"><CheckCircle2 className="w-3.5 h-3.5" /> Connected</span>}
                    </div>
                    <p className="text-sm text-zinc-400">{meta.blurb}</p>
                  </div>
                </div>
              </div>
              <div className="flex flex-col sm:flex-row gap-2 mt-4">
                <input type="password" value={values[key] || ""} onChange={e => setValues(v => ({ ...v, [key]: e.target.value }))}
                  placeholder={connected ? "•••••••• (paste to replace)" : meta.placeholder}
                  className="flex-1 field text-sm" />
                <button onClick={() => connect(key)} disabled={busy === key}
                  className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50">
                  {busy === key ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                  {connected ? "Update" : "Connect"}
                </button>
              </div>
              {msg[key] && (
                <div className={`flex items-center gap-2 text-sm mt-2 ${msg[key] === "Connected." ? "text-green-400" : "text-red-400"}`}>
                  {msg[key] === "Connected." ? <CheckCircle2 className="w-4 h-4" /> : <AlertCircle className="w-4 h-4" />}{msg[key]}
                </div>
              )}
            </div>
          );
        })}
      </div>

      <p className="text-xs text-zinc-600 mt-6">Full OAuth (one-click GitHub/Slack) is planned; for now connect with a token / webhook.</p>
    </div>
  );
}
