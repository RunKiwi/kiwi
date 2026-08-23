"use client";

import React, { useState } from "react";
import {
  Radar,
  CheckCircle2,
  TrendingUp,
  Clock,
  GitPullRequest,
} from "lucide-react";
import { Logo, type KiwiPose } from "@/components/Logo";

export default function DesignLabPage() {
  const [selectedPose, setSelectedPose] = useState<KiwiPose>("vibing");
  const [isAnimated, setIsAnimated] = useState<boolean>(true);

  return (
    <div className="space-y-10 max-w-6xl mx-auto font-sans text-stone-900 pb-16">
      
      {/* Header Banner with Hybrid Mesh + Animated Mascot */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-6 rounded-3xl bg-gradient-to-r from-sand-100 via-white to-kiwi-50/80 border border-sand-200 shadow-sm relative overflow-hidden">
        {/* Grain overlay */}
        <div
          className="absolute inset-0 opacity-[0.035] pointer-events-none"
          style={{
            backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
          }}
        />
        {/* Ambient Corner Aura */}
        <div className="absolute -top-16 -right-16 w-48 h-48 bg-kiwi-400/25 rounded-full blur-3xl pointer-events-none" />

        <div className="space-y-1 z-10">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono font-bold uppercase tracking-wider bg-kiwi-100 text-kiwi-900 px-2 py-0.5 rounded-full border border-kiwi-200">
              Selected Theme
            </span>
            <span className="text-[10px] font-mono text-stone-500">Hybrid System (Options 1 + 2 + 4 + Animated Mascot)</span>
          </div>
          <h1 className="text-xl sm:text-2xl font-bold text-stone-900 tracking-tight">
            Hybrid Card Design &amp; Animated Mascot System
          </h1>
          <p className="text-xs text-stone-600 max-w-2xl leading-relaxed">
            Combining frosted glass surfaces, targeted chromatic light auras, fine-grain texture, live hardware sparkline telemetry, and interactive animated 8-bit Kiwi mascots.
          </p>
        </div>

        <div className="flex items-center gap-3 z-10 shrink-0">
          <div className="w-14 h-14 rounded-2xl bg-white border border-sand-200 shadow-2xs flex items-center justify-center">
            <Logo variant="full-color" pose="vibing" animated={true} className="w-9 h-9" />
          </div>
        </div>
      </div>

      {/* ========================================================================= */}
      {/* SECTION 1: ANIMATED MASCOT STUDIO (OPTION 5) */}
      {/* ========================================================================= */}
      <section className="space-y-4">
        <div className="flex items-center justify-between border-b border-sand-200 pb-2">
          <div>
            <span className="text-[10px] font-mono font-bold uppercase text-amber-800 bg-amber-100 px-2 py-0.5 rounded-full border border-amber-200">
              Option 5: Live Mascot System
            </span>
            <h2 className="text-base font-bold text-stone-900 mt-1">Animated 8-Bit Mascot Poses</h2>
            <p className="text-xs text-stone-500">Each pose has tailored CSS micro-animations (visor pulse, hack scan-beam, shield aura, rocket flames, dance hop).</p>
          </div>

          <button
            onClick={() => setIsAnimated(!isAnimated)}
            className={`px-3 py-1 rounded-xl text-xs font-mono font-bold transition-all flex items-center gap-1.5 cursor-pointer ${
              isAnimated ? "bg-kiwi-600 text-white shadow-2xs" : "bg-sand-100 text-stone-600 border border-sand-200"
            }`}
          >
            <span>{isAnimated ? "⚡ Animations: ON" : "Animations: PAUSED"}</span>
          </button>
        </div>

        {/* Mascot Pose Showcase Grid */}
        <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-7 gap-3">
          {(["vibing", "hacking", "guarding", "flying", "dancing", "sleeping", "idle"] as KiwiPose[]).map((p) => {
            const isSelected = selectedPose === p;
            return (
              <button
                key={p}
                onClick={() => setSelectedPose(p)}
                className={`p-3 rounded-2xl border text-center transition-all flex flex-col items-center gap-2 group cursor-pointer ${
                  isSelected
                    ? "bg-white border-kiwi-400 shadow-md ring-2 ring-kiwi-200"
                    : "bg-white/80 border-sand-200 hover:bg-white hover:border-sand-300 shadow-2xs"
                }`}
              >
                <div className="w-10 h-10 rounded-xl bg-sand-50 border border-sand-200 flex items-center justify-center group-hover:scale-110 transition-transform">
                  <Logo variant="full-color" pose={p} animated={isAnimated} className="w-6 h-6" />
                </div>
                <span className="text-[11px] font-mono font-bold text-stone-800 capitalize">{p}</span>
              </button>
            );
          })}
        </div>

        {/* Selected Mascot Dynamic Context Banner */}
        <div className="relative overflow-hidden p-6 rounded-3xl bg-gradient-to-br from-sand-50/90 via-white to-kiwi-50/60 border border-sand-200 shadow-sm flex items-center gap-5">
          <div
            className="absolute inset-0 opacity-[0.035] pointer-events-none"
            style={{
              backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
            }}
          />
          <div className="absolute -top-12 -right-12 w-32 h-32 bg-kiwi-400/20 rounded-full blur-2xl" />

          <div className="w-16 h-16 rounded-2xl bg-white border border-sand-200 shadow-2xs flex items-center justify-center shrink-0 z-10">
            <Logo variant="full-color" pose={selectedPose} animated={isAnimated} className="w-10 h-10" />
          </div>

          <div className="space-y-1 z-10">
            <div className="flex items-center gap-2">
              <span className="text-xs font-bold text-stone-900 capitalize">
                Kiwi is {selectedPose === "guarding" ? "Guarding PR Telemetry" : selectedPose === "hacking" ? "Synthesizing Automated Patch" : selectedPose === "flying" ? "Deploying Release Monitors" : selectedPose === "dancing" ? "Celebrating 100% Pass Rate!" : `${selectedPose}!`}
              </span>
              <span className="text-[10px] font-mono bg-kiwi-100 text-kiwi-900 px-1.5 py-0.2 rounded border border-kiwi-200 uppercase font-bold">
                ACTIVE
              </span>
            </div>
            <p className="text-xs text-stone-600 leading-relaxed max-w-xl">
              {selectedPose === "guarding"
                ? "All post-merge PR watchdogs report 0 regression. Latency within baseline and error rates flat at 0.00%."
                : selectedPose === "hacking"
                ? "Autonomous architect loop analyzing repository AST, resolving type dependencies and writing offline verification tests."
                : selectedPose === "flying"
                ? "Canary verification containers active across 3 private BYOC fleet nodes."
                : selectedPose === "dancing"
                ? "Task completed successfully. Clean build, all test assertions passed, PR #148 opened and ready to merge."
                : "Continuous multi-agent pair programming runtime standing by."}
            </p>
          </div>
        </div>
      </section>

      {/* ========================================================================= */}
      {/* SECTION 2: HYBRID HARDWARE TELEMETRY & AMBIENT GRAIN CARDS (OPTIONS 1+2+4) */}
      {/* ========================================================================= */}
      <section className="space-y-4">
        <div className="flex items-center justify-between border-b border-sand-200 pb-2">
          <div>
            <span className="text-[10px] font-mono font-bold uppercase text-emerald-800 bg-emerald-100 px-2 py-0.5 rounded-full border border-emerald-200">
              Options 1 + 2 + 4: Hybrid Telemetry Cards
            </span>
            <h2 className="text-base font-bold text-stone-900 mt-1">Frosted Glass, Light Auras &amp; Sparkline KPI Gauges</h2>
            <p className="text-xs text-stone-500">Cards feature frosted glass translucency, chromatic corner light, fine grain, and real-time sparkline tracks.</p>
          </div>
        </div>

        {/* 4 Hybrid KPI Cards */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          
          {/* Card 1: Active Pipelines (Kiwi Green Aura + Sparkline) */}
          <div className="relative overflow-hidden p-5 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:shadow-island-hover hover:border-kiwi-300 transition-all group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-24 h-24 bg-kiwi-400/25 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 mb-2">
              <span className="text-[11px] font-mono font-bold uppercase tracking-wider">Active Pipelines</span>
              <span className="relative flex h-2 w-2">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-600" />
              </span>
            </div>

            <div className="relative z-10 flex items-baseline gap-2">
              <span className="text-2xl font-bold font-mono text-stone-900">3</span>
              <span className="text-xs font-mono text-stone-500">running</span>
              <span className="text-[10px] font-mono font-bold text-emerald-700 bg-emerald-50 border border-emerald-200 px-1.5 py-0.2 rounded ml-auto flex items-center gap-0.5">
                <TrendingUp className="w-3 h-3" /> +2
              </span>
            </div>

            {/* Sparkline Track */}
            <div className="relative z-10 mt-3.5 flex items-end gap-1 h-5">
              {[35, 60, 45, 80, 50, 90, 75].map((h, i) => (
                <div key={i} className="flex-1 bg-kiwi-200 hover:bg-kiwi-400 rounded-xs transition-all" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Card 2: PR Verification Rate (Sky Blue Aura + Sparkline) */}
          <div className="relative overflow-hidden p-5 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:shadow-island-hover hover:border-sky-300 transition-all group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-24 h-24 bg-sky-400/25 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 mb-2">
              <span className="text-[11px] font-mono font-bold uppercase tracking-wider">Verification Rate</span>
              <CheckCircle2 className="w-3.5 h-3.5 text-sky-600" />
            </div>

            <div className="relative z-10 flex items-baseline gap-2">
              <span className="text-2xl font-bold font-mono text-stone-900">98.2%</span>
              <span className="text-[10px] font-mono font-bold text-sky-800 bg-sky-50 border border-sky-200 px-1.5 py-0.2 rounded ml-auto">
                48/49 Passed
              </span>
            </div>

            <div className="relative z-10 mt-3.5 flex items-end gap-1 h-5">
              {[90, 95, 92, 100, 98, 96, 98].map((h, i) => (
                <div key={i} className="flex-1 bg-sky-200 hover:bg-sky-400 rounded-xs transition-all" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Card 3: Mean Latency (Purple Aura + Sparkline) */}
          <div className="relative overflow-hidden p-5 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:shadow-island-hover hover:border-purple-300 transition-all group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-24 h-24 bg-purple-400/25 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 mb-2">
              <span className="text-[11px] font-mono font-bold uppercase tracking-wider">Mean Latency</span>
              <Clock className="w-3.5 h-3.5 text-purple-600" />
            </div>

            <div className="relative z-10 flex items-baseline gap-2">
              <span className="text-2xl font-bold font-mono text-stone-900">1m 48s</span>
              <span className="text-[10px] font-mono font-bold text-purple-800 bg-purple-50 border border-purple-200 px-1.5 py-0.2 rounded ml-auto">
                -22s vs avg
              </span>
            </div>

            <div className="relative z-10 mt-3.5 flex items-end gap-1 h-5">
              {[65, 55, 70, 50, 45, 48, 42].map((h, i) => (
                <div key={i} className="flex-1 bg-purple-200 hover:bg-purple-400 rounded-xs transition-all" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>

          {/* Card 4: PRs Opened (Sunset Amber Aura + Sparkline) */}
          <div className="relative overflow-hidden p-5 rounded-2xl bg-white/85 backdrop-blur-xl border border-sand-200 shadow-2xs hover:shadow-island-hover hover:border-amber-300 transition-all group">
            <div
              className="absolute inset-0 opacity-[0.035] pointer-events-none"
              style={{
                backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 200 200' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='3'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E")`,
              }}
            />
            <div className="absolute -top-10 -right-10 w-24 h-24 bg-amber-400/25 rounded-full blur-2xl group-hover:scale-125 transition-transform" />

            <div className="relative z-10 flex items-center justify-between text-stone-600 mb-2">
              <span className="text-[11px] font-mono font-bold uppercase tracking-wider">PRs Opened</span>
              <GitPullRequest className="w-3.5 h-3.5 text-amber-600" />
            </div>

            <div className="relative z-10 flex items-baseline gap-2">
              <span className="text-2xl font-bold font-mono text-stone-900">14</span>
              <span className="text-xs font-mono text-stone-500">today</span>
              <span className="text-[10px] font-mono font-bold text-amber-800 bg-amber-100 border border-amber-200 px-1.5 py-0.2 rounded ml-auto">
                12 Merged
              </span>
            </div>

            <div className="relative z-10 mt-3.5 flex items-end gap-1 h-5">
              {[30, 45, 60, 40, 75, 90, 85].map((h, i) => (
                <div key={i} className="flex-1 bg-amber-200 hover:bg-amber-400 rounded-xs transition-all" style={{ height: `${h}%` }} />
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ========================================================================= */}
      {/* SECTION 3: COMPOSER & PIPELINE ACTION CARDS */}
      {/* ========================================================================= */}
      <section className="space-y-4">
        <div className="flex items-center justify-between border-b border-sand-200 pb-2">
          <div>
            <span className="text-[10px] font-mono font-bold uppercase text-indigo-800 bg-indigo-100 px-2 py-0.5 rounded-full border border-indigo-200">
              Interactive Flow
            </span>
            <h2 className="text-base font-bold text-stone-900 mt-1">Live Pipeline &amp; Task Cards</h2>
            <p className="text-xs text-stone-500">Integrated cards for prompt assignment, telemetry watchdogs, and execution tracking.</p>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          
          {/* Card A: Task Execution Stream Card */}
          <div className="relative rounded-3xl p-6 overflow-hidden border border-sand-200 bg-white/80 backdrop-blur-xl shadow-sm group">
            <div className="absolute top-0 right-0 w-40 h-40 bg-kiwi-300/30 rounded-full blur-2xl -mr-10 -mt-10" />

            <div className="relative z-10 flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-8 h-8 rounded-xl bg-kiwi-100 border border-kiwi-200 flex items-center justify-center">
                  <Logo variant="full-color" pose="hacking" animated={isAnimated} className="w-5 h-5" />
                </div>
                <div>
                  <div className="text-xs font-bold text-stone-900">Refactor Ed25519 Signatures</div>
                  <div className="text-[10px] font-mono text-stone-500">RunKiwi/kiwi • #job_9841</div>
                </div>
              </div>
              <span className="text-[10px] font-mono bg-indigo-100 text-indigo-900 px-2 py-0.5 rounded-full font-bold border border-indigo-200 flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-indigo-600 animate-ping" />
                IMPLEMENTING
              </span>
            </div>

            <div className="relative z-10 space-y-2 mt-4">
              <div className="flex items-center justify-between text-[11px] font-mono text-stone-600">
                <span>Verification progress</span>
                <span className="font-bold text-emerald-700">3/4 Steps Complete</span>
              </div>
              <div className="w-full bg-sand-150 h-2 rounded-full overflow-hidden">
                <div className="bg-gradient-to-r from-kiwi-500 via-emerald-500 to-indigo-500 h-full rounded-full w-3/4 animate-pulse" />
              </div>
            </div>
          </div>

          {/* Card B: PR Watchdog Canary Card */}
          <div className="relative rounded-3xl p-6 overflow-hidden border border-sand-200 bg-white/80 backdrop-blur-xl shadow-sm group">
            <div className="absolute top-0 right-0 w-40 h-40 bg-sky-300/30 rounded-full blur-2xl -mr-10 -mt-10" />

            <div className="relative z-10 flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <div className="w-8 h-8 rounded-xl bg-sky-100 border border-sky-200 flex items-center justify-center">
                  <Logo variant="full-color" pose="guarding" animated={isAnimated} className="w-5 h-5" />
                </div>
                <div>
                  <div className="text-xs font-bold text-stone-900">Post-Merge Canary Monitor</div>
                  <div className="text-[10px] font-mono text-stone-500">RunKiwi/kiwi #142 • Commit @9a8f21</div>
                </div>
              </div>
              <span className="text-[10px] font-mono bg-sky-100 text-sky-900 px-2 py-0.5 rounded-full font-bold border border-sky-200 flex items-center gap-1">
                <Radar className="w-3 h-3 text-sky-600" />
                OBSERVING
              </span>
            </div>

            <div className="relative z-10 space-y-2 mt-4">
              <div className="flex items-center justify-between text-[11px] font-mono text-stone-600">
                <span>Observation window: 24 hours</span>
                <span className="font-bold text-sky-800">18h 42m remaining</span>
              </div>
              <div className="w-full bg-sand-150 h-2 rounded-full overflow-hidden">
                <div className="bg-gradient-to-r from-sky-500 to-indigo-500 h-full rounded-full w-[22%]" />
              </div>
            </div>
          </div>

        </div>
      </section>

    </div>
  );
}
