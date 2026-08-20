"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { MessageSquare, CheckCircle2, ShieldCheck, ArrowRight } from "lucide-react";
import { client, type SlackInstallation } from "@/lib/api";
import { parseActionableError } from "@/lib/errors";

export function SlackAppCard() {
  const [installs, setInstalls] = useState<SlackInstallation[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [unavailable, setUnavailable] = useState(false);

  const load = () =>
    client
      .listSlackInstallations()
      .then((r) => setInstalls(r.installations))
      .catch(() => setInstalls([]));

  useEffect(() => {
    load();
    if (typeof window !== "undefined") {
      const params = new URLSearchParams(window.location.search);
      if (params.get("slack") === "connected") {
        window.history.replaceState({}, "", window.location.pathname);
      }
    }
  }, []);

  const connect = async () => {
    setBusy(true);
    setError("");
    try {
      const { install_url } = await client.getSlackInstallURL();
      window.location.href = install_url;
    } catch (e) {
      const parsed = parseActionableError(e);
      if (/501|not configured/i.test(parsed.message)) {
        setUnavailable(true);
      } else {
        setError(parsed.message);
      }
      setBusy(false);
    }
  };

  const connected = (installs?.length ?? 0) > 0;

  return (
    <div className="glass-panel border border-white/10 rounded-2xl p-5">
      <div className="flex items-start justify-between gap-4 mb-4">
        <div className="flex items-start gap-3 min-w-0">
          <div className="w-10 h-10 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center shrink-0">
            <MessageSquare className="w-5 h-5 text-zinc-200" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h3 className="font-medium">Slack App</h3>
              {connected && (
                <span className="flex items-center gap-1 text-[11px] text-green-400 font-medium">
                  <CheckCircle2 className="w-3.5 h-3.5" /> Connected
                </span>
              )}
            </div>
            <p className="text-sm text-zinc-400">
              Trigger Kiwi tasks and receive status updates directly from your Slack workspace.
            </p>
          </div>
        </div>
      </div>

      {installs === null ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : unavailable ? (
        <p className="text-sm text-zinc-500">
          Not configured on this deployment.
        </p>
      ) : (
        <>
          {connected && (
            <div className="mb-4 flex flex-col gap-3">
              <ul className="flex flex-col gap-2">
                {installs.map((i) => (
                  <li
                    key={i.team_id}
                    className="flex items-center justify-between gap-3 text-sm rounded-xl border border-white/10 bg-white/5 px-3 py-2"
                  >
                    <span className="flex items-center gap-2 min-w-0">
                      <ShieldCheck className="w-4 h-4 text-green-400 shrink-0" />
                      <span className="truncate font-medium">{i.team_name || i.team_id}</span>
                      <span className="text-zinc-500 shrink-0 text-xs">{i.team_id}</span>
                    </span>
                  </li>
                ))}
              </ul>
              <div>
                <Link
                  href="/integrations/slack"
                  className="inline-flex items-center gap-1.5 text-xs text-blue-400 hover:text-blue-300 transition-colors"
                >
                  Manage channel bindings <ArrowRight className="w-3.5 h-3.5" />
                </Link>
              </div>
            </div>
          )}

          <button
            onClick={connect}
            disabled={busy}
            className="rounded-xl bg-white text-black text-sm font-medium px-4 py-2 disabled:opacity-50 hover:bg-zinc-200 transition-colors"
          >
            {busy
              ? "Opening Slack…"
              : connected
                ? "Add another workspace"
                : "Add to Slack"}
          </button>

          {error && <p className="mt-3 text-sm text-red-400">{error}</p>}
        </>
      )}
    </div>
  );
}
