"use client";

import { useEffect, useState, useMemo, useCallback } from "react";
import { client, type Integration, type GithubInstallation, type SlackInstallation } from "@/lib/api";
import {
  CheckCircle2,
  AlertCircle,
  Search,
  SlidersHorizontal,
  ShieldCheck,
  Sparkles,
  ArrowRight,
  RefreshCw,
} from "lucide-react";
import { SiGithub, SiGit, SiAnthropic, SiGooglegemini, SiDatadog, SiPrometheus } from "react-icons/si";
import { RiOpenaiFill } from "react-icons/ri";
import { FaSlack } from "react-icons/fa6";
import { IntegrationDrawer, type CatalogIntegration } from "@/components/IntegrationDrawer";

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
    brandAccent: "#ffffff",
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
    id: "git",
    name: "Git Push Token",
    category: "scm",
    categoryLabel: "Source Control",
    description: "Dedicated token used by daemon runners to authenticate and push branches to your git repos.",
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
    id: "anthropic",
    name: "Anthropic",
    category: "llm",
    categoryLabel: "AI & Models",
    description: "Powers Claude 3.7 Sonnet, Claude 3.5 Sonnet, and Opus models for reasoning and code generation.",
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
    description: "Powers Google Gemini 2.5 Pro and Flash models for high-throughput code execution.",
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
    description: "Powers GPT-4o, GPT-4.1, and o1/o3 reasoning models for task implementation.",
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
    id: "slack",
    name: "Slack",
    category: "notifications",
    categoryLabel: "Notifications",
    description: "Trigger Kiwi tasks by @mentioning the bot in a channel or thread, and get status updates and notifications back.",
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
    id: "datadog",
    name: "Datadog",
    category: "telemetry",
    categoryLabel: "Telemetry & Metrics",
    description: "Allows post-merge canary verification to pull latency & error metrics directly from Datadog.",
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
    description: "Enables post-merge canary monitors to query Prometheus PromQL endpoints for regression signals.",
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
        label: `${githubInstalls.length} account${githubInstalls.length > 1 ? "s" : ""} (App)`,
        summary: `${githubInstalls.map((g) => g.account_login).join(", ")}`,
      };
    }
    if (status["github"]) {
      return {
        status: "connected" as const,
        label: "Personal Access Token Active",
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
        label: `${slackInstalls.length} workspace${slackInstalls.length > 1 ? "s" : ""}`,
        summary: slackInstalls.map((s) => s.team_name || s.team_id).join(", "),
      };
    }
    if (status["slack"]) {
      return {
        status: "connected" as const,
        label: "Notifications only",
        summary: "Webhook set, but no workspace installed for triggers",
      };
    }
    return {
      status: "unconnected" as const,
      label: "Not connected",
      summary: "Add to Slack to trigger tasks from a channel",
    };
  }

  if (item.fields.length === 1) {
    const connected = !!status[item.fields[0].key];
    return {
      status: connected ? ("connected" as const) : ("unconnected" as const),
      label: connected ? "Connected" : "Not configured",
      summary: connected ? "API Key active and verified" : "No credentials added",
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
      id: "llm",
      label: "AI & Models",
      count: CATALOG.filter((c) => c.category === "llm").length,
    },
    {
      id: "notifications",
      label: "Notifications",
      count: CATALOG.filter((c) => c.category === "notifications").length,
    },
    {
      id: "telemetry",
      label: "Telemetry",
      count: CATALOG.filter((c) => c.category === "telemetry").length,
    },
  ];

  return (
    <div className="p-8 max-w-6xl mx-auto h-full flex flex-col text-stone-900">
      {/* Page Header */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 mb-8">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-3xl font-light tracking-tight">Integrations</h1>
            <span className="text-xs px-2.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 font-medium">
              {stats.connected} of {stats.total} Active
            </span>
          </div>
          <p className="text-sm text-stone-500 mt-1.5 max-w-2xl">
            Connect your source control, AI provider keys, alerting webhooks, and telemetry endpoints.
            Credentials are encrypted at rest and sealed to daemon runtimes.
          </p>
        </div>

        <button
          onClick={load}
          disabled={isRefreshing}
          className="btn-ghost self-start md:self-auto text-xs px-3.5 py-2 rounded-xl flex items-center gap-2 text-stone-500 hover:text-stone-900"
          title="Refresh statuses"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isRefreshing ? "animate-spin text-emerald-400" : ""}`} />
          <span>Refresh</span>
        </button>
      </div>

      {/* Connectivity Overview Banner */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 mb-8">
        <div className="bg-white shadow-2xs p-4 flex items-center gap-4">
          <div className="w-10 h-10 rounded-xl bg-emerald-500/10 border border-emerald-500/20 flex items-center justify-center shrink-0">
            <CheckCircle2 className="w-5 h-5 text-emerald-400" />
          </div>
          <div>
            <div className="text-xl font-light text-stone-900">{stats.connected}</div>
            <div className="text-[11px] text-stone-500 uppercase tracking-wider font-medium">Connected</div>
          </div>
        </div>

        <div className="bg-white shadow-2xs p-4 flex items-center gap-4">
          <div className="w-10 h-10 rounded-xl bg-amber-500/10 border border-amber-500/20 flex items-center justify-center shrink-0">
            <AlertCircle className="w-5 h-5 text-amber-400" />
          </div>
          <div>
            <div className="text-xl font-light text-stone-900">{stats.incomplete}</div>
            <div className="text-[11px] text-stone-500 uppercase tracking-wider font-medium">Partially Set</div>
          </div>
        </div>

        <div className="bg-white shadow-2xs p-4 flex items-center gap-4">
          <div className="w-10 h-10 rounded-xl bg-sand-50 border border-sand-200 flex items-center justify-center shrink-0">
            <Sparkles className="w-5 h-5 text-stone-500" />
          </div>
          <div>
            <div className="text-xl font-light text-stone-900">{stats.unconnected}</div>
            <div className="text-[11px] text-stone-500 uppercase tracking-wider font-medium">Available to Add</div>
          </div>
        </div>
      </div>

      {/* Filter & Search Bar */}
      <div className="flex flex-col lg:flex-row items-stretch lg:items-center justify-between gap-4 mb-6">
        {/* Category Pills */}
        <div className="flex items-center gap-1.5 overflow-x-auto pb-1 scrollbar-none">
          {categories.map((cat) => {
            const active = selectedCategory === cat.id;
            return (
              <button
                key={cat.id}
                onClick={() => setSelectedCategory(cat.id)}
                className={`flex items-center gap-2 px-3.5 py-1.5 rounded-xl text-xs font-medium transition-all shrink-0 ${
                  active
                    ? "bg-white text-black font-semibold shadow-sm"
                    : "bg-sand-50 text-stone-500 hover:text-stone-900 hover:bg-sand-100 border border-sand-150"
                }`}
              >
                <span>{cat.label}</span>
                <span
                  className={`text-[10px] px-1.5 py-0.2 rounded-full ${
                    active ? "bg-black/15 text-black font-bold" : "bg-sand-100 text-stone-500"
                  }`}
                >
                  {cat.count}
                </span>
              </button>
            );
          })}
        </div>

        {/* Search & Status Controls */}
        <div className="flex items-center gap-2.5">
          <div className="relative flex-1 sm:w-64">
            <Search className="w-3.5 h-3.5 text-stone-500 absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search integrations…"
              className="w-full field text-xs pl-8.5 pr-3 py-1.5 rounded-xl"
            />
          </div>

          <div className="flex items-center bg-sand-50 border border-sand-200 rounded-xl p-0.5 shrink-0">
            <button
              onClick={() => setStatusFilter("all")}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                statusFilter === "all" ? "bg-white/15 text-stone-900 font-semibold" : "text-stone-500 hover:text-stone-900"
              }`}
            >
              All
            </button>
            <button
              onClick={() => setStatusFilter("connected")}
              className={`px-2.5 py-1 text-xs rounded-lg transition-colors ${
                statusFilter === "connected"
                  ? "bg-white/15 text-emerald-400 font-semibold"
                  : "text-stone-500 hover:text-stone-900"
              }`}
            >
              Connected
            </button>
          </div>
        </div>
      </div>

      {/* Integrations Grid */}
      {filteredCatalog.length === 0 ? (
        <div className="bg-white shadow-2xs p-12 text-center rounded-2xl border border-sand-200 my-6">
          <SlidersHorizontal className="w-8 h-8 text-stone-400 mx-auto mb-3" />
          <h3 className="text-sm font-medium text-stone-900 mb-1">No integrations match your filters</h3>
          <p className="text-xs text-stone-400 mb-4 max-w-sm mx-auto">
            Try adjusting your search keywords or switching category filters to find the integration you need.
          </p>
          <button
            onClick={() => {
              setSearchQuery("");
              setSelectedCategory("all");
              setStatusFilter("all");
            }}
            className="btn-ghost text-xs px-3.5 py-1.5 rounded-xl text-stone-700"
          >
            Clear all filters
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 mb-8">
          {filteredCatalog.map((item) => {
            const Icon = item.icon;
            const state = deriveIntegrationState(item, status, githubInstalls, slackInstalls);
            const isConnected = state.status === "connected";
            const isIncomplete = state.status === "incomplete";

            return (
              <div
                key={item.id}
                className={`bg-white shadow-2xs border rounded-2xl p-5 flex flex-col justify-between transition-all duration-200 hover:border-sand-200 group relative overflow-hidden ${
                  isConnected
                    ? "border-emerald-500/20 bg-gradient-to-b from-[#10202C] to-[#0E1A24]"
                    : isIncomplete
                    ? "border-amber-500/20"
                    : "border-sand-200"
                }`}
              >
                <div>
                  {/* Top row: Icon, Title, Status */}
                  <div className="flex items-start justify-between gap-3 mb-3">
                    <div className="flex items-center gap-3.5 min-w-0">
                      <div
                        className={`w-11 h-11 rounded-xl flex items-center justify-center shrink-0 border border-sand-200 transition-transform group-hover:scale-105 duration-200 ${item.iconBg}`}
                      >
                        <Icon className={`w-5 h-5 ${item.iconColor}`} />
                      </div>
                      <div className="min-w-0">
                        <div className="flex items-center gap-2">
                          <h3 className="font-medium text-stone-900 tracking-tight text-base truncate">
                            {item.name}
                          </h3>
                        </div>
                        <span className="text-[10px] uppercase font-bold tracking-wider text-stone-400">
                          {item.categoryLabel}
                        </span>
                      </div>
                    </div>

                    {/* Status Badge */}
                    <div className="shrink-0">
                      {isConnected ? (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                          <CheckCircle2 className="w-3.5 h-3.5" />
                          <span>{state.label}</span>
                        </span>
                      ) : isIncomplete ? (
                        <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] font-medium bg-amber-500/10 text-amber-400 border border-amber-500/20">
                          <AlertCircle className="w-3.5 h-3.5" />
                          <span>{state.label}</span>
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-medium bg-sand-50 text-stone-400 border border-sand-150">
                          <span>Not set</span>
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Blurb */}
                  <p className="text-xs text-stone-500 leading-relaxed mb-4 line-clamp-2">
                    {item.description}
                  </p>

                  {/* GitHub Specific Connected Orgs Pill Preview */}
                  {item.isGithubHybrid && githubInstalls.length > 0 && (
                    <div className="mb-4 flex flex-wrap gap-1.5">
                      {githubInstalls.slice(0, 3).map((g) => (
                        <span
                          key={g.installation_id}
                          className="inline-flex items-center gap-1 text-[11px] font-mono px-2 py-0.5 rounded-md bg-sand-50 border border-sand-200 text-stone-700"
                        >
                          <ShieldCheck className="w-3 h-3 text-emerald-400" />
                          <span className="truncate max-w-[120px]">{g.account_login}</span>
                        </span>
                      ))}
                      {githubInstalls.length > 3 && (
                        <span className="text-[10px] text-stone-400 self-center">
                          +{githubInstalls.length - 3} more
                        </span>
                      )}
                    </div>
                  )}

                  {/* Slack Specific Connected Workspaces Pill Preview */}
                  {item.isSlackHybrid && slackInstalls.length > 0 && (
                    <div className="mb-4 flex flex-wrap gap-1.5">
                      {slackInstalls.slice(0, 3).map((s) => (
                        <span
                          key={s.team_id}
                          className="inline-flex items-center gap-1 text-[11px] font-mono px-2 py-0.5 rounded-md bg-sand-50 border border-sand-200 text-stone-700"
                        >
                          <ShieldCheck className="w-3 h-3 text-emerald-400" />
                          <span className="truncate max-w-[120px]">{s.team_name || s.team_id}</span>
                        </span>
                      ))}
                      {slackInstalls.length > 3 && (
                        <span className="text-[10px] text-stone-400 self-center">
                          +{slackInstalls.length - 3} more
                        </span>
                      )}
                    </div>
                  )}

                  {/* Multi-field parameters preview for Datadog / Prometheus */}
                  {item.fields.length > 1 && (
                    <div className="mb-4 flex flex-wrap gap-1.5 text-[10px]">
                      {item.fields.map((f) => {
                        const isFieldConnected = !!status[f.key];
                        return (
                          <span
                            key={f.key}
                            className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-md border font-mono ${
                              isFieldConnected
                                ? "bg-emerald-500/5 text-emerald-300 border-emerald-500/20"
                                : "bg-sand-50 text-stone-400 border-sand-200"
                            }`}
                          >
                            <span
                              className={`w-1.5 h-1.5 rounded-full ${
                                isFieldConnected ? "bg-emerald-400" : "bg-stone-300"
                              }`}
                            />
                            {f.label}
                          </span>
                        );
                      })}
                    </div>
                  )}
                </div>

                {/* Bottom Row: Actions */}
                <div className="flex items-center justify-between pt-3 border-t border-sand-150 mt-auto">
                  <span className="text-[11px] text-stone-400 truncate max-w-[200px]">
                    {state.summary}
                  </span>

                  <button
                    type="button"
                    onClick={() => setActiveIntegration(item)}
                    className={`flex items-center gap-1.5 text-xs font-semibold px-3.5 py-1.5 rounded-xl transition-all ${
                      isConnected
                        ? "bg-sand-100 hover:bg-white/15 text-stone-900 border border-sand-200"
                        : "btn-primary text-black"
                    }`}
                  >
                    <span>{isConnected ? "Configure" : "Connect"}</span>
                    <ArrowRight className="w-3 h-3" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Slide-Over Drawer for Selected Integration */}
      <IntegrationDrawer
        integration={activeIntegration}
        status={status}
        onClose={() => setActiveIntegration(null)}
        onRefreshStatus={load}
      />
    </div>
  );
}
