"use client";

import React, { useState } from "react";
import { Check, Zap, ChevronDown, ChevronUp, ArrowRight } from "lucide-react";
import { PLAN_FEATURES, type PlanFeatureValue } from "@/lib/plans";
import { ENTERPRISE_MAILTO } from "@/lib/api";
import { UpgradeButton } from "@/components/UpgradeButton";

export function PlanComparison({ currentPlan }: { currentPlan?: string | null }) {
  const currentPlanId = currentPlan || "free";
  const [showFullMatrix, setShowFullMatrix] = useState(false);

  const renderValue = (val: PlanFeatureValue) => {
    return (
      <div className="flex items-center gap-2">
        {val.value === true ? (
          <Check className="w-4 h-4 text-emerald-600 shrink-0" />
        ) : val.value === false || val.value === "—" ? (
          <span className="text-stone-300 font-mono">—</span>
        ) : (
          <span className="text-stone-800 font-medium text-xs">{val.value}</span>
        )}
        {val.soon && (
          <span className="text-[9px] font-bold font-mono uppercase tracking-wider text-stone-500 bg-sand-100 border border-sand-200 px-1.5 py-0.5 rounded">
            Soon
          </span>
        )}
      </div>
    );
  };

  return (
    <div className="flex flex-col gap-6 w-full font-sans">
      {/* 3 Elevated Tier Cards */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        {/* FREE TIER CARD */}
        <div
          className={`p-6 rounded-2xl bg-white border shadow-2xs flex flex-col justify-between space-y-6 transition-all ${
            currentPlanId === "free" ? "border-stone-400 ring-1 ring-stone-400" : "border-sand-200"
          }`}
        >
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold font-mono uppercase tracking-wider text-stone-500">Free Tier</span>
              {currentPlanId === "free" && (
                <span className="text-[10px] font-bold font-mono uppercase tracking-wider text-stone-700 bg-sand-100 border border-sand-200 px-2 py-0.5 rounded-full">
                  Current Plan
                </span>
              )}
            </div>

            <div>
              <div className="text-3xl font-bold font-mono text-stone-900">$0</div>
              <div className="text-xs text-stone-500 mt-1">Free forever for individuals & hobbyists</div>
            </div>

            <ul className="space-y-2.5 text-xs text-stone-600 pt-2 border-t border-sand-150">
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                <span><strong className="text-stone-900">200</strong> agent-minutes / month</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                <span><strong className="text-stone-900">1</strong> concurrent task runner</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                <span>Shared managed compute fleet</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                <span>GitHub pull request integration</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                <span>gVisor sandbox & sealed keys</span>
              </li>
            </ul>
          </div>

          <div>
            {currentPlanId === "free" ? (
              <button disabled className="w-full py-2 rounded-xl bg-sand-100 text-stone-500 font-bold text-xs cursor-default">
                Your Current Plan
              </button>
            ) : (
              <button disabled className="w-full py-2 rounded-xl bg-sand-50 text-stone-400 font-medium text-xs">
                Included Base
              </button>
            )}
          </div>
        </div>

        {/* PRO TIER CARD (FEATURED) */}
        <div
          className={`p-6 rounded-2xl bg-white border shadow-2xs flex flex-col justify-between space-y-6 relative overflow-hidden transition-all ${
            currentPlanId === "pro"
              ? "border-kiwi-500 ring-2 ring-kiwi-500/20"
              : "border-sand-300 hover:border-stone-400"
          }`}
        >
          <div className="absolute top-0 right-0">
            <span className="bg-stone-900 text-kiwi-400 font-mono text-[9px] font-bold uppercase tracking-widest px-3 py-1 rounded-bl-xl border-l border-b border-sand-200 shadow-2xs flex items-center gap-1">
              <Zap className="w-2.5 h-2.5 fill-current" />
              Most Popular
            </span>
          </div>

          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold font-mono uppercase tracking-wider text-kiwi-800">Pro Tier</span>
              {currentPlanId === "pro" && (
                <span className="text-[10px] font-bold font-mono uppercase tracking-wider text-kiwi-800 bg-kiwi-50 border border-kiwi-200 px-2 py-0.5 rounded-full">
                  Current Plan
                </span>
              )}
            </div>

            <div>
              <div className="text-3xl font-bold font-mono text-stone-900">
                $18 <span className="text-xs font-normal text-stone-500">/ user / mo</span>
              </div>
              <div className="text-xs text-stone-500 mt-1">High-throughput execution for engineering teams</div>
            </div>

            <ul className="space-y-2.5 text-xs text-stone-600 pt-2 border-t border-sand-150">
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-kiwi-600 shrink-0 font-bold" />
                <span><strong className="text-stone-900">2,000</strong> pooled agent-min / seat</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-kiwi-600 shrink-0 font-bold" />
                <span><strong className="text-stone-900">20</strong> concurrent task runners</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-kiwi-600 shrink-0 font-bold" />
                <span><strong className="text-stone-900">Private BYOC</strong> &amp; dedicated runners</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-kiwi-600 shrink-0 font-bold" />
                <span>GitHub, Slack &amp; Linear triggers</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-kiwi-600 shrink-0 font-bold" />
                <span>Higher swarm width &amp; critic models</span>
              </li>
            </ul>
          </div>

          <div>
            {currentPlanId === "pro" ? (
              <button disabled className="w-full py-2.5 rounded-xl bg-kiwi-50 text-kiwi-900 font-bold text-xs border border-kiwi-200 cursor-default">
                Active Pro Subscription
              </button>
            ) : (
              <UpgradeButton variant="full" className="w-full" label="Upgrade to Pro" />
            )}
          </div>
        </div>

        {/* ENTERPRISE TIER CARD */}
        <div
          className={`p-6 rounded-2xl bg-white border shadow-2xs flex flex-col justify-between space-y-6 transition-all ${
            currentPlanId === "enterprise" ? "border-stone-900 ring-2 ring-stone-900" : "border-sand-200"
          }`}
        >
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-xs font-bold font-mono uppercase tracking-wider text-stone-500">Enterprise</span>
              {currentPlanId === "enterprise" && (
                <span className="text-[10px] font-bold font-mono uppercase tracking-wider text-stone-900 bg-sand-100 border border-sand-200 px-2 py-0.5 rounded-full">
                  Current Plan
                </span>
              )}
            </div>

            <div>
              <div className="text-3xl font-bold font-mono text-stone-900">Custom</div>
              <div className="text-xs text-stone-500 mt-1">Zero-knowledge VPC, on-prem &amp; custom SLAs</div>
            </div>

            <ul className="space-y-2.5 text-xs text-stone-600 pt-2 border-t border-sand-150">
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-stone-900 shrink-0 font-bold" />
                <span><strong className="text-stone-900">Custom</strong> pooled agent-minutes</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-stone-900 shrink-0 font-bold" />
                <span><strong className="text-stone-900">Unlimited</strong> concurrent jobs</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-stone-900 shrink-0 font-bold" />
                <span>Firecracker microVMs &amp; VPC peer</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-stone-900 shrink-0 font-bold" />
                <span>SSO / SAML &amp; custom audit export</span>
              </li>
              <li className="flex items-center gap-2">
                <Check className="w-3.5 h-3.5 text-stone-900 shrink-0 font-bold" />
                <span>Dedicated slack &amp; 99.9% uptime SLA</span>
              </li>
            </ul>
          </div>

          <div>
            <a
              href={ENTERPRISE_MAILTO}
              className="flex items-center justify-center w-full py-2.5 rounded-xl bg-white hover:bg-sand-100 text-stone-900 font-bold text-xs border border-sand-200 shadow-2xs transition-all"
            >
              <span>Contact Enterprise Sales</span>
              <ArrowRight className="w-3.5 h-3.5 ml-1 text-stone-400" />
            </a>
          </div>
        </div>
      </div>

      {/* Feature Matrix Collapsible Card */}
      <div className="bg-white border border-sand-200 rounded-2xl shadow-2xs p-5 sm:p-6 space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-bold text-stone-900 uppercase tracking-wider">Detailed Feature Matrix</h3>
            <p className="text-xs text-stone-500 mt-0.5">Compare features and resource limits across all plans.</p>
          </div>

          <button
            onClick={() => setShowFullMatrix(!showFullMatrix)}
            className="text-xs font-semibold text-stone-600 hover:text-stone-900 flex items-center gap-1 transition-colors px-3 py-1.5 rounded-xl border border-sand-200 hover:bg-sand-50"
          >
            <span>{showFullMatrix ? "Hide Feature Matrix" : "View Full Feature Matrix"}</span>
            {showFullMatrix ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
          </button>
        </div>

        {showFullMatrix && (
          <div className="pt-3 border-t border-sand-150 overflow-x-auto no-scrollbar">
            <table className="w-full text-left border-collapse">
              <thead>
                <tr className="border-b border-sand-200 text-stone-500 font-mono text-[11px] uppercase">
                  <th className="py-2.5 pr-4 font-bold">Feature / Resource</th>
                  <th className="py-2.5 px-3 font-bold w-1/4">Free</th>
                  <th className="py-2.5 px-3 font-bold w-1/4 text-kiwi-800">Pro</th>
                  <th className="py-2.5 px-3 font-bold w-1/4 text-stone-900">Enterprise</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-sand-150 text-xs">
                {PLAN_FEATURES.map((feature, idx) => (
                  <tr key={idx} className="hover:bg-sand-50/50 transition-colors">
                    <td className="py-3 pr-4 font-medium text-stone-800">{feature.name}</td>
                    <td className="py-3 px-3">{renderValue(feature.free)}</td>
                    <td className="py-3 px-3 bg-kiwi-50/20">{renderValue(feature.pro)}</td>
                    <td className="py-3 px-3">{renderValue(feature.enterprise)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}
