"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import {
  LayoutGrid,
  Radar,
  Receipt,
  ShieldAlert,
  Sliders,
  Zap,
  Building2,
  Folder,
  FolderOpen,
  Plus,
  Search,
  LogOut,
  PanelLeft,
  ChevronLeft,
  ChevronRight,
  GitPullRequest,
  Activity,
  CheckCircle2,
  Sparkles,
  Layers,
  Cpu,
  Link2,
  Users,
  CreditCard,
  RotateCcw,
  Play,
  ShieldCheck,
  Server,
} from "lucide-react";
import { api, type UsageResponse, type ValidateResponse, type GithubRepo } from "@/lib/api";
import { CustomLoadersStudio } from "@/components/CustomLoadersStudio";
import { useFleetStore } from "@/store/useFleetStore";

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const router = useRouter();
  const { jobs, daemons, loadJobs, loadDaemons } = useFleetStore();

  const [isSuperAdmin, setIsSuperAdmin] = useState(false);
  const [org, setOrg] = useState<ValidateResponse | null>(null);
  const [usage, setUsage] = useState<UsageResponse | null>(null);
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [primaryCollapsed, setPrimaryCollapsed] = useState(false);
  const [simulatedPlan, setSimulatedPlan] = useState<"free" | "pro" | "enterprise">("free");
  const [showLoadersModal, setShowLoadersModal] = useState(false);
  const [showCommandPalette, setShowCommandPalette] = useState(false);
  const [showPlanModal, setShowPlanModal] = useState(false);
  const [cmdQuery, setCmdQuery] = useState("");

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
        setShowLoadersModal(false);
        setShowPlanModal(false);
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  useEffect(() => {
    api.validate()
      .then((v) => {
        setOrg(v);
        if (v.plan === "pro" || v.plan === "enterprise") {
          setSimulatedPlan(v.plan as any);
        }
      })
      .catch(() => {});

    api.getUsage()
      .then((u) => {
        setUsage(u);
        setIsSuperAdmin(Boolean(u.is_super_admin));
      })
      .catch(() => {});

    api.listGithubRepos()
      .then((r) => {
        setRepos(r.repos || []);
      })
      .catch(() => {});
  }, []);

  const handleLogout = () => {
    if (typeof window !== "undefined") {
      localStorage.removeItem("kiwi_token");
      router.push("/login");
    }
  };

  const plan = simulatedPlan;
  const usedMinutes = usage?.agent_minutes_used ?? 0;
  const limitMinutes = usage?.agent_minutes_limit ?? 500;
  const percentUsed = limitMinutes > 0 ? Math.min(100, Math.round((usedMinutes / limitMinutes) * 100)) : 0;

  const planReviewsCount = (jobs || []).filter((j) => j.status === "PLAN_REVIEW" || j.status === "AWAITING_PLAN_APPROVAL" || j.requires_plan_approval).length;
  const awaitingInputCount = (jobs || []).filter((j) => j.status === "WAITING_USER").length;
  const needsAttentionCount = planReviewsCount + awaitingInputCount;
  const activeTasksCount = (jobs || []).filter((j) => j.status === "LEASED" || j.status === "RUNNING").length;
  const runnersCount = (daemons || []).length;

  return (
    <div className="h-screen max-h-screen overflow-hidden p-3 md:p-4 flex flex-col font-sans bg-[#F4F3EE] text-stone-900 selection:bg-kiwi-200">
      
      {/* ================= TOP FIXED NAVBAR & SIMULATION BAR ================= */}
      <header className="shrink-0 mb-2.5 px-3 py-1.5 flex flex-wrap items-center justify-between gap-3 text-xs bg-sand-50/70 backdrop-blur-md rounded-2xl border border-sand-200/70 shadow-2xs z-30">
        <div className="flex items-center gap-2.5">
          <button
            onClick={() => setPrimaryCollapsed(!primaryCollapsed)}
            className="p-1 rounded-lg hover:bg-sand-150 text-stone-600 hover:text-stone-900 transition-all flex items-center gap-1 font-mono text-[11px]"
            title="Toggle Left Sidebar (⌘B)"
          >
            <PanelLeft className="w-4 h-4 text-stone-700" />
            <span className="hidden sm:inline text-stone-500 font-medium">Sidebar</span>
          </button>

          <div className="flex items-center gap-2 px-2.5 py-0.5 rounded-full bg-white border border-sand-200 text-stone-700 font-mono font-semibold text-[11px] shadow-2xs">
            <span className="w-2 h-2 rounded-full bg-kiwi-400 inline-block badge-pulse" />
            <span>KIWI PLATFORM</span>
          </div>

          <span className="text-stone-500 hidden md:inline font-medium">
            Org: <strong className="text-stone-800">{org?.org_name || "Acme Global"}</strong> • Active Plan:{" "}
            <span className="font-bold text-stone-800 bg-sand-200 px-1.5 py-0.5 rounded text-[10px] font-mono uppercase">
              {plan} PLAN
            </span>
          </span>
        </div>

        {/* Live Tier Switcher & Simulation Controls */}
        <div className="flex items-center gap-2">
          <div className="flex items-center bg-white rounded-xl p-0.5 border border-sand-200 shadow-2xs text-[11px]">
            <span className="text-[10px] font-mono uppercase text-stone-400 font-bold px-2">Simulate Plan:</span>
            <button
              onClick={() => setSimulatedPlan("free")}
              className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                simulatedPlan === "free" ? "bg-stone-900 text-white" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              Free
            </button>
            <button
              onClick={() => setSimulatedPlan("pro")}
              className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                simulatedPlan === "pro" ? "bg-stone-900 text-white" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              Pro
            </button>
            <button
              onClick={() => setSimulatedPlan("enterprise")}
              className={`px-2.5 py-1 rounded-lg font-semibold transition-all ${
                simulatedPlan === "enterprise" ? "bg-stone-900 text-white" : "text-stone-600 hover:text-stone-900"
              }`}
            >
              Enterprise
            </button>
          </div>

          <button
            onClick={() => router.push("/?simulate=true")}
            className="flex items-center gap-1.5 px-3 py-1 rounded-xl bg-white hover:bg-sand-50 border border-sand-200 text-[11px] font-medium text-stone-700 shadow-2xs transition-all"
          >
            <Play className="w-3.5 h-3.5 text-emerald-600" />
            <span>Simulate Stream</span>
          </button>

          <button
            onClick={() => setShowLoadersModal(true)}
            className="flex items-center gap-1.5 px-3 py-1 rounded-xl bg-kiwi-50 hover:bg-kiwi-100 border border-kiwi-300 text-kiwi-900 text-[11px] font-bold shadow-2xs transition-all"
            title="View & Test Bespoke Loaders Design Suite"
          >
            <span>✦ Custom Loaders Studio</span>
          </button>

          <button
            onClick={handleLogout}
            className="flex items-center gap-1.5 px-2.5 py-1 rounded-xl bg-white hover:bg-rose-50 hover:text-rose-700 border border-sand-200 text-stone-600 text-[11px] font-medium shadow-2xs transition-all"
            title="Sign Out of Kiwi"
          >
            <LogOut className="w-3.5 h-3.5 text-rose-500" />
            <span>Sign Out</span>
          </button>

          <button
            onClick={() => window.location.reload()}
            className="p-1.5 rounded-xl bg-white hover:bg-sand-50 border border-sand-200 text-stone-500 hover:text-stone-900 shadow-2xs transition-all"
            title="Reset Demo Data"
          >
            <RotateCcw className="w-3.5 h-3.5" />
          </button>
        </div>
      </header>

      {/* ================= DUAL-ISLAND SIDEBARS & SCROLLABLE MAIN CONTENT ================= */}
      <div className="flex-1 flex gap-3 overflow-hidden min-h-0">
        
        {/* COLUMN 1: COLLAPSIBLE PRIMARY WORKSPACE & REPO ISLAND (~185px Expanded / 54px Collapsed) */}
        <aside
          className={`island-sidebar p-3 flex flex-col shrink-0 select-none shadow-island relative h-full overflow-hidden transition-all duration-200 ${
            primaryCollapsed ? "w-14 items-center" : "w-48"
          }`}
        >
          {/* Brand & Org Header */}
          <div className="shrink-0 flex items-center justify-between px-1 mb-3 w-full">
            <div className="flex items-center gap-2 min-w-0">
              <div className="w-7 h-7 rounded-lg bg-kiwi-100 border border-kiwi-200 flex items-center justify-center font-bold text-kiwi-700 text-sm shrink-0 shadow-2xs">
                🥝
              </div>
              {!primaryCollapsed && (
                <div className="min-w-0">
                  <div className="flex items-center gap-1">
                    <span className="font-bold text-stone-900 text-xs tracking-tight">Kiwi</span>
                    <span className="text-[9px] font-mono font-bold bg-amber-100 text-amber-800 px-1 py-0.2 rounded border border-amber-200 uppercase">
                      {plan}
                    </span>
                  </div>
                  <p className="text-[10px] text-stone-500 font-medium truncate">{org?.org_name || "Acme Global"}</p>
                </div>
              )}
            </div>

            <button
              onClick={() => setPrimaryCollapsed(!primaryCollapsed)}
              className="text-stone-400 hover:text-stone-700 p-1"
              title="Collapse / Expand Sidebar"
            >
              {primaryCollapsed ? <ChevronRight className="w-3.5 h-3.5" /> : <ChevronLeft className="w-3.5 h-3.5" />}
            </button>
          </div>

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
          <div className="shrink-0 mb-3 w-full">
            <Link
              href="/composer"
              className="w-full flex items-center justify-center gap-1.5 py-2 px-2.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all active:scale-[0.98]"
              title="Assign New Task"
            >
              <Plus className="w-3.5 h-3.5 stroke-[2.5] text-kiwi-400 shrink-0" />
              {!primaryCollapsed && <span>New Task</span>}
            </Link>
          </div>

          {/* Scrollable Workspace Folder Tree */}
          <div className="flex-1 overflow-y-auto space-y-3 px-0.5 text-xs min-h-0 w-full">
            {!primaryCollapsed ? (
              <div>
                <div className="text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold px-1 mb-1.5 flex items-center justify-between">
                  <span>Repositories</span>
                  <Link href="/integrations" className="text-kiwi-700 hover:underline normal-case text-[10px]">+ Add</Link>
                </div>
                <div className="space-y-0.5 text-stone-600">
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

                  {repos.map((r) => {
                    const repoName = r.full_name || r.name || "repo";
                    const repoJobs = (jobs || []).filter((j) => j.repo === repoName);
                    const hasAction = repoJobs.some((j) => j.status === "PLAN_REVIEW" || j.status === "WAITING_USER");
                    const isRunning = repoJobs.some((j) => j.status === "LEASED" || j.status === "RUNNING");
                    const prCount = repoJobs.filter((j) => j.pr_urls && j.pr_urls.length > 0).length;

                    return (
                      <Link
                        key={repoName}
                        href={`/?repo=${encodeURIComponent(repoName)}`}
                        className="w-full flex items-center justify-between p-1.5 rounded-lg hover:bg-sand-150 hover:text-stone-900 transition-all text-left"
                        title={repoName}
                      >
                        <span className="flex items-center gap-1.5 truncate">
                          <Folder className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                          <span className="truncate">{repoName.split("/").pop() || repoName}</span>
                        </span>
                        {hasAction ? (
                          <span className="text-[9px] font-mono font-bold text-rose-700 bg-rose-50 border border-rose-200 px-1.5 py-0.2 rounded-full flex items-center gap-1">
                            <span className="w-1 h-1 rounded-full bg-rose-500" /> action
                          </span>
                        ) : isRunning ? (
                          <span className="text-[9px] font-mono font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-1.5 py-0.2 rounded-full flex items-center gap-1">
                            <span className="w-1 h-1 rounded-full bg-emerald-500" /> run
                          </span>
                        ) : prCount > 0 ? (
                          <span className="text-[9px] font-mono font-bold text-purple-700 bg-purple-50 border border-purple-200 px-1.5 py-0.2 rounded-full flex items-center gap-1">
                            <span className="w-1 h-1 rounded-full bg-purple-500" /> {prCount} PR
                          </span>
                        ) : (
                          <span className="text-[9px] font-mono text-stone-400">idle</span>
                        )}
                      </Link>
                    );
                  })}
                </div>
              </div>
            ) : (
              <div className="flex flex-col items-center gap-3 pt-2">
                <Folder className="w-4 h-4 text-stone-400" />
              </div>
            )}
          </div>

          {/* COMPUTE QUOTA & UPGRADE CARD */}
          {!primaryCollapsed && (
            <div className="shrink-0 my-2 p-2.5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-2 text-xs w-full">
              <div className="flex items-center justify-between">
                <span className="font-bold text-[11px] text-stone-900 flex items-center gap-1">
                  <Zap className="w-3 h-3 text-amber-500 fill-current" />
                  Agent Compute
                </span>
                <span className="text-[9px] font-mono font-bold text-amber-800 bg-amber-50 border border-amber-200 px-1.5 py-0.2 rounded">
                  {usedMinutes} / {limitMinutes}m
                </span>
              </div>
              <div className="w-full h-1.5 bg-sand-200 rounded-full overflow-hidden">
                <div className="h-full bg-amber-500 rounded-full transition-all duration-500" style={{ width: `${percentUsed}%` }} />
              </div>
              <div className="flex items-center justify-between text-[10px] text-stone-500 font-mono">
                <span>{percentUsed}% consumed</span>
                <Link href="/spend" className="text-kiwi-700 font-bold hover:underline">+ Boost Tier</Link>
              </div>
            </div>
          )}

          {/* PINNED USER PROFILE & LOGOUT FOOTER */}
          <div className="shrink-0 pt-2.5 border-t border-sand-200 space-y-1.5 w-full">
            <div className="flex items-center justify-between p-1.5 rounded-xl bg-white border border-sand-200 shadow-2xs">
              <div className="flex items-center gap-2 min-w-0">
                <div className="w-6 h-6 rounded-full bg-stone-800 text-white font-bold flex items-center justify-center text-[10px] shrink-0 uppercase">
                  {org?.user_email ? org.user_email.slice(0, 2).toUpperCase() : org?.role ? org.role.slice(0, 2).toUpperCase() : "KW"}
                </div>
                {!primaryCollapsed && (
                  <div className="min-w-0">
                    <p className="text-[11px] font-bold text-stone-800 truncate leading-tight">
                      {org?.user_email ? org.user_email.split("@")[0] : org?.org_name || "Kiwi User"}
                    </p>
                    <p className="text-[9px] text-stone-400 font-mono leading-none mt-0.5 capitalize">{plan} Tier</p>
                  </div>
                )}
              </div>
              {!primaryCollapsed && (
                <button onClick={handleLogout} className="p-1 rounded-lg text-stone-400 hover:text-rose-600 hover:bg-rose-50 transition-all" title="Sign Out">
                  <LogOut className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {!primaryCollapsed && (
              <button
                onClick={handleLogout}
                className="w-full flex items-center justify-center gap-1.5 py-1.5 px-2 rounded-xl text-stone-500 hover:text-rose-700 hover:bg-rose-50/80 border border-sand-200/60 hover:border-rose-200 font-medium text-[11px] transition-all bg-white/40"
              >
                <LogOut className="w-3.5 h-3.5 text-rose-500" />
                <span>Sign Out</span>
              </button>
            )}
          </div>
        </aside>

        {/* COLUMN 2: PINNED SECONDARY CATEGORY SUB-RAIL (~165px) */}
        <aside className="w-44 py-1.5 hidden md:flex flex-col shrink-0 text-xs select-none h-full overflow-hidden transition-all duration-200">
          <div className="flex-1 overflow-y-auto space-y-3.5 pr-1 min-h-0">
            
            {/* Group 0: ACTION REQUIRED */}
            <div className="p-2 rounded-2xl bg-sand-50 border border-sand-200 shadow-2xs space-y-1">
              <div className="text-[9px] font-mono font-bold uppercase tracking-wider text-stone-500 px-1 flex items-center justify-between">
                <span className="flex items-center gap-1 text-stone-700">
                  <span className="w-1.5 h-1.5 rounded-full bg-rose-500" />
                  <span>Needs Attention</span>
                </span>
                <span className="bg-stone-900 text-white text-[9px] px-1.5 py-0.2 rounded-full font-bold">{needsAttentionCount}</span>
              </div>

              <div className="space-y-0.5 pt-0.5">
                <Link
                  href="/?filter=plan"
                  className="w-full flex items-center justify-between px-2 py-1 rounded-xl text-[11px] font-bold text-indigo-950 hover:bg-indigo-50/90 transition-all text-left group"
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <span className="relative flex h-2 w-2 shrink-0">
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-indigo-400 opacity-75" />
                      <span className="relative inline-flex rounded-full h-2 w-2 bg-indigo-600" />
                    </span>
                    <span className="truncate">Plan Reviews</span>
                  </span>
                  <span className="text-[10px] font-mono font-bold text-indigo-800 bg-indigo-100 border border-indigo-200 px-1.5 py-0.2 rounded-full">{planReviewsCount}</span>
                </Link>

                <Link
                  href="/?filter=waiting"
                  className="w-full flex items-center justify-between px-2 py-1 rounded-xl text-[11px] font-bold text-amber-950 hover:bg-amber-50/90 transition-all text-left group"
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <span className="relative flex h-2 w-2 shrink-0">
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75" />
                      <span className="relative inline-flex rounded-full h-2 w-2 bg-amber-600" />
                    </span>
                    <span className="truncate">Awaiting Input</span>
                  </span>
                  <span className="text-[10px] font-mono font-bold text-amber-800 bg-amber-100 border border-amber-200 px-1.5 py-0.2 rounded-full">{awaitingInputCount}</span>
                </Link>
              </div>
            </div>

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
                  <span className="flex items-center gap-1.5 truncate">
                    <LayoutGrid className="w-3.5 h-3.5 text-stone-700 shrink-0" />
                    <span className="truncate">Dashboard</span>
                  </span>
                  <span className="text-[9px] font-bold font-mono bg-white px-1.5 py-0.2 rounded-md border border-sand-200">{activeTasksCount} Active</span>
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
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium transition-all text-left ${
                    pathname === "/monitors" ? "bg-sand-200/90 text-stone-900 shadow-2xs" : "text-stone-600 hover:bg-sand-150"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Radar className="w-3.5 h-3.5 text-sky-600 shrink-0" />
                    <span className="truncate">PR Watchdogs</span>
                  </span>
                </Link>
                <Link href="/activity" className="w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <Activity className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                  <span className="truncate">Activity Log</span>
                </Link>
                <Link href="/records" className="w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <span className="flex items-center gap-1.5 truncate">
                    <ShieldCheck className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                    <span className="truncate">Audit Receipts</span>
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
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium transition-all text-left ${
                    pathname === "/spend" ? "bg-sand-200/90 text-stone-900 shadow-2xs" : "text-stone-600 hover:bg-sand-150"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <Receipt className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                    <span className="truncate">Cost & Usage</span>
                  </span>
                </Link>
                <Link href="/fleet" className="w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <span className="flex items-center gap-1.5 truncate">
                    <Server className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                    <span className="truncate">Private Runners</span>
                  </span>
                  <span className="text-[9px] font-mono text-stone-400 font-bold">{runnersCount}</span>
                </Link>
                <Link href="/models" className="w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <span className="flex items-center gap-1.5 truncate">
                    <Cpu className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                    <span className="truncate">Models</span>
                  </span>
                </Link>
              </div>
            </div>

            {/* Group 4: Settings */}
            <div>
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1">Settings</div>
              <div className="space-y-0.5">
                <Link href="/integrations" className="w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <span className="flex items-center gap-1.5 truncate">
                    <Link2 className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                    <span className="truncate">GitHub & Slack</span>
                  </span>
                  <span className="w-1.5 h-1.5 rounded-full bg-emerald-500" />
                </Link>
                <Link href="/team" className="w-full flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left">
                  <Users className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                  <span className="truncate">Team Members</span>
                </Link>
                <button
                  onClick={() => setShowPlanModal(true)}
                  className="w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium text-stone-600 hover:bg-sand-150 transition-all text-left cursor-pointer"
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <CreditCard className="w-3.5 h-3.5 text-stone-400 shrink-0" />
                    <span className="truncate">Plans & Billing</span>
                  </span>
                  <span className="text-[9px] font-mono font-bold text-amber-800 bg-amber-100 px-1 rounded">Upgrade</span>
                </button>
              </div>
            </div>

            {/* Group 5: Staff Super Admin Console */}
            <div className="pt-2">
              <div className="text-[10px] font-semibold text-stone-400 px-2 mb-1 flex items-center justify-between">
                <span>Kiwi Staff</span>
                <span className="text-[8px] font-mono font-bold bg-rose-100 text-rose-800 px-1 py-0.2 rounded border border-rose-200">SUPER ADMIN</span>
              </div>
              <div className="space-y-0.5">
                <Link
                  href="/admin"
                  className={`w-full flex items-center justify-between px-2.5 py-1.5 rounded-xl font-medium transition-all text-left ${
                    pathname === "/admin" ? "bg-rose-100 text-rose-900 border border-rose-200 shadow-2xs" : "text-stone-600 hover:bg-sand-150"
                  }`}
                >
                  <span className="flex items-center gap-1.5 truncate">
                    <ShieldAlert className="w-3.5 h-3.5 text-rose-600 shrink-0" />
                    <span className="truncate font-semibold text-rose-950">Super Admin</span>
                  </span>
                  <span className="text-[9px] font-mono text-rose-700 bg-rose-50 px-1.5 py-0.2 rounded font-bold">142 Orgs</span>
                </Link>
              </div>
            </div>

          </div>

          {/* Static Sign Out inside Sub-rail Footer */}
          <div className="shrink-0 pt-2 border-t border-sand-200/60 mt-1">
            <button
              onClick={handleLogout}
              className="w-full flex items-center gap-2 px-2.5 py-1.5 rounded-xl font-medium text-stone-500 hover:text-rose-700 hover:bg-rose-50/80 transition-all text-left cursor-pointer"
            >
              <LogOut className="w-3.5 h-3.5 text-rose-500" />
              <span>Log Out</span>
            </button>
          </div>
        </aside>

        {/* COLUMN 3: MAIN CONTENT SHEET */}
        <main className="flex-1 floating-island p-6 overflow-y-auto h-full min-h-0">
          {children}
        </main>
      </div>

      {/* UNIVERSAL COMMAND PALETTE (⌘K) */}
      {showCommandPalette && (
        <div className="fixed inset-0 z-50 flex items-start justify-center pt-24 bg-stone-900/40 backdrop-blur-xs p-4 animate-in fade-in duration-150">
          <div
            className="w-full max-w-xl bg-white border border-sand-200 rounded-2xl shadow-popover overflow-hidden animate-in zoom-in-95 duration-150"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="p-3 border-b border-sand-200 flex items-center gap-3">
              <Search className="w-4 h-4 text-stone-400 shrink-0" />
              <input
                type="text"
                autoFocus
                value={cmdQuery}
                onChange={(e) => setCmdQuery(e.target.value)}
                placeholder="Type a command, search tasks, or navigate..."
                className="w-full text-sm font-medium bg-transparent outline-none placeholder:text-stone-400"
              />
              <span className="text-[10px] font-mono bg-sand-150 px-1.5 py-0.5 rounded text-stone-500">ESC</span>
            </div>
            <div className="max-h-80 overflow-y-auto p-2 space-y-1 text-xs">
              <div className="px-2 py-1 text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold">Quick Navigation</div>
              <Link
                href="/composer"
                onClick={() => setShowCommandPalette(false)}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all"
              >
                <span className="flex items-center gap-2">
                  <Sparkles className="w-3.5 h-3.5 text-kiwi-600" />
                  <span className="font-medium">Create New Task</span>
                </span>
                <span className="text-[10px] font-mono text-stone-400">/composer</span>
              </Link>
              <Link
                href="/"
                onClick={() => setShowCommandPalette(false)}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all"
              >
                <span className="flex items-center gap-2">
                  <LayoutGrid className="w-3.5 h-3.5 text-stone-600" />
                  <span className="font-medium">Task Dashboard</span>
                </span>
                <span className="text-[10px] font-mono text-stone-400">/</span>
              </Link>
              <Link
                href="/monitors"
                onClick={() => setShowCommandPalette(false)}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all"
              >
                <span className="flex items-center gap-2">
                  <Radar className="w-3.5 h-3.5 text-sky-600" />
                  <span className="font-medium">PR Watchdogs</span>
                </span>
                <span className="text-[10px] font-mono text-stone-400">/monitors</span>
              </Link>
              <Link
                href="/spend"
                onClick={() => setShowCommandPalette(false)}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all"
              >
                <span className="flex items-center gap-2">
                  <Receipt className="w-3.5 h-3.5 text-stone-600" />
                  <span className="font-medium">Cost & Velocity Analytics</span>
                </span>
                <span className="text-[10px] font-mono text-stone-400">/spend</span>
              </Link>
              <Link
                href="/fleet"
                onClick={() => setShowCommandPalette(false)}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all"
              >
                <span className="flex items-center gap-2">
                  <Server className="w-3.5 h-3.5 text-stone-600" />
                  <span className="font-medium">Private Runners & Fleet</span>
                </span>
                <span className="text-[10px] font-mono text-stone-400">/fleet</span>
              </Link>
              <div className="px-2 py-1 text-[10px] font-mono uppercase tracking-wider text-stone-400 font-bold mt-2">Actions</div>
              <button
                onClick={() => {
                  setShowCommandPalette(false);
                  setShowPlanModal(true);
                }}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all text-left"
              >
                <span className="flex items-center gap-2">
                  <CreditCard className="w-3.5 h-3.5 text-amber-600" />
                  <span className="font-medium">Compare & Upgrade Plans</span>
                </span>
                <span className="text-[10px] font-mono text-amber-800 bg-amber-50 px-1.5 py-0.2 rounded border border-amber-200">Upgrade</span>
              </button>
              <button
                onClick={() => {
                  setShowCommandPalette(false);
                  setShowLoadersModal(true);
                }}
                className="w-full flex items-center justify-between p-2 rounded-xl hover:bg-sand-100 text-stone-800 transition-all text-left"
              >
                <span className="flex items-center gap-2">
                  <Sparkles className="w-3.5 h-3.5 text-kiwi-600" />
                  <span className="font-medium">Open Custom Loaders Studio</span>
                </span>
                <span className="text-[10px] font-mono text-kiwi-800 bg-kiwi-50 px-1.5 py-0.2 rounded border border-kiwi-200">Studio</span>
              </button>
            </div>
          </div>
        </div>
      )}

      {/* PLAN COMPARISON & UPGRADE MODAL */}
      {showPlanModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-stone-900/40 backdrop-blur-xs p-4 animate-in fade-in duration-150">
          <div className="w-full max-w-3xl bg-white border border-sand-200 rounded-3xl shadow-popover p-6 space-y-6 animate-in zoom-in-95 duration-150">
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-lg font-bold text-stone-900">Kiwi Platform Plans & Compute Tiers</h3>
                <p className="text-xs text-stone-500">Autonomous software engineering agents for high-velocity teams</p>
              </div>
              <button
                onClick={() => setShowPlanModal(false)}
                className="p-1.5 rounded-xl hover:bg-sand-150 text-stone-400 hover:text-stone-700"
              >
                ✕
              </button>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              {/* Free Plan */}
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50 space-y-3 flex flex-col justify-between">
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-bold text-sm text-stone-900">Free Tier</span>
                    <span className="text-[10px] font-mono bg-stone-200 text-stone-700 px-1.5 py-0.5 rounded font-semibold">Active</span>
                  </div>
                  <div className="text-xl font-bold text-stone-900">$0 <span className="text-xs font-normal text-stone-500">/ month</span></div>
                  <p className="text-xs text-stone-600">Great for evaluating autonomous pull request workflows.</p>
                  <ul className="text-xs space-y-1.5 text-stone-600 pt-2 border-t border-sand-200">
                    <li>✓ 500 Compute Mins / mo</li>
                    <li>✓ 2 Concurrent Workers</li>
                    <li>✓ Standard PR Generation</li>
                    <li>✗ BYOC Runners Locked</li>
                  </ul>
                </div>
                <button disabled className="w-full py-2 rounded-xl bg-sand-200 text-stone-500 font-semibold text-xs cursor-default">
                  Current Tier
                </button>
              </div>

              {/* Pro Plan */}
              <div className="p-4 rounded-2xl border-2 border-kiwi-400 bg-white shadow-sm space-y-3 flex flex-col justify-between relative">
                <span className="absolute -top-2.5 right-4 bg-kiwi-500 text-white text-[9px] font-bold px-2 py-0.5 rounded-full uppercase tracking-wider">
                  Recommended
                </span>
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-bold text-sm text-stone-900">Pro</span>
                    <span className="text-[10px] font-mono text-kiwi-800 bg-kiwi-100 px-1.5 py-0.5 rounded font-semibold">Team</span>
                  </div>
                  <div className="text-xl font-bold text-stone-900">$49 <span className="text-xs font-normal text-stone-500">/ seat / mo</span></div>
                  <p className="text-xs text-stone-600">For fast-moving engineering teams scaling automation.</p>
                  <ul className="text-xs space-y-1.5 text-stone-600 pt-2 border-t border-sand-200">
                    <li>✓ 5,000 Compute Mins / mo</li>
                    <li>✓ 8 Concurrent Workers</li>
                    <li>✓ BYOC Private Runners</li>
                    <li>✓ PR Watchdog Auto-Remediate</li>
                  </ul>
                </div>
                <a
                  href={`mailto:support@runkiwi.dev?subject=${encodeURIComponent("Upgrade to Kiwi Pro")}`}
                  className="w-full py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs text-center transition-all shadow-xs"
                >
                  Upgrade to Pro
                </a>
              </div>

              {/* Enterprise Plan */}
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50 space-y-3 flex flex-col justify-between">
                <div className="space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-bold text-sm text-stone-900">Enterprise</span>
                    <span className="text-[10px] font-mono text-indigo-800 bg-indigo-100 px-1.5 py-0.5 rounded font-semibold">BYOC</span>
                  </div>
                  <div className="text-xl font-bold text-stone-900">Custom</div>
                  <p className="text-xs text-stone-600">VPC isolation, audit trails, and custom LLM routing.</p>
                  <ul className="text-xs space-y-1.5 text-stone-600 pt-2 border-t border-sand-200">
                    <li>✓ Unlimited Compute Mins</li>
                    <li>✓ 16+ Concurrent Workers</li>
                    <li>✓ Full VPC & Airgap Deploy</li>
                    <li>✓ Dedicated Slack Support</li>
                  </ul>
                </div>
                <a
                  href={`mailto:support@runkiwi.dev?subject=${encodeURIComponent("Kiwi Enterprise BYOC Inquiry")}`}
                  className="w-full py-2 rounded-xl bg-sand-200 hover:bg-sand-300 text-stone-800 font-semibold text-xs text-center transition-all"
                >
                  Contact Sales
                </a>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* CUSTOM LOADERS STUDIO MODAL */}
      <CustomLoadersStudio isOpen={showLoadersModal} onClose={() => setShowLoadersModal(false)} />
    </div>
  );
}
