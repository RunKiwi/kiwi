"use client";

import { useEffect, useRef } from "react";
import Link from "next/link";
import { Gauge, ExternalLink, Zap, Key } from "lucide-react";
import {
  CLASS_ORDER,
  modelClassLabel,
  formatTokens,
  type AllowanceBucket,
  type UsageResponse,
} from "@/lib/api";
import type { OverallAllowanceHealth } from "@/lib/allowanceUtils";

interface AllowancePopoverProps {
  allowance: AllowanceBucket[];
  usage: UsageResponse | null;
  health: OverallAllowanceHealth;
  isOpen: boolean;
  onClose: () => void;
}

export function AllowancePopover({
  allowance,
  usage,
  isOpen,
  onClose,
}: AllowancePopoverProps) {
  const popoverRef = useRef<HTMLDivElement>(null);

  // Close on Escape or click outside
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };

    const handleMouseDown = (e: MouseEvent) => {
      if (
        popoverRef.current &&
        !popoverRef.current.contains(e.target as Node) &&
        !(e.target as HTMLElement).closest("[data-allowance-trigger]")
      ) {
        onClose();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    document.addEventListener("mousedown", handleMouseDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      document.removeEventListener("mousedown", handleMouseDown);
    };
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const allowanceMap = new Map<string, AllowanceBucket>();
  for (const a of allowance) {
    allowanceMap.set(a.tier, a);
  }

  const hasMinutesCap = !!usage && usage.agent_minutes_limit > 0;
  const minutesPct = hasMinutesCap
    ? Math.min(100, Math.round((usage.agent_minutes_used / usage.agent_minutes_limit) * 100))
    : 0;
  const minutesOver = hasMinutesCap && usage.agent_minutes_used >= usage.agent_minutes_limit;
  const minutesNear = hasMinutesCap && !minutesOver && minutesPct >= 80;

  return (
    <div
      ref={popoverRef}
      role="dialog"
      aria-label="Monthly allowance status"
      className="absolute right-0 bottom-full mb-2.5 z-50 w-80 rounded-2xl border border-white/15 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-12px_rgba(0,0,0,0.85)] p-4 space-y-3.5 text-xs select-none"
    >
      {/* Header */}
      <div className="flex items-center justify-between border-b border-white/5 pb-2.5">
        <div className="flex items-center gap-2">
          <Gauge className="w-4 h-4 text-[#93C645]" />
          <span className="font-semibold text-white tracking-wide">Monthly Allowance</span>
        </div>
        <span className="text-[10px] font-mono text-zinc-500 bg-white/5 px-1.5 py-0.5 rounded">
          Resets on 1st
        </span>
      </div>

      {/* Token Tier Progress Bars */}
      <div className="space-y-2.5">
        {CLASS_ORDER.filter((t) => allowanceMap.has(t)).map((tier) => {
          const bucket = allowanceMap.get(tier)!;
          const isUnlimited = bucket.granted < 0;
          const isExhausted = !isUnlimited && bucket.remaining <= 0;
          const pct = isUnlimited
            ? 100
            : Math.min(100, Math.round((bucket.used / Math.max(bucket.granted, 1)) * 100));
          const isNear = !isUnlimited && !isExhausted && pct >= 75;

          const dotColor = isUnlimited
            ? "bg-blue-400"
            : isExhausted
            ? "bg-red-400"
            : isNear
            ? "bg-amber-400"
            : "bg-[#93C645]";

          const barColor = isUnlimited
            ? "bg-blue-400/80"
            : isExhausted
            ? "bg-red-500"
            : isNear
            ? "bg-amber-400"
            : "bg-[#93C645]";

          return (
            <div key={tier} className="space-y-1">
              <div className="flex items-center justify-between text-[11px]">
                <span className="text-zinc-300 flex items-center gap-1.5 font-medium">
                  <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${dotColor}`} />
                  {modelClassLabel(tier)}
                </span>
                <span
                  className={`font-mono ${
                    isExhausted ? "text-red-400 font-semibold" : isNear ? "text-amber-300" : "text-zinc-400"
                  }`}
                >
                  {isUnlimited
                    ? "Unlimited"
                    : isExhausted
                    ? "Exhausted"
                    : `${formatTokens(bucket.remaining)} left of ${formatTokens(bucket.granted)}`}
                </span>
              </div>
              <div className="w-full bg-white/5 rounded-full h-1.5 overflow-hidden border border-white/5">
                <div
                  className={`h-full transition-all duration-300 ${barColor}`}
                  style={{ width: `${pct}%` }}
                />
              </div>
            </div>
          );
        })}

        {/* Agent-Minutes */}
        {hasMinutesCap && (
          <div className="space-y-1 pt-1.5 border-t border-white/5">
            <div className="flex items-center justify-between text-[11px]">
              <span className="text-zinc-300 flex items-center gap-1.5 font-medium">
                <span
                  className={`w-1.5 h-1.5 rounded-full shrink-0 ${
                    minutesOver ? "bg-red-400" : minutesNear ? "bg-amber-400" : "bg-purple-400"
                  }`}
                />
                Agent-Minutes
              </span>
              <span
                className={`font-mono ${
                  minutesOver ? "text-red-400 font-semibold" : minutesNear ? "text-amber-300" : "text-zinc-400"
                }`}
              >
                {usage.agent_minutes_used.toFixed(1)} / {usage.agent_minutes_limit} min
              </span>
            </div>
            <div className="w-full bg-white/5 rounded-full h-1.5 overflow-hidden border border-white/5">
              <div
                className={`h-full transition-all duration-300 ${
                  minutesOver ? "bg-red-500" : minutesNear ? "bg-amber-400" : "bg-purple-400"
                }`}
                style={{ width: `${minutesPct}%` }}
              />
            </div>
          </div>
        )}
      </div>

      {/* Footer Info & BYOK Hint */}
      <div className="p-2.5 rounded-xl bg-black/40 border border-white/5 text-[11px] text-zinc-400 space-y-1.5">
        <div className="flex items-start gap-1.5">
          <Key className="w-3.5 h-3.5 text-zinc-400 shrink-0 mt-0.5" />
          <span className="leading-tight">
            Models connected to your own API key draw on none of this and are unlimited.
          </span>
        </div>
        <div className="flex items-center justify-between pt-1 border-t border-white/5 text-[10px]">
          <Link
            href="/integrations"
            className="text-blue-400 hover:text-blue-300 flex items-center gap-1 transition-colors"
          >
            <span>Connect API keys</span>
            <ExternalLink className="w-2.5 h-2.5" />
          </Link>
          <Link
            href="/spend"
            className="text-zinc-400 hover:text-zinc-200 flex items-center gap-1 transition-colors"
          >
            <span>Spend details</span>
            <ExternalLink className="w-2.5 h-2.5" />
          </Link>
        </div>
      </div>
    </div>
  );
}
