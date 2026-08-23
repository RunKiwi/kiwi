"use client";

import React from "react";

// Kiwi Core Multi-Ring Harmonic Spinner — counter-rotating dual arcs with a
// pulsating nucleus. Markup and classes match enterprise_saas_showcase.html's
// Loaders & Agent Particle Studio exactly (.kiwi-loader-core*, globals.css).
export function KiwiCoreSpinner({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const dim = size === "sm" ? 20 : size === "lg" ? 40 : 28;
  return (
    <div
      className={`kiwi-loader-core shrink-0 ${className}`}
      style={{ width: dim, height: dim }}
      role="status"
      aria-label="Loading"
    >
      <svg className="w-full h-full" viewBox="0 0 40 40">
        <circle className="kiwi-loader-core-ring-1 stroke-kiwi-600" cx="20" cy="20" r="14" fill="none" strokeWidth="3" />
        <circle className="kiwi-loader-core-ring-2 stroke-stone-900" cx="20" cy="20" r="9" fill="none" strokeWidth="2.5" />
      </svg>
      <div className="kiwi-loader-core-dot bg-kiwi-600" />
    </div>
  );
}

// AST Token Waveform Stream — staggered harmonic wave simulating LLM reasoning.
export function KiwiASTWave({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md";
  className?: string;
}) {
  const height = size === "sm" ? 12 : 16;
  return (
    <div
      className={`kiwi-loader-ast shrink-0 ${className}`}
      style={{ height }}
      role="status"
      aria-label="Reasoning AST"
    >
      <span className="bg-kiwi-600" />
      <span className="bg-purple-600" />
      <span className="bg-amber-500" />
      <span className="bg-stone-900" />
    </div>
  );
}

// Test Guard Sandbox Orbit Radar — concentric ping radar with an orbiting
// satellite electron.
export function KiwiTestOrbit({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md";
  className?: string;
}) {
  const dim = size === "sm" ? 20 : 28;
  return (
    <div
      className={`kiwi-loader-orbit shrink-0 ${className}`}
      style={{ width: dim, height: dim }}
      role="status"
      aria-label="Executing test guard"
    >
      <div className="kiwi-loader-orbit-ping" />
      <div className="kiwi-loader-orbit-ring">
        <div className="kiwi-loader-orbit-satellite" />
      </div>
      <div className="w-2.5 h-2.5 rounded-full bg-emerald-600 z-10 shadow-xs" />
    </div>
  );
}

// Precision Button Micro Dash Spinner — ultra-clean variable-velocity SVG
// dash loader, sized for inline button loading states.
export function KiwiMicroButtonLoader({ className = "" }: { className?: string }) {
  return (
    <div
      className={`kiwi-loader-micro w-3.5 h-3.5 text-current shrink-0 ${className}`}
      role="status"
      aria-label="Loading"
    >
      <svg className="w-full h-full" viewBox="0 0 32 32">
        <circle className="kiwi-loader-micro-circle" cx="16" cy="16" r="12" fill="none" strokeWidth="3" />
      </svg>
    </div>
  );
}
