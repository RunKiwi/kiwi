"use client";

import React, { useState } from "react";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import { Logo } from "@/components/Logo";
import type { OrbState } from "@/lib/orbState";

const ORB_STATES: { state: OrbState; label: string; desc: string }[] = [
  { state: "working", label: "1. working", desc: "install: npm ci / build" },
  { state: "searching", label: "2. searching", desc: "read_file / grep" },
  { state: "solving", label: "3. solving", desc: "go test suite" },
  { state: "composing", label: "4. composing", desc: "actor: edit_file" },
  { state: "connecting", label: "5. connecting", desc: "clone / container boot" },
  { state: "breathing", label: "6. breathing", desc: "critic review" },
  { state: "weaving", label: "7. weaving", desc: "AST prompt compaction" },
  { state: "shaping", label: "8. shaping", desc: "LoadingState fallback" },
  { state: "listening", label: "9. listening", desc: "voice / input" },
];

export function CustomLoadersStudio({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const [orbState, setOrbState] = useState<OrbState>("working");
  const [theme, setTheme] = useState<"kiwi" | "mono">("kiwi");

  if (!isOpen) return null;

  const currentMeta = ORB_STATES.find((s) => s.state === orbState) || ORB_STATES[0];

  return (
    <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs z-50 flex items-start justify-center pt-10 p-4" onClick={onClose}>
      <div className="bg-white border border-sand-200 rounded-3xl w-full max-w-3xl shadow-popover overflow-hidden flex flex-col max-h-[90vh]" onClick={(e) => e.stopPropagation()}>
        {/* Header */}
        <div className="p-4 border-b border-sand-200 bg-sand-50/70 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-xl bg-kiwi-600 text-white flex items-center justify-center shadow-2xs">
              <Logo className="w-4 h-4" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-xs font-bold text-stone-900">Loaders & Agent Particle Studio</h3>
                <span className="text-[9px] font-mono font-bold bg-indigo-100 text-indigo-800 px-1.5 py-0.2 rounded border border-indigo-300">
                  CURRENT: thinking-orbs
                </span>
              </div>
              <p className="text-[10px] text-stone-500 font-mono">
                Interactive 2D Canvas engine mapping agent lifecycle phases to 9 mathematical Fibonacci particle sphere animations.
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <kbd className="font-mono text-[10px] bg-sand-200 px-2 py-0.5 rounded text-stone-600 font-semibold">ESC</kbd>
            <button onClick={onClose} className="p-1 rounded-lg text-stone-400 hover:text-stone-700 hover:bg-sand-200/60 transition-all">
              ✕
            </button>
          </div>
        </div>

        {/* Studio Content */}
        <div className="p-6 overflow-y-auto space-y-6">
          {/* Interactive Live Canvas Orb Workbench */}
          <div className="p-4 rounded-2xl border border-sand-200 bg-white space-y-4 shadow-2xs">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-sand-200 pb-3">
              <div>
                <h4 className="text-xs font-bold text-stone-900 flex items-center gap-1.5">
                  <span>Interactive 2D Canvas Orb Simulator</span>
                  <span className="text-[9px] font-mono font-bold bg-indigo-100 text-indigo-800 px-1.5 py-0.2 rounded border border-indigo-200 uppercase">
                    STATE: {orbState}
                  </span>
                </h4>
                <p className="text-[10px] text-stone-500 font-mono">60fps Fibonacci lattice canvas particles with throbbing ambient glow</p>
              </div>

              {/* Theme Switcher */}
              <div className="flex items-center gap-1 bg-sand-50 p-1 rounded-xl border border-sand-200">
                <button
                  onClick={() => setTheme("mono")}
                  className={`px-2 py-0.5 rounded-lg font-mono text-[10px] transition-all ${
                    theme === "mono" ? "bg-stone-900 text-white font-bold shadow-2xs" : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  Monochrome
                </button>
                <button
                  onClick={() => setTheme("kiwi")}
                  className={`px-2 py-0.5 rounded-lg font-mono text-[10px] transition-all ${
                    theme === "kiwi" ? "bg-stone-900 text-white font-bold shadow-2xs" : "text-stone-600 hover:text-stone-900"
                  }`}
                >
                  ✦ Kiwi Glow
                </button>
              </div>
            </div>

            {/* Canvas Stage & Grid */}
            <div className="grid grid-cols-1 lg:grid-cols-12 gap-4 items-center">
              {/* Left: Dark Ambient Stage */}
              <div className="lg:col-span-5 flex flex-col items-center justify-center p-6 rounded-2xl bg-stone-950 border border-stone-800 relative overflow-hidden min-h-[220px] shadow-inner">
                <ThinkingOrb state={orbState} size={84} theme={theme} glow={true} />
                <div className="text-center mt-3 relative z-10">
                  <span className="font-mono text-xs font-bold text-white tracking-wide capitalize">{orbState}…</span>
                  <p className="text-[10px] text-stone-400 font-mono mt-0.5">{currentMeta.desc}</p>
                </div>
              </div>

              {/* Right: 9 States Grid */}
              <div className="lg:col-span-7 grid grid-cols-3 gap-2 text-[11px]">
                {ORB_STATES.map(({ state: st, label, desc }) => (
                  <button
                    key={st}
                    onClick={() => setOrbState(st)}
                    className={`p-2.5 rounded-xl border text-left transition-all font-mono ${
                      orbState === st
                        ? "bg-stone-900 text-white border-stone-800 shadow-2xs"
                        : "bg-sand-50 hover:bg-sand-100 border-sand-200 text-stone-800"
                    }`}
                  >
                    <div className="font-bold">{label}</div>
                    <div className={`text-[9px] truncate ${orbState === st ? "text-stone-400" : "text-stone-500"}`}>{desc}</div>
                  </button>
                ))}
              </div>
            </div>
          </div>

          {/* In-Situ Live Previews */}
          <div className="space-y-3">
            <h4 className="text-xs font-bold text-stone-900 border-b border-sand-200 pb-2">
              Live In-Situ Previews (How Thinking Orbs Power Kiwi)
            </h4>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {/* LiveRun Feed */}
              <div className="p-3.5 rounded-2xl bg-sand-50 border border-sand-200 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-[10px] font-mono font-bold text-stone-600">A. Inside LiveRun.tsx (Task Progress Feed)</span>
                  <span className="text-[9px] font-mono text-indigo-700 bg-indigo-50 border border-indigo-200 px-1.5 py-0.2 rounded font-bold">
                    20px inline orb
                  </span>
                </div>
                <div className="p-2.5 rounded-xl bg-stone-950 border border-stone-800 flex items-center gap-2.5 font-mono text-xs text-stone-200">
                  <ThinkingOrb state="composing" size={20} theme="kiwi" glow={false} />
                  <span className="text-stone-300 font-semibold">actor: edit_file</span>
                  <code className="text-[11px] text-stone-500 truncate">pkg/auth/jwt.go</code>
                  <span className="ml-auto text-[11px] text-stone-500 tabular-nums shrink-0">18s</span>
                </div>
              </div>

              {/* LoadingState Fallback */}
              <div className="p-3.5 rounded-2xl bg-sand-50 border border-sand-200 space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-[10px] font-mono font-bold text-stone-600">B. Inside LoadingState.tsx (Suspense Fallback)</span>
                  <span className="text-[9px] font-mono text-indigo-700 bg-indigo-50 border border-indigo-200 px-1.5 py-0.2 rounded font-bold">
                    48px shaping orb
                  </span>
                </div>
                <div className="p-3 rounded-xl bg-white border border-sand-200 flex items-center justify-center gap-3">
                  <ThinkingOrb state="shaping" size={48} theme="kiwi" glow={true} />
                  <span className="text-xs font-semibold text-stone-700 font-mono">Loading repository workspace…</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
