import { Check } from "lucide-react";
import { PLAN_TIERS, PLAN_FEATURES, PlanTier, PlanFeatureValue } from "@/lib/plans";
import { PRO_UPGRADE_MAILTO, ENTERPRISE_MAILTO } from "@/lib/api";

export function PlanComparison({ currentPlan }: { currentPlan?: string | null }) {
  const currentPlanId = currentPlan || "free";

  const renderValue = (val: PlanFeatureValue) => {
    return (
      <div className="flex items-center gap-2">
        {val.value === true ? (
          <Check className="w-4 h-4 text-[#93C645]" />
        ) : val.value === false || val.value === "—" ? (
          <span className="text-zinc-600">—</span>
        ) : (
          <span className="text-zinc-300">{val.value}</span>
        )}
        {val.soon && (
          <span className="text-[10px] font-medium uppercase tracking-wider text-[#93C645] bg-[#93C645]/10 px-1.5 py-0.5 rounded">
            Coming soon
          </span>
        )}
      </div>
    );
  };

  const renderCTA = (tier: PlanTier) => {
    if (tier.id === "free") {
      if (currentPlanId === "free") {
        return (
          <button disabled className="w-full px-4 py-2 text-sm rounded-lg bg-white/5 text-zinc-500 font-medium cursor-not-allowed">
            Current plan
          </button>
        );
      }
      return <div className="h-[36px]" />;
    }
    if (tier.id === "pro") {
      if (currentPlanId === "pro") {
        return (
          <button disabled className="w-full px-4 py-2 text-sm rounded-lg bg-[#93C645]/10 text-[#93C645] font-medium cursor-not-allowed border border-[#93C645]/20">
            Current plan
          </button>
        );
      }
      return (
        <a href={PRO_UPGRADE_MAILTO} className="flex items-center justify-center w-full px-4 py-2 text-sm rounded-lg bg-[#93C645] text-[#0B141D] font-medium hover:bg-[#a4d656] transition-colors shadow-[0_0_15px_rgba(147,198,69,0.3)]">
          Upgrade to Pro
        </a>
      );
    }
    if (tier.id === "enterprise") {
      return (
        <a href={ENTERPRISE_MAILTO} className="flex items-center justify-center w-full px-4 py-2 text-sm rounded-lg bg-white/10 text-white font-medium hover:bg-white/20 transition-colors border border-white/10">
          Contact sales
        </a>
      );
    }
  };

  return (
    <div className="flex flex-col gap-4 w-full glass-panel p-6">
      <h2 className="text-lg font-medium text-white mb-2">Compare Plans</h2>
      <div className="overflow-x-auto">
        <table className="w-full text-left border-collapse min-w-[700px]">
          <thead>
            <tr>
              <th className="p-4 w-1/4"></th>
              {PLAN_TIERS.map(tier => {
                const isCurrent = currentPlanId === tier.id;
                return (
                  <th key={tier.id} className={`p-4 w-1/4 rounded-t-xl border-x border-t relative ${isCurrent ? 'bg-white/[0.04] border-[#93C645]/30' : 'bg-transparent border-transparent'}`}>
                    {isCurrent && (
                      <div className="absolute top-0 left-1/2 -translate-x-1/2 -translate-y-1/2">
                        <span className="text-[10px] font-medium uppercase tracking-wider text-[#93C645] bg-[#0B141D] border border-[#93C645]/30 px-2 py-0.5 rounded-full whitespace-nowrap">
                          Current plan
                        </span>
                      </div>
                    )}
                    <div className="flex flex-col gap-1 mb-4">
                      <div className="text-lg font-medium text-white">{tier.name}</div>
                      <div className="text-sm text-zinc-400">{tier.price}</div>
                    </div>
                    {renderCTA(tier)}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {PLAN_FEATURES.map((feature, idx) => (
              <tr key={idx} className="border-t border-white/[0.06]">
                <td className="p-4 text-sm font-medium text-zinc-400 border-r border-transparent">
                  {feature.name}
                </td>
                {PLAN_TIERS.map(tier => {
                  const isCurrent = currentPlanId === tier.id;
                  const val = feature[tier.id];
                  return (
                    <td key={tier.id} className={`p-4 text-sm border-x ${isCurrent ? 'bg-white/[0.04] border-[#93C645]/30' : 'bg-transparent border-transparent'}`}>
                      {renderValue(val)}
                    </td>
                  );
                })}
              </tr>
            ))}
            <tr>
              <td className="p-0 h-4 border-r border-transparent"></td>
              {PLAN_TIERS.map(tier => {
                const isCurrent = currentPlanId === tier.id;
                return (
                  <td key={tier.id} className={`p-0 h-4 rounded-b-xl border-x border-b ${isCurrent ? 'bg-white/[0.04] border-[#93C645]/30' : 'bg-transparent border-transparent'}`}></td>
                );
              })}
            </tr>
          </tbody>
        </table>
      </div>
      <div className="text-xs text-zinc-500 text-center mt-2">
        Pro agent-minutes are per seat, pooled across your org.
      </div>
    </div>
  );
}
