import type { CatalogModel, AllowanceBucket, UsageResponse } from "./api.ts";
import { formatTokens, modelClassLabel } from "./api.ts";

export interface ModelAllowanceStatus {
  isBYOK: boolean;
  tier: string;
  tierLabel: string;
  isUnlimited: boolean;
  isExhausted: boolean;
  isWarning: boolean;
  used: number;
  granted: number;
  remaining: number;
  percentage: number;
  hint?: string;
}

export interface OverallAllowanceHealth {
  status: "healthy" | "warning" | "exhausted";
  summaryText: string;
  dotColorClass: string;
  badgeClass: string;
  barColorClass: string;
  worstPercentage: number;
  outOfMinutes: boolean;
  exhaustedTiers: string[];
  warningTiers: string[];
}

/**
 * Checks the allowance and quota status for a specific model ID.
 */
export function getModelAllowanceStatus(
  modelId: string,
  catalogModels: CatalogModel[],
  allowance: AllowanceBucket[]
): ModelAllowanceStatus {
  const catalog = catalogModels.find((c) => c.model_id === modelId);

  // If not found in catalog or not Kiwi-provided, it runs on the user's BYOK provider key
  if (!catalog || !catalog.kiwi_provided) {
    return {
      isBYOK: true,
      tier: "byok",
      tierLabel: "Your Key",
      isUnlimited: true,
      isExhausted: false,
      isWarning: false,
      used: 0,
      granted: -1,
      remaining: -1,
      percentage: 0,
      hint: "Your key · Unlimited",
    };
  }
  const bucket = allowance.find((a) => a.tier === catalog.tier);
  const tierLabel = modelClassLabel(catalog.tier);

  // If allowance data is not loaded yet or missing for this tier, treat as unresolved/unavailable
  if (!bucket) {
    return {
      isBYOK: false,
      tier: catalog.tier,
      tierLabel,
      isUnlimited: false,
      isExhausted: true,
      isWarning: false,
      used: 0,
      granted: 0,
      remaining: 0,
      percentage: 0,
      hint: `${tierLabel} · Checking allowance...`,
    };
  }

  const isUnlimited = bucket.granted < 0;
  if (isUnlimited) {
    return {
      isBYOK: false,
      tier: catalog.tier,
      tierLabel,
      isUnlimited: true,
      isExhausted: false,
      isWarning: false,
      used: bucket.used,
      granted: -1,
      remaining: -1,
      percentage: 0,
      hint: `${tierLabel} · Unlimited`,
    };
  }

  const granted = Math.max(bucket.granted, 0);
  const used = Math.max(bucket.used, 0);
  const remaining = Math.max(bucket.remaining, 0);
  const percentage = granted > 0 ? Math.min(100, Math.round((used / granted) * 100)) : 100;
  const isExhausted = remaining <= 0 || used >= granted;
  const isWarning = !isExhausted && percentage >= 75;

  let hint: string;
  if (isExhausted) {
    hint = `${tierLabel} · ⛔ Exhausted`;
  } else if (isWarning) {
    hint = `${tierLabel} · ⚠️ ${formatTokens(remaining)} left`;
  } else {
    hint = `${tierLabel} · ${formatTokens(remaining)} left`;
  }

  return {
    isBYOK: false,
    tier: catalog.tier,
    tierLabel,
    isUnlimited: false,
    isExhausted,
    isWarning,
    used,
    granted,
    remaining,
    percentage,
    hint,
  };
}

/**
 * Computes an aggregated health score across all token tiers and agent minutes.
 */
export function getOverallAllowanceHealth(
  allowance: AllowanceBucket[],
  usage: UsageResponse | null
): OverallAllowanceHealth {
  const outOfMinutes =
    !!usage && usage.agent_minutes_limit > 0 && usage.agent_minutes_used >= usage.agent_minutes_limit;

  const minutesPct =
    usage && usage.agent_minutes_limit > 0
      ? Math.min(100, Math.round((usage.agent_minutes_used / usage.agent_minutes_limit) * 100))
      : 0;

  const exhaustedTiers: string[] = [];
  const warningTiers: string[] = [];
  let worstPercentage = minutesPct;

  for (const a of allowance) {
    if (a.granted < 0) continue; // Unlimited
    const pct = a.granted > 0 ? Math.min(100, Math.round((a.used / a.granted) * 100)) : 100;
    if (pct > worstPercentage) {
      worstPercentage = pct;
    }

    if (a.remaining <= 0 || a.used >= a.granted) {
      exhaustedTiers.push(a.tier);
    } else if (pct >= 75) {
      warningTiers.push(a.tier);
    }
  }

  if (outOfMinutes || exhaustedTiers.length > 0) {
    let summaryText = "";
    if (outOfMinutes) {
      summaryText = "Agent-minutes exhausted";
    } else if (exhaustedTiers.length === 1) {
      summaryText = `${modelClassLabel(exhaustedTiers[0])} exhausted`;
    } else {
      summaryText = `${exhaustedTiers.map(modelClassLabel).join(" & ")} exhausted`;
    }

    return {
      status: "exhausted",
      summaryText,
      dotColorClass: "bg-red-400",
      badgeClass: "border-red-500/40 text-red-300 bg-red-950/30",
      barColorClass: "bg-red-500",
      worstPercentage: 100,
      outOfMinutes,
      exhaustedTiers,
      warningTiers,
    };
  }

  if (minutesPct >= 80 || warningTiers.length > 0) {
    let summaryText = "";
    if (minutesPct >= 80) {
      summaryText = `Minutes ${minutesPct}% used`;
    } else if (warningTiers.length === 1) {
      const b = allowance.find((x) => x.tier === warningTiers[0]);
      const pct = b && b.granted > 0 ? Math.round((b.used / b.granted) * 100) : 80;
      summaryText = `${modelClassLabel(warningTiers[0])} ${pct}% spent`;
    } else {
      summaryText = "Allowances near limit";
    }

    return {
      status: "warning",
      summaryText,
      dotColorClass: "bg-amber-400",
      badgeClass: "border-amber-500/40 text-amber-300 bg-amber-950/20",
      barColorClass: "bg-amber-400",
      worstPercentage,
      outOfMinutes: false,
      exhaustedTiers: [],
      warningTiers,
    };
  }

  return {
    status: "healthy",
    summaryText: usage && usage.agent_minutes_limit > 0
      ? `${usage.agent_minutes_used.toFixed(1)}/${usage.agent_minutes_limit}m`
      : "Allowances healthy",
    dotColorClass: "bg-[#93C645]",
    badgeClass: "border-white/10 text-zinc-300 bg-black/40",
    barColorClass: "bg-[#93C645]",
    worstPercentage,
    outOfMinutes: false,
    exhaustedTiers: [],
    warningTiers,
  };
}

/**
 * Finds the best No-cost fallback model available in the catalog whose Free allowance is available.
 * Returns null if no selectable free model is available or the Free tier allowance is exhausted.
 */
export function findFallbackNoCostModel(
  catalogModels: CatalogModel[],
  allowance: AllowanceBucket[]
): string | null {
  const freeBucket = allowance.find((a) => a.tier === "free");
  if (
    freeBucket &&
    freeBucket.granted >= 0 &&
    (freeBucket.remaining <= 0 || freeBucket.used >= freeBucket.granted)
  ) {
    return null;
  }
  const freeModel = catalogModels.find(
    (c) => c.kiwi_provided && c.tier === "free" && c.selectable !== false
  );
  return freeModel ? freeModel.model_id : null;
}
