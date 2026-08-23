"use client";

import React from "react";

export function KiwiCoreSpinner({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md" | "lg";
  className?: string;
}) {
  const dim = size === "sm" ? 18 : size === "lg" ? 40 : 26;
  return (
    <div
      className={`relative inline-flex items-center justify-center shrink-0 ${className}`}
      style={{ width: dim, height: dim }}
      role="status"
      aria-label="Loading"
    >
      <svg
        className="absolute inset-0 w-full h-full animate-[spin_2.4s_linear_infinite]"
        viewBox="0 0 32 32"
        fill="none"
      >
        <circle cx="16" cy="16" r="13" stroke="#E5E5E0" strokeWidth="2.5" />
        <circle
          cx="16"
          cy="16"
          r="13"
          stroke="#65A30D"
          strokeWidth="2.5"
          strokeDasharray="45 40"
          strokeLinecap="round"
        />
      </svg>
      <svg
        className="absolute inset-1 w-[calc(100%-8px)] h-[calc(100%-8px)] animate-[spin_1.2s_linear_infinite_reverse]"
        viewBox="0 0 24 24"
        fill="none"
      >
        <circle
          cx="12"
          cy="12"
          r="9"
          stroke="#4D7C0F"
          strokeWidth="2"
          strokeDasharray="25 35"
          strokeLinecap="round"
        />
      </svg>
      <span className="w-1.5 h-1.5 rounded-full bg-lime-500 animate-ping opacity-75" />
    </div>
  );
}

export function KiwiASTWave({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md";
  className?: string;
}) {
  const height = size === "sm" ? 14 : 20;
  const barWidth = size === "sm" ? 2.5 : 3.5;
  return (
    <div
      className={`inline-flex items-center gap-1 shrink-0 ${className}`}
      style={{ height }}
      role="status"
      aria-label="Reasoning AST"
    >
      {[0, 150, 300, 450].map((delay, idx) => (
        <span
          key={idx}
          className="rounded-full bg-gradient-to-t from-lime-600 to-emerald-400 animate-pulse"
          style={{
            width: barWidth,
            height: "100%",
            animationDuration: "900ms",
            animationDelay: `${delay}ms`,
          }}
        />
      ))}
    </div>
  );
}

export function KiwiTestOrbit({
  size = "md",
  className = "",
}: {
  size?: "sm" | "md";
  className?: string;
}) {
  const dim = size === "sm" ? 18 : 26;
  return (
    <div
      className={`relative inline-flex items-center justify-center shrink-0 ${className}`}
      style={{ width: dim, height: dim }}
      role="status"
      aria-label="Executing test guard"
    >
      <div className="absolute inset-0 rounded-full border border-sand-300 animate-ping opacity-40" />
      <div className="w-full h-full rounded-full border-2 border-emerald-500/30 border-t-emerald-600 animate-spin" />
      <span className="absolute w-2 h-2 rounded-full bg-emerald-600" />
    </div>
  );
}

export function KiwiMicroButtonLoader({ className = "" }: { className?: string }) {
  return (
    <svg
      className={`animate-spin h-3.5 w-3.5 text-current shrink-0 ${className}`}
      xmlns="http://www.w3.org/2000/svg"
      fill="none"
      viewBox="0 0 24 24"
    >
      <circle
        className="opacity-25"
        cx="12"
        cy="12"
        r="10"
        stroke="currentColor"
        strokeWidth="4"
      />
      <path
        className="opacity-75"
        fill="currentColor"
        d="M4 12a8 8 0 018-8v4a4 4 0 00-4 4H4z"
      />
    </svg>
  );
}
