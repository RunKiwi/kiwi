"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import {
  Sparkles,
  Compass,
  Hammer,
  ShieldCheck,
  Play,
  ChevronsUpDown,
  Search,
  Sliders,
  FolderGit2,
  Lock,
} from "lucide-react";
import { api, type GithubRepo } from "@/lib/api";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { ModelSelector } from "@/components/TaskComposer/ModelSelector";

export default function ComposerPage() {
  const router = useRouter();

  const [prompt, setPrompt] = useState("");
  const [repos, setRepos] = useState<GithubRepo[]>([]);
  const [repo, setRepo] = useState("");
  const [branch, setBranch] = useState("main");
  const [testCmd, setTestCmd] = useState("go test -race ./pkg/auth/...");
  const [strategy, setStrategy] = useState<"direct" | "plan">("direct");
  const [spendCap, setSpendCap] = useState(0.50);
  const [duration, setDuration] = useState("5m");
  const [mode, setMode] = useState<"pr" | "dryrun">("pr");
  const [architectModel, setArchitectModel] = useState("claude-3-7-sonnet");
  const [workerModel, setWorkerModel] = useState("claude-3-5-haiku");
  const [isSubmitting, setIsSubmitting] = useState(false);

  useEffect(() => {
    api.listGithubRepos()
      .then((r) => {
        const list = r.repos || [];
        setRepos(list);
        if (list.length > 0) {
          setRepo(list[0].full_name || list[0].name || "acme-corp/core-api");
        } else {
          setRepo("acme-corp/core-api");
        }
      })
      .catch(() => {
        setRepo("acme-corp/core-api");
      });
  }, []);

  const templates = [
    { title: "🛡️ Fix Vulnerability", desc: "Patch CVE & add race test", p: "Fix CVE-2026-4182 JWT algorithm confusion vulnerability in pkg/auth/jwt.go and add negative test guards." },
    { title: "⚡ Async Refactor", desc: "Convert sync loops to worker pools", p: "Refactor synchronous report generation loops in pkg/reports to use bounded goroutine worker pool with errgroup." },
    { title: "🧪 Fix Flaky Test", desc: "Eliminate timing race conditions", p: "Diagnose and fix intermittent race conditions in pkg/queue/broker_test.go under high concurrency." },
    { title: "📈 Add Test Coverage", desc: "Generate table-driven tests", p: "Generate comprehensive table-driven unit tests for billing calculation logic in pkg/billing/calculator.go with 100% branch coverage." },
  ];

  const handleStart = async () => {
    if (!prompt.trim()) return;
    setIsSubmitting(true);
    try {
      await api.submitPlan({
        task: prompt.trim(),
        repo_url: repo,
        test_cmd: testCmd,
        architect_model: architectModel,
        model: workerModel,
        plan_mode: strategy === "plan",
        spend_cap_usd: spendCap,
        dry_run: mode === "dryrun",
      });
      router.push("/");
    } catch {
      router.push("/");
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl mx-auto font-sans text-stone-900">
      {/* Header */}
      <div className="border-b border-sand-150 pb-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono font-bold bg-kiwi-100 text-kiwi-800 px-2 py-0.5 rounded">NEW TASK</span>
            <h1 className="text-lg font-bold text-stone-900 tracking-tight">Assign Work to AI Agent</h1>
          </div>
          <button onClick={() => setPrompt("")} className="text-xs text-stone-400 hover:text-stone-700 font-medium">
            Reset Form
          </button>
        </div>
        <p className="text-xs text-stone-500 mt-1">
          Describe what needs to be built or fixed. Kiwi writes the code, verifies tests in an isolated container, and opens a ready-to-merge Pull Request.
        </p>
      </div>

      {/* 1-Click Starter Presets */}
      <div className="space-y-2">
        <span className="text-xs font-bold text-stone-700 flex items-center gap-1.5">
          <Sparkles className="w-3.5 h-3.5 text-kiwi-700" />
          Quick-Start Templates:
        </span>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2 text-xs">
          {templates.map((tpl, i) => (
            <button
              key={i}
              onClick={() => setPrompt(tpl.p)}
              className="p-2.5 rounded-xl border border-sand-200 bg-sand-50/60 hover:bg-white hover:border-sand-300 text-left transition-all group shadow-2xs"
            >
              <span className="font-bold text-stone-800 block group-hover:text-kiwi-800">{tpl.title}</span>
              <span className="text-[11px] text-stone-500">{tpl.desc}</span>
            </button>
          ))}
        </div>
      </div>

      {/* Task Prompt Box */}
      <div className="p-5 rounded-2xl border border-sand-200 bg-sand-50/50 shadow-sm space-y-4">
        <div>
          <div className="flex items-center justify-between mb-1.5">
            <label className="text-xs font-bold text-stone-800">Task Objective</label>
            <span className="text-xs text-kiwi-700 hover:underline font-medium cursor-pointer">
              Import from GitHub Issue
            </span>
          </div>
          <div className="rounded-xl border border-sand-300 focus-within:border-kiwi-500 focus-within:ring-2 focus-within:ring-kiwi-100 bg-white transition-all shadow-sm">
            <textarea
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="e.g. Refactor the JWT authentication middleware in pkg/auth to use Ed25519 asymmetric verification and add race-condition unit tests in jwt_test.go"
              className="w-full p-4 text-sm text-stone-900 placeholder-stone-400 font-sans resize-none outline-none bg-transparent leading-relaxed"
            />
            <div className="p-2.5 bg-sand-50/80 border-t border-sand-150 flex flex-wrap items-center justify-between gap-2 text-xs">
              <span className="text-[11px] text-stone-500">
                ✨ Tip: Mention files like <code className="font-mono text-stone-800 bg-sand-200/60 px-1 py-0.5 rounded">pkg/auth/jwt.go</code> for faster pinpoint changes
              </span>
              <span className="text-[11px] font-mono text-emerald-700 font-semibold flex items-center gap-1">
                <ShieldCheck className="w-3.5 h-3.5" />
                Zero credential egress guaranteed
              </span>
            </div>
          </div>
        </div>

        {/* Configuration Grid 1: Target Repository & Test Guard */}
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 text-xs">
          <div>
            <label className="block font-semibold text-stone-700 mb-1">Target Repository</label>
            <div className="w-full px-3 py-2 rounded-xl bg-sand-50/90 border border-sand-200 text-stone-900 font-medium flex items-center gap-2 shadow-2xs">
              <FolderGit2 className="w-3.5 h-3.5 text-stone-500 shrink-0" />
              <select
                value={repo}
                onChange={(e) => setRepo(e.target.value)}
                className="w-full bg-transparent font-mono text-xs font-bold text-stone-900 outline-none cursor-pointer"
              >
                {repos.length > 0 ? (
                  repos.map((r) => {
                    const repoName = r.full_name || r.name || "repo";
                    return (
                      <option key={repoName} value={repoName}>
                        {repoName}
                      </option>
                    );
                  })
                ) : (
                  <option value={repo || "acme-corp/core-api"}>{repo || "acme-corp/core-api"}</option>
                )}
              </select>
            </div>
          </div>

          <div>
            <label className="block font-semibold text-stone-700 mb-1 flex items-center justify-between">
              <span>Automated Verification Guard</span>
              <span className="text-stone-400 font-normal">Must pass 100%</span>
            </label>
            <input
              type="text"
              value={testCmd}
              onChange={(e) => setTestCmd(e.target.value)}
              className="w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 hover:bg-white focus:bg-white border border-sand-200 text-stone-900 font-mono text-xs outline-none focus:border-kiwi-500 transition-all font-medium"
            />
          </div>
        </div>

        {/* Configuration Grid 2: TWO DEDICATED MODEL SELECTORS */}
        <ModelSelector
          architectModel={architectModel}
          workerModel={workerModel}
          onArchitectChange={setArchitectModel}
          onWorkerChange={setWorkerModel}
        />

        {/* Configuration Grid 3: Execution Strategy, Spend Cap, Timeout & Mode */}
        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs space-y-3">
          <div className="flex items-center justify-between border-b border-sand-150 pb-2">
            <div className="flex items-center gap-2">
              <Sliders className="w-4 h-4 text-kiwi-700" />
              <span className="text-xs font-bold text-stone-900">Execution Strategy & Safety Guardrails</span>
            </div>
            <span className="text-[11px] text-stone-500 font-mono">Plan First vs Direct Loop</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 text-xs">
            {/* Strategy */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1 flex items-center justify-between">
                <span>Execution Strategy</span>
                <span className="text-[9px] font-mono text-indigo-700 font-bold bg-indigo-50 px-1 rounded">Interactive</span>
              </label>
              <div className="space-y-1">
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-800 text-[11px] font-medium p-1.5 rounded-lg hover:bg-sand-50">
                  <input type="radio" name="strategy" checked={strategy === "direct"} onChange={() => setStrategy("direct")} className="accent-kiwi-600" />
                  <span>⚡ Direct Execution</span>
                </label>
                <label className="flex items-center gap-1.5 cursor-pointer text-indigo-900 text-[11px] font-bold p-1.5 rounded-lg bg-indigo-50/70 border border-indigo-200/80">
                  <input type="radio" name="strategy" checked={strategy === "plan"} onChange={() => setStrategy("plan")} className="accent-indigo-600" />
                  <span>📋 Plan Mode (Approve)</span>
                </label>
              </div>
            </div>

            {/* Spend Cap */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Max Spend Hard-Cap</label>
              <div className="flex items-center gap-1">
                {[0.50, 1.00, 2.50].map((cap) => (
                  <button
                    key={cap}
                    type="button"
                    onClick={() => setSpendCap(cap)}
                    className={`px-2.5 py-1.5 rounded-lg font-mono font-bold text-[11px] transition-all ${
                      spendCap === cap ? "bg-stone-900 text-white" : "bg-sand-100 hover:bg-sand-200 text-stone-700"
                    }`}
                  >
                    ${cap.toFixed(2)}
                  </button>
                ))}
              </div>
              <p className="text-[10px] text-stone-400 mt-1 font-mono">Pauses safely if reached</p>
            </div>

            {/* Duration */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Max Loop Duration</label>
              <div className="flex items-center gap-1">
                {["5m", "10m", "30m"].map((dur) => (
                  <button
                    key={dur}
                    type="button"
                    onClick={() => setDuration(dur)}
                    className={`px-2.5 py-1.5 rounded-lg font-mono font-bold text-[11px] transition-all ${
                      duration === dur ? "bg-stone-900 text-white" : "bg-sand-100 hover:bg-sand-200 text-stone-700"
                    }`}
                  >
                    {dur}
                  </button>
                ))}
              </div>
              <p className="text-[10px] text-stone-400 mt-1 font-mono">{duration} hard timeout</p>
            </div>

            {/* Target Action */}
            <div>
              <label className="block font-semibold text-stone-700 mb-1">Target Action</label>
              <div className="space-y-1">
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-800 text-[11px] font-medium p-1 rounded hover:bg-sand-50">
                  <input type="radio" name="mode" checked={mode === "pr"} onChange={() => setMode("pr")} className="accent-kiwi-600" />
                  <span>Open GitHub PR</span>
                </label>
                <label className="flex items-center gap-1.5 cursor-pointer text-stone-600 text-[11px] font-medium p-1 rounded hover:bg-sand-50">
                  <input type="radio" name="mode" checked={mode === "dryrun"} onChange={() => setMode("dryrun")} className="accent-kiwi-600" />
                  <span>🧪 Dry-Run (No Push)</span>
                </label>
              </div>
            </div>
          </div>
        </div>

        {/* Pre-Flight Estimation Bar & Start Button */}
        <div className="pt-3 border-t border-sand-200 flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-3 text-xs text-stone-500 font-mono">
            <span>Mode: <strong className="text-indigo-700 font-bold">{strategy === "plan" ? "Plan Mode" : "Direct Autonomous"}</strong></span>
            <span>•</span>
            <span>Est. Cost: <strong className="text-kiwi-700 font-bold">~$0.18 USD</strong></span>
            <span>•</span>
            <span>Cap: <strong className="text-stone-900 font-bold">${spendCap.toFixed(2)} USD</strong></span>
            <span>•</span>
            <span>Target: <strong className="text-stone-900">{mode === "pr" ? "GitHub Pull Request" : "Dry-Run Local"}</strong></span>
          </div>

          <button
            onClick={handleStart}
            disabled={isSubmitting || !prompt.trim()}
            className="px-6 py-2.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-bold text-xs shadow-sm flex items-center gap-2 transition-all active:scale-[0.98] disabled:opacity-50"
          >
            {isSubmitting ? <KiwiMicroButtonLoader /> : <Play className="w-4 h-4 text-kiwi-400 fill-current" />}
            <span>Start Task</span>
          </button>
        </div>
      </div>
    </div>
  );
}
