"use client";

import React from "react";
import { Zap } from "lucide-react";

export interface UpgradeButtonProps {
  variant?: "full" | "compact" | "minimal";
  className?: string;
  subject?: string;
  body?: string;
  label?: string;
}

export function UpgradeButton({
  variant = "full",
  className = "",
  subject = "Upgrade to Kiwi Pro",
  body = "Hi Kiwi Team,\n\nI would like to upgrade our account to the Kiwi Pro plan.\n\nOrganization:\nContact Email:",
  label = "Upgrade to Pro",
}: UpgradeButtonProps) {
  const mailtoUrl = `mailto:support@runkiwi.dev?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;

  if (variant === "minimal") {
    return (
      <a
        href={mailtoUrl}
        className={`text-kiwi-700 font-bold hover:underline inline-flex items-center gap-1 text-[11px] ${className}`}
        title="Upgrade to Pro (Contact Sales/Support)"
      >
        <Zap className="w-3 h-3 text-kiwi-500 fill-current" />
        <span>{label}</span>
      </a>
    );
  }

  if (variant === "compact") {
    return (
      <a
        href={mailtoUrl}
        className={`inline-flex items-center justify-center gap-1.5 px-2.5 py-1 rounded-lg bg-stone-900 hover:bg-stone-800 text-white font-semibold text-[10px] shadow-2xs transition-all active:scale-[0.98] ${className}`}
        title="Upgrade to Pro (Contact Sales/Support)"
      >
        <Zap className="w-3 h-3 text-kiwi-400 fill-current" />
        <span>{label}</span>
      </a>
    );
  }

  return (
    <a
      href={mailtoUrl}
      className={`inline-flex items-center justify-center gap-1.5 px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs transition-all active:scale-[0.98] ${className}`}
      title="Upgrade to Pro (Contact Sales/Support)"
    >
      <Zap className="w-3.5 h-3.5 text-kiwi-400 fill-current" />
      <span>{label}</span>
    </a>
  );
}
