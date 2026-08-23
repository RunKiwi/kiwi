"use client";

import React, { useState } from "react";
import { KiwiCoreSpinner, KiwiASTWave, KiwiTestOrbit, KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { ThinkingOrb } from "@/components/ThinkingOrb";
import type { OrbState } from "@/lib/orbState";

export function CustomLoadersStudio({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const [orbState, setOrbState] = useState<OrbState>("working");

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs z-50 flex items-start justify-center pt-10 p-4" onClick={onClose}>
      <div className="bg-white border border-sand-200 rounded-3xl w-full max-w-3xl shadow-popover overflow-hidden flex flex-col max-h-[90vh]" onClick={(e) => e.stopPropagation()}>
        <div className="p-4 border-b border-sand-200 bg-sand-50/70 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-2.5">
            <div className="w-8 h-8 rounded-xl bg-kiwi-600 text-white flex items-center justify-center font-bold text-sm shadow-2xs">
              🥝
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-xs font-bold text-stone-900">Loaders & Agent Particle Studio</h3>
                <span className="text-[9px] font-mono font-bold bg-indigo-100 text-indigo-800 px-1.5 py-0.2 rounded border border-indigo-300">CURRENT: thinking-orbs</span>
              </div>
              <p className="text-[10px] text-stone-500 font-mono">Interactive prototypes of third-party library (thinking-orbs) + Kiwi bespoke UI loaders.</p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            <button onClick={onClose} className="p-1 rounded-lg text-stone-400 hover:text-stone-700 hover:bg-sand-200/60 transition-all">
              ✕
            </button>
          </div>
        </div>

        <div className="p-6 overflow-y-auto space-y-6">
          {/* Section 1: thinking-orbs */}
          <div className="p-4 rounded-2xl border border-indigo-200 bg-indigo-50/30 space-y-3">
            <div className="flex items-center justify-between border-b border-indigo-100 pb-2">
              <span className="text-xs font-bold text-indigo-950">1. Thinking Orb (Library: thinking-orbs)</span>
              <div className="flex items-center gap-1 text-[10px] font-mono">
                {(["working", "solving", "searching", "weaving"] as const).map((st) => (
                  <button
                    key={st}
                    onClick={() => setOrbState(st)}
                    className={`px-2 py-0.5 rounded capitalize ${orbState === st ? "bg-indigo-600 text-white font-bold" : "bg-white text-stone-600 hover:bg-sand-100"}`}
                  >
                    {st}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex items-center justify-center py-4">
              <ThinkingOrb state={orbState} size={64} />
            </div>
          </div>

          {/* Section 2: Bespoke Loaders Suite */}
          <div className="space-y-3">
            <span className="text-xs font-bold text-stone-900">2. Kiwi Bespoke Micro-Loaders Family</span>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 flex flex-col items-center justify-center gap-2 text-center">
                <KiwiCoreSpinner size="lg" />
                <span className="text-[10px] font-mono text-stone-600 font-bold">Core Multi-Ring</span>
              </div>
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 flex flex-col items-center justify-center gap-2 text-center">
                <KiwiASTWave />
                <span className="text-[10px] font-mono text-stone-600 font-bold">AST Stream Wave</span>
              </div>
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 flex flex-col items-center justify-center gap-2 text-center">
                <KiwiTestOrbit />
                <span className="text-[10px] font-mono text-stone-600 font-bold">Test Orbit Radar</span>
              </div>
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/50 flex flex-col items-center justify-center gap-2 text-center">
                <KiwiMicroButtonLoader />
                <span className="text-[10px] font-mono text-stone-600 font-bold">Button Micro Dash</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
