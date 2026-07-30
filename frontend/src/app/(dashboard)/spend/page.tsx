"use client";

import { useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { client, SpendResponse } from "@/lib/api";
import { 
  AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid,
  BarChart, Bar
} from "recharts";
import { Activity, LayoutGrid, List } from "lucide-react";

export default function SpendPage() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [data, setData] = useState<SpendResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<"charts" | "table">("charts");

  // Default to last 30 days
  const defaultTo = new Date();
  const defaultFrom = new Date(defaultTo.getTime() - 30 * 24 * 60 * 60 * 1000);
  
  const fromParam = searchParams.get("from");
  const toParam = searchParams.get("to");
  
  const from = fromParam || defaultFrom.toISOString();
  const to = toParam || defaultTo.toISOString();

  useEffect(() => {
    async function load() {
      setLoading(true);
      setError(null);
      try {
        const res = await client.getSpend(from, to);
        setData(res);
      } catch (e: any) {
        setError(e.message || "Failed to load spend data");
      } finally {
        setLoading(false);
      }
    }
    load();
  }, [from, to]);

  const setDateRange = (days: number) => {
    const end = new Date();
    const start = new Date(end.getTime() - days * 24 * 60 * 60 * 1000);
    const params = new URLSearchParams(searchParams.toString());
    params.set("from", start.toISOString());
    params.set("to", end.toISOString());
    router.replace(`/spend?${params.toString()}`);
  };

  if (loading && !data) {
    return (
      <div className="flex h-full items-center justify-center">
        <Activity className="h-8 w-8 animate-spin text-gray-500" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="rounded-md bg-red-900/20 p-4 border border-red-900">
        <h3 className="text-sm font-medium text-red-500">Error loading spend data</h3>
        <p className="mt-2 text-sm text-red-400">{error}</p>
      </div>
    );
  }

  if (!data) return null;

  const brandGreen = "#10B981"; // emerald-500
  const colors = ["#10B981", "#3B82F6", "#F59E0B", "#EF4444", "#8B5CF6", "#6366F1", "#6B7280"];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold tracking-tight text-white">Spend</h1>
        
        <div className="flex items-center space-x-4">
          <select 
            className="bg-[#1E1E1E] border-gray-700 text-white rounded-md text-sm focus:ring-emerald-500 focus:border-emerald-500"
            onChange={(e) => setDateRange(parseInt(e.target.value))}
            value={Math.round((new Date(to).getTime() - new Date(from).getTime()) / (1000 * 60 * 60 * 24))}
          >
            <option value={7}>Last 7 days</option>
            <option value={30}>Last 30 days</option>
            <option value={90}>Last 90 days</option>
            <option value={365}>Last 365 days</option>
          </select>

          <div className="flex bg-[#1E1E1E] rounded-md p-1">
            <button
              onClick={() => setViewMode("charts")}
              className={`p-1.5 rounded ${viewMode === "charts" ? "bg-gray-700 text-white" : "text-gray-400 hover:text-white"}`}
            >
              <LayoutGrid className="w-4 h-4" />
            </button>
            <button
              onClick={() => setViewMode("table")}
              className={`p-1.5 rounded ${viewMode === "table" ? "bg-gray-700 text-white" : "text-gray-400 hover:text-white"}`}
            >
              <List className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
          <h3 className="text-sm font-medium text-gray-400 mb-2">Total Spend</h3>
          <div className="text-4xl font-bold text-white">${data.total_usd.toFixed(2)}</div>
        </div>
        <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
          <h3 className="text-sm font-medium text-gray-400 mb-2">Jobs Submitted</h3>
          <div className="text-3xl font-semibold text-white">{data.job_count}</div>
          {data.metered_jobs < data.job_count && (
            <div className="text-xs text-gray-500 mt-1">({data.metered_jobs} metered)</div>
          )}
        </div>
        <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
          <h3 className="text-sm font-medium text-gray-400 mb-2">Cost per Job (Avg)</h3>
          <div className="text-3xl font-semibold text-white">
            ${data.metered_jobs > 0 ? (data.total_usd / data.metered_jobs).toFixed(2) : "0.00"}
          </div>
        </div>
      </div>

      {viewMode === "charts" ? (
        <div className="space-y-6">
          <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
            <h3 className="text-lg font-medium text-white mb-6">Daily Spend</h3>
            <div className="h-72">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={data.daily}>
                  <defs>
                    <linearGradient id="colorTotal" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor={brandGreen} stopOpacity={0.3}/>
                      <stop offset="95%" stopColor={brandGreen} stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="#2D2D2D" vertical={false} />
                  <XAxis 
                    dataKey="date" 
                    stroke="#6B7280"
                    tickFormatter={(val) => {
                      const d = new Date(val);
                      return `${d.getMonth()+1}/${d.getDate()}`;
                    }}
                  />
                  <YAxis 
                    stroke="#6B7280"
                    tickFormatter={(val) => `$${val.toFixed(2)}`}
                  />
                  <Tooltip 
                    contentStyle={{ backgroundColor: "#1F2937", border: "none", color: "#fff" }}
                    itemStyle={{ color: "#fff" }}
                    formatter={(val: number) => [`$${val.toFixed(4)}`, "Spend"]}
                    labelFormatter={(label) => `Date: ${label}`}
                  />
                  <Area 
                    type="monotone" 
                    dataKey={(d) => d.planner_usd + d.worker_usd}
                    stroke={brandGreen} 
                    fillOpacity={1} 
                    fill="url(#colorTotal)" 
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
              <h3 className="text-lg font-medium text-white mb-6">By Repository</h3>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={data.by_repository} layout="vertical" margin={{ left: 80 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#2D2D2D" horizontal={true} vertical={false} />
                    <XAxis type="number" stroke="#6B7280" tickFormatter={(val) => `$${val.toFixed(2)}`} />
                    <YAxis type="category" dataKey="label" stroke="#6B7280" width={120} />
                    <Tooltip 
                      cursor={{ fill: "#2D2D2D" }}
                      contentStyle={{ backgroundColor: "#1F2937", border: "none", color: "#fff" }}
                      formatter={(val: number) => [`$${val.toFixed(4)}`, "Spend"]}
                    />
                    <Bar dataKey="value_usd" fill={colors[1]} radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>

            <div className="bg-[#161616] border border-gray-800 rounded-lg p-6">
              <h3 className="text-lg font-medium text-white mb-6">By Model</h3>
              <div className="h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={data.by_model} layout="vertical" margin={{ left: 80 }}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#2D2D2D" horizontal={true} vertical={false} />
                    <XAxis type="number" stroke="#6B7280" tickFormatter={(val) => `$${val.toFixed(2)}`} />
                    <YAxis type="category" dataKey="label" stroke="#6B7280" width={120} />
                    <Tooltip 
                      cursor={{ fill: "#2D2D2D" }}
                      contentStyle={{ backgroundColor: "#1F2937", border: "none", color: "#fff" }}
                      formatter={(val: number) => [`$${val.toFixed(4)}`, "Spend"]}
                    />
                    <Bar dataKey="value_usd" fill={colors[2]} radius={[0, 4, 4, 0]} />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </div>
          </div>
        </div>
      ) : (
        <div className="bg-[#161616] border border-gray-800 rounded-lg overflow-hidden">
          <table className="min-w-full divide-y divide-gray-800">
            <thead className="bg-[#1E1E1E]">
              <tr>
                <th className="px-6 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Date</th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-400 uppercase tracking-wider">Planner Cost</th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-400 uppercase tracking-wider">Worker Cost</th>
                <th className="px-6 py-3 text-right text-xs font-medium text-gray-400 uppercase tracking-wider">Total</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-800 bg-[#161616]">
              {data.daily.map((d) => (
                <tr key={d.date}>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-gray-300">{d.date}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-right text-gray-400">${d.planner_usd.toFixed(4)}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-right text-gray-400">${d.worker_usd.toFixed(4)}</td>
                  <td className="px-6 py-4 whitespace-nowrap text-sm text-right font-medium text-white">
                    ${(d.planner_usd + d.worker_usd).toFixed(4)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
