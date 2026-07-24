import Link from "next/link";

export function FreePlanBanner({ plan }: { plan: string | null }) {
  if (plan !== "free") return null;

  return (
    <div className="sticky top-0 z-40 flex items-center justify-center gap-3 px-4 py-2 bg-[#93C645]/10 border-b border-[#93C645]/20 backdrop-blur-md">
      <span className="text-sm font-medium text-[#93C645]">
        You&apos;re on the Free plan.
      </span>
      <Link 
        href="/settings" 
        className="text-xs font-semibold px-3 py-1 rounded bg-[#93C645] text-[#0B141D] hover:bg-[#a4d656] transition-colors shadow-[0_0_10px_rgba(147,198,69,0.2)]"
      >
        Upgrade to Pro
      </Link>
    </div>
  );
}
