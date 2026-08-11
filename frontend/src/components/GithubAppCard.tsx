"use client";

import { useEffect, useState } from "react";
import { Boxes, CheckCircle2, ExternalLink, ShieldCheck } from "lucide-react";
import { client, type GithubInstallation } from "@/lib/api";
import { parseActionableError } from "@/lib/errors";
import { capture } from "@/lib/analytics";

// GithubAppCard connects repositories through the Kiwi GitHub App.
//
// Kept separate from the credential cards on this page because it is not a
// credential. There is nothing for the user to paste and nothing for Kiwi to
// store: the customer grants access on GitHub, and each git operation buys a
// token that expires within the hour. The old flow asked for a personal access
// token carrying org-wide write with no expiry, which is what this replaces.
export function GithubAppCard() {
  const [installs, setInstalls] = useState<GithubInstallation[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  // The App is optional infrastructure. A deployment with none configured
  // answers 501, and the card then says so rather than offering a button that
  // cannot work.
  const [unavailable, setUnavailable] = useState(false);

  const load = () =>
    client
      .listGithubInstallations()
      .then((r) => setInstalls(r.installations))
      .catch(() => setInstalls([]));

  useEffect(() => {
    load();
    // The callback returns here with ?github=connected once GitHub redirects
    // back, so the freshly connected account appears without a manual reload.
    if (typeof window !== "undefined") {
      const params = new URLSearchParams(window.location.search);
      if (params.get("github") === "connected") {
        capture("repo_connected", { surface: "github_app" });
        window.history.replaceState({}, "", window.location.pathname);
      }
    }
  }, []);

  const connect = async () => {
    setBusy(true);
    setError("");
    try {
      const { install_url } = await client.githubInstallUrl();
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
            <Boxes className="w-5 h-5 text-zinc-200" />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h3 className="font-medium">GitHub App</h3>
              <span className="text-[10px] uppercase tracking-wide px-1.5 py-0.5 rounded bg-white/10 text-zinc-300">
                Recommended
              </span>
              {connected && (
                <span className="flex items-center gap-1 text-[11px] text-green-400 font-medium">
                  <CheckCircle2 className="w-3.5 h-3.5" /> Connected
                </span>
              )}
            </div>
            <p className="text-sm text-zinc-400">
              Pick the repositories Kiwi may touch. Access expires hourly and you
              can revoke it from GitHub at any time.
            </p>
          </div>
        </div>
      </div>

      {installs === null ? (
        <p className="text-sm text-zinc-500">Loading…</p>
      ) : unavailable ? (
        <p className="text-sm text-zinc-500">
          Not configured on this deployment. Connect a token below instead.
        </p>
      ) : (
        <>
          {connected && (
            <ul className="mb-4 flex flex-col gap-2">
              {installs.map((i) => (
                <li
                  key={i.installation_id}
                  className="flex items-center justify-between gap-3 text-sm rounded-xl border border-white/10 bg-white/5 px-3 py-2"
                >
                  <span className="flex items-center gap-2 min-w-0">
                    <ShieldCheck className="w-4 h-4 text-green-400 shrink-0" />
                    <span className="truncate">{i.account_login}</span>
                    <span className="text-zinc-500 shrink-0">
                      {i.repo_selection === "all"
                        ? "all repositories"
                        : "selected repositories"}
                    </span>
                  </span>
                  <a
                    href={`https://github.com/settings/installations`}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-1 text-xs text-zinc-400 hover:text-white shrink-0"
                  >
                    Manage <ExternalLink className="w-3 h-3" />
                  </a>
                </li>
              ))}
            </ul>
          )}

          <button
            onClick={connect}
            disabled={busy}
            className="rounded-xl bg-white text-black text-sm font-medium px-4 py-2 disabled:opacity-50"
          >
            {busy
              ? "Opening GitHub…"
              : connected
                ? "Add another account"
                : "Connect GitHub"}
          </button>

          {error && <p className="mt-3 text-sm text-red-400">{error}</p>}

          {!connected && (
            <p className="mt-3 text-xs text-zinc-600">
              Prefer a token? Use the GitHub or Git entries below. The App is
              narrower: it reaches only what you select, and nothing long-lived
              is stored.
            </p>
          )}
        </>
      )}
    </div>
  );
}
