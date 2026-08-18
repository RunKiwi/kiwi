"use client";

import { useEffect, useRef, useState } from "react";
import { client, type TelemetryMetric, type GithubRepo } from "@/lib/api";
import { LineChart, Plus, Trash2, Loader2, AlertCircle, CheckCircle2 } from "lucide-react";
import { Select } from "@/components/Select";

const PROVIDER_OPTIONS = [
  { value: "prometheus", label: "Prometheus" },
  { value: "datadog", label: "Datadog" },
];

const DIRECTION_OPTIONS = [
  { value: "lower_is_better", label: "Lower is better (latency, error rate)" },
  { value: "higher_is_better", label: "Higher is better (throughput)" },
];

export default function MetricsPage() {
  const [metrics, setMetrics] = useState<TelemetryMetric[]>([]);
  const [repos, setRepos] = useState<GithubRepo[]>([]);

  const [repo, setRepo] = useState("");
  const [name, setName] = useState("");
  const [provider, setProvider] = useState("prometheus");
  const [query, setQuery] = useState("");
  const [direction, setDirection] = useState("lower_is_better");

  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null);
  const [testedQueryKey, setTestedQueryKey] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(false);
  const [reposLoading, setReposLoading] = useState(true);
  const [error, setError] = useState("");

  const load = async () => {
    setLoading(true);
    try {
      const [metricsRes, reposRes] = await Promise.all([
        client.listTelemetryMetrics(),
        client.listGithubRepos(),
      ]);
      setMetrics(metricsRes.metrics);
      setRepos(reposRes.repos);
      setReposLoading(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load data");
    } finally {
      setLoading(false);
    }
  };
  useEffect(() => { load(); }, []);

  // The test result is keyed to the exact provider+query it was run
  // against — editing either after a successful test must re-disable Save,
  // or a stale "tested OK" badge on since-edited text would defeat the
  // point of the gate.
  const currentQueryKey = `${provider}::${query}`;
  const isTested = testedQueryKey === currentQueryKey;
  const canSave = !!repo && !!name.trim() && !!query.trim() && isTested && testResult?.ok === true;

  // Tracks the live provider+query key so an in-flight runTest call can
  // notice, at resolution time, that the query was edited after it started
  // — otherwise a stale "Queried OK" banner can clobber the current
  // (already-edited, already-disabled-Save) state. currentQueryKey alone
  // can't be used for this: it's read from the closure captured when
  // runTest was invoked, which is frozen at the pre-edit value.
  const liveQueryKeyRef = useRef(currentQueryKey);
  useEffect(() => {
    liveQueryKeyRef.current = currentQueryKey;
  }, [currentQueryKey]);

  const runTest = async () => {
    if (!query.trim()) { setError("Enter a query first."); return; }
    const keyAtCallTime = currentQueryKey;
    setError(""); setTesting(true); setTestResult(null);
    try {
      const res = await client.testTelemetryQuery(provider, query.trim());
      if (keyAtCallTime !== liveQueryKeyRef.current) return;
      setTestResult({ ok: true, message: `Queried OK — ${res.sample_count} sample(s) in the last 15 minutes (mean ${res.mean.toFixed(2)}).` });
      setTestedQueryKey(keyAtCallTime);
    } catch (e) {
      if (keyAtCallTime !== liveQueryKeyRef.current) return;
      setTestResult({ ok: false, message: e instanceof Error ? e.message : "Query failed" });
      setTestedQueryKey(null);
    } finally {
      setTesting(false);
    }
  };

  const save = async () => {
    if (!canSave) return;
    setError(""); setSaving(true);
    try {
      await client.createTelemetryMetric(repo, name.trim(), provider, query.trim(), direction);
      setRepo(""); setName(""); setQuery(""); setProvider("prometheus"); setDirection("lower_is_better");
      setTestResult(null); setTestedQueryKey(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save metric");
    } finally {
      setSaving(false);
    }
    // Refresh the list after save completes (keep loading state active)
    await load();
  };

  const remove = async (id: string) => {
    if (!confirm("Remove this metric? Post-merge verification will stop watching it.")) return;
    setError("");
    try {
      await client.deleteTelemetryMetric(id);
      await load();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to delete metric");
    }
  };

  return (
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col text-white">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight mb-2">Metrics</h1>
        <p className="text-zinc-400">
          Telemetry metrics post-merge verification watches after a Kiwi PR merges. Configure one below,
          or leave this empty — repos with none run on GitHub-native signals alone.
        </p>
      </div>

      <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-8">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-3">
          <div>
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Repository</label>
            {reposLoading ? (
              <div className="flex items-center gap-2 text-sm text-zinc-500">
                <Loader2 className="w-4 h-4 animate-spin" /> Loading repositories...
              </div>
            ) : repos.length > 0 ? (
              <Select
                ariaLabel="Repository" value={repo} onChange={setRepo} placeholder="Select…" searchable
                options={repos.map(r => ({ value: r.full_name, label: r.full_name, hint: r.private ? "private" : undefined }))}
              />
            ) : (
              <p className="text-xs text-amber-400/90">
                No repositories yet. Connect GitHub under Integrations, then pick one here.
              </p>
            )}
          </div>
          <div>
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Name</label>
            <input value={name} onChange={e => setName(e.target.value)} placeholder="checkout_p95_latency"
              className="w-full field text-sm" />
          </div>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-3">
          <div>
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Provider</label>
            <Select ariaLabel="Provider" value={provider}
              onChange={v => { setProvider(v); setTestResult(null); setTestedQueryKey(null); }}
              options={PROVIDER_OPTIONS} />
          </div>
          <div>
            <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Direction</label>
            <Select ariaLabel="Comparison direction" value={direction} onChange={setDirection} options={DIRECTION_OPTIONS} />
          </div>
        </div>
        <div className="mb-3">
          <label className="block text-[10px] font-bold text-zinc-500 uppercase tracking-widest mb-1.5">Query</label>
          <textarea
            value={query}
            onChange={e => { setQuery(e.target.value); setTestResult(null); setTestedQueryKey(null); }}
            placeholder={provider === "datadog" ? "p95:trace.checkout{env:prod}" : "rate(http_requests_total[5m])"}
            rows={2}
            className="w-full field text-sm font-mono"
          />
        </div>

        {testResult && (
          <div className={`flex items-center gap-2 text-sm mb-3 ${testResult.ok ? "text-green-400" : "text-red-400"}`}>
            {testResult.ok ? <CheckCircle2 className="w-4 h-4 shrink-0" /> : <AlertCircle className="w-4 h-4 shrink-0" />}
            {testResult.message}
          </div>
        )}
        {error && <div className="flex items-center gap-2 text-red-400 text-sm mb-3"><AlertCircle className="w-4 h-4" />{error}</div>}

        <div className="flex gap-3">
          <button onClick={runTest} disabled={testing || !query.trim()}
            className="flex items-center justify-center gap-2 px-4 py-2 rounded-lg font-semibold border border-white/10 bg-white/5 text-white hover:bg-white/10 disabled:opacity-50 h-[38px]">
            {testing ? <Loader2 className="w-4 h-4 animate-spin" /> : null} Test query
          </button>
          <button onClick={save} disabled={saving || !canSave}
            title={!isTested ? "Test the query first" : undefined}
            className="flex items-center justify-center gap-2 btn-primary px-4 py-2 rounded-lg font-semibold disabled:opacity-50 h-[38px]">
            {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />} Save
          </button>
        </div>
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Configured</h2>
      {metrics.length === 0 ? (
        <p className="text-zinc-500 text-sm">No metrics configured yet.</p>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {metrics.map(m => (
            <div key={m.id} className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between group">
              <div className="flex items-center gap-3 min-w-0">
                <LineChart className="w-5 h-5 text-zinc-400 shrink-0" />
                <div className="min-w-0">
                  <div className="text-sm text-white truncate">{m.name}</div>
                  <div className="text-xs text-zinc-500 truncate">{m.repo} · {m.provider}</div>
                  <div className="text-[11px] text-zinc-600 font-mono truncate">{m.query}</div>
                </div>
              </div>
              <button
                onClick={() => remove(m.id)}
                aria-label={`Delete metric ${m.name}`}
                className="text-zinc-600 hover:text-red-400 transition-colors opacity-100 md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100 shrink-0"
              >
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
