"use client";

import { useEffect, useState } from "react";
import { client, type Integration } from "@/lib/api";
import { Boxes, MessageSquare, KeyRound, GitBranch, Sparkles, Bot, CheckCircle2, Activity, LineChart, Globe } from "lucide-react";
import { CredentialField } from "@/components/CredentialField";
import { parseActionableError } from "@/lib/errors";
import { capture } from "@/lib/analytics";
import { GithubAppCard } from "@/components/GithubAppCard";

// UI catalog: which integrations we surface and how to connect them. `credName`
// is the credential the backend stores; `kind` classifies it.
const CATALOG: Record<string, {
  title: string; blurb: string; credName: string; kind: string;
  placeholder: string; icon: React.ComponentType<{ className?: string }>;
}> = {
  github: { title: "GitHub token (fallback)", blurb: "Only needed if you have not installed the GitHub App above.", credName: "GITHUB_TOKEN", kind: "github", placeholder: "github_pat_… (repo scope)", icon: Boxes },
  slack:  { title: "Slack",  blurb: "Notify a channel when jobs finish.", credName: "SLACK_TOKEN", kind: "slack", placeholder: "xoxb-… or a webhook URL", icon: MessageSquare },
  git:    { title: "Git push token", blurb: "Token the daemon uses to push branches.", credName: "GIT_TOKEN", kind: "git", placeholder: "github_pat_…", icon: GitBranch },
  anthropic: { title: "Anthropic", blurb: "API key for Claude models.", credName: "ANTHROPIC_API_KEY", kind: "llm", placeholder: "sk-ant-…", icon: Sparkles },
  gemini: { title: "Gemini", blurb: "API key for Google Gemini models.", credName: "GEMINI_API_KEY", kind: "llm", placeholder: "AIza…", icon: KeyRound },
  openai: { title: "OpenAI", blurb: "API key for GPT models.", credName: "OPENAI_API_KEY", kind: "llm", placeholder: "sk-…", icon: Bot },
  datadog: { title: "Datadog API key", blurb: "Lets post-merge verification pull metrics from Datadog.", credName: "DATADOG_API_KEY", kind: "telemetry", placeholder: "dd_api_key…", icon: Activity },
  "datadog-app-key": { title: "Datadog application key", blurb: "Paired with the Datadog API key above — Datadog requires both.", credName: "DATADOG_APP_KEY", kind: "telemetry", placeholder: "dd_app_key…", icon: KeyRound },
  prometheus: { title: "Prometheus bearer token", blurb: "Lets post-merge verification query your Prometheus instance.", credName: "PROMETHEUS_BEARER_TOKEN", kind: "telemetry", placeholder: "Bearer token", icon: LineChart },
  "prometheus-base-url": { title: "Prometheus base URL", blurb: "Paired with the bearer token above — the endpoint to query.", credName: "PROMETHEUS_BASE_URL", kind: "telemetry", placeholder: "https://prometheus.example.com", icon: Globe },
};
const ORDER = ["github", "slack", "anthropic", "gemini", "openai", "git", "datadog", "datadog-app-key", "prometheus", "prometheus-base-url"];

export default function IntegrationsPage() {
  const [status, setStatus] = useState<Record<string, boolean>>({});
  const [values, setValues] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState<string | null>(null);
  const [msg, setMsg] = useState<Record<string, string>>({});
  const [isErr, setIsErr] = useState<Record<string, boolean>>({});

  const load = () => client.listIntegrations()
    .then(r => setStatus(Object.fromEntries(r.integrations.map((i: Integration) => [i.key, i.connected]))))
    .catch(() => {});
  useEffect(() => { load(); }, []);

  const connect = async (key: string) => {
    const meta = CATALOG[key];
    const val = (values[key] || "").trim();
    if (!val) {
      setMsg(m => ({ ...m, [key]: "Paste a token first." }));
      setIsErr(e => ({ ...e, [key]: true }));
      return;
    }
    setBusy(key);
    setMsg(m => ({ ...m, [key]: "Verifying reachability…" }));
    setIsErr(e => ({ ...e, [key]: false }));

    try {
      await client.setCredential(meta.credName, meta.kind, val);
      // `key` is a catalog id ("github", "anthropic"), never the token itself.
      if (meta.kind === "llm") {
        capture("model_key_added", { provider: key, surface: "integrations" });
      } else if (meta.kind === "github") {
        capture("repo_connected", { surface: "integrations" });
      }
      setValues(v => ({ ...v, [key]: "" }));
      setMsg(m => ({ ...m, [key]: "Connected and verified ✓" }));
      setIsErr(e => ({ ...e, [key]: false }));
      await load();
    } catch (e) {
      const parsed = parseActionableError(e);
      setMsg(m => ({ ...m, [key]: parsed.message }));
      setIsErr(e => ({ ...e, [key]: true }));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="p-8 max-w-4xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Integrations</h1>
        <p className="text-zinc-400">Connect your tools. Tokens are encrypted at rest and sealed to the runtime — never shown again.</p>
      </div>

      <div className="flex flex-col gap-4">
        <GithubAppCard />

        {ORDER.map(key => {
          const meta = CATALOG[key];
          const Icon = meta.icon;
          const connected = status[key];
          return (
            <div key={key} className="glass-panel border border-white/10 rounded-2xl p-5">
              <div className="flex items-start justify-between gap-4 mb-4">
                <div className="flex items-start gap-3 min-w-0">
                  <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center shrink-0">
                    <Icon className="w-5 h-5 text-zinc-200" />
                  </div>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="font-medium">{meta.title}</h3>
                      {connected && (
                        <span className="flex items-center gap-1 text-[11px] text-green-400 font-medium">
                          <CheckCircle2 className="w-3.5 h-3.5" /> Connected
                        </span>
                      )}
                    </div>
                    <p className="text-sm text-zinc-400">{meta.blurb}</p>
                  </div>
                </div>
              </div>

              <CredentialField
                id={`cred-${key}`}
                value={values[key] || ""}
                onChange={(v) => setValues(prev => ({ ...prev, [key]: v }))}
                placeholder={meta.placeholder}
                connected={connected}
                busy={busy === key}
                onSubmit={() => connect(key)}
                submitLabel={connected ? "Update" : "Connect"}
                statusMessage={msg[key]}
                isError={isErr[key]}
              />
            </div>
          );
        })}
      </div>

      <p className="text-xs text-zinc-600 mt-6">GitHub connects through the App above. Other integrations use a token or webhook for now.</p>
    </div>
  );
}
