"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { api, type SlackChannelBinding, type SlackInstallation } from "@/lib/api";
import {
  Trash2,
  Plus,
  AlertCircle,
  ArrowLeft,
  Hash,
  GitBranch,
  Cpu,
  Terminal,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
} from "lucide-react";
import { FaSlack } from "react-icons/fa6";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";

export default function SlackBindingsPage() {
  const [bindings, setBindings] = useState<SlackChannelBinding[]>([]);
  const [installations, setInstallations] = useState<SlackInstallation[]>([]);
  const [loading, setLoading] = useState(true);

  // Form State
  const [teamID, setTeamID] = useState("");
  const [channelID, setChannelID] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [defaultRef, setDefaultRef] = useState("");
  const [defaultTestCmd, setDefaultTestCmd] = useState("");
  const [defaultModel, setDefaultModel] = useState("");
  const [defaultArchitectModel, setDefaultArchitectModel] = useState("");
  const [showAdvanced, setShowAdvanced] = useState(false);

  const [error, setError] = useState<string | null>(null);
  const [createError, setCreateError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [deletingId, setDeletingId] = useState<string | null>(null);

  async function loadData() {
    const [bRes, iRes] = await Promise.all([
      api.listSlackBindings(),
      api.listSlackInstallations().catch(() => ({ installations: [] })),
    ]);
    return { bindings: bRes.bindings || [], installations: iRes.installations || [] };
  }

  async function refresh() {
    try {
      const { bindings, installations } = await loadData();
      setBindings(bindings);
      setInstallations(installations);
      if (installations.length > 0 && !teamID) {
        setTeamID(installations[0].team_id);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load bindings");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    let active = true;
    loadData()
      .then(({ bindings, installations }) => {
        if (!active) return;
        setBindings(bindings);
        setInstallations(installations);
        if (installations.length > 0) {
          setTeamID(installations[0].team_id);
        }
      })
      .catch((err) => {
        if (!active) return;
        setError(err instanceof Error ? err.message : "Failed to load bindings");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, []);

  async function onCreate(e: React.FormEvent) {
    e.preventDefault();
    setCreateError(null);
    setCreating(true);
    try {
      await api.createSlackBinding({
        team_id: teamID.trim(),
        channel_id: channelID.trim(),
        repo_url: repoURL.trim(),
        default_ref: defaultRef.trim() || undefined,
        default_test_cmd: defaultTestCmd.trim() || undefined,
        default_model: defaultModel.trim() || undefined,
        default_architect_model: defaultArchitectModel.trim() || undefined,
      });
      setChannelID("");
      setRepoURL("");
      setDefaultRef("");
      setDefaultTestCmd("");
      setDefaultModel("");
      setDefaultArchitectModel("");
      await refresh();
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : "Failed to create binding");
    } finally {
      setCreating(false);
    }
  }

  async function onDelete(id: string) {
    if (!confirm("Are you sure you want to remove this channel mapping?")) return;
    setDeletingId(id);
    try {
      await api.deleteSlackBinding(id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to delete binding");
    } finally {
      setDeletingId(null);
    }
  }

  return (
    <div className="max-w-6xl mx-auto flex flex-col gap-6 w-full font-sans text-stone-900">
      {/* ================= HERO HEADER ================= */}
      <div className="flex flex-col gap-3 pb-2 border-b border-sand-200">
        <Link
          href="/integrations"
          className="inline-flex items-center gap-1.5 text-xs text-stone-500 hover:text-stone-900 transition-colors w-fit font-medium"
        >
          <ArrowLeft className="w-3.5 h-3.5" />
          <span>Back to Integrations</span>
        </Link>

        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 className="text-2xl font-bold tracking-tight text-stone-900 flex items-center gap-2.5">
              <div className="w-8 h-8 rounded-xl bg-[#ECB22E]/15 text-[#ECB22E] flex items-center justify-center border border-[#ECB22E]/30">
                <FaSlack className="w-4 h-4" />
              </div>
              <span>Slack Channel Bindings</span>
              <span className="text-xs font-mono font-bold bg-sand-100 text-stone-600 border border-sand-200 px-2 py-0.5 rounded-md">
                {bindings.length} Mappings
              </span>
            </h1>
            <p className="text-xs text-stone-500 mt-1 max-w-2xl leading-relaxed">
              Bind Slack channels to GitHub repositories so <span className="font-mono bg-sand-100 text-stone-700 px-1 py-0.5 rounded">@kiwi</span> mentions in that channel automatically execute against the correct repository, branch, test suite, and models.
            </p>
          </div>
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* ================= 3 KPI METRIC TILES ================= */}
      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3.5">
        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Configured Bindings</span>
            <Hash className="w-4 h-4 text-stone-400" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{bindings.length}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              {bindings.length > 0 ? "Active channel triggers" : "No channels bound yet"}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Slack Workspaces</span>
            <FaSlack className="w-4 h-4 text-[#ECB22E]" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-stone-900">{installations.length}</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5 truncate">
              {installations.map((i) => i.team_name || i.team_id).join(", ") || "No team installed"}
            </div>
          </div>
        </div>

        <div className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between">
          <div className="flex items-center justify-between text-xs text-stone-500 font-medium">
            <span>Trigger Routing</span>
            <CheckCircle2 className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="mt-2">
            <div className="text-2xl font-bold font-mono text-emerald-800">Direct</div>
            <div className="text-[10px] text-stone-400 font-mono mt-0.5">
              Automatic PR dispatch from mentions
            </div>
          </div>
        </div>
      </div>

      {/* ================= ADD CHANNEL BINDING FORM ================= */}
      <div className="bg-white shadow-2xs border border-sand-200 rounded-2xl p-5 space-y-4">
        <div>
          <h2 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Plus className="w-3.5 h-3.5 text-kiwi-600 stroke-[2.5]" />
            <span>Map New Channel to Repository</span>
          </h2>
          <p className="text-xs text-stone-500 mt-0.5">
            When someone mentions the bot in this channel, tasks will target this repository.
          </p>
        </div>

        <form onSubmit={onCreate} className="space-y-3.5 text-xs">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3.5">
            <div>
              <label className="block font-bold text-stone-700 mb-1">Slack Workspace</label>
              {installations.length > 0 ? (
                <select
                  value={teamID}
                  onChange={(e) => setTeamID(e.target.value)}
                  className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-semibold text-stone-900 focus:outline-none focus:border-stone-800 focus:bg-white"
                  required
                >
                  {installations.map((i) => (
                    <option key={i.team_id} value={i.team_id}>
                      {i.team_name || i.team_id} ({i.team_id})
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  placeholder="e.g. T0123456789"
                  value={teamID}
                  onChange={(e) => setTeamID(e.target.value)}
                  className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 focus:bg-white"
                  required
                />
              )}
            </div>

            <div>
              <label className="block font-bold text-stone-700 mb-1">Slack Channel ID</label>
              <input
                placeholder="e.g. C0123456789 or channel name"
                value={channelID}
                onChange={(e) => setChannelID(e.target.value)}
                className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 focus:bg-white"
                required
              />
            </div>

            <div>
              <label className="block font-bold text-stone-700 mb-1">GitHub Repository URL</label>
              <input
                placeholder="https://github.com/owner/repo"
                value={repoURL}
                onChange={(e) => setRepoURL(e.target.value)}
                className="w-full bg-sand-50/80 border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 focus:bg-white"
                required
              />
            </div>
          </div>

          {/* Advanced / Optional Overrides */}
          <div className="pt-1">
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="text-stone-500 hover:text-stone-900 text-xs font-semibold flex items-center gap-1 transition-colors"
            >
              <span>{showAdvanced ? "Hide Advanced Overrides" : "+ Show Advanced Overrides (Branch, Tests, Models)"}</span>
              {showAdvanced ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
            </button>

            {showAdvanced && (
              <div className="mt-3 p-3.5 rounded-xl bg-sand-50/60 border border-sand-200 space-y-3">
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3.5">
                  <div>
                    <label className="block font-bold text-stone-700 mb-1">Default Base Branch (Optional)</label>
                    <input
                      placeholder="main"
                      value={defaultRef}
                      onChange={(e) => setDefaultRef(e.target.value)}
                      className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800"
                    />
                  </div>

                  <div>
                    <label className="block font-bold text-stone-700 mb-1">Default Test Command (Optional)</label>
                    <input
                      placeholder="go test ./... or npm test"
                      value={defaultTestCmd}
                      onChange={(e) => setDefaultTestCmd(e.target.value)}
                      className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800"
                    />
                  </div>
                </div>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3.5">
                  <div>
                    <label className="block font-bold text-stone-700 mb-1">Worker Model Override (Optional)</label>
                    <input
                      placeholder="e.g. claude-3-5-sonnet-20241022"
                      value={defaultModel}
                      onChange={(e) => setDefaultModel(e.target.value)}
                      className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800"
                    />
                  </div>

                  <div>
                    <label className="block font-bold text-stone-700 mb-1">Architect Model Override (Optional)</label>
                    <input
                      placeholder="e.g. claude-3-7-sonnet"
                      value={defaultArchitectModel}
                      onChange={(e) => setDefaultArchitectModel(e.target.value)}
                      className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800"
                    />
                  </div>
                </div>
              </div>
            )}
          </div>

          <div className="pt-2 border-t border-sand-150 flex justify-end">
            <button
              type="submit"
              disabled={creating || !teamID.trim() || !channelID.trim() || !repoURL.trim()}
              className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all disabled:opacity-50"
            >
              {creating ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
              <span>Bind Channel</span>
            </button>
          </div>
        </form>

        {createError && (
          <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
            <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
            <span>{createError}</span>
          </div>
        )}
      </div>

      {/* ================= ACTIVE BINDINGS DIRECTORY ================= */}
      <div className="space-y-3">
        <h2 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-2">
          <Hash className="w-3.5 h-3.5 text-stone-500" />
          <span>Active Channel Bindings ({bindings.length})</span>
        </h2>

        {loading ? (
          <div className="p-8 text-stone-500 flex flex-col items-center justify-center gap-2 bg-white border border-sand-200 rounded-2xl shadow-2xs">
            <KiwiMicroButtonLoader />
            <span className="text-xs font-mono">Loading channel bindings...</span>
          </div>
        ) : bindings.length === 0 ? (
          <div className="p-8 text-center text-stone-400 font-mono bg-white border border-sand-200 rounded-2xl shadow-2xs space-y-1">
            <Hash className="w-6 h-6 text-stone-300 mx-auto" />
            <p>No Slack channels bound to repositories yet.</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3.5">
            {bindings.map((b) => (
              <div
                key={b.id}
                className="p-4 rounded-2xl bg-white border border-sand-200 shadow-2xs flex flex-col justify-between space-y-3 hover:border-sand-300 transition-all group"
              >
                <div className="space-y-2">
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <div className="w-7 h-7 rounded-lg bg-sand-100 border border-sand-200 flex items-center justify-center shrink-0">
                        <Hash className="w-3.5 h-3.5 text-stone-600" />
                      </div>
                      <div className="min-w-0">
                        <span className="font-mono text-xs font-bold text-stone-900 truncate block">
                          {b.channel_id}
                        </span>
                        <span className="text-[10px] font-mono text-stone-400">Team: {b.team_id}</span>
                      </div>
                    </div>

                    <button
                      onClick={() => onDelete(b.id)}
                      disabled={deletingId === b.id}
                      className="p-1.5 text-stone-400 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors"
                      title="Remove channel binding"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>

                  <div className="p-2 rounded-xl bg-sand-50/70 border border-sand-200 font-mono text-[11px] text-stone-800 truncate">
                    {b.repo_url}
                  </div>
                </div>

                <div className="pt-2 border-t border-sand-150 flex items-center justify-between text-[10px] font-mono text-stone-500 flex-wrap gap-2">
                  <div className="flex items-center gap-2 flex-wrap">
                    {b.default_ref && (
                      <span className="inline-flex items-center gap-1 bg-sand-100 px-1.5 py-0.5 rounded border border-sand-200">
                        <GitBranch className="w-3 h-3 text-stone-500" />
                        <span>{b.default_ref}</span>
                      </span>
                    )}
                    {b.default_test_cmd && (
                      <span className="inline-flex items-center gap-1 bg-sand-100 px-1.5 py-0.5 rounded border border-sand-200">
                        <Terminal className="w-3 h-3 text-stone-500" />
                        <span className="truncate max-w-[120px]">{b.default_test_cmd}</span>
                      </span>
                    )}
                    {b.default_model && (
                      <span className="inline-flex items-center gap-1 bg-purple-50 text-purple-900 px-1.5 py-0.5 rounded border border-purple-200">
                        <Cpu className="w-3 h-3 text-purple-600" />
                        <span className="truncate max-w-[100px]">{b.default_model}</span>
                      </span>
                    )}
                  </div>

                  <span className="text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200 font-bold">
                    Active
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
