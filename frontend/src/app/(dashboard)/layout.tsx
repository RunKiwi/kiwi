"use client";

import React, { useEffect, useState, useMemo } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  LayoutGrid,
  Radar,
  Receipt,
  ShieldAlert,
  Sliders,
  Zap,
  Folder,
  FolderOpen,
  Plus,
  Search,
  LogOut,
  PanelLeftClose,
  PanelLeftOpen,
  HelpCircle,
  Activity,
  Sparkles,
  Cpu,
  Link2,
  Users,
  CreditCard,
  ShieldCheck,
  Server,
  GitPullRequest,
  Menu,
  X,
  LineChart,
} from "lucide-react";
import { api, type UsageResponse, type ValidateResponse, type GithubRepo } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { SiGithub, SiDatadog, SiPrometheus } from "react-icons/si";
import { FaSlack } from "react-icons/fa6";
import { Logo } from "@/components/Logo";
import { UpgradeButton } from "@/components/UpgradeButton";
import { PlatformTour } from "@/components/PlatformTour";
import { useFleetStore } from "@/store/useFleetStore";
import Fuse from "fuse.js";

interface SearchItem {
  id: string;
  title: string;
  subtitle?: string;
  category: "Navigation" | "Integrations" | "Repositories" | "Tasks" | "Team & Org" | "Actions";
  href?: string;
  action?: () => void;
  icon: React.ReactNode;
  hint?: string;
  keywords?: string;
}

function getAvatarInitials(email?: string, name?: string): string {
  if (email && email.trim()) {
    const localPart = email.split("@")[0].trim();
    if (localPart.length >= 2) {
      return localPart.slice(0, 2).toUpperCase();
    }
  }
  if (name && name.trim()) {
    const parts = name.trim().split(/[\s_.-]+/);
    if (parts.length >= 2 && parts[0] && parts[1]) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }
  return "KW";
}

function UserAvatar({
  email,
  name,
  avatarUrl,
  githubLogin,
  className = "w-6 h-6",
}: {
  email?: string;
  name?: string;
  avatarUrl?: string;
  githubLogin?: string;
  className?: string;
}) {
  const [imgError, setImgError] = useState(false);
  const src = avatarUrl || (githubLogin ? `https://github.com/${githubLogin}.png?size=64` : undefined);
  const initials = getAvatarInitials(email, name);

  if (src && !imgError) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        src={src}
        alt={name || email || "User Avatar"}
        onError={() => setImgError(true)}
        className={`${className} rounded-full object-cover shrink-0 border border-sand-300 shadow-2xs bg-stone-100`}
      />
    );
  }

  return (
    <div
      className={`${className} rounded-full bg-stone-800 text-white font-bold flex items-center justify-center text-[10px] shrink-0 uppercase select-none`}
    >
      {initials}
    </div>
  );
}

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { isAuthenticated } = useAuth();
  const { jobs, daemons, loadJobs, loadDaemons } = useFleetStore();

  const [isSuperAdmin, setIsSuperAdmin] = useState(false);
  const [org, setOrg] = useState<ValidateResponse | null>(null);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [repoSearchQuery, setRepoSearchQuery] = useState("");
  const [primaryCollapsed, setPrimaryCollapsed] = useState(false);
  const [showMobileMenu, setShowMobileMenu] = useState(false);
  const [showCommandPalette, setShowCommandPalette] = useState(false);
  const [cmdQuery, setCmdQuery] = useState("");
  const [selectedIndex, setSelectedIndex] = useState(0);

  // Auto-close mobile drawer on navigation
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setShowMobileMenu(false);
  }, [pathname]);

  useEffect(() => {
    if (isAuthenticated === false) {
      router.push("/login");
    }
  }, [isAuthenticated, router]);

  useEffect(() => {
    loadJobs().catch(() => {});
    loadDaemons().catch(() => {});
  }, [loadJobs, loadDaemons]);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setShowCommandPalette((prev) => !prev);
      } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "b") {
        e.preventDefault();
        setPrimaryCollapsed((prev) => !prev);
      } else if (e.key === "Escape") {
        setShowCommandPalette(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  useEffect(() => {
    api.validate()
      .then(setOrg)
      .catch(() => {});

    api.getUsage()
      .then((u) => {
        setUsage(u);
        setIsSuperAdmin(Boolean(u.is_super_admin));
      })
      .catch(() => {});

    api.listGithubRepos()
      .then((r) => {
        const repoList = r.repos || [];
        setRepos(repoList);
        if (typeof window !== "undefined") {
          const isCompleted = localStorage.getItem("kiwi_onboarding_completed");
          if (!isCompleted) {
            if (repoList.length > 0 || jobs.length > 0) {
              // Existing account on a new device: auto-mark onboarding complete
              localStorage.setItem("kiwi_onboarding_completed", "1");
            } else if (pathname === "/") {
              // Genuinely fresh new account: guide to onboarding
              router.push("/onboarding");
            }
          }
        }
      })
      .catch(() => {});
  }, [jobs.length, pathname, router]);

  const handleLogout = () => {
    if (typeof window !== "undefined") {
      localStorage.removeItem("kiwi_token");
      router.push("/login");
    }
  };

  const plan = usage?.plan || "free";
  const usedMinutes = usage?.agent_minutes_used ?? 0;
  // agent_minutes_limit is 0 = unlimited once usage has loaded (see api.ts) —
  // there is no plan-based default to guess here, and guessing one broke any
  // plan (e.g. enterprise) not covered by the guess.
  const limitMinutes = usage?.agent_minutes_limit ?? 0;
  const hasMinutesCap = usage != null && limitMinutes > 0;
  const percentUsed = hasMinutesCap ? Math.min(100, Math.round((usedMinutes / limitMinutes) * 100)) : 0;

  const planReviewsCount = (jobs || []).filter((j) => j.status === "PLAN_REVIEW" || j.requires_plan_approval).length;
  const needsAttentionCount = planReviewsCount;
  const activeTasksCount = (jobs || []).filter((j) => j.status === "LEASED" || j.status === "RUNNING").length;
  const runnersCount = (daemons || []).length;

  // Group repos by organization / owner
  const reposByOrg = useMemo(() => {
    const q = repoSearchQuery.trim().toLowerCase();
    const filtered = repos.filter((r) => {
      const fn = (r.full_name || r.name || "").toLowerCase();
      return fn.includes(q);
    });

    const groups: Record<string, GithubRepo[]> = {};
    for (const r of filtered) {
      const parts = (r.full_name || r.name || "").split("/");
      const orgKey = parts.length > 1 ? parts[0] : "Repositories";
      if (!groups[orgKey]) groups[orgKey] = [];
      groups[orgKey].push(r);
    }
    return groups;
  }, [repos, repoSearchQuery]);

  const orgKeys = Object.keys(reposByOrg);

  // Universal Fuzzy Search Index across Navigation, Integrations, Repositories, Tasks, and Actions
  const allSearchItems = useMemo<SearchItem[]>(() => {
    const items: SearchItem[] = [
      // 1. Navigation Pages
      {
        id: "nav-composer",
        title: "Create New Task",
        subtitle: "Launch autonomous multi-agent coding task",
        category: "Navigation",
        href: "/composer",
        icon: <Sparkles className="w-3.5 h-3.5 text-kiwi-600" />,
        hint: "/composer",
        keywords: "create task new prompt assign architect implementer coding build plan agent composer",
      },
      {
        id: "nav-dashboard",
        title: "Tasks Dashboard",
        subtitle: "Kanban pipeline of all running and completed tasks",
        category: "Navigation",
        href: "/",
        icon: <LayoutGrid className="w-3.5 h-3.5 text-stone-600" />,
        hint: "/",
        keywords: "tasks jobs active execution board list kanban pipeline dashboard home",
      },
      {
        id: "nav-monitors",
        title: "PR Watchdogs",
        subtitle: "Continuous telemetry verification after pull request merge",
        category: "Navigation",
        href: "/monitors",
        icon: <Radar className="w-3.5 h-3.5 text-sky-600" />,
        hint: "/monitors",
        keywords: "canary telemetry p99 error rate latency post-merge watchdog release pull request pr monitors",
      },
      {
        id: "nav-activity",
        title: "Live Fleet Activity Log",
        subtitle: "Real-time Gantt timeline of daemon execution traces",
        category: "Navigation",
        href: "/activity",
        icon: <Activity className="w-3.5 h-3.5 text-indigo-600" />,
        hint: "/activity",
        keywords: "activity log live stream execution gantt traces timeline fleet daemons runners",
      },
      {
        id: "nav-spend",
        title: "Cost & Usage Analytics",
        subtitle: "Token utilization, execution time, and model spend metrics",
        category: "Navigation",
        href: "/spend",
        icon: <Receipt className="w-3.5 h-3.5 text-stone-600" />,
        hint: "/spend",
        keywords: "spend analytics cost tokens latency graphs metrics velocity billing tokens usage",
      },
      {
        id: "nav-fleet",
        title: "Runners & Fleets",
        subtitle: "Inspect runner capacity, active microVMs, and private BYOC nodes",
        category: "Navigation",
        href: "/fleet",
        icon: <Server className="w-3.5 h-3.5 text-emerald-600" />,
        hint: "/fleet",
        keywords: "fleet daemons runners byoc nodes compute private hosting self-hosted cluster",
      },
      {
        id: "nav-models",
        title: "AI Models & Providers",
        subtitle: "Configure Claude, Gemini, GPT, and custom model endpoints",
        category: "Navigation",
        href: "/models",
        icon: <Cpu className="w-3.5 h-3.5 text-purple-600" />,
        hint: "/models",
        keywords: "models claude gemini openai anthropic frontier economy custom llm tokens providers",
      },
      {
        id: "nav-integrations",
        title: "Integrations Hub",
        subtitle: "Connect GitHub, Slack, Datadog, Prometheus, and API keys",
        category: "Navigation",
        href: "/integrations",
        icon: <Link2 className="w-3.5 h-3.5 text-stone-700" />,
        hint: "/integrations",
        keywords: "integrations catalog slack github datadog prometheus git tokens seal webhooks connections",
      },
      {
        id: "nav-slack",
        title: "Slack Channel Bindings",
        subtitle: "Map repository triggers and team alert channels",
        category: "Navigation",
        href: "/integrations/slack",
        icon: <FaSlack className="w-3.5 h-3.5 text-[#ECB22E]" />,
        hint: "/integrations/slack",
        keywords: "slack bot channel mapping workspace alerts triggers notifications chat bindings",
      },
      {
        id: "nav-team",
        title: "Team Members & Roles",
        subtitle: "Manage organization seats, developer roles, and invites",
        category: "Navigation",
        href: "/team",
        icon: <Users className="w-3.5 h-3.5 text-stone-600" />,
        hint: "/team",
        keywords: "team users members invite seats roles admin billing developer email collaborators",
      },
      {
        id: "nav-settings",
        title: "Plans & Billing",
        subtitle: "Subscription tier, pooled agent-minutes quota, and invoices",
        category: "Navigation",
        href: "/settings",
        icon: <CreditCard className="w-3.5 h-3.5 text-amber-600" />,
        hint: "/settings",
        keywords: "plans billing quota upgrade pro subscription invoices agent-minutes stripe pricing settings",
      },
      {
        id: "nav-records",
        title: "Audit Receipts",
        subtitle: "Verifiable tamper-evident execution logs and task receipts",
        category: "Navigation",
        href: "/records",
        icon: <ShieldCheck className="w-3.5 h-3.5 text-emerald-600" />,
        hint: "/records",
        keywords: "records audit verifiable receipts cryptographic hashes ledger compliance security logs",
      },
      {
        id: "nav-metrics",
        title: "Telemetry & SLOs",
        subtitle: "Configure Prometheus & Datadog post-merge regression monitors",
        category: "Navigation",
        href: "/metrics",
        icon: <LineChart className="w-3.5 h-3.5 text-indigo-600" />,
        hint: "/metrics",
        keywords: "metrics telemetry slo prometheus datadog monitoring latency error rate p99 queries canary",
      },
      ...(isSuperAdmin
        ? [
            {
              id: "nav-admin",
              title: "Staff Super Admin Console",
              subtitle: "Cluster-wide organization, daemon, and tenant control plane",
              category: "Navigation" as const,
              href: "/admin",
              icon: <ShieldAlert className="w-3.5 h-3.5 text-rose-600" />,
              hint: "/admin",
              keywords: "super admin staff orgs backend server control plane tenants internal system",
            },
          ]
        : []),

      // 2. Integrations & Third-party Services
      {
        id: "int-github",
        title: "GitHub App & VCS",
        subtitle: "Pull requests, review watchdogs, and code repository webhooks",
        category: "Integrations",
        href: "/integrations",
        icon: <SiGithub className="w-3.5 h-3.5 text-stone-900" />,
        hint: "VCS Hub",
        keywords: "github source control pull request webhook git token repos app version control",
      },
      {
        id: "int-slack",
        title: "Slack Bot & Notifications",
        subtitle: "Interactive trigger commands and team alert routing",
        category: "Integrations",
        href: "/integrations/slack",
        icon: <FaSlack className="w-3.5 h-3.5 text-[#ECB22E]" />,
        hint: "Team Chat",
        keywords: "slack bot notifications channels workspace team chat triggers alerts messaging",
      },
      {
        id: "int-anthropic",
        title: "Anthropic Claude Key",
        subtitle: "Claude 3.7 Sonnet, Opus 3, and Haiku frontier models",
        category: "Integrations",
        href: "/integrations",
        icon: <Cpu className="w-3.5 h-3.5 text-[#D97757]" />,
        hint: "AI Provider",
        keywords: "anthropic claude sonnet opus 3.7 haiku api key provider frontier intelligence",
      },
      {
        id: "int-gemini",
        title: "Google Gemini Key",
        subtitle: "Gemini 2.5 Flash, Thinking, and Pro multi-modal models",
        category: "Integrations",
        href: "/integrations",
        icon: <Cpu className="w-3.5 h-3.5 text-[#4C8DF6]" />,
        hint: "AI Provider",
        keywords: "gemini 2.5 flash thinking pro google ai api key provider multi-modal",
      },
      {
        id: "int-openai",
        title: "OpenAI GPT Key",
        subtitle: "GPT-4o, o3-mini, and reasoning models",
        category: "Integrations",
        href: "/integrations",
        icon: <Cpu className="w-3.5 h-3.5 text-[#10A37F]" />,
        hint: "AI Provider",
        keywords: "openai gpt-4o o3 reasoning chatgpt api key provider models",
      },
      {
        id: "int-datadog",
        title: "Datadog Telemetry",
        subtitle: "Continuous APM, latency traces, and canary monitors",
        category: "Integrations",
        href: "/integrations",
        icon: <SiDatadog className="w-3.5 h-3.5 text-[#632CA6]" />,
        hint: "Telemetry",
        keywords: "datadog dd_api_key apm metrics traces alerts logs canary observability",
      },
      {
        id: "int-prometheus",
        title: "Prometheus PromQL",
        subtitle: "Custom timeseries metrics query and regression alerts",
        category: "Integrations",
        href: "/integrations",
        icon: <SiPrometheus className="w-3.5 h-3.5 text-[#E6522C]" />,
        hint: "Metrics",
        keywords: "prometheus promql canary alerts timeseries metrics endpoint server",
      },

      // 3. Connected Repositories
      ...repos.map((r) => {
        const repoName = r.full_name || r.name || "Repository";
        return {
          id: `repo-${repoName}`,
          title: repoName,
          subtitle: r.default_branch ? `Branch: ${r.default_branch}` : "Connected Git Repository",
          category: "Repositories" as const,
          href: `/composer?repo=${encodeURIComponent(repoName)}`,
          icon: <SiGithub className="w-3.5 h-3.5 text-stone-700" />,
          hint: "New Task",
          keywords: `repo repository github git codebase ${repoName}`,
        };
      }),

      // 4. Tasks & Jobs
      ...(jobs || []).slice(0, 30).map((j) => {
        const jobTitle = j.task ? (j.task.length > 55 ? j.task.slice(0, 55) + "…" : j.task) : (j.job_id || "Task Run");
        return {
          id: `job-${j.job_id}`,
          title: jobTitle,
          subtitle: `ID: ${(j.job_id || "").slice(0, 10)} • Status: ${j.status || "UNKNOWN"}${j.repo ? ` • ${j.repo}` : ""}`,
          category: "Tasks" as const,
          href: `/?job=${j.job_id}`,
          icon: <Sparkles className="w-3.5 h-3.5 text-sky-600" />,
          hint: j.status || "RUN",
          keywords: `task job execution prompt ${j.job_id || ""} ${j.task || ""} ${j.repo || ""} ${j.status || ""}`,
        };
      }),

      // 5. Team & Workspace
      {
        id: "workspace-org",
        title: org?.org_name || "Workspace Profile",
        subtitle: `Org ID: ${org?.org_id || "—"} • ${plan.toUpperCase()} TIER`,
        category: "Team & Org",
        href: "/settings",
        icon: <Users className="w-3.5 h-3.5 text-emerald-600" />,
        hint: "Workspace",
        keywords: `org organization workspace company ${org?.org_name || ""} ${org?.org_id || ""} ${org?.user_email || ""}`,
      },

      // 6. Quick Actions
      {
        id: "action-tour",
        title: "Start Platform Tour",
        subtitle: "Guided interactive walkthrough of swarm feeds, monitors, fleets, and spend",
        category: "Actions",
        action: () => {
          if (typeof window !== "undefined") {
            window.dispatchEvent(new Event("kiwi:start-tour"));
          }
        },
        icon: <Sparkles className="w-3.5 h-3.5 text-amber-500" />,
        hint: "Tour",
        keywords: "tour guide walkthrough tutorial onboarding demo spotlight intro help",
      },
      {
        id: "action-upgrade",
        title: plan === "free" ? "Upgrade to Kiwi Pro" : "Contact Kiwi Enterprise",
        subtitle: plan === "free" ? "Unlock 2,000 pooled minutes / seat, BYOC runners & priority" : "Explore dedicated VPC, custom compute clusters & SLAs",
        category: "Actions",
        href: plan === "free" ? "/settings" : undefined,
        action: plan !== "free" ? () => {
          if (typeof window !== "undefined") {
            window.location.href = "mailto:support@runkiwi.dev?subject=Kiwi%20Enterprise%20Fleet%20Inquiry";
          }
        } : undefined,
        icon: <Zap className="w-3.5 h-3.5 text-amber-500 fill-current" />,
        hint: plan === "free" ? "Upgrade" : "Enterprise",
        keywords: "upgrade pro enterprise subscription payment tiers quota agent-minutes billing",
      },
      {
        id: "action-support",
        title: "Contact Kiwi Support",
        subtitle: "Reach out to support@runkiwi.dev for assistance",
        category: "Actions",
        action: () => {
          if (typeof window !== "undefined") {
            window.location.href = "mailto:support@runkiwi.dev?subject=Kiwi%20Support%20Inquiry";
          }
        },
        icon: <HelpCircle className="w-3.5 h-3.5 text-stone-500" />,
        hint: "Email",
        keywords: "support help email contact issue question bug assistance",
      },
      {
        id: "action-plans-filter",
        title: "Filter by Plan Reviews (Needs Attention)",
        subtitle: "View all tasks waiting for human plan approval",
        category: "Actions",
        href: "/?filter=plan",
        icon: <Sparkles className="w-3.5 h-3.5 text-indigo-600" />,
        hint: "Filter",
        keywords: "plan reviews approve critic architect attention pending review",
      },
    ];

    return items;
  }, [repos, jobs, org, plan, isSuperAdmin]);

  // Configure Fuse for super-fuzzy, loose multi-key matching
  const fuse = useMemo(() => {
    return new Fuse(allSearchItems, {
      keys: [
        { name: "title", weight: 2.5 },
        { name: "keywords", weight: 2.0 },
        { name: "subtitle", weight: 1.2 },
        { name: "category", weight: 1.0 },
        { name: "id", weight: 1.0 },
      ],
      threshold: 0.45, // Super loose tolerance for typos, partial tokens, and fragmented phrases
      ignoreLocation: true,
      minMatchCharLength: 1,
      shouldSort: true,
    });
  }, [allSearchItems]);

  // Compute matched items
  const searchResults = useMemo(() => {
    const q = cmdQuery.trim();
    if (!q) {
      // Default view: All navigation + top integrations + top actions
      return allSearchItems.filter(
        (i) => i.category === "Navigation" || i.id === "int-github" || i.id === "int-slack" || i.category === "Actions"
      );
    }
    return fuse.search(q).map((res) => res.item);
  }, [cmdQuery, fuse, allSearchItems]);

  // Reset selected index when results change
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setSelectedIndex(0);
  }, [cmdQuery, showCommandPalette]);

  // Don't render the dashboard shell until we've confirmed authentication —
  // otherwise a visitor with no/expired token briefly sees the full
  // interactive UI before the redirect below fires.
  if (isAuthenticated === null || isAuthenticated === false) {
    return null;
  }

  return (
    <div className="h-screen max-h-screen overflow-hidden p-2.5 sm:p-3 md:p-4 flex flex-col font-sans bg-[#F8F7F4] bg-dot-grid text-stone-900 selection:bg-kiwi-200">

      {/* ================= TOP COMPACT NAVBAR ================= */}
      <header className="shrink-0 mb-2 px-2.5 sm:px-3 py-1.5 flex items-center justify-between gap-2 sm:gap-3 text-xs bg-sand-50/80 backdrop-blur-md rounded-xl sm:rounded-2xl border border-sand-200/80 shadow-2xs z-30 font-sans">
        <div className="flex items-center gap-2 sm:gap-3 min-w-0">
          {/* Mobile Menu Toggle Button */}
          <button
            onClick={() => setShowMobileMenu(true)}
            className="md:hidden p-1.5 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 shadow-2xs transition-all shrink-0 cursor-pointer"
            title="Open Navigation Menu"
          >
            <Menu className="w-4 h-4" />
          </button>

          {/* Kiwi Brand Identity */}
          <Link
            href="/"
            className="flex items-center gap-2 px-1 py-0.5 rounded-xl hover:bg-sand-150 transition-all group shrink-0 select-none"
            title="Kiwi Platform Dashboard"
          >
            <div className="w-7 h-7 rounded-xl bg-white border border-sand-200/90 flex items-center justify-center shadow-2xs group-hover:scale-105 transition-transform">
              <Logo variant="full-color" className="w-4.5 h-4.5" />
            </div>
            <div className="flex items-center gap-1.5">
              <span className="font-bold text-stone-900 text-xs tracking-tight">Kiwi</span>
              <span className="text-[9px] font-mono font-bold bg-amber-100/90 text-amber-900 px-1.5 py-0.2 rounded-md border border-amber-200 uppercase">
                {plan}
              </span>
            </div>
          </Link>

          <div className="h-4 w-px bg-sand-200/80 hidden sm:block" />

          {/* Global Search / Command Palette Trigger */}
          <button
            onClick={() => setShowCommandPalette(true)}
            className="flex items-center gap-2 px-2.5 sm:px-3 py-1.5 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-500 hover:text-stone-800 text-xs shadow-2xs transition-all group min-w-0 max-w-[260px]"
          >
            <Search className="w-3.5 h-3.5 text-stone-400 group-hover:text-stone-600 shrink-0" />
            <span className="text-stone-500 font-medium truncate hidden sm:inline">Search tasks, repos, or jump to...</span>
            <span className="text-stone-500 font-medium truncate sm:hidden">Search...</span>
            <kbd className="hidden sm:inline-flex items-center gap-0.5 text-[10px] font-mono bg-sand-100 text-stone-500 px-1.5 py-0.5 rounded border border-sand-200 ml-auto">
              ⌘K
            </kbd>
          </button>
        </div>

        <div className="flex items-center gap-1.5 sm:gap-3">
          {/* Live Agent Compute Minutes Meter Pill */}
          <Link
            href="/settings"
            className="hidden sm:flex items-center gap-1.5 px-2.5 py-1 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-[11px] font-mono text-stone-700 shadow-2xs transition-all"
            title="Agent Compute Minutes Used (Click to view Plans & Usage)"
          >
            <Zap className="w-3 h-3 text-amber-500 fill-current" />
            <span>
              {usedMinutes.toFixed(1)} <span className="text-stone-400">/ {hasMinutesCap ? `${limitMinutes}m` : "Unlimited"}</span>
            </span>
          </Link>

          {/* Current Plan & Upgrade CTA */}
          {plan === "free" ? (
            <div className="flex items-center gap-1.5">
              <span className="hidden md:inline-block px-2 py-0.5 rounded-lg bg-sand-100 border border-sand-200 text-[10px] font-mono font-bold uppercase text-stone-700">
                Free Tier
              </span>
              <UpgradeButton variant="compact" label="Upgrade" />
            </div>
          ) : (
            <span className="px-2 sm:px-2.5 py-1 rounded-xl bg-kiwi-50 border border-kiwi-200 text-[10px] sm:text-[11px] font-mono font-bold text-kiwi-900 shadow-2xs flex items-center gap-1.5">
              <Zap className="w-3 h-3 text-kiwi-600 fill-current" />
              <span className="hidden sm:inline">Pro Active</span>
              <span className="sm:hidden">Pro</span>
            </span>
          )}

          <div className="h-4 w-px bg-sand-200 hidden sm:block" />

          {/* Quick Help & Support */}
          <a
            href="mailto:support@runkiwi.dev?subject=Kiwi%20Support%20Request"
            className="flex items-center gap-1.5 px-2 sm:px-2.5 py-1 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-600 hover:text-stone-900 text-xs font-semibold shadow-2xs transition-all"
            title="Support & Docs (support@runkiwi.dev)"
          >
            <HelpCircle className="w-3.5 h-3.5 text-stone-500" />
            <span className="hidden md:inline">Support</span>
          </a>

          {/* Guided Tour Trigger */}
          <button
            onClick={() => {
              if (typeof window !== "undefined") {
                window.dispatchEvent(new Event("kiwi:start-tour"));
              }
            }}
            className="flex items-center gap-1.5 px-2 sm:px-2.5 py-1 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-600 hover:text-stone-900 text-xs font-semibold shadow-2xs transition-all cursor-pointer"
            title="Start Guided Platform Tour"
          >
            <Sparkles className="w-3.5 h-3.5 text-amber-500" />
            <span className="hidden md:inline">Tour</span>
          </button>
        </div>
      </header>

      {/* ================= DUAL-ISLAND SIDEBARS & SCROLLABLE MAIN CONTENT ================= */}
      <div className="flex-1 flex gap-3 overflow-hidden min-h-0">
        
        {/* COLUMN 1: COLLAPSIBLE PRIMARY WORKSPACE & REPO ISLAND (~185px Expanded / 54px Collapsed on Desktop) */}
        <aside
          className={`island-sidebar p-3 hidden md:flex flex-col shrink-0 select-none shadow-island relative h-full overflow-hidden transition-all duration-200 ${
            primaryCollapsed ? "w-14 items-center" : "w-48"
          }`}
        >
          {/* Workspace Context Header */}
          {primaryCollapsed ? (
            <div className="shrink-0 flex flex-col items-center gap-2 mb-3 w-full">
              <button
                onClick={() => setPrimaryCollapsed(false)}
                className="w-8 h-8 rounded-xl bg-white hover:bg-sand-100 text-stone-700 hover:text-stone-950 border border-sand-200 shadow-2xs flex items-center justify-center transition-all cursor-pointer group"
                title="Expand Sidebar (⌘B)"
              >
                <PanelLeftOpen className="w-4 h-4 text-stone-600 group-hover:text-stone-900" />
              </button>
            </div>
          ) : (
            <div className="shrink-0 flex items-center justify-between px-1 mb-3 w-full">
              <div className="min-w-0 pr-1">
                <p className="text-[11px] font-bold text-stone-900 truncate leading-tight">
                  {org?.org_name || "My Workspace"}
                </p>
                <p className="text-[9px] font-mono text-stone-400 truncate mt-0.5">
                  #{org?.org_id ? org.org_id.slice(0, 10) : "org_default"}
                </p>
              </div>

              <button
                onClick={() => setPrimaryCollapsed(true)}
                className="p-1.5 rounded-lg bg-sand-100 hover:bg-sand-200 text-stone-600 hover:text-stone-900 border border-sand-200 shadow-2xs transition-all cursor-pointer shrink-0"
                title="Collapse Sidebar (⌘B)"
              >
                <PanelLeftClose className="w-3.5 h-3.5" />
              </button>
            </div>
          )}

          {/* Quick Action Icons Row */}
          {!primaryCollapsed && (
            <div className="shrink-0 flex items-center justify-between px-1 mb-3 text-stone-500 w-full">
              <button
                onClick={() => setShowCommandPalette(true)}
                className="p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-800 transition-all cursor-pointer"
                title="Search Everything (⌘K)"
              >
                <Search className="w-3.5 h-3.5" />
              </button>
              <Link href="/" className="p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-800 transition-all" title="Task Dashboard">
                <LayoutGrid className="w-3.5 h-3.5" />
              </Link>
              <Link href="/monitors" className="p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-800 transition-all" title="PR Watchdogs">
                <Radar className="w-3.5 h-3.5" />
              </Link>
              <Link href="/settings" className="p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-800 transition-all" title="Settings">
                <Sliders className="w-3.5 h-3.5" />
              </Link>
            </div>
          )}

          {/* Primary Action: New Task Pill */}
          <div className="shrink-0 mb-3 w-full" data-tour="new-task-btn">
            <Link
              href="/composer"
              className="w-full flex items-center justify-center gap-1.5 py-2 px-2.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all active:scale-[0.98]"
              title="Assign New Task"
            >
              <Plus className="w-3.5 h-3.5 stroke-[2.5] text-kiwi-400 shrink-0" />
              {!primaryCollapsed && <span>New Task</span>}
            </Link>
          </div>

          {/* Workspace Folder Tree & Search (Capped at ~36vh) */}
          <div className="shrink-0 space-y-2 px-0.5 text-xs w-full">
            {!primaryCollapsed ? (
              <div>
                <div className="text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold px-1 mb-1.5 flex items-center justify-between">
                  <span>Repositories</span>
                  <Link href="/integrations" className="text-kiwi-700 hover:underline normal-case text-[10px]">+ Add</Link>
                </div>

                {/* Compact Search Bar */}
                <div className="relative mb-2">
                  <Search className="w-3 h-3 text-stone-400 absolute left-2 top-2 pointer-events-none" />
                  <input
                    type="text"
                    value={repoSearchQuery}
                    onChange={(e) => setRepoSearchQuery(e.target.value)}
                    placeholder="Search repos..."
                    className="w-full bg-sand-100/70 border border-sand-200 rounded-lg pl-6 pr-2 py-1 text-[11px] font-mono placeholder:text-stone-400 focus:outline-none focus:border-stone-400 focus:bg-white transition-all"
                  />
                </div>

                {/* Capped Repo List (~36% max height) */}
                <div className="max-h-[34vh] overflow-y-auto space-y-2 pr-0.5 custom-scrollbar">
                  <Link
                    href="/"
                    className="w-full flex items-center justify-between p-1.5 rounded-lg font-semibold bg-sand-150 text-stone-900 transition-all text-left"
                    title="All Repositories"
                  >
                    <span className="flex items-center gap-1.5 truncate">
                      <FolderOpen className="w-3.5 h-3.5 text-stone-700 shrink-0" />
                      <span className="truncate">All Repos</span>
                    </span>
                    <span className="text-[10px] font-mono text-stone-400">{repos.length}</span>
                  </Link>

                  {orgKeys.length === 0 ? (
                    <p className="text-[10px] text-stone-400 px-1 py-2 text-center font-mono">No matching repos</p>
                  ) : (
                    orgKeys.map((orgName) => {
                      const orgRepos = reposByOrg[orgName];
                      return (
                        <div key={orgName} className="space-y-0.5 pt-1">
                          <div className="text-[9px] font-mono font-bold text-stone-400 uppercase tracking-wider px-1 flex items-center justify-between">
                            <span>{orgName}</span>
                            <span className="font-normal text-[9px]">{orgRepos.length}</span>
                          </div>

                          {orgRepos.map((r) => {
                            const repoName = r.full_name || r.name || "repo";
                            const shortName = r.full_name.includes("/") ? r.full_name.split("/")[1] : (r.name || r.full_name);
                            const repoJobs = (jobs || []).filter((j) => j.repo === repoName);
                            const hasAction = repoJobs.some((j) => j.status === "PLAN_REVIEW");
                            const isRunning = repoJobs.some((j) => j.status === "LEASED" || j.status === "RUNNING");
                            const prCount = repoJobs.filter((j) => j.pr_urls && j.pr_urls.length > 0).length;

                            return (
                              <Link
                                key={repoName}
                                href={`/composer?repo=${encodeURIComponent(repoName)}`}
                                className="w-full flex items-center justify-between p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-900 transition-all text-left group"
                                title={`Create task for ${repoName}`}
                              >
                                <span className="flex items-center gap-1.5 min-w-0 pr-1 truncate">
                                  <Folder className="w-3.5 h-3.5 text-stone-400 group-hover:text-stone-700 shrink-0 transition-colors" />
                                  <span className="truncate text-stone-700 group-hover:text-stone-900 font-medium">{shortName}</span>
                                </span>
                                {hasAction ? (
                                  <span className="h-4.5 px-1.5 rounded-full text-[9px] font-mono font-bold text-rose-700 bg-rose-50 border border-rose-200 inline-flex items-center gap-1 shrink-0 whitespace-nowrap leading-none">
                                    <span className="w-1.5 h-1.5 rounded-full bg-rose-500" /> action
                                  </span>
                                ) : isRunning ? (
                                  <span className="h-4.5 px-1.5 rounded-full text-[9px] font-mono font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 inline-flex items-center gap-1 shrink-0 whitespace-nowrap leading-none">
                                    <span className="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse" /> run
                                  </span>
                                ) : prCount > 0 ? (
                                  <span className="h-4.5 px-1.5 rounded-full text-[9px] font-mono font-bold text-purple-700 bg-purple-50 border border-purple-200 inline-flex items-center gap-1 shrink-0 whitespace-nowrap leading-none">
                                    <GitPullRequest className="w-2.5 h-2.5 text-purple-600 shrink-0" />
                                    <span>{prCount} PR</span>
                                  </span>
                                ) : (
                                  <span className="text-[9px] font-mono text-stone-400 shrink-0 whitespace-nowrap px-1">idle</span>
                                )}
                              </Link>
                            );
                          })}
                        </div>
                      );
                    })
                  )}
                </div>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-3 pt-2">
                <Folder className="w-4 h-4 text-stone-400" />
              </div>
            )}
          </div>

          {/* Spacer to keep middle half spacious for future features */}
          <div className="flex-1 min-h-0" />

          {/* COMPUTE QUOTA & UPGRADE CARD */}
          {!primaryCollapsed && (
            <div className="shrink-0 my-2 p-3 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-2.5 text-xs w-full">
              {/* Row 1: Header */}
              <div className="flex items-center justify-between gap-1">
                <div className="flex items-center gap-1.5 min-w-0">
                  <Zap className="w-3.5 h-3.5 text-amber-500 fill-current shrink-0" />
                  <span className="font-bold text-xs text-stone-900 truncate">Compute Quota</span>
                </div>
                <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-600 bg-sand-100 px-1.5 py-0.5 rounded border border-sand-200/80 shrink-0">
                  {plan}
                </span>
              </div>

              {/* Row 2: Minutes counter and percentage used */}
              <div className="flex items-baseline justify-between font-mono">
                <div className="flex items-baseline gap-1">
                  <span className="text-sm font-bold text-stone-900 leading-none">
                    {usedMinutes.toFixed(1)}
                  </span>
                  <span className="text-[11px] text-stone-400 font-normal">
                    / {hasMinutesCap ? `${limitMinutes}m` : "Unlimited"}
                  </span>
                </div>
                <span
                  className={`text-[11px] font-medium ${
                    percentUsed >= 90
                      ? "text-rose-600 font-bold"
                      : percentUsed >= 75
                      ? "text-amber-700 font-semibold"
                      : "text-stone-500"
                  }`}
                >
                  {percentUsed}% used
                </span>
              </div>

              {/* Row 3: Progress Bar */}
              <div className="w-full h-1.5 bg-sand-200/80 rounded-full overflow-hidden">
                <div
                  className={`h-full rounded-full transition-all duration-500 ${
                    percentUsed >= 90
                      ? "bg-rose-500"
                      : percentUsed >= 75
                      ? "bg-amber-500"
                      : "bg-kiwi-500"
                  }`}
                  style={{ width: `${Math.min(100, Math.max(percentUsed, usedMinutes > 0 ? 2 : 0))}%` }}
                />
              </div>

              {/* Row 4: Full-width Upgrade / Enterprise Button */}
              <div className="pt-0.5">
                <UpgradeButton plan={plan} variant="full" className="w-full justify-center py-1.5 text-[11px] font-bold rounded-xl" />
              </div>
            </div>
          )}

          {/* PINNED USER PROFILE (Single Logout Action) */}
          <div className="shrink-0 pt-2 border-t border-sand-200 w-full">
            {primaryCollapsed ? (
              <div className="flex flex-col items-center gap-1.5 w-full">
                <Link
                  href="/settings"
                  className="w-8 h-8 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 shadow-2xs flex items-center justify-center transition-all shrink-0 cursor-pointer group"
                  title={`${org?.name || org?.email || org?.user_email || org?.org_name || "Kiwi User"} (${plan} Tier)`}
                >
                  <UserAvatar
                    email={org?.email || org?.user_email}
                    name={org?.name || org?.org_name}
                    avatarUrl={org?.avatar_url}
                    githubLogin={org?.github_login}
                    className="w-5.5 h-5.5"
                  />
                </Link>
                <button
                  onClick={handleLogout}
                  className="p-1 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-all cursor-pointer"
                  title="Sign Out"
                >
                  <LogOut className="w-3.5 h-3.5" />
                </button>
              </div>
            ) : (
              <div className="flex items-center justify-between p-1.5 rounded-xl bg-white border border-sand-200 shadow-2xs w-full">
                <div className="flex items-center gap-2 min-w-0">
                  <UserAvatar
                    email={org?.email || org?.user_email}
                    name={org?.name || org?.org_name}
                    avatarUrl={org?.avatar_url}
                    githubLogin={org?.github_login}
                    className="w-6 h-6"
                  />
                  <div className="min-w-0">
                    <p className="text-[11px] font-bold text-stone-800 truncate leading-tight">
                      {org?.name || (org?.email || org?.user_email ? (org.email || org.user_email)!.split("@")[0] : org?.org_name || "Kiwi User")}
                    </p>
                    <p className="text-[9px] text-stone-400 font-mono leading-none mt-0.5 capitalize">{plan} Tier</p>
                  </div>
                </div>
                <button onClick={handleLogout} className="p-1 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-all cursor-pointer" title="Sign Out">
                  <LogOut className="w-3.5 h-3.5" />
                </button>
              </div>
            )}
          </div>
        </aside>

        {/* COLUMN 2: PINNED SECONDARY CATEGORY SUB-RAIL (~176px) */}
        <aside className="w-44 py-1.5 hidden md:flex flex-col shrink-0 text-xs select-none h-full overflow-hidden transition-all duration-200">
          <div className="flex-1 overflow-y-auto space-y-3.5 pr-1 min-h-0">
            
            {/* Group 0: ACTION REQUIRED (Only show when > 0) */}
            {needsAttentionCount > 0 && (
              <div className="p-2 rounded-2xl bg-amber-50/70 border border-amber-200 shadow-2xs space-y-1 animate-in fade-in duration-200">
                <div className="text-[9px] font-mono font-bold uppercase tracking-wider text-amber-900 px-1 flex items-center justify-between">
                  <span className="flex items-center gap-1 text-amber-800">
                    <span className="w-1.5 h-1.5 rounded-full bg-rose-500 animate-pulse" />
                    <span>Needs Attention</span>
                  </span>
                  <span className="bg-amber-800 text-white text-[9px] px-1.5 py-0.2 rounded-full font-bold">{needsAttentionCount}</span>
                </div>

                <div className="space-y-0.5 pt-0.5">
                  <Link
                    href="/?filter=plan"
                    className="w-full flex items-center justify-between px-2 py-1 rounded-xl text-[11px] font-bold text-indigo-950 hover:bg-indigo-100/80 transition-all text-left group"
                  >
                    <span className="flex items-center gap-1.5 min-w-0 pr-1 truncate">
                      <span className="relative flex h-2 w-2 shrink-0">
                        <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75" />
                        <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-600" />
                      </span>
                      <span className="truncate">Plan Reviews</span>
                    </span>
                    <span className="text-[10px] font-mono font-bold text-indigo-800 bg-indigo-100 border border-indigo-200 px-1.5 py-0.2 rounded-full shrink-0 whitespace-nowrap">{planReviewsCount}</span>
                  </Link>
                </div>
              </div>
            )}

            {/* Group 1: Tasks & Pipelines */}
            <div>
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1">Tasks & Pipelines</div>
              <div className="space-y-0.5">
                <Link
                  href="/"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-semibold transition-all text-left ${
                    pathname === "/" ? "bg-sand-200/90 text-stone-900 shadow-2xs" : "text-stone-600 hover:bg-sand-150"
                  }`}
                >
                  <span className="flex items-center gap-1.5 min-w-0 pr-1">
                    <LayoutGrid className="w-3.5 h-3.5 text-stone-700 shrink-0" />
                    <span>Tasks</span>
                  </span>
                  <span className="text-[9px] font-bold font-mono bg-white text-stone-600 px-1.5 py-0.5 rounded-md border border-sand-200 shrink-0 whitespace-nowrap">
                    {activeTasksCount} {activeTasksCount === 1 ? "task" : "tasks"}
                  </span>
                </Link>

                <Link
                  href="/composer"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium transition-all text-left ${
                    pathname === "/composer" ? "bg-sand-200/90 text-stone-900 shadow-2xs" : "text-stone-600 hover:bg-sand-150"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Sparkles className="w-3.5 h-3.5 text-kiwi-700 shrink-0" />
                    <span className="truncate">Create Task</span>
                  </span>
                </Link>
              </div>
            </div>

            {/* Group 2: Quality & Health */}
            <div>
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1">Quality & Health</div>
              <div className="space-y-0.5">
                <Link
                  href="/monitors"
                  data-tour="nav-monitors"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/monitors" || pathname.startsWith("/monitors/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Radar className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/monitors") ? "text-sky-700" : "text-sky-600"}`} />
                    <span className="truncate">PR Watchdogs</span>
                  </span>
                </Link>
                <Link
                  href="/activity"
                  className={`w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/activity" || pathname.startsWith("/activity/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <Activity className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/activity") ? "text-stone-900" : "text-stone-400"}`} />
                  <span className="truncate">Activity Log</span>
                </Link>
                <Link
                  href="/records"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/records" || pathname.startsWith("/records/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <ShieldCheck className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/records") ? "text-emerald-700" : "text-emerald-600"}`} />
                    <span className="truncate">Audit Receipts</span>
                  </span>
                </Link>
                <Link
                  href="/metrics"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/metrics" || pathname.startsWith("/metrics/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <LineChart className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/metrics") ? "text-indigo-700" : "text-indigo-600"}`} />
                    <span className="truncate">Telemetry &amp; SLOs</span>
                  </span>
                </Link>
              </div>
            </div>

            {/* Group 3: Compute & Cost */}
            <div>
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1">Compute & Costs</div>
              <div className="space-y-0.5">
                <Link
                  href="/spend"
                  data-tour="nav-spend"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/spend" || pathname.startsWith("/spend/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Receipt className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/spend") ? "text-stone-900" : "text-stone-400"}`} />
                    <span className="truncate">Cost & Usage</span>
                  </span>
                </Link>
                <Link
                  href="/fleet"
                  data-tour="nav-fleet"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/fleet" || pathname.startsWith("/fleet/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 min-w-0 pr-1 truncate">
                    <Server className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/fleet") ? "text-stone-900" : "text-stone-400"}`} />
                    <span className="truncate">Runners & Fleets</span>
                  </span>
                  <span className="text-[9px] font-mono text-stone-400 font-bold shrink-0 whitespace-nowrap">{runnersCount}</span>
                </Link>
                <Link
                  href="/models"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/models" || pathname.startsWith("/models/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Cpu className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/models") ? "text-stone-900" : "text-stone-400"}`} />
                    <span className="truncate">Models</span>
                  </span>
                </Link>
              </div>
            </div>

            {/* Group 4: Settings */}
            <div>
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1">Settings</div>
              <div className="space-y-0.5">
                <Link
                  href="/integrations"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left group ${
                    pathname === "/integrations" || pathname.startsWith("/integrations/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 min-w-0 pr-1">
                    <Link2 className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/integrations") ? "text-stone-900" : "text-stone-400"}`} />
                    <span className="truncate">Integrations</span>
                  </span>

                  {/* Horizontally stacked micro-avatars that slightly expand on hover while staying stacked */}
                  <div className="flex items-center -space-x-2 group-hover:-space-x-1 transition-all duration-200 ease-out shrink-0">
                    <div
                      title="GitHub"
                      className="w-4 h-4 rounded-full bg-white ring-1 ring-sand-300 shadow-2xs flex items-center justify-center p-0.5 shrink-0 transition-transform duration-150 group-hover:scale-105"
                    >
                      <SiGithub className="w-2.5 h-2.5 text-stone-900" />
                    </div>
                    <div
                      title="Datadog"
                      className="w-4 h-4 rounded-full bg-white ring-1 ring-sand-300 shadow-2xs flex items-center justify-center p-0.5 shrink-0 transition-transform duration-150 group-hover:scale-105"
                    >
                      <SiDatadog className="w-2.5 h-2.5 text-[#632CA6]" />
                    </div>
                    <div
                      title="Slack"
                      className="w-4 h-4 rounded-full bg-white ring-1 ring-sand-300 shadow-2xs flex items-center justify-center p-0.5 shrink-0 transition-transform duration-150 group-hover:scale-105"
                    >
                      <FaSlack className="w-2.5 h-2.5 text-[#ECB22E]" />
                    </div>
                    <div
                      title="Prometheus"
                      className="w-4 h-4 rounded-full bg-white ring-1 ring-sand-300 shadow-2xs flex items-center justify-center p-0.5 shrink-0 transition-transform duration-150 group-hover:scale-105"
                    >
                      <SiPrometheus className="w-2.5 h-2.5 text-[#E6522C]" />
                    </div>
                  </div>
                </Link>
                <Link
                  href="/team"
                  className={`w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/team" || pathname.startsWith("/team/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <Users className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/team") ? "text-stone-900" : "text-stone-400"}`} />
                  <span className="truncate">Team Members</span>
                </Link>
                <Link
                  href="/settings"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl transition-all text-left ${
                    pathname === "/settings" || pathname.startsWith("/settings/")
                      ? "bg-sand-200/90 text-stone-900 shadow-2xs font-semibold"
                      : "text-stone-600 hover:bg-sand-150 font-medium"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <CreditCard className={`w-3.5 h-3.5 shrink-0 ${pathname.startsWith("/settings") ? "text-stone-900" : "text-stone-400"}`} />
                    <span className="truncate">Plans & Billing</span>
                  </span>
                  {plan === "free" && (
                    <span className="text-[9px] font-mono font-bold text-amber-800 bg-amber-100 px-1 rounded">Upgrade</span>
                  )}
                </Link>
              </div>
            </div>

            {/* Group 5: Staff Super Admin Console */}
            {isSuperAdmin && (
              <div className="pt-2">
                <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1 flex items-center justify-between">
                  <span>Kiwi Staff</span>
                  <span className="text-[8px] font-mono font-bold bg-rose-100 text-rose-800 px-1 py-0.2 rounded border border-rose-200">SUPER ADMIN</span>
                </div>
                <div className="space-y-0.5">
                  <Link
                    href="/admin"
                    className={`w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl font-medium transition-all text-left ${
                      pathname === "/admin" || pathname.startsWith("/admin/")
                        ? "bg-rose-100 text-rose-900 border border-rose-200 shadow-2xs font-semibold"
                        : "text-stone-600 hover:bg-sand-150"
                    }`}
                  >
                    <ShieldAlert className="w-3.5 h-3.5 text-rose-600 shrink-0" />
                    <span className="truncate font-semibold text-rose-950">Super Admin</span>
                  </Link>
                </div>
              </div>
            )}

          </div>
        </aside>

        {/* COLUMN 3: MAIN CONTENT SHEET */}
        <main className="flex-1 floating-island p-3.5 sm:p-5 md:p-6 overflow-y-auto overflow-x-hidden h-full min-h-0 rounded-xl sm:rounded-2xl">
          {children}
        </main>
      </div>

      {/* UNIVERSAL FUZZY COMMAND PALETTE (⌘K) */}
      {showCommandPalette && (
        <div
          className="fixed inset-0 z-50 flex items-start justify-center pt-20 sm:pt-24 bg-stone-900/40 backdrop-blur-xs p-4 animate-in fade-in duration-150"
          onClick={() => setShowCommandPalette(false)}
        >
          <div
            className="w-full max-w-xl bg-white border border-sand-200 rounded-2xl shadow-popover overflow-hidden animate-in zoom-in-95 duration-150 flex flex-col max-h-[520px]"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Input Bar */}
            <div className="p-3.5 border-b border-sand-200 flex items-center gap-3 bg-sand-50/40">
              <Search className="w-4 h-4 text-stone-400 shrink-0" />
              <input
                type="text"
                autoFocus
                value={cmdQuery}
                onChange={(e) => setCmdQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "ArrowDown") {
                    e.preventDefault();
                    if (searchResults.length > 0) {
                      setSelectedIndex((prev) => (prev + 1) % searchResults.length);
                    }
                  } else if (e.key === "ArrowUp") {
                    e.preventDefault();
                    if (searchResults.length > 0) {
                      setSelectedIndex((prev) => (prev - 1 + searchResults.length) % searchResults.length);
                    }
                  } else if (e.key === "Enter") {
                    e.preventDefault();
                    const activeItem = searchResults[selectedIndex];
                    if (activeItem) {
                      setShowCommandPalette(false);
                      if (activeItem.action) {
                        activeItem.action();
                      } else if (activeItem.href) {
                        router.push(activeItem.href);
                      }
                    }
                  } else if (e.key === "Escape") {
                    setShowCommandPalette(false);
                  }
                }}
                placeholder="Search everything: tasks, repos, integrations, tools, users..."
                className="w-full text-sm font-medium bg-transparent outline-none placeholder:text-stone-400 font-sans"
              />
              <span className="text-[10px] font-mono bg-sand-150 px-2 py-0.5 rounded text-stone-500 font-bold">ESC</span>
            </div>

            {/* Results List */}
            <div className="overflow-y-auto p-2 space-y-1 text-xs no-scrollbar flex-1">
              {searchResults.length === 0 ? (
                <div className="py-12 text-center text-stone-400 font-mono text-xs space-y-1">
                  <div>No matching resources or commands for &ldquo;{cmdQuery}&rdquo;</div>
                  <div className="text-[11px] text-stone-400">Try searching for a repo, job prompt, tool, or integration</div>
                </div>
              ) : (
                searchResults.map((item, idx) => {
                  const isSelected = idx === selectedIndex;
                  return (
                    <div
                      key={item.id}
                      onMouseEnter={() => setSelectedIndex(idx)}
                      onClick={() => {
                        setShowCommandPalette(false);
                        if (item.action) {
                          item.action();
                        } else if (item.href) {
                          router.push(item.href);
                        }
                      }}
                      className={`w-full flex items-center justify-between p-2.5 rounded-xl cursor-pointer transition-all text-left ${
                        isSelected
                          ? "bg-sand-200/90 text-stone-950 shadow-2xs font-semibold"
                          : "hover:bg-sand-100 text-stone-800"
                      }`}
                    >
                      <div className="flex items-center gap-3 min-w-0">
                        <div className={`p-1.5 rounded-lg border flex items-center justify-center shrink-0 ${
                          isSelected ? "bg-white border-sand-300" : "bg-sand-50 border-sand-200"
                        }`}>
                          {item.icon}
                        </div>

                        <div className="min-w-0">
                          <div className="flex items-center gap-2">
                            <span className="font-semibold text-stone-900 truncate text-xs">{item.title}</span>
                            <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-600 bg-sand-100 px-1.5 py-0.2 rounded border border-sand-200">
                              {item.category}
                            </span>
                          </div>
                          {item.subtitle && (
                            <p className="text-[11px] text-stone-500 font-normal truncate mt-0.5 font-mono">
                              {item.subtitle}
                            </p>
                          )}
                        </div>
                      </div>

                      {item.hint && (
                        <span className="text-[10px] font-mono text-stone-400 shrink-0 ml-2 font-semibold">
                          {item.hint}
                        </span>
                      )}
                    </div>
                  );
                })
              )}
            </div>

            {/* Footer Tip */}
            <div className="px-3.5 py-2 border-t border-sand-150 bg-sand-50/70 text-[10px] font-mono text-stone-500 flex items-center justify-between">
              <span>Super-fuzzy search across tasks, integrations &amp; repos</span>
              <span>↑↓ to navigate • ↵ to select</span>
            </div>
          </div>
        </div>
      )}

      {/* ================= MOBILE SLIDE-OVER NAVIGATION DRAWER ================= */}
      {showMobileMenu && (
        <>
          <div
            className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs z-50 md:hidden transition-opacity animate-in fade-in duration-150"
            onClick={() => setShowMobileMenu(false)}
            aria-hidden="true"
          />
          <div className="fixed inset-y-0 left-0 w-72 max-w-[85vw] bg-[#F8F7F4] bg-dot-grid border-r border-sand-200 shadow-popover z-50 md:hidden flex flex-col p-4 space-y-4 animate-in slide-in-from-left duration-200">
            {/* Mobile Header */}
            <div className="flex items-center justify-between pb-3 border-b border-sand-200">
              <div className="flex items-center gap-2">
                <div className="w-7 h-7 rounded-xl bg-white border border-sand-200 flex items-center justify-center shadow-2xs">
                  <Logo variant="full-color" className="w-4.5 h-4.5" />
                </div>
                <div>
                  <p className="font-bold text-stone-900 text-xs">Kiwi Platform</p>
                  <p className="text-[10px] font-mono text-stone-400 capitalize">{plan} Plan</p>
                </div>
              </div>
              <button
                onClick={() => setShowMobileMenu(false)}
                className="p-1.5 rounded-lg text-stone-400 hover:text-stone-800 hover:bg-sand-150 transition-all cursor-pointer"
                title="Close Navigation Menu"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Quick Action: New Task */}
            <Link
              href="/composer"
              onClick={() => setShowMobileMenu(false)}
              className="w-full flex items-center justify-center gap-2 py-2 px-3 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all"
            >
              <Plus className="w-3.5 h-3.5 text-kiwi-400" />
              <span>Assign New Task</span>
            </Link>

            {/* Navigation Links */}
            <div className="flex-1 overflow-y-auto space-y-4 pr-1 text-xs no-scrollbar">
              <div className="space-y-1">
                <div className="text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold px-2">Navigation</div>
                <Link
                  href="/"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname === "/" ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <LayoutGrid className="w-4 h-4 text-stone-600" />
                  <span>Tasks Dashboard</span>
                </Link>
                <Link
                  href="/monitors"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/monitors") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <Radar className="w-4 h-4 text-stone-600" />
                  <span>PR Watchdogs</span>
                </Link>
                <Link
                  href="/fleet"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/fleet") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <Server className="w-4 h-4 text-stone-600" />
                  <span>Runners &amp; Fleets</span>
                </Link>
                <Link
                  href="/spend"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/spend") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <Receipt className="w-4 h-4 text-stone-600" />
                  <span>Cost &amp; Usage</span>
                </Link>
                <Link
                  href="/records"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/records") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <ShieldCheck className="w-4 h-4 text-emerald-600" />
                  <span>Audit Receipts</span>
                </Link>
                <Link
                  href="/metrics"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/metrics") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <LineChart className="w-4 h-4 text-indigo-600" />
                  <span>Telemetry &amp; SLOs</span>
                </Link>
              </div>

              <div className="space-y-1 pt-2 border-t border-sand-200">
                <div className="text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold px-2">Settings &amp; Org</div>
                <Link
                  href="/integrations"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/integrations") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <Link2 className="w-4 h-4 text-stone-600" />
                  <span>Integrations</span>
                </Link>
                <Link
                  href="/team"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/team") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <Users className="w-4 h-4 text-stone-600" />
                  <span>Team Members</span>
                </Link>
                <Link
                  href="/settings"
                  onClick={() => setShowMobileMenu(false)}
                  className={`flex items-center gap-2 px-2.5 py-2 rounded-xl transition-all ${
                    pathname.startsWith("/settings") ? "bg-sand-200 text-stone-900 font-bold shadow-2xs" : "text-stone-700 hover:bg-sand-150"
                  }`}
                >
                  <CreditCard className="w-4 h-4 text-stone-600" />
                  <span>Plans &amp; Billing</span>
                </Link>
                {isSuperAdmin && (
                  <Link
                    href="/admin"
                    onClick={() => setShowMobileMenu(false)}
                    className="flex items-center gap-2 px-2.5 py-2 rounded-xl bg-rose-50 text-rose-900 border border-rose-200 font-semibold"
                  >
                    <ShieldAlert className="w-4 h-4 text-rose-600" />
                    <span>Super Admin</span>
                  </Link>
                )}
              </div>
            </div>

            {/* Mobile Footer: User Avatar & Logout */}
            <div className="pt-3 border-t border-sand-200 flex items-center justify-between">
              <div className="flex items-center gap-2 min-w-0">
                <UserAvatar
                  email={org?.email || org?.user_email}
                  name={org?.name || org?.org_name}
                  avatarUrl={org?.avatar_url}
                  githubLogin={org?.github_login}
                  className="w-7 h-7"
                />
                <div className="min-w-0">
                  <p className="text-xs font-bold text-stone-900 truncate">
                    {org?.name || org?.email || org?.user_email || "Kiwi User"}
                  </p>
                  <p className="text-[10px] text-stone-400 font-mono capitalize">{plan} Tier</p>
                </div>
              </div>
              <button
                onClick={handleLogout}
                className="p-1.5 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-all cursor-pointer"
                title="Sign Out"
              >
                <LogOut className="w-4 h-4" />
              </button>
            </div>
          </div>
        </>
      )}

      {/* Guided Interactive Platform Tour */}
      <PlatformTour />
    </div>
  );
}
