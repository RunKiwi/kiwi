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
  Terminal,
  ChevronDown,
  ChevronUp,
} from "lucide-react";
import { FaSlack } from "react-icons/fa6";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";
import { Logo } from "@/components/Logo";

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
      setCreateError(err instanceof Error ? err.message : "Failed to create Slack channel binding");
    } finally {
      setCreating(false);
    }
  }

  async function onDelete(id: string) {
    if (!confirm("Are you sure you want to remove this channel binding?")) return;
    setDeletingId(id);
    try {
      await api.deleteSlackBinding(id);
      await refresh();
    } catch (err) {
      alert("Failed to delete binding: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setDeletingId(null);
    }
  }

  return (
    <div className="max-w-4xl mx-auto flex flex-col gap-4 w-full font-sans text-stone-900 select-none">
      
      {/* Back Link */}
      <div>
        <Link
          href="/integrations"
          className="inline-flex items-center gap-1.5 text-xs font-semibold text-stone-500 hover:text-stone-900 transition-colors"
        >
          <ArrowLeft className="w-3.5 h-3.5" />
          <span>Back to Integrations Hub</span>
        </Link>
      </div>

      {/* Header Banner with Modern Swiss Styling */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs flex flex-col sm:flex-row items-start sm:items-center justify-between gap-4">
        <div className="flex items-center gap-3.5">
          <div className="w-12 h-12 rounded-2xl bg-[#ECB22E]/10 border border-[#ECB22E]/30 shadow-2xs flex items-center justify-center shrink-0">
            <FaSlack className="w-6 h-6 text-[#ECB22E]" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-[#ECB22E] bg-amber-50 px-2 py-0.5 rounded border border-amber-200">
                SLACK WORKSPACE ROUTING
              </span>
            </div>
            <h1 className="text-lg font-bold text-stone-900 tracking-tight mt-0.5">
              Slack Channel-to-Repository Bindings
            </h1>
            <p className="text-xs text-stone-500 mt-0.5">
              Bind Slack channels directly to repositories. @mention Kiwi in any bound channel to launch autonomous tasks.
            </p>
          </div>
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 shadow-2xs font-mono">
          <AlertCircle className="w-4 h-4 text-rose-600 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* Main Bind Form */}
      <div className="p-4 sm:p-5 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-4">
        <div>
          <h2 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Plus className="w-3.5 h-3.5 text-stone-700" />
            <span>Create New Channel Binding</span>
          </h2>
          <p className="text-xs text-stone-500 mt-0.5">
            Link a Slack Channel ID (e.g. C0123456789) to a GitHub repository URL.
          </p>
        </div>

        <form onSubmit={onCreate} className="space-y-3">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
            <div>
              <label className="block font-bold text-stone-800 mb-1">Slack Workspace Team ID</label>
              {installations.length > 0 ? (
                <select
                  value={teamID}
                  onChange={(e) => setTeamID(e.target.value)}
                  className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                  required
                >
                  {installations.map((inst) => (
                    <option key={inst.team_id} value={inst.team_id}>
                      {inst.team_name ? `${inst.team_name} (${inst.team_id})` : inst.team_id}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  value={teamID}
                  onChange={(e) => setTeamID(e.target.value)}
                  placeholder="T0123456789"
                  className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                  required
                />
              )}
            </div>

            <div>
              <label className="block font-bold text-stone-800 mb-1">Slack Channel ID</label>
              <input
                value={channelID}
                onChange={(e) => setChannelID(e.target.value)}
                placeholder="C08ABCDEF12"
                className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                required
              />
            </div>

            <div className="sm:col-span-2">
              <label className="block font-bold text-stone-800 mb-1">Target Repository URL</label>
              <input
                value={repoURL}
                onChange={(e) => setRepoURL(e.target.value)}
                placeholder="https://github.com/RunKiwi/kiwi"
                className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                required
              />
            </div>
          </div>

          {/* Advanced defaults toggle */}
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-xs font-semibold text-stone-500 hover:text-stone-800 flex items-center gap-1 pt-1 cursor-pointer"
          >
            {showAdvanced ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
            <span>{showAdvanced ? "Hide Advanced Channel Defaults" : "Configure Default Branch, Tests & Models"}</span>
          </button>

          {showAdvanced && (
            <div className="p-3.5 rounded-xl bg-sand-50/70 border border-sand-200 space-y-3 animate-in fade-in duration-150">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                <div>
                  <label className="block font-bold text-stone-800 mb-1 flex items-center gap-1">
                    <GitBranch className="w-3.5 h-3.5 text-stone-500" />
                    <span>Default Git Branch / Ref</span>
                  </label>
                  <input
                    value={defaultRef}
                    onChange={(e) => setDefaultRef(e.target.value)}
                    placeholder="main"
                    className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                  />
                </div>

                <div>
                  <label className="block font-bold text-stone-800 mb-1 flex items-center gap-1">
                    <Terminal className="w-3.5 h-3.5 text-stone-500" />
                    <span>Default Verification Command</span>
                  </label>
                  <input
                    value={defaultTestCmd}
                    onChange={(e) => setDefaultTestCmd(e.target.value)}
                    placeholder="npm test / go test ./..."
                    className="w-full px-3 py-2 rounded-xl border border-sand-200 bg-white text-xs font-mono outline-none focus:border-stone-900 transition-all shadow-2xs"
                  />
                </div>
              </div>
            </div>
          )}

          {createError && (
            <div className="p-2.5 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 font-mono">
              <AlertCircle className="w-3.5 h-3.5 shrink-0 text-rose-600" />
              <span>{createError}</span>
            </div>
          )}

          <div className="pt-2 flex justify-end">
            <button
              type="submit"
              disabled={creating || !teamID.trim() || !channelID.trim() || !repoURL.trim()}
              className="px-5 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-bold text-xs shadow-2xs flex items-center gap-1.5 transition-all cursor-pointer disabled:opacity-40"
            >
              {creating ? <KiwiMicroButtonLoader /> : <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />}
              <span>Save Channel Binding</span>
            </button>
          </div>
        </form>
      </div>

      {/* Active Channel Bindings List */}
      <div className="bg-white border border-sand-200/90 rounded-2xl shadow-2xs p-4 sm:p-5 space-y-3">
        <div>
          <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider flex items-center gap-1.5">
            <Hash className="w-3.5 h-3.5 text-stone-600" />
            <span>Active Channel Bindings ({bindings.length})</span>
          </h3>
          <p className="text-xs text-stone-500 mt-0.5">
            Channels where @Kiwi automatically maps requests to the assigned codebase.
          </p>
        </div>

        {loading ? (
          <div className="p-8 text-center text-xs font-mono text-stone-400">Loading channel bindings...</div>
        ) : bindings.length === 0 ? (
          <div className="p-8 rounded-2xl border border-sand-200/90 bg-sand-50/40 text-center space-y-2.5 shadow-2xs">
            <div className="w-12 h-12 mx-auto rounded-2xl bg-white border border-sand-200/90 shadow-2xs flex items-center justify-center">
              <Logo variant="full-color" pose="sleeping" animated={true} className="w-7 h-7" />
            </div>
            <div className="space-y-0.5">
              <div className="text-stone-900 font-bold text-xs">No Channel Bindings Configured</div>
              <p className="text-xs text-stone-500 max-w-xs mx-auto">
                Bind a Slack channel above to trigger Kiwi tasks right inside your team chat.
              </p>
            </div>
          </div>
        ) : (
          <div className="divide-y divide-sand-200/80 border border-sand-200/90 rounded-xl overflow-hidden shadow-2xs">
            {bindings.map((b) => (
              <div
                key={b.id}
                className="p-3.5 bg-white hover:bg-sand-50/80 transition-colors flex items-center justify-between gap-3"
              >
                <div className="min-w-0 space-y-1">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-mono text-xs font-bold text-stone-900 flex items-center gap-1">
                      <Hash className="w-3.5 h-3.5 text-stone-500" />
                      <span>{b.channel_id}</span>
                    </span>
                    <span className="text-[10px] font-mono text-stone-400">→</span>
                    <span className="font-mono text-xs font-semibold text-stone-700 truncate">{b.repo_url}</span>
                  </div>

                  <div className="flex items-center gap-3 text-[10px] font-mono text-stone-400 flex-wrap">
                    <span>Workspace: <strong className="text-stone-600">{b.team_id}</strong></span>
                    {b.default_ref && <span>Ref: <strong className="text-stone-600">{b.default_ref}</strong></span>}
                    {b.default_test_cmd && <span>Test: <strong className="text-stone-600">{b.default_test_cmd}</strong></span>}
                  </div>
                </div>

                <button
                  onClick={() => onDelete(b.id)}
                  disabled={deletingId === b.id}
                  className="p-1 text-stone-400 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-colors cursor-pointer"
                  title="Remove binding"
                >
                  {deletingId === b.id ? <KiwiMicroButtonLoader /> : <Trash2 className="w-3.5 h-3.5" />}
                </button>
              </div>
            ))}
          </div>
        )}
      </div>

    </div>
  );
}
