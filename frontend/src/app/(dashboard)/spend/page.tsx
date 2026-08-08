"use client";

import { Suspense, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import {
  AreaChart, Area, BarChart, Bar, XAxis, YAxis, Tooltip,
  ResponsiveContainer, CartesianGrid, Cell,
} from "recharts";
import { AlertCircle, Table2, BarChart3, Gauge } from "lucide-react";
import { client, modelClassLabel, planLabel, formatTokens, CLASS_ORDER, type SpendResponse } from "@/lib/api";
import { LoadingState } from "@/components/LoadingState";

// Ranges the page offers. Values are days; "month" resolves to the 1st.
const RANGES = [
  { key: "7d", label: "7 days" },
  { key: "30d", label: "30 days" },
  { key: "90d", label: "90 days" },
  { key: "month", label: "This month" },
] as const;
type RangeKey = (typeof RANGES)[number]["key"];

const parseRange = (raw: string | null): RangeKey =>
  RANGES.some(r => r.key === raw) ? (raw as RangeKey) : "30d";

function rangeBounds(key: RangeKey): { from: string; to: string } {
  const to = new Date();
  const from = new Date(to);
  if (key === "month") {
    from.setUTCDate(1);
    from.setUTCHours(0, 0, 0, 0);
  } else {
    from.setUTCDate(from.getUTCDate() - parseInt(key, 10));
  }
  return { from: from.toISOString(), to: to.toISOString() };
}

// Sequential — one hue for every magnitude mark. Repos and models are nominal,
// so a darker-where-bigger ramp would double-encode bar length as colour and
// spend the only free channel on information the bar already carries.
const SEQ = "#93C645";

// Categorical, used only for the two-segment planner/worker split. Validated
// against the app surface #0E1A24: all checks pass, but adjacent tritan
// separation sits at the accessibility floor — so the direct labels and the
// surface gap between segments below are load-bearing, not decoration.
const CAT_PLANNER = "#6E9B33";
const CAT_WORKER = "#4A86DB";

const usd = (n: number) => `$${n.toFixed(2)}`;
const compactUsd = (n: number) => (n >= 1 ? `$${n.toFixed(2)}` : `$${n.toFixed(4)}`);

function SpendContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [range, setRange] = useState<RangeKey>(() => parseRange(searchParams.get("range")));
  const [data, setData] = useState<SpendResponse | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [asTable, setAsTable] = useState(false);

  useEffect(() => {
    router.replace(range === "30d" ? "/spend" : `/spend?range=${range}`, { scroll: false });
  }, [range, router]);

  // Fetch on range change. State is only ever set from the promise callbacks —
  // the effect body itself starts the request and nothing more.
  useEffect(() => {
    let live = true;
    const { from, to } = rangeBounds(range);
    client.getSpend(from, to)
      .then(res => {
        if (!live) return;
        setData(res);
        setError("");
      })
      .catch((e: unknown) => {
        if (!live) return;
        setError(e instanceof Error ? e.message : "Failed to load spend");
      })
      .finally(() => {
        if (live) setLoading(false);
      });
    return () => { live = false; };
  }, [range]);

  const header = (
    <div className="mb-6">
      <p className="eyebrow mb-3"><span className="dot" /> Spend</p>
      <h1 className="text-[32px] font-semibold tracking-tight text-white mb-2">What your runs cost</h1>
      <p className="text-zinc-400 max-w-2xl">
        Estimated from published model prices, for planning and work combined. Your provider bills
        you directly — this is not an invoice.
      </p>
    </div>
  );

  const rangeBar = (
    <div className="flex flex-wrap items-center gap-2 mb-6">
      {RANGES.map(r => (
        <button
          key={r.key}
          type="button"
          onClick={() => setRange(r.key)}
          aria-pressed={range === r.key}
          className={`chip cursor-pointer ${range === r.key ? "border-[#93C645]/40 bg-[#93C645]/10 text-white" : "text-zinc-400"}`}
        >
          {r.label}
        </button>
      ))}
      <div className="flex-1" />
      <button
        type="button"
        onClick={() => setAsTable(v => !v)}
        aria-pressed={asTable}
        className="chip cursor-pointer text-zinc-400 hover:text-white"
      >
        {asTable ? <BarChart3 className="w-3.5 h-3.5" /> : <Table2 className="w-3.5 h-3.5" />}
        {asTable ? "Charts" : "Table"}
      </button>
    </div>
  );

  if (loading) {
    return <div className="p-8 max-w-6xl mx-auto">{header}<div className="glass-panel h-64 animate-pulse" /></div>;
  }

  if (error) {
    return (
      <div className="p-8 max-w-6xl mx-auto">
        {header}
        <div className="glass-panel p-6 flex items-start gap-3 text-sm text-red-300">
          <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
          <span>{error}</span>
        </div>
      </div>
    );
  }

  if (!data) return null;

  // Nothing in this range carries a cost measurement. Showing $0.00 would state
  // a number the data does not support — metering began when it shipped, and
  // jobs older than that were never measured at all.
  if (data.metered_jobs === 0) {
    return (
      <div className="p-8 max-w-6xl mx-auto">
        {header}
        {rangeBar}
        <div className="glass-panel p-10 flex flex-col items-center text-center">
          <Gauge className="w-10 h-10 text-zinc-700 mb-3" />
          <p className="text-sm font-medium text-zinc-300">No measured spend in this range</p>
          <p className="text-xs text-zinc-500 mt-2 max-w-md">
            {data.job_count > 0
              ? `${data.job_count} job${data.job_count === 1 ? "" : "s"} ran, but none carry cost data — metering starts from the day it was deployed, and earlier runs cannot be reconstructed.`
              : "Launch a task and its cost will appear here."}
          </p>
          <Link href="/" className="btn-ghost mt-5">Go to Tasks</Link>
        </div>
      </div>
    );
  }

  const partial = data.metered_jobs < data.job_count;
  const avg = data.metered_jobs > 0 ? data.cost_usd / data.metered_jobs : 0;
  const splitTotal = data.planner_usd + data.worker_usd;
  const plannerPct = splitTotal > 0 ? (data.planner_usd / splitTotal) * 100 : 0;

  const tiles = [
    { label: "Agent-minutes", value: data.agent_minutes.toFixed(1) },
    { label: "Tokens in", value: data.tokens_in.toLocaleString() },
    { label: "Tokens out", value: data.tokens_out.toLocaleString() },
    { label: "Avg per measured job", value: usd(avg) },
  ];

  return (
    <div className="p-8 max-w-6xl mx-auto pb-24">
      {header}
      {rangeBar}

      {/* Two ledgers, side by side. They are different currencies on purpose:
          the left is money the org owes, the right is a quota Kiwi funds. A
          single combined figure would either bill them for work they were not
          charged for, or hide the work Kiwi covered. */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
        <div className="glass-panel p-6">
          <div className="flex items-baseline justify-between mb-1">
            <h2 className="text-sm font-medium text-white">Your keys</h2>
            <span className="text-[10px] text-zinc-600 uppercase tracking-widest">Billed to you</span>
          </div>
          <p className="text-xs text-zinc-500 mb-5">
            Work run on provider keys you connected. This is the only figure you are charged for,
            and it lands on your own provider invoice.
          </p>
          <div className="text-4xl font-light text-white mb-1">
            ${data.cost_usd.toFixed(2)}
          </div>
          <div className="text-xs text-zinc-500 mb-5">
            planner ${data.planner_usd.toFixed(2)} · worker ${data.worker_usd.toFixed(2)}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="p-3 rounded-lg bg-zinc-900/50 border border-white/5">
              <div className="text-lg font-light text-white">{formatTokens(data.tokens_in)}</div>
              <div className="text-[10px] text-zinc-500 uppercase tracking-widest mt-1">Tokens in</div>
            </div>
            <div className="p-3 rounded-lg bg-zinc-900/50 border border-white/5">
              <div className="text-lg font-light text-white">{formatTokens(data.tokens_out)}</div>
              <div className="text-[10px] text-zinc-500 uppercase tracking-widest mt-1">Tokens out</div>
            </div>
          </div>
        </div>

        <div className="glass-panel p-6">
          <div className="flex items-baseline justify-between mb-1">
            <h2 className="text-sm font-medium text-white">Kiwi-provided</h2>
            <span className="text-[10px] text-zinc-600 uppercase tracking-widest">
              {data.plan ? planLabel(data.plan) : "Covered by Kiwi"}
            </span>
          </div>
          <p className="text-xs text-zinc-500 mb-5">
            Work run on Kiwi&apos;s keys. Costs you nothing; it draws on a monthly token allowance
            that resets on the 1st.
          </p>

          {data.allowance_stale ? (
            <div className="text-sm text-amber-400/90">
              Usage could not be read just now, so balances are hidden rather than shown understated.
            </div>
          ) : data.allowance && data.allowance.length > 0 ? (
            <div className="flex flex-col gap-4">
              {CLASS_ORDER.filter(t => data.allowance!.some(a => a.tier === t)).map(t => {
                const a = data.allowance!.find(x => x.tier === t)!;
                const unlimited = a.granted < 0;
                const exhausted = !unlimited && a.remaining <= 0;
                const pct = unlimited ? 100 : Math.min(100, (a.used / Math.max(a.granted, 1)) * 100);
                return (
                  <div key={t}>
                    <div className="flex justify-between items-baseline mb-1">
                      <span className="text-sm text-zinc-200">{modelClassLabel(t)}</span>
                      <span className={`text-xs ${exhausted ? "text-red-400" : "text-zinc-400"}`}>
                        {unlimited ? "Unlimited" : (
                          <>
                            <span className="text-white">{formatTokens(a.remaining)}</span> left
                            <span className="text-zinc-600"> of {formatTokens(a.granted)}</span>
                          </>
                        )}
                      </span>
                    </div>
                    <div className="w-full bg-zinc-800/50 rounded-full h-1.5 overflow-hidden border border-white/5">
                      <div
                        className={`h-full transition-all ${exhausted ? "bg-red-500/70" : "bg-[#93C645]"}`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </div>
                );
              })}
              <div className="text-xs text-zinc-500 pt-1">
                {formatTokens(data.kiwi_tokens_in)} in · {formatTokens(data.kiwi_tokens_out)} out in this range
              </div>
            </div>
          ) : (
            <div className="text-sm text-zinc-500">
              No Kiwi-provided models are available on this deployment yet.
            </div>
          )}
        </div>
      </div>

      {/* Hero. When coverage is partial the figure is a floor, and says so —
          a total that silently excludes unmeasured jobs is a wrong number. */}
      <div className="glass-panel p-6 mb-4">
        <h2 className="text-sm font-medium text-white mb-4">BYOK Costs</h2>
        <div className="text-xs uppercase tracking-widest text-zinc-500 mb-2">
          {partial ? "At least" : "Total owed to providers"}
        </div>
        <div className="text-[48px] leading-none font-semibold text-white">{usd(data.cost_usd)}</div>
        {partial && (
          <p className="text-xs text-amber-400/90 mt-3">
            {data.metered_jobs} of {data.job_count} jobs in this range carry cost data. Earlier runs
            predate metering and are not included.
          </p>
        )}
      </div>

      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-4">
        {tiles.map(t => (
          <div key={t.label} className="glass-panel p-5">
            <div className="text-2xl font-light text-white">{t.value}</div>
            <div className="text-xs text-zinc-500 uppercase tracking-widest mt-1">{t.label}</div>
          </div>
        ))}
      </div>

      {/* Planning vs work. Two segments, directly labelled, with a surface gap
          between them — required because the two hues sit at the CVD floor. */}
      <div className="glass-panel p-6 mb-4">
        <h2 className="text-sm font-medium text-white mb-1">Planning vs work</h2>
        <p className="text-xs text-zinc-500 mb-4">
          Planning defaults to a more capable model than the workers it schedules.
        </p>
        <div className="flex h-8 w-full rounded-lg overflow-hidden gap-[2px]">
          <div style={{ width: `${plannerPct}%`, background: CAT_PLANNER }} className="min-w-[2px]" />
          <div style={{ width: `${100 - plannerPct}%`, background: CAT_WORKER }} className="min-w-[2px]" />
        </div>
        <div className="flex flex-wrap gap-x-6 gap-y-1 mt-3 text-xs">
          <span className="flex items-center gap-2 text-zinc-300">
            <span className="w-2.5 h-2.5 rounded-sm" style={{ background: CAT_PLANNER }} />
            Planning <span className="text-zinc-500">{usd(data.planner_usd)}</span>
          </span>
          <span className="flex items-center gap-2 text-zinc-300">
            <span className="w-2.5 h-2.5 rounded-sm" style={{ background: CAT_WORKER }} />
            Work <span className="text-zinc-500">{usd(data.worker_usd)}</span>
          </span>
        </div>
      </div>

      {asTable ? (
        <TableView data={data} />
      ) : (
        <div className="flex flex-col gap-4">
          <div className="glass-panel p-6">
            <h2 className="text-sm font-medium text-white mb-4">Cost per day</h2>
            <div className="w-full h-[220px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={data.daily.map(d => ({ date: d.date, total: d.planner_usd + d.worker_usd }))}>
                  <CartesianGrid stroke="rgba(234,240,242,0.08)" vertical={false} />
                  <XAxis dataKey="date" stroke="#6E8290" fontSize={11} tickLine={false} />
                  <YAxis stroke="#6E8290" fontSize={11} tickLine={false} axisLine={false} width={56} />
                  <Tooltip
                    contentStyle={{ background: "#0E1A24", border: "1px solid rgba(234,240,242,0.16)", borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: "#9DB0BC" }}
                    formatter={(v) => compactUsd(Number(v))}
                  />
                  <Area type="monotone" dataKey="total" stroke={SEQ} strokeWidth={2} fill={SEQ} fillOpacity={0.15} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>

          <Breakdown title="Cost by repository" rows={data.by_repo} />
          <Breakdown title="Cost by model" rows={data.by_model} />
          <Breakdown title="Cost by provider" rows={data.by_provider} />
        </div>
      )}
    </div>
  );
}

function Breakdown({ title, rows }: { title: string; rows: SpendResponse["by_repo"] }) {
  if (rows.length === 0) return null;
  return (
    <div className="glass-panel p-6">
      <h2 className="text-sm font-medium text-white mb-4">{title}</h2>
      <div className="w-full" style={{ height: Math.max(120, rows.length * 38) }}>
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={rows} layout="vertical" margin={{ left: 8, right: 16 }}>
            <CartesianGrid stroke="rgba(234,240,242,0.08)" horizontal={false} />
            <XAxis type="number" stroke="#6E8290" fontSize={11} tickLine={false} axisLine={false} />
            <YAxis type="category" dataKey="label" stroke="#9DB0BC" fontSize={11} width={150} tickLine={false} axisLine={false} />
            <Tooltip
              cursor={{ fill: "rgba(255,255,255,0.04)" }}
              contentStyle={{ background: "#0E1A24", border: "1px solid rgba(234,240,242,0.16)", borderRadius: 8, fontSize: 12 }}
              formatter={(v) => compactUsd(Number(v))}
            />
            {/* One hue for every bar: these categories have no natural order. */}
            <Bar dataKey="total_usd" radius={[0, 4, 4, 0]} barSize={18}>
              {rows.map(r => <Cell key={r.label} fill={SEQ} fillOpacity={0.75} />)}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}

function TableView({ data }: { data: SpendResponse }) {
  const section = (title: string, rows: SpendResponse["by_repo"]) => (
    <div className="glass-panel p-6 overflow-x-auto">
      <h2 className="text-sm font-medium text-white mb-3">{title}</h2>
      <table className="w-full text-sm min-w-[420px]">
        <thead>
          <tr className="text-left text-[11px] uppercase tracking-widest text-zinc-500">
            <th className="pb-2 font-medium">Name</th>
            <th className="pb-2 font-medium text-right">Planning</th>
            <th className="pb-2 font-medium text-right">Work</th>
            <th className="pb-2 font-medium text-right">Total</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(r => (
            <tr key={r.label} className="border-t border-white/5">
              <td className="py-2 text-zinc-200 font-mono text-xs">{r.label}</td>
              <td className="py-2 text-right text-zinc-400">{compactUsd(r.planner_usd)}</td>
              <td className="py-2 text-right text-zinc-400">{compactUsd(r.worker_usd)}</td>
              <td className="py-2 text-right text-zinc-200">{compactUsd(r.total_usd)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );

  return (
    <div className="flex flex-col gap-4">
      {section("Cost by repository", data.by_repo)}
      {section("Cost by model", data.by_model)}
      {section("Cost by provider", data.by_provider)}
    </div>
  );
}

export default function SpendPage() {
  return (
    <Suspense fallback={<LoadingState label="Loading spend…" className="min-h-[70vh]" />}>
      <SpendContent />
    </Suspense>
  );
}
