"use client";

import React from "react";
import { Zap, Building2, ShieldCheck } from "lucide-react";

export interface UpgradeButtonProps {
  plan?: string;
  variant?: "full" | "compact" | "minimal";
  className?: string;
  subject?: string;
  body?: string;
  label?: string;
}

export function UpgradeButton({
  plan = "free",
  variant = "full",
  className = "",
  subject,
  body,
  label,
}: UpgradeButtonProps) {
  const isPro = plan === "pro" || plan === "individual" || plan === "team";
  const isEnterprise = plan === "enterprise";

  const resolvedLabel =
    label ||
    (isEnterprise
      ? "Enterprise Support"
      : isPro
      ? "Contact Enterprise"
      : "Upgrade to Pro");

  const resolvedSubject =
    subject ||
    (isEnterprise
      ? "Kiwi Enterprise Support"
      : isPro
      ? "Kiwi Enterprise Fleet Inquiry"
      : "Upgrade to Kiwi Pro");

  const resolvedBody =
    body ||
    (isEnterprise
      ? "Hi Kiwi Team,\n\nWe need assistance with our enterprise cluster configuration.\n\nOrganization:\nContact Email:"
      : isPro
      ? "Hi Kiwi Team,\n\nWe are currently on Kiwi Pro and would like to learn more about Kiwi Enterprise (custom compute clusters, zero-knowledge VPC, dedicated SLAs).\n\nOrganization:\nTeam Size:\nContact Email:"
      : "Hi Kiwi Team,\n\nI would like to upgrade our account to the Kiwi Pro plan.\n\nOrganization:\nContact Email:");

  const mailtoUrl = `mailto:support@runkiwi.dev?subject=${encodeURIComponent(resolvedSubject)}&body=${encodeURIComponent(resolvedBody)}`;

  const IconComponent = isEnterprise ? ShieldCheck : isPro ? Building2 : Zap;
  const iconColor = isEnterprise ? "text-purple-400" : isPro ? "text-amber-400" : "text-kiwi-400";

  if (variant === "minimal") {
    return (
      <a
        href={mailtoUrl}
        className={`text-stone-700 font-bold hover:underline inline-flex items-center gap-1 text-[11px] cursor-pointer ${className}`}
        title={resolvedLabel}
      >
        <IconComponent className={`w-3 h-3 ${isPro ? "text-amber-600" : "text-kiwi-500 fill-current"}`} />
        <span>{resolvedLabel}</span>
      </a>
    );
  }

  if (variant === "compact") {
    return (
      <a
        href={mailtoUrl}
        className={`inline-flex items-center justify-center gap-1.5 px-2.5 py-1 rounded-lg bg-stone-900 hover:bg-stone-800 text-white font-semibold text-[10px] shadow-2xs transition-all active:scale-[0.98] cursor-pointer ${className}`}
        title={resolvedLabel}
      >
        <IconComponent className={`w-3 h-3 ${iconColor} ${isPro || isEnterprise ? "" : "fill-current"}`} />
        <span>{resolvedLabel}</span>
      </a>
    );
  }

  return (
    <a
      href={mailtoUrl}
      className={`inline-flex items-center justify-center gap-1.5 px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-2xs transition-all active:scale-[0.98] cursor-pointer ${className}`}
      title={resolvedLabel}
    >
      <IconComponent className={`w-3.5 h-3.5 ${iconColor} ${isPro || isEnterprise ? "" : "fill-current"}`} />
      <span>{resolvedLabel}</span>
    </a>
  );
}
