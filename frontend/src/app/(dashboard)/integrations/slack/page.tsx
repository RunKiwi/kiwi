"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { api, SlackChannelBinding, SlackInstallation } from "@/lib/api";
import { Trash2, Plus, Loader2, AlertCircle, ArrowLeft, Hash } from "lucide-react";

export default function SlackBindingsPage() {
  const [bindings, setBindings] = useState<SlackChannelBinding[]>([]);
  const [installations, setInstallations] = useState<SlackInstallation[]>([]);
  const [loading, setLoading] = useState(true);
  const [teamID, setTeamID] = useState("");
  const [channelID, setChannelID] = useState("");
  const [repoURL, setRepoURL] = useState("");
  const [defaultRef, setDefaultRef] = useState("");
  const [defaultTestCmd, setDefaultTestCmd] = useState("");
  const [defaultModel, setDefaultModel] = useState("");
  const [defaultArchitectModel, setDefaultArchitectModel] = useState("");
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

  // Used by the create/delete handlers to reload after a mutation — a plain
  // async function is fine there since it runs from an event handler, not an
  // effect body.
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

  // The initial load runs its own Promise chain (rather than calling
  // refresh()) so state is only ever set once the fetch resolves and the
  // effect hasn't been cleaned up — calling a state-setting function
  // directly from an effect body trips react-hooks/set-state-in-effect.
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
    <div className="p-8 max-w-5xl mx-auto h-full flex flex-col text-white">
      <div className="mb-6">
        <Link
          href="/integrations"
          className="inline-flex items-center gap-1.5 text-xs text-zinc-400 hover:text-white mb-4 transition-colors"
        >
          <ArrowLeft className="w-3.5 h-3.5" /> Back to Integrations
        </Link>
        <h1 className="text-3xl font-light tracking-tight mb-2">Slack Channel Bindings</h1>
        <p className="text-zinc-400">
          Bind a Slack channel to a repository so an @mention in that channel knows which repository to act on.
        </p>
      </div>

      {error && (
        <div className="flex items-center gap-2 text-red-400 text-sm mb-4">
          <AlertCircle className="w-4 h-4 shrink-0" />
          {error}
        </div>
      )}

      <div className="glass-panel border border-white/10 rounded-2xl p-5 mb-8">
        <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-4">Add Channel Binding</h2>
        <form onSubmit={onCreate} className="flex flex-col gap-4">
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">Slack Workspace / Team ID</label>
              {installations.length > 0 ? (
                <select
                  value={teamID}
                  onChange={(e) => setTeamID(e.target.value)}
                  className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
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
                  className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
                  required
                />
              )}
            </div>
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">Channel ID</label>
              <input
                placeholder="e.g. C0123456789"
                value={channelID}
                onChange={(e) => setChannelID(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
                required
              />
            </div>
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">Repository URL</label>
              <input
                placeholder="https://github.com/owner/repo"
                value={repoURL}
                onChange={(e) => setRepoURL(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
                required
              />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">Default Branch / Ref (Optional)</label>
              <input
                placeholder="main"
                value={defaultRef}
                onChange={(e) => setDefaultRef(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
              />
            </div>
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">Default Test Command (Optional)</label>
              <input
                placeholder="go test ./..."
                value={defaultTestCmd}
                onChange={(e) => setDefaultTestCmd(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white"
              />
            </div>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">
                Worker Model (Optional) <span className="text-zinc-600">— leave blank to auto-pick the cheapest available at run time</span>
              </label>
              <input
                placeholder="e.g. claude-haiku-4-5-20251001"
                value={defaultModel}
                onChange={(e) => setDefaultModel(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white font-mono"
              />
            </div>
            <div>
              <label className="block text-[11px] text-zinc-400 mb-1">
                Architect Model (Optional) <span className="text-zinc-600">— plans &amp; reviews; platform default if blank</span>
              </label>
              <input
                placeholder="e.g. claude-opus-4-8"
                value={defaultArchitectModel}
                onChange={(e) => setDefaultArchitectModel(e.target.value)}
                className="w-full field text-sm bg-zinc-900 border border-white/10 rounded-lg p-2 text-white font-mono"
              />
            </div>
          </div>

          <div className="flex justify-end mt-2">
            <button
              type="submit"
              disabled={creating || !teamID.trim() || !channelID.trim() || !repoURL.trim()}
              className="flex items-center justify-center gap-2 bg-white text-black px-4 py-2 rounded-lg font-medium text-sm disabled:opacity-50 hover:bg-zinc-200 transition-colors"
            >
              {creating ? <Loader2 className="w-4 h-4 animate-spin" /> : <Plus className="w-4 h-4" />}
              Bind Channel
            </button>
          </div>
        </form>
        {createError && (
          <div className="flex items-center gap-2 text-red-400 text-sm mt-3">
            <AlertCircle className="w-4 h-4 shrink-0" />
            {createError}
          </div>
        )}
      </div>

      <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Active Bindings</h2>
      {loading ? (
        <div className="flex items-center gap-2 text-sm text-zinc-500">
          <Loader2 className="w-4 h-4 animate-spin" /> Loading bindings...
        </div>
      ) : bindings.length === 0 ? (
        <p className="text-zinc-500 text-sm">No channels bound yet.</p>
      ) : (
        <div className="flex flex-col gap-3">
          {bindings.map((b) => (
            <div
              key={b.id}
              className="glass-panel p-4 border border-white/10 rounded-xl flex items-center justify-between group"
            >
              <div className="flex items-center gap-3 min-w-0">
                <div className="w-8 h-8 rounded-lg bg-white/5 border border-white/10 flex items-center justify-center shrink-0">
                  <Hash className="w-4 h-4 text-zinc-400" />
                </div>
                <div className="min-w-0">
                  <div className="text-sm font-medium text-white flex items-center gap-2 flex-wrap">
                    <span>{b.channel_id}</span>
                    <span className="text-zinc-500">→</span>
                    <span className="text-zinc-300 font-mono text-xs">{b.repo_url}</span>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-zinc-500 mt-1">
                    <span>Team: {b.team_id}</span>
                    {b.default_ref && <span>Ref: {b.default_ref}</span>}
                    {b.default_test_cmd && <span>Test: {b.default_test_cmd}</span>}
                    {b.default_model && <span>Worker: {b.default_model}</span>}
                    {b.default_architect_model && <span>Architect: {b.default_architect_model}</span>}
                  </div>
                </div>
              </div>
              <button
                onClick={() => onDelete(b.id)}
                disabled={deletingId === b.id}
                aria-label="Remove binding"
                className="p-2 text-zinc-400 hover:text-red-400 rounded-lg hover:bg-white/5 transition-colors disabled:opacity-50"
              >
                {deletingId === b.id ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : (
                  <Trash2 className="w-4 h-4" />
                )}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
