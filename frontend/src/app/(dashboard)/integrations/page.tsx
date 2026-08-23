"use client";

import React, { useEffect, useState, useMemo, useCallback } from "react";
import { client, type Integration, type GithubInstallation, type SlackInstallation } from "@/lib/api";
import {
  CheckCircle2,
  Search,
  SlidersHorizontal,
  ShieldCheck,
  ArrowRight,
  RefreshCw,
  ExternalLink,
  Layers,
  Hash,
} from "lucide-react";
import { SiGithub, SiGit, SiAnthropic, SiGooglegemini, SiDatadog, SiPrometheus } from "react-icons/si";
import { RiOpenaiFill } from "react-icons/ri";
import { FaSlack } from "react-icons/fa6";
import { IntegrationDrawer, type CatalogIntegration } from "@/components/IntegrationDrawer";
import Link from "next/link";

const CATALOG: CatalogIntegration[] = [
  {
    id: "github",
    name: "GitHub",
    category: "scm",
    categoryLabel: "Source Control",
    description: "Connect repositories for automated pull requests, review workflows, and branch tracking.",
    icon: SiGithub,
    iconBg: "bg-sand-100",
    iconColor: "text-stone-900",
    brandAccent: "#18181B",
    docUrl: "https://github.com/settings/installations",
    docLabel: "GitHub Installations",
    isGithubHybrid: true,
    fields: [
      {
        key: "github",
        label: "Personal Access Token (Fallback)",
        credName: "GITHUB_TOKEN",
        kind: "github",
        placeholder: "github_pat_… (repo scope)",
        helpText: "Only needed if you have not installed the Kiwi GitHub App.",
      },
    ],
  },
  {
    id: "slack",
    name: "Slack",
    category: "notifications",
    categoryLabel: "Team Chat & Triggers",
    description: "Trigger Kiwi agent tasks by @mentioning the bot in a channel, stream live logs, and receive verdict notifications.",
    icon: FaSlack,
    iconBg: "bg-[#ECB22E]/10",
    iconColor: "text-[#ECB22E]",
    brandAccent: "#ECB22E",
    docUrl: "https://api.slack.com/messaging/webhooks",
    docLabel: "Slack Webhooks Guide",
    isSlackHybrid: true,
    fields: [
      {
        key: "slack",
        label: "Notification Webhook URL",
        credName: "SLACK_WEBHOOK_URL",
        kind: "webhook",
        placeholder: "https://hooks.slack.com/services/…",
        helpText: "Optional — posts monitor verdicts to a channel independent of the app install above.",
      },
    ],
  },
  {
    id: "anthropic",
    name: "Anthropic",
    category: "llm",
    categoryLabel: "AI & Models",
    description: "Powers Claude 3.7 Sonnet, Claude 3.5 Sonnet, and Opus models for high-reasoning planning and code generation.",
    icon: SiAnthropic,
    iconBg: "bg-[#D97757]/10",
    iconColor: "text-[#D97757]",
    brandAccent: "#D97757",
    docUrl: "https://console.anthropic.com/settings/keys",
    docLabel: "Anthropic Console",
    fields: [
      {
        key: "anthropic",
        label: "Anthropic API Key",
        credName: "ANTHROPIC_API_KEY",
        kind: "llm",
        placeholder: "sk-ant-api03-…",
      },
    ],
  },
  {
    id: "gemini",
    name: "Google Gemini",
    category: "llm",
    categoryLabel: "AI & Models",
    description: "Powers Google Gemini 2.5 Pro and Gemini 2.0 Flash models for high-throughput code execution and analysis.",
    icon: SiGooglegemini,
    iconBg: "bg-[#4C8DF6]/10",
    iconColor: "text-[#4C8DF6]",
    brandAccent: "#4C8DF6",
    docUrl: "https://aistudio.google.com/app/apikey",
    docLabel: "Google AI Studio",
    fields: [
      {
        key: "gemini",
        label: "Gemini API Key",
        credName: "GEMINI_API_KEY",
        kind: "llm",
        placeholder: "AIzaSy…",
      },
    ],
  },
  {
    id: "openai",
    name: "OpenAI",
    category: "llm",
    categoryLabel: "AI & Models",
    description: "Powers GPT-4o, GPT-4.1, and o1/o3 reasoning models for complex refactoring and task implementation.",
    icon: RiOpenaiFill,
    iconBg: "bg-[#10A37F]/10",
    iconColor: "text-[#10A37F]",
    brandAccent: "#10A37F",
    docUrl: "https://platform.openai.com/api-keys",
    docLabel: "OpenAI Platform",
    fields: [
      {
        key: "openai",
        label: "OpenAI API Key",
        credName: "OPENAI_API_KEY",
        kind: "llm",
        placeholder: "sk-proj-…",
      },
    ],
  },
  {
    id: "git",
    name: "Git Push Token",
    category: "scm",
    categoryLabel: "Source Control",
    description: "Dedicated token used by daemon runners to authenticate and push branches to your git repositories.",
    icon: SiGit,
    iconBg: "bg-[#F05032]/10",
    iconColor: "text-[#F05032]",
    brandAccent: "#F05032",
    docUrl: "https://github.com/settings/tokens",
    docLabel: "GitHub PAT Settings",
    fields: [
      {
        key: "git",
        label: "Git Push Token",
        credName: "GIT_TOKEN",
        kind: "git",
        placeholder: "github_pat_… (workflow/repo scope)",
      },
    ],
  },
  {
    id: "datadog",
    name: "Datadog",
    category: "telemetry",
    categoryLabel: "Telemetry & Metrics",
    description: "Allows post-merge canary verification to pull latency & error metrics directly from your Datadog dashboards.",
    icon: SiDatadog,
    iconBg: "bg-[#632CA6]/10",
    iconColor: "text-[#a464f7]",
    brandAccent: "#632CA6",
    docUrl: "https://app.datadoghq.com/organization-settings/api-keys",
    docLabel: "Datadog API Keys",
    fields: [
      {
        key: "datadog",
        label: "API Key",
        credName: "DATADOG_API_KEY",
        kind: "telemetry",
        placeholder: "dd_api_key…",
      },
      {
        key: "datadog-app-key",
        label: "Application Key",
        credName: "DATADOG_APP_KEY",
        kind: "telemetry",
        placeholder: "dd_app_key…",
      },
    ],
  },
  {
    id: "prometheus",
    name: "Prometheus",
    category: "telemetry",
    categoryLabel: "Telemetry & Metrics",
    description: "Enables post-merge canary monitors to query PromQL endpoints for runtime regression signals.",
    icon: SiPrometheus,
    iconBg: "bg-[#E6522C]/10",
    iconColor: "text-[#E6522C]",
    brandAccent: "#E6522C",
    docUrl: "https://prometheus.io/docs/prometheus/latest/querying/api/",
    docLabel: "Prometheus API Docs",
    fields: [
      {
        key: "prometheus-base-url",
        label: "Base URL",
        credName: "PROMETHEUS_BASE_URL",
        kind: "telemetry",
        placeholder: "https://prometheus.example.com",
        type: "text",
      },
      {
        key: "prometheus",
        label: "Bearer Token",
        credName: "PROMETHEUS_BEARER_TOKEN",
        kind: "telemetry",
        placeholder: "Bearer token…",
      },
    ],
  },
];

type CategoryFilter = "all" | "scm" | "llm" | "notifications" | "telemetry";
type StatusFilter = "all" | "connected" | "unconnected";

function deriveIntegrationState(
  item: CatalogIntegration,
  status: Record<string, boolean>,
  githubInstalls: GithubInstallation[],
  slackInstalls: SlackInstallation[]
) {
  if (item.isGithubHybrid) {
    if (githubInstalls.length > 0) {
      return {
        status: "connected" as const,
        label: `${githubInstalls.length} Org${githubInstalls.length > 1 ? "s" : ""} (App)`,
        summary: `Connected: ${githubInstalls.map((g) => g.account_login).join(", ")}`,
      };
    }
    if (status["github"]) {
      return {
        status: "connected" as const,
        label: "PAT Token Active",
        summary: "Connected via fallback PAT",
      };
    }
    return {
      status: "unconnected" as const,
      label: "Not connected",
      summary: "Install GitHub App or paste PAT",
    };
  }

  if (item.isSlackHybrid) {
    if (slackInstalls.length > 0) {
      return {
        status: "connected" as const,
        label: `${slackInstalls.length} Workspace${slackInstalls.length > 1 ? "s" : ""}`,
        summary: slackInstalls.map((s) => s.team_name || s.team_id).join(", "),
      };
    }
    if (status["slack"]) {
      return {
        status: "connected" as const,
        label: "Webhook Active",
        summary: "Verdict notifications enabled",
      };
    }
    return {
      status: "unconnected" as const,
      label: "Not connected",
      summary: "Connect Slack App to trigger tasks",
    };
  }

  if (item.fields.length === 1) {
    const connected = !!status[item.fields[0].key];
    return {
      status: connected ? ("connected" as const) : ("unconnected" as const),
      label: connected ? "Connected" : "Not configured",
      summary: connected ? "Encrypted credential verified" : "No credentials added",
    };
  }

  // Multi-field services (Datadog, Prometheus)
  const connectedCount = item.fields.filter((f) => status[f.key]).length;
  if (connectedCount === item.fields.length) {
    return {
      status: "connected" as const,
      label: "Fully Configured",
      summary: `All ${item.fields.length} parameters configured`,
    };
  }
  if (connectedCount > 0) {
    const missingField = item.fields.find((f) => !status[f.key]);
    return {
      status: "incomplete" as const,
      label: `Missing ${missingField?.label || "Key"}`,
      summary: `${connectedCount}/${item.fields.length} credentials provided`,
    };
  }
  return {
    status: "unconnected" as const,
    label: "Not configured",
    summary: "Requires API key & endpoint setup",
  };
}

export default function IntegrationsPage() {
  const [status, setStatus] = useState<Record<string, boolean>>({});
  const [githubInstalls, setGithubInstalls] = useState<GithubInstallation[]>([]);
  const [slackInstalls, setSlackInstalls] = useState<SlackInstallation[]>([]);
  const [activeIntegration, setActiveIntegration] = useState<CatalogIntegration | null>(null);
  const [selectedCategory, setSelectedCategory] = useState<CategoryFilter>("all");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [searchQuery, setSearchQuery] = useState("");
  const [isRefreshing, setIsRefreshing] = useState(false);

  const load = useCallback(async () => {
    setIsRefreshing(true);
    try {
      const [integrationsRes, githubRes, slackRes] = await Promise.all([
        client.listIntegrations().catch(() => ({ integrations: [] })),
        client.listGithubInstallations().catch(() => ({ installations: [] })),
        client.listSlackInstallations().catch(() => ({ installations: [] })),
      ]);
      setStatus(
        Object.fromEntries(
          integrationsRes.integrations.map((i: Integration) => [i.key, i.connected])
        )
      );
      setGithubInstalls(githubRes.installations || []);
      setSlackInstalls(slackRes.installations || []);
    } finally {
      setIsRefreshing(false);
    }
  }, []);

  useEffect(() => {
    let active = true;
    Promise.all([
      client.listIntegrations().catch(() => ({ integrations: [] })),
      client.listGithubInstallations().catch(() => ({ installations: [] })),
      client.listSlackInstallations().catch(() => ({ installations: [] })),
    ]).then(([integrationsRes, githubRes, slackRes]) => {
      if (!active) return;
      setStatus(
        Object.fromEntries(
          integrationsRes.integrations.map((i: Integration) => [i.key, i.connected])
        )
      );
      setGithubInstalls(githubRes.installations || []);
      setSlackInstalls(slackRes.installations || []);
    });
    return () => {
      active = false;
    };
  }, []);

  // Connectivity summary statistics
  const stats = useMemo(() => {
    let connected = 0;
    let incomplete = 0;
    let unconnected = 0;

    CATALOG.forEach((item) => {
      const state = deriveIntegrationState(item, status, githubInstalls, slackInstalls);
      if (state.status === "connected") connected++;
      else if (state.status === "incomplete") incomplete++;
      else unconnected++;
    });

    return { total: CATALOG.length, connected, incomplete, unconnected };
  }, [status, githubInstalls, slackInstalls]);

  // Filter and search catalog
  const filteredCatalog = useMemo(() => {
    return CATALOG.filter((item) => {
      // Category filter
      if (selectedCategory !== "all" && item.category !== selectedCategory) {
        return false;
      }

      // Status filter
      const state = deriveIntegrationState(item, status, githubInstalls, slackInstalls);
      if (statusFilter === "connected" && state.status !== "connected") {
        return false;
      }
      if (statusFilter === "unconnected" && state.status === "connected") {
        return false;
      }

      // Search query filter
      if (searchQuery.trim()) {
        const query = searchQuery.toLowerCase();
        const matchesName = item.name.toLowerCase().includes(query);
        const matchesDesc = item.description.toLowerCase().includes(query);
        const matchesCategory = item.categoryLabel.toLowerCase().includes(query);
        const matchesFields = item.fields.some(
          (f) =>
            f.label.toLowerCase().includes(query) || f.credName.toLowerCase().includes(query)
        );
        return matchesName || matchesDesc || matchesCategory || matchesFields;
      }

      return true;
    });
  }, [selectedCategory, statusFilter, searchQuery, status, githubInstalls, slackInstalls]);

  const categories: { id: CategoryFilter; label: string; count: number }[] = [
    { id: "all", label: "All Integrations", count: CATALOG.length },
    {
      id: "scm",
      label: "Source Control",
      count: CATALOG.filter((c) => c.category === "scm").length,
    },
    {
      id: "notifications",
      label: "Team Chat & Triggers",
      count: CATALOG.filter((c) => c.category === "notifications").length,
    },
    {
      id: "llm",
      label: "AI & Models",
      count: CATALOG.filter((c) => c.category === "llm").length,
    },
    {
      id: "telemetry",
      label: "Telemetry & Observability",
      count: CATALOG.filter((c) => c.category === "telemetry").length,
    },
  ];

  const githubIntegration = CATALOG.find((c) => c.id === "github")!;
  const slackIntegration = CATALOG.find((c) => c.id === "slack")!;
  const githubState = deriveIntegrationState(githubIntegration, status, githubInstalls, slackInstalls);
  const slackState = deriveIntegrationState(slackIntegration, status, githubInstalls, slackInstalls);

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER ================= */}
      <div className="flex flex-wrap items-start justify-between gap-4 pb-2 border-b border-sand-200">
        <div>
          <h1 className="text-2xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
            <span>Integrations</span>
            <span className="text-xs font-mono font-bold bg-emerald-50 text-emerald-800 border border-emerald-200 px-2 py-0.5 rounded-md flex items-center gap-1">
              <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
              {stats.connected} of {stats.total} Active
            </span>
          </h1>
          <p className="text-xs text-stone-500 mt-1 max-w-2xl leading-relaxed">
            Connect source control (GitHub), collaboration hubs (Slack), custom LLM provider keys, and telemetry metrics. All credentials are KMS-sealed at rest.
          </p>
        </div>

        <button
          onClick={load}
          disabled={isRefreshing}
          className="px-3.5 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
          title="Refresh Integration Statuses"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isRefreshing ? "animate-spin text-emerald-600" : "text-stone-500"}`} />
          <span>Refresh</span>
        </button>
      </div>

      {/* ================= TOP 4 KPI TILES ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3.5">
        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Active Integrations</span>
            <Layers className="w-4 h-4 text-stone-400" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">
              {stats.connected} <span className="text-sm font-normal text-stone-400">/ {stats.total}</span>
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {stats.unconnected} available to connect
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>GitHub VCS Status</span>
            <SiGithub className="w-4 h-4 text-stone-900" />
          </div>
          <div className="mt-2">
            <div className="text-base font-bold font-mono text-stone-900 truncate">
              {githubInstalls.length > 0 ? `${githubInstalls.length} Org Connected` : status["github"] ? "PAT Active" : "Not Connected"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5 truncate">
              {githubInstalls[0]?.account_login || "Repo sync & PR dispatch"}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Slack Collaboration</span>
            <FaSlack className="w-4 h-4 text-[#ECB22E]" />
          </div>
          <div className="mt-2">
            <div className="text-base font-bold font-mono text-stone-900 truncate">
              {slackInstalls.length > 0 ? `${slackInstalls.length} Workspace` : status["slack"] ? "Webhook Active" : "Not Connected"}
            </div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              <Link href="/integrations/slack" className="text-kiwi-700 font-semibold hover:underline">
                Channel Bindings &rarr;
              </Link>
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Secret Storage</span>
            <ShieldCheck className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="mt-2">
            <div className="text-base font-bold font-mono text-stone-900">KMS Sealed</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              AES-256 GCM Envelope Encryption
            </div>
          </div>
        </div>
      </div>

      {/* ================= FEATURED HUBS: GITHUB & SLACK ================= */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* GITHUB HUB CARD */}
        <div className="p-5 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-4 hover:border-sand-300 transition-all">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-xl bg-stone-900 text-white flex items-center justify-center shadow-2xs">
                  <SiGithub className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="text-sm font-bold text-stone-900">GitHub Hub</h3>
                  <p className="text-[11px] text-stone-500 font-mono">Source Control & Automated PR Workflow</p>
                </div>
              </div>

              <span
                className={`px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold border ${
                  githubState.status === "connected"
                    ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                    : "bg-sand-100 text-stone-600 border-sand-200"
                }`}
              >
                {githubState.label}
              </span>
            </div>

            <p className="text-xs text-stone-600 leading-relaxed">
              Connect your repositories for automated pull requests, review watchdogs, background linting, and branch generation.
            </p>

            {githubInstalls.length > 0 && (
              <div className="p-2.5 rounded-xl bg-sand-50/80 border border-sand-200 text-xs flex items-center gap-2">
                <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0" />
                <span className="font-mono text-[11px] text-stone-700 truncate">
                  Installed on: <span className="font-bold text-stone-900">{githubInstalls.map((g) => g.account_login).join(", ")}</span>
                </span>
              </div>
            )}
          </div>

          <div className="pt-3 border-t border-sand-150 flex items-center justify-between">
            <a
              href="https://github.com/settings/installations"
              target="_blank"
              rel="noopener noreferrer"
              className="text-[11px] font-semibold text-stone-500 hover:text-stone-900 flex items-center gap-1 transition-colors"
            >
              <span>GitHub App Settings</span>
              <ExternalLink className="w-3 h-3 text-stone-400" />
            </a>

            <button
              onClick={() => setActiveIntegration(githubIntegration)}
              className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
            >
              <span>Configure GitHub</span>
              <ArrowRight className="w-3.5 h-3.5 text-kiwi-400" />
            </button>
          </div>
        </div>

        {/* SLACK HUB CARD */}
        <div className="p-5 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-4 hover:border-sand-300 transition-all">
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="w-10 h-10 rounded-xl bg-[#ECB22E]/15 text-[#ECB22E] flex items-center justify-center border border-[#ECB22E]/30 shadow-2xs">
                  <FaSlack className="w-5 h-5" />
                </div>
                <div>
                  <h3 className="text-sm font-bold text-stone-900">Slack Collaboration Hub</h3>
                  <p className="text-[11px] text-stone-500 font-mono">Team Chat, @kiwi Mentions & Verdicts</p>
                </div>
              </div>

              <span
                className={`px-2.5 py-0.5 rounded-full text-[10px] font-mono font-bold border ${
                  slackState.status === "connected"
                    ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                    : "bg-sand-100 text-stone-600 border-sand-200"
                }`}
              >
                {slackState.label}
              </span>
            </div>

            <p className="text-xs text-stone-600 leading-relaxed">
              Trigger Kiwi tasks by @mentioning the bot in any Slack channel. Stream live execution status, task results, and canary alerts.
            </p>

            <div className="p-2.5 rounded-xl bg-sand-50/80 border border-sand-200 text-xs flex items-center justify-between">
              <div className="flex items-center gap-2 min-w-0">
                <Hash className="w-4 h-4 text-stone-500 shrink-0" />
                <span className="font-mono text-[11px] text-stone-700 truncate">
                  Map channels to code repositories
                </span>
              </div>
              <Link
                href="/integrations/slack"
                className="text-[11px] font-bold text-kiwi-700 hover:underline shrink-0"
              >
                Manage Channel Bindings &rarr;
              </Link>
            </div>
          </div>

          <div className="pt-3 border-t border-sand-150 flex items-center justify-between">
            <Link
              href="/integrations/slack"
              className="text-[11px] font-semibold text-stone-600 hover:text-stone-900 flex items-center gap-1 transition-colors"
            >
              <Hash className="w-3 h-3 text-stone-400" />
              <span>Channel Bindings Page</span>
            </Link>

            <button
              onClick={() => setActiveIntegration(slackIntegration)}
              className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all"
            >
              <span>Configure Slack</span>
              <ArrowRight className="w-3.5 h-3.5 text-kiwi-400" />
            </button>
          </div>
        </div>
      </div>

      {/* ================= ALL INTEGRATIONS DIRECTORY ================= */}
      <div className="space-y-4">
        {/* Search & Filter Toolbar */}
        <div className="flex flex-wrap items-center justify-between gap-3 bg-white p-3 rounded-2xl border border-sand-200 shadow-2xs text-xs">
          <div className="flex items-center gap-2.5 flex-1 min-w-[220px]">
            <div className="relative flex-1">
              <Search className="w-3.5 h-3.5 text-stone-400 absolute left-3 top-2.5 pointer-events-none" />
              <input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search integrations by service, name, or credential key..."
                className="w-full bg-sand-50/70 border border-sand-200 rounded-xl pl-8 pr-3 py-1.5 text-xs placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all font-mono"
              />
            </div>
          </div>

          <div className="flex items-center gap-2 flex-wrap">
            {/* Category Filter */}
            <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200 overflow-x-auto no-scrollbar">
              {categories.map((cat) => {
                const active = selectedCategory === cat.id;
                return (
                  <button
                    key={cat.id}
                    onClick={() => setSelectedCategory(cat.id)}
                    className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all whitespace-nowrap ${
                      active
                        ? "bg-white text-stone-900 shadow-2xs"
                        : "text-stone-600 hover:text-stone-900"
                    }`}
                  >
                    <span>{cat.label}</span>
                    <span className={`ml-1.5 text-[10px] font-mono px-1 rounded-full ${active ? "bg-sand-200 text-stone-800" : "text-stone-400"}`}>
                      {cat.count}
                    </span>
                  </button>
                );
              })}
            </div>

            {/* Status Filter */}
            <div className="flex items-center gap-1 bg-sand-100 p-0.5 rounded-xl border border-sand-200">
              <button
                onClick={() => setStatusFilter("all")}
                className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                  statusFilter === "all" ? "bg-white text-stone-900 shadow-2xs" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                All
              </button>
              <button
                onClick={() => setStatusFilter("connected")}
                className={`px-2.5 py-1 rounded-lg text-xs font-semibold transition-all ${
                  statusFilter === "connected" ? "bg-white text-stone-900 shadow-2xs text-emerald-800" : "text-stone-600 hover:text-stone-900"
                }`}
              >
                Connected
              </button>
            </div>
          </div>
        </div>

        {/* Catalog Grid */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3.5">
          {filteredCatalog.map((item) => {
            const Icon = item.icon;
            const state = deriveIntegrationState(item, status, githubInstalls, slackInstalls);
            const isConnected = state.status === "connected";
            const isIncomplete = state.status === "incomplete";

            return (
              <div
                key={item.id}
                onClick={() => setActiveIntegration(item)}
                className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-3 hover:border-sand-300 hover:shadow-island-hover transition-all cursor-pointer group"
              >
                <div className="space-y-2.5">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2.5">
                      <div className={`w-9 h-9 rounded-xl flex items-center justify-center shrink-0 border border-sand-200 shadow-2xs ${item.iconBg}`}>
                        <Icon className={`w-4 h-4 ${item.iconColor}`} />
                      </div>
                      <div>
                        <h4 className="text-xs font-bold text-stone-900 group-hover:text-stone-950 transition-colors">
                          {item.name}
                        </h4>
                        <span className="text-[10px] font-mono text-stone-400">{item.categoryLabel}</span>
                      </div>
                    </div>

                    <span
                      className={`px-2 py-0.5 rounded-full text-[10px] font-mono font-bold border shrink-0 ${
                        isConnected
                          ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                          : isIncomplete
                          ? "bg-amber-50 text-amber-800 border-amber-200"
                          : "bg-sand-100 text-stone-600 border-sand-200"
                      }`}
                    >
                      {state.label}
                    </span>
                  </div>

                  <p className="text-xs text-stone-500 line-clamp-2 leading-relaxed">{item.description}</p>
                </div>

                <div className="pt-2.5 border-t border-sand-150 flex items-center justify-between text-[11px]">
                  <span className="font-mono text-[10px] text-stone-400">
                    {item.fields.length} credential{item.fields.length > 1 ? "s" : ""}
                  </span>

                  <span className="font-bold text-stone-800 group-hover:text-stone-950 flex items-center gap-1 text-xs transition-colors">
                    <span>{isConnected ? "Manage" : "Configure"}</span>
                    <ArrowRight className="w-3 h-3 text-stone-400 group-hover:translate-x-0.5 transition-transform" />
                  </span>
                </div>
              </div>
            );
          })}
        </div>

        {filteredCatalog.length === 0 && (
          <div className="p-12 text-center text-stone-400 font-mono bg-white border border-sand-200 rounded-2xl shadow-2xs space-y-2">
            <SlidersHorizontal className="w-8 h-8 text-stone-300 mx-auto" />
            <p>No integrations match your search criteria.</p>
          </div>
        )}
      </div>

      {/* Slide-over Drawer for Credential Entry & OAuth Setup */}
      <IntegrationDrawer
        integration={activeIntegration}
        status={status}
        onClose={() => setActiveIntegration(null)}
        onRefreshStatus={load}
      />
    </div>
  );
}
