"use client";

import { useEffect, useRef, useState, useMemo } from "react";
import { client, type TelemetryMetric, type GithubRepo } from "@/lib/api";
import {
  LineChart,
  Plus,
  Trash2,
  AlertCircle,
  CheckCircle2,
  Activity,
  Radar,
  ArrowDownRight,
  ArrowUpRight,
  Database,
} from "lucide-react";
import { Select } from "@/components/Select";
import { Logo } from "@/components/Logo";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { SiDatadog, SiPrometheus } from "react-icons/si";

const PROVIDER_OPTIONS = [
  { value: "prometheus", label: "Prometheus Metric Store" },
  { value: "datadog", label: "Datadog Real-Time APM" },
];

const DIRECTION_OPTIONS = [
  { value: "lower_is_better", label: "Lower is better (Latency, Error Rate, Memory)" },
  { value: "higher_is_better", label: "Higher is better (Throughput, RPS, Success Rate)" },
];

export default function MetricsPage() {
  const [metrics, setMetrics] = useState<TelemetryMetric[]>([]);
  const [repos, setRepos] = useState<GithubRepo[]>([]);

  const [repo, setRepo] = useState("");
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("prometheus");
  const [query, setQuery] = useState("");
  const [direction, setDirection] = useState("lower_is_better");

  const [testResult, setTestResult] = useState<{ ok: boolean; message: string; sampleCount?: number; mean?: number } | null>(null);
  const [testedQueryKey, setTestedQueryKey] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [reposLoading, setReposLoading] = useState(true);
  const [error, setError] = useState("");

  const load = async () => {
    try {
      const [metricsRes, reposRes] = await Promise.all([
        client.listTelemetryMetrics().catch(() => ({ metrics: [] })),
        client.listGithubRepos().catch(() => ({ repos: [] })),
      ]);
      setMetrics(metricsRes.metrics || []);
      setRepos(reposRes.repos || []);
      if (!repo && reposRes.repos && reposRes.repos.length > 0) {
        setRepo(reposRes.repos[0].full_name);
      }
      setReposLoading(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load telemetry metrics");
      setReposLoading(false);
    }
  };

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const currentQueryKey = `${provider}::${query}`;
  const isTested = testedQueryKey === currentQueryKey;
  const canSave = !!repo && !!name.trim() && !!query.trim() && isTested && testResult?.ok === true;

  const liveQueryKeyRef = useRef(currentQueryKey);
  useEffect(() => {
    liveQueryKeyRef.current = currentQueryKey;
  }, [currentQueryKey]);

  const runTest = async () => {
    if (!query.trim()) {
      setError("Please input a query string first.");
      return;
    }
    const keyAtCallTime = currentQueryKey;
    setError("");
    setTesting(true);
    setTestResult(null);
    try {
      const res = await client.testTelemetryQuery(provider, query.trim());
      if (keyAtCallTime !== liveQueryKeyRef.current) return;
      setTestResult({
        ok: true,
        message: `Query verified successfully — ${res.sample_count} sample(s) collected in the last 15 minutes (mean: ${res.mean.toFixed(2)}).`,
        sampleCount: res.sample_count,
        mean: res.mean,
      });
      setTestedQueryKey(keyAtCallTime);
    } catch (e) {
      if (keyAtCallTime !== liveQueryKeyRef.current) return;
      setTestResult({ ok: false, message: e instanceof Error ? e.message : "Query execution failed on remote provider." });
      setTestedQueryKey(null);
    } finally {
      setTesting(false);
    }
  };

  const save = async () => {
    if (!canSave) return;
    setError("");
    setSaving(true);
    try {
      await client.createTelemetryMetric(repo, name.trim(), provider, query.trim(), direction);
      setName("");
      setQuery("");
      setProvider("prometheus");
      setDirection("lower_is_better");
      setTestResult(null);
      setTestedQueryKey(null);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save metric rule");
    } finally {
      setSaving(false);
    }
  };

  const remove = async (id: string) => {
    if (!confirm("Remove this telemetry metric? Post-merge verification will no longer watch it.")) return;
    setError("");
    try {
      await client.deleteTelemetryMetric(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete metric");
    }
  };

  // Stats calculation
  const stats = useMemo(() => {
    const promCount = metrics.filter((m) => m.provider === "prometheus").length;
    const ddCount = metrics.filter((m) => m.provider === "datadog").length;
    const reposWithMetrics = new Set(metrics.map((m) => m.repo)).size;
    return { total: metrics.length, promCount, ddCount, reposWithMetrics };
  }, [metrics]);

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-4 w-full font-sans text-stone-900 select-none">
      
      {/* Header Banner with Modern Swiss Aesthetics */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-sand-50 border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
            <Logo variant="full-color" pose="guarding" animated={true} className="w-7 h-7" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-indigo-800 bg-indigo-50 px-2 py-0.5 rounded border border-indigo-200 flex items-center gap-1">
                <LineChart className="w-3 h-3 text-indigo-600" />
                <span>SLO &amp; TELEMETRY GOVERNANCE</span>
              </span>
            </div>
            <h1 className="text-lg font-bold text-stone-900 tracking-tight mt-0.5">
              Production Telemetry &amp; SLO Monitors
            </h1>
            <p className="text-xs text-stone-500 mt-0.5">
              Connect Datadog and Prometheus metrics. Kiwi Watchdogs verify p99 latency, error rates, and regressions after merging.
            </p>
          </div>
        </div>

        <button
          onClick={load}
          className="px-3 py-1.5 rounded-xl bg-sand-50/80 hover:bg-sand-100 border border-sand-200/90 text-stone-700 font-semibold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer self-end sm:self-center"
        >
          <Activity className="w-3.5 h-3.5 text-stone-500" />
          <span>Refresh</span>
        </button>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 shadow-2xs font-mono">
          <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* KPI Tiles */}
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Configured SLOs</span>
            <LineChart className="w-3.5 h-3.5 text-indigo-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-stone-900">{stats.total}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Active canary queries</div>
          </div>
        </div>

        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Protected Repositories</span>
            <Database className="w-3.5 h-3.5 text-emerald-600" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-emerald-800">{stats.reposWithMetrics}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">Monitored codebases</div>
          </div>
        </div>

        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Prometheus Rules</span>
            <SiPrometheus className="w-3.5 h-3.5 text-[#E6522C]" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-stone-900">{stats.promCount}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">PromQL queries</div>
          </div>
        </div>

        <div className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Datadog Monitors</span>
            <SiDatadog className="w-3.5 h-3.5 text-[#632CA6]" />
          </div>
          <div className="mt-2">
            <div className="text-xl font-bold font-mono text-stone-900">{stats.ddCount}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">APM &amp; Trace metrics</div>
          </div>
        </div>
      </div>

      {/* Main Metric Configuration Card */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-4">
        <div>
          <h2 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Plus className="w-3.5 h-3.5 text-stone-700" />
            <span>Create Canary SLO Metric</span>
          </h2>
          <p className="text-xs text-stone-500 mt-0.5">
            Define a query that Kiwi will sample after PRs merge. If regression bounds exceed thresholds, Kiwi alerts and auto-triages.
          </p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
          <div>
            <label className="block font-bold text-stone-800 mb-1">Target Repository</label>
            {reposLoading ? (
              <div className="p-2 rounded-xl bg-sand-50 border border-sand-200 text-xs text-stone-400 font-mono">
                Loading repositories...
              </div>
            ) : repos.length > 0 ? (
              <Select
                ariaLabel="Target Repository"
                value={repo}
                onChange={setRepo}
                placeholder="Select codebase..."
                searchable
                options={repos.map((r) => ({
                  value: r.full_name,
                  label: r.full_name,
                  hint: r.private ? "private" : "public",
                }))}
              />
            ) : (
              <p className="text-xs text-amber-800 bg-amber-50 p-2 rounded-xl border border-amber-200">
                No repositories connected. Connect GitHub in Integrations first.
              </p>
            )}
          </div>

          <div>
            <label className="block font-bold text-stone-800 mb-1">Metric Metric Identifier</label>
            <input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. checkout_p99_latency"
              className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono text-stone-900 outline-none focus:border-stone-900 transition-all shadow-2xs"
            />
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
          <div>
            <label className="block font-bold text-stone-800 mb-1">Telemetry Provider</label>
            <Select
              ariaLabel="Provider"
              value={provider}
              onChange={(v) => {
                setProvider(v);
                setTestResult(null);
                setTestedQueryKey(null);
              }}
              options={PROVIDER_OPTIONS}
            />
          </div>

          <div>
            <label className="block font-bold text-stone-800 mb-1">Health Direction</label>
            <Select
              ariaLabel="Comparison Direction"
              value={direction}
              onChange={setDirection}
              options={DIRECTION_OPTIONS}
            />
          </div>
        </div>

        <div>
          <label className="block font-bold text-stone-800 mb-1 flex items-center justify-between">
            <span>Query String</span>
            <span className="text-[10px] font-mono text-stone-400">
              {provider === "datadog" ? "Datadog metric syntax" : "PromQL syntax"}
            </span>
          </label>
          <textarea
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
              setTestResult(null);
              setTestedQueryKey(null);
            }}
            placeholder={
              provider === "datadog"
                ? "p99:trace.checkout.request{env:production}"
                : "histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))"
            }
            rows={2}
            className="w-full p-3 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono text-stone-900 outline-none focus:border-stone-900 transition-all shadow-2xs resize-none"
          />
        </div>

        {/* Live Query Test Feedback */}
        {testResult && (
          <div
            className={`p-3 rounded-xl text-xs font-mono border flex items-start gap-2 shadow-2xs ${
              testResult.ok
                ? "bg-emerald-50 border-emerald-200 text-emerald-900"
                : "bg-rose-50 border-rose-200 text-rose-900"
            }`}
          >
            {testResult.ok ? (
              <CheckCircle2 className="w-4 h-4 text-emerald-600 shrink-0 mt-0.5" />
            ) : (
              <AlertCircle className="w-4 h-4 text-rose-600 shrink-0 mt-0.5" />
            )}
            <div className="min-w-0">
              <span className="font-bold">{testResult.ok ? "Verification Passed: " : "Error: "}</span>
              <span>{testResult.message}</span>
            </div>
          </div>
        )}

        {/* Action Buttons */}
        <div className="flex items-center justify-between pt-2 border-t border-sand-200/80">
          <button
            onClick={runTest}
            disabled={testing || !query.trim()}
            className="px-4 py-2 rounded-xl border border-sand-200 bg-sand-50/90 hover:bg-sand-100 text-stone-800 text-xs font-bold flex items-center gap-1.5 transition-all cursor-pointer disabled:opacity-40 shadow-2xs"
          >
            {testing ? <KiwiMicroButtonLoader /> : <Radar className="w-3.5 h-3.5 text-indigo-600" />}
            <span>Test Query Connection</span>
          </button>

          <button
            onClick={save}
            disabled={saving || !canSave}
            title={!isTested ? "Test query before saving" : undefined}
            className="px-5 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white text-xs font-bold flex items-center gap-1.5 transition-all cursor-pointer disabled:opacity-40 shadow-2xs"
          >
            {saving ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
            <span>Save Metric Rule</span>
          </button>
        </div>
      </div>

      {/* Configured Metrics Roster Card */}
      <div className="bg-white border border-sand-200/90 rounded-2xl shadow-2xs p-4 sm:p-5 space-y-3">
        <div>
          <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Activity className="w-3.5 h-3.5 text-stone-600" />
            <span>Configured Telemetry Metrics ({metrics.length})</span>
          </h3>
          <p className="text-xs text-stone-500 mt-0.5">
            Metrics evaluated on canary releases after pull request merges.
          </p>
        </div>

        {metrics.length === 0 ? (
          <div className="p-8 rounded-2xl border border-sand-200/90 bg-sand-50/40 text-center space-y-2.5 shadow-2xs">
            <div className="w-12 h-12 mx-auto rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center">
              <Logo variant="full-color" pose="sleeping" animated={true} className="w-7 h-7" />
            </div>
            <div className="space-y-0.5">
              <div className="text-stone-900 font-bold text-xs">No Canary Metrics Configured</div>
              <p className="text-xs text-stone-500 max-w-xs mx-auto">
                Add a Datadog or Prometheus query above to automatically monitor service health and detect regressions.
              </p>
            </div>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            {metrics.map((m) => {
              const isLower = m.comparison_direction === "lower_is_better";
              const isProm = m.provider === "prometheus";

              return (
                <div
                  key={m.id}
                  className="p-3.5 rounded-xl bg-white border border-sand-200/90 shadow-2xs flex flex-col justify-between space-y-2 hover:border-sand-300 transition-all group"
                >
                  <div className="flex items-start justify-between gap-2.5">
                    <div className="min-w-0 space-y-0.5">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {isProm ? (
                          <SiPrometheus className="w-3.5 h-3.5 text-[#E6522C] shrink-0" />
                        ) : (
                          <SiDatadog className="w-3.5 h-3.5 text-[#632CA6] shrink-0" />
                        )}
                        <span className="font-mono text-xs font-bold text-stone-900 truncate">{m.name}</span>
                      </div>
                      <div className="text-[10px] font-mono text-stone-500 truncate">
                        {m.repo} • <span className="capitalize">{m.provider}</span>
                      </div>
                    </div>

                    <button
                      onClick={() => remove(m.id)}
                      className="p-1 text-stone-400 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors cursor-pointer"
                      title="Delete metric rule"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>

                  <div className="p-2 rounded-lg bg-sand-50/80 border border-sand-200/80 font-mono text-[11px] text-stone-700 truncate select-all">
                    <code>{m.query}</code>
                  </div>

                  <div className="flex items-center justify-between text-[10px] font-mono pt-1">
                    <span
                      className={`px-2 py-0.5 rounded-md border flex items-center gap-1 font-bold ${
                        isLower
                          ? "bg-sky-50 text-sky-800 border-sky-200"
                          : "bg-emerald-50 text-emerald-800 border-emerald-200"
                      }`}
                    >
                      {isLower ? (
                        <ArrowDownRight className="w-3 h-3 text-sky-600" />
                      ) : (
                        <ArrowUpRight className="w-3 h-3 text-emerald-600" />
                      )}
                      <span>{isLower ? "Lower is better" : "Higher is better"}</span>
                    </span>

                    <span className="text-stone-400">Canary Guard</span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

    </div>
  );
}
