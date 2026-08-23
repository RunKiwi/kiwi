"use client";

import { useEffect, useRef, useState, useId } from "react";
import { X, CheckCircle2, AlertCircle, Loader2, ExternalLink, Eye, EyeOff, ShieldCheck, Lock, ArrowRight } from "lucide-react";
import Link from "next/link";
import { client, type GithubInstallation, type SlackInstallation } from "@/lib/api";
import { parseActionableError } from "@/lib/errors";
import { capture } from "@/lib/analytics";

export type IntegrationCategory = "scm" | "llm" | "notifications" | "telemetry";

export interface IntegrationField {
  key: string;
  label: string;
  credName: string;
  kind: string;
  placeholder: string;
  type?: "password" | "text";
  helpText?: string;
}

export interface CatalogIntegration {
  id: string;
  name: string;
  category: IntegrationCategory;
  categoryLabel: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
  iconBg: string;
  iconColor: string;
  brandAccent?: string;
  docUrl?: string;
  docLabel?: string;
  fields: IntegrationField[];
  isGithubHybrid?: boolean;
  isSlackHybrid?: boolean;
}

interface IntegrationDrawerProps {
  integration: CatalogIntegration | null;
  status: Record<string, boolean>;
  onClose: () => void;
  onRefreshStatus: () => Promise<void>;
}

export function IntegrationDrawer({
  integration,
  status,
  onClose,
  onRefreshStatus,
}: IntegrationDrawerProps) {
  const drawerRef = useRef<HTMLDivElement>(null);
  const headingId = useId();

  // Handle ESC key to close drawer
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape" && integration) {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [integration, onClose]);

  if (!integration) return null;

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-stone-900/30 backdrop-blur-xs z-40 transition-opacity animate-in fade-in duration-200"
        onClick={onClose}
        aria-hidden="true"
      />

      {/* Slide-over Drawer */}
      <div
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={headingId}
        tabIndex={-1}
        className="fixed inset-y-0 right-0 w-full sm:w-[560px] max-w-full bg-white border-l border-sand-200 shadow-popover z-50 flex flex-col outline-none animate-in slide-in-from-right duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]"
      >
        <IntegrationDrawerBody
          key={integration.id}
          headingId={headingId}
          integration={integration}
          status={status}
          onClose={onClose}
          onRefreshStatus={onRefreshStatus}
        />
      </div>
    </>
  );
}

function IntegrationDrawerBody({
  headingId,
  integration,
  status,
  onClose,
  onRefreshStatus,
}: {
  headingId: string;
  integration: CatalogIntegration;
  status: Record<string, boolean>;
  onClose: () => void;
  onRefreshStatus: () => Promise<void>;
}) {
  const [fieldValues, setFieldValues] = useState<Record<string, string>>({});
  const [showPassword, setShowPassword] = useState<Record<string, boolean>>({});
  const [savingKey, setSavingKey] = useState<string | null>(null);
  const [fieldMsg, setFieldMsg] = useState<Record<string, string>>({});
  const [fieldErr, setFieldErr] = useState<Record<string, boolean>>({});

  // GitHub App specific state
  const [installs, setInstalls] = useState<GithubInstallation[] | null>(null);
  const [githubBusy, setGithubBusy] = useState(false);
  const [githubError, setGithubError] = useState("");
  const [appUnavailable, setAppUnavailable] = useState(false);
  const [showFallbackToken, setShowFallbackToken] = useState(false);

  // Slack App specific state
  const [slackInstalls, setSlackInstalls] = useState<SlackInstallation[] | null>(null);
  const [slackBusy, setSlackBusy] = useState(false);
  const [slackError, setSlackError] = useState("");
  const [slackAppUnavailable, setSlackAppUnavailable] = useState(false);
  const [showNotificationWebhook, setShowNotificationWebhook] = useState(false);

  useEffect(() => {
    if (!integration.isGithubHybrid) return;
    let active = true;
    client
      .listGithubInstallations()
      .then((r) => {
        if (active) setInstalls(r.installations);
      })
      .catch(() => {
        if (active) setInstalls([]);
      });
    return () => {
      active = false;
    };
  }, [integration.isGithubHybrid]);

  useEffect(() => {
    if (!integration.isSlackHybrid) return;
    let active = true;
    client
      .listSlackInstallations()
      .then((r) => {
        if (active) setSlackInstalls(r.installations);
      })
      .catch(() => {
        if (active) setSlackInstalls([]);
      });
    return () => {
      active = false;
    };
  }, [integration.isSlackHybrid]);

  const Icon = integration.icon;
  const isAllConnected =
    integration.fields.length > 0
      ? integration.fields.every((f) => status[f.key])
      : false;

  const handleConnectGithubApp = async () => {
    setGithubBusy(true);
    setGithubError("");
    try {
      const { install_url } = await client.githubInstallUrl();
      window.location.href = install_url;
    } catch (e) {
      const parsed = parseActionableError(e);
      if (/501|not configured/i.test(parsed.message)) {
        setAppUnavailable(true);
      } else {
        setGithubError(parsed.message);
      }
      setGithubBusy(false);
    }
  };

  const handleConnectSlack = async () => {
    setSlackBusy(true);
    setSlackError("");
    try {
      const { install_url } = await client.getSlackInstallURL();
      window.location.href = install_url;
    } catch (e) {
      const parsed = parseActionableError(e);
      if (/501|not configured/i.test(parsed.message)) {
        setSlackAppUnavailable(true);
      } else {
        setSlackError(parsed.message);
      }
      setSlackBusy(false);
    }
  };

  const handleSaveField = async (field: IntegrationField) => {
    const val = (fieldValues[field.key] || "").trim();
    if (!val) {
      setFieldMsg((m) => ({ ...m, [field.key]: "Please enter a value first." }));
      setFieldErr((e) => ({ ...e, [field.key]: true }));
      return;
    }

    setSavingKey(field.key);
    setFieldMsg((m) => ({ ...m, [field.key]: "Verifying reachability…" }));
    setFieldErr((e) => ({ ...e, [field.key]: false }));

    try {
      await client.setCredential(field.credName, field.kind, val);
      if (field.kind === "llm") {
        capture("model_key_added", { provider: field.key, surface: "integrations" });
      } else if (field.kind === "github") {
        capture("repo_connected", { surface: "integrations" });
      }
      setFieldValues((v) => ({ ...v, [field.key]: "" }));
      setFieldMsg((m) => ({ ...m, [field.key]: "Saved and verified ✓" }));
      setFieldErr((e) => ({ ...e, [field.key]: false }));
      await onRefreshStatus();
    } catch (e) {
      const parsed = parseActionableError(e);
      setFieldMsg((m) => ({ ...m, [field.key]: parsed.message }));
      setFieldErr((e) => ({ ...e, [field.key]: true }));
    } finally {
      setSavingKey(null);
    }
  };

  const handleSaveAllFields = async () => {
    const fieldsToSave = integration.fields.filter(
      (f) => (fieldValues[f.key] || "").trim().length > 0
    );

    if (fieldsToSave.length === 0) {
      integration.fields.forEach((f) => {
        if (!status[f.key]) {
          setFieldMsg((m) => ({ ...m, [f.key]: "Field is required." }));
          setFieldErr((e) => ({ ...e, [f.key]: true }));
        }
      });
      return;
    }

    for (const field of fieldsToSave) {
      await handleSaveField(field);
    }
  };

  return (
    <>
      {/* Drawer Header */}
      <div className="flex items-start justify-between p-6 border-b border-sand-200 bg-stone-900">
        <div className="flex items-center gap-4 min-w-0">
          <div
            className={`w-12 h-12 rounded-2xl flex items-center justify-center shrink-0 border border-sand-200 ${integration.iconBg}`}
          >
            <Icon className={`w-6 h-6 ${integration.iconColor}`} />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h2 id={headingId} className="text-lg font-medium text-stone-900 tracking-tight">
                {integration.name}
              </h2>
              <span className="text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded-full bg-sand-100 text-stone-500">
                {integration.categoryLabel}
              </span>
              {isAllConnected && (
                <span className="flex items-center gap-1 text-xs text-emerald-400 font-medium px-2 py-0.5 rounded-full bg-emerald-500/10 border border-emerald-500/20">
                  <CheckCircle2 className="w-3.5 h-3.5" /> Connected
                </span>
              )}
            </div>
            <p className="text-xs text-stone-500 mt-1 line-clamp-2">{integration.description}</p>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-2 hover:bg-sand-100 rounded-full transition-colors text-stone-500 hover:text-stone-900 shrink-0 ml-2"
          title="Close drawer (Esc)"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Drawer Content */}
      <div className="flex-1 overflow-y-auto p-6 space-y-6">
        {/* Security Banner */}
        <div className="flex items-start gap-3 p-3.5 rounded-xl border border-sand-150 bg-sand-50/60 text-xs text-stone-500">
          <Lock className="w-4 h-4 text-stone-500 shrink-0 mt-0.5" />
          <div>
            <span className="text-stone-800 font-medium">Encrypted & Sealed: </span>
            Tokens are AES-encrypted at rest and sealed to daemon runtimes. They are never rendered
            or returned to the browser.
          </div>
        </div>

        {/* Docs Reference Link */}
        {integration.docUrl && (
          <div className="flex items-center justify-between text-xs p-3 rounded-xl border border-sand-150 bg-sand-50/60">
            <span className="text-stone-500">Need help creating credentials?</span>
            <a
              href={integration.docUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1.5 text-blue-400 hover:text-blue-300 font-medium transition-colors"
            >
              <span>{integration.docLabel || "Documentation"}</span>
              <ExternalLink className="w-3.5 h-3.5" />
            </a>
          </div>
        )}

        {/* GitHub Hybrid Flow */}
        {integration.isGithubHybrid ? (
          <div className="space-y-6">
            {/* GitHub App Section */}
            <div className="p-4 rounded-xl border border-sand-200 bg-sand-50/60 space-y-4">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-medium text-stone-900">Kiwi GitHub App</h3>
                    <span className="text-[10px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-400 border border-emerald-500/25">
                      Recommended
                    </span>
                  </div>
                  <p className="text-xs text-stone-500 mt-1">
                    Installs hourly rotating tokens scoped strictly to repositories you select.
                    Revocable anytime via GitHub.
                  </p>
                </div>
              </div>

              {installs === null ? (
                <div className="flex items-center gap-2 text-xs text-stone-400 py-2">
                  <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading installations…
                </div>
              ) : appUnavailable ? (
                <p className="text-xs text-stone-400">
                  GitHub App is not configured on this deployment. Please use a Personal Access
                  Token below.
                </p>
              ) : (
                <>
                  {(installs?.length ?? 0) > 0 && (
                    <ul className="flex flex-col gap-2">
                      {installs.map((i) => (
                        <li
                          key={i.installation_id}
                          className="flex items-center justify-between gap-3 text-xs rounded-xl border border-sand-200 bg-sand-50 px-3 py-2"
                        >
                          <span className="flex items-center gap-2 min-w-0">
                            <ShieldCheck className="w-4 h-4 text-emerald-400 shrink-0" />
                            <span className="truncate text-stone-900 font-mono">{i.account_login}</span>
                            <span className="text-stone-400 shrink-0">
                              ({i.repo_selection === "all" ? "all repositories" : "selected repos"})
                            </span>
                          </span>
                          <a
                            href="https://github.com/settings/installations"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-1 text-[11px] text-stone-500 hover:text-stone-900 shrink-0 transition-colors"
                          >
                            Manage <ExternalLink className="w-3 h-3" />
                          </a>
                        </li>
                      ))}
                    </ul>
                  )}

                  <div className="flex items-center gap-3 pt-1">
                    <button
                      type="button"
                      onClick={handleConnectGithubApp}
                      disabled={githubBusy}
                      className="rounded-lg bg-white hover:bg-sand-100 text-black text-xs font-semibold px-4 py-2 disabled:opacity-50 transition-colors flex items-center gap-2"
                    >
                      {githubBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
                      {githubBusy
                        ? "Opening GitHub…"
                        : (installs?.length ?? 0) > 0
                        ? "Install on another account"
                        : "Install GitHub App"}
                    </button>
                  </div>

                  {githubError && (
                    <p className="text-xs text-rose-600 flex items-center gap-1.5">
                      <AlertCircle className="w-3.5 h-3.5 shrink-0" /> {githubError}
                    </p>
                  )}
                </>
              )}
            </div>

            {/* Fallback PAT Section */}
            <div className="p-4 rounded-xl border border-sand-200 bg-sand-50/60 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-medium text-stone-900">Fallback Personal Access Token</h3>
                  <p className="text-xs text-stone-400 mt-0.5">
                    Only required if you cannot install the GitHub App.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setShowFallbackToken((v) => !v)}
                  className="text-xs text-stone-500 hover:text-stone-900 underline transition-colors"
                >
                  {showFallbackToken ? "Hide" : status["github"] ? "Manage PAT" : "Set PAT"}
                </button>
              </div>

              {showFallbackToken &&
                integration.fields.map((field) => {
                  const isConnected = status[field.key];
                  const isBusy = savingKey === field.key;
                  const isPwd = (field.type || "password") === "password";
                  const visible = showPassword[field.key] || false;
                  const msg = fieldMsg[field.key];
                  const isErr = fieldErr[field.key];

                  return (
                    <div key={field.key} className="space-y-2 pt-2 border-t border-sand-150">
                      <div className="flex items-center justify-between">
                        <label className="text-xs font-medium text-stone-700">{field.label}</label>
                        {isConnected && (
                          <span className="flex items-center gap-1 text-[11px] text-emerald-400 font-medium">
                            <CheckCircle2 className="w-3 h-3" /> Token Active
                          </span>
                        )}
                      </div>

                      <div className="flex gap-2">
                        <div className="relative flex-1">
                          <input
                            type={isPwd && !visible ? "password" : "text"}
                            value={fieldValues[field.key] || ""}
                            onChange={(e) =>
                              setFieldValues((prev) => ({ ...prev, [field.key]: e.target.value }))
                            }
                            placeholder={
                              isConnected
                                ? "•••••••• (paste new PAT to replace)"
                                : field.placeholder
                            }
                            className="w-full field text-xs pr-9"
                          />
                          {isPwd && (
                            <button
                              type="button"
                              onClick={() =>
                                setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                              }
                              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-500 hover:text-stone-900 p-1"
                              title={visible ? "Hide token" : "Show token"}
                            >
                              {visible ? (
                                <EyeOff className="w-3.5 h-3.5" />
                              ) : (
                                <Eye className="w-3.5 h-3.5" />
                              )}
                            </button>
                          )}
                        </div>

                        <button
                          type="button"
                          onClick={() => handleSaveField(field)}
                          disabled={isBusy || !(fieldValues[field.key] || "").trim()}
                          className="btn-primary text-xs px-3.5 py-1.5 rounded-lg disabled:opacity-50 shrink-0"
                        >
                          {isBusy ? (
                            <Loader2 className="w-3.5 h-3.5 animate-spin" />
                          ) : isConnected ? (
                            "Update"
                          ) : (
                            "Save"
                          )}
                        </button>
                      </div>

                      {msg && (
                        <div
                          className={`flex items-center gap-1.5 text-xs ${
                            isErr ? "text-rose-600" : "text-emerald-400"
                          }`}
                        >
                          {isErr ? (
                            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                          ) : (
                            <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                          )}
                          <span>{msg}</span>
                        </div>
                      )}
                    </div>
                  );
                })}
            </div>
          </div>
        ) : integration.isSlackHybrid ? (
          <div className="space-y-6">
            {/* Slack App Section */}
            <div className="p-4 rounded-xl border border-sand-200 bg-sand-50/60 space-y-4">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-sm font-medium text-stone-900">Kiwi Slack App</h3>
                    <span className="text-[10px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded bg-emerald-500/15 text-emerald-400 border border-emerald-500/25">
                      Recommended
                    </span>
                  </div>
                  <p className="text-xs text-stone-500 mt-1">
                    Trigger Kiwi tasks by @mentioning the bot in a channel or thread, with status
                    updates posted back in the thread. Revocable anytime from Slack.
                  </p>
                </div>
              </div>

              {slackInstalls === null ? (
                <div className="flex items-center gap-2 text-xs text-stone-400 py-2">
                  <Loader2 className="w-3.5 h-3.5 animate-spin" /> Loading workspaces…
                </div>
              ) : slackAppUnavailable ? (
                <p className="text-xs text-stone-400">
                  Slack App is not configured on this deployment.
                </p>
              ) : (
                <>
                  {(slackInstalls?.length ?? 0) > 0 && (
                    <ul className="flex flex-col gap-2">
                      {slackInstalls.map((s) => (
                        <li
                          key={s.team_id}
                          className="flex items-center justify-between gap-3 text-xs rounded-xl border border-sand-200 bg-sand-50 px-3 py-2"
                        >
                          <span className="flex items-center gap-2 min-w-0">
                            <ShieldCheck className="w-4 h-4 text-emerald-400 shrink-0" />
                            <span className="truncate text-stone-900 font-mono">{s.team_name || s.team_id}</span>
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}

                  <div className="flex items-center gap-3 pt-1">
                    <button
                      type="button"
                      onClick={handleConnectSlack}
                      disabled={slackBusy}
                      className="rounded-lg bg-white hover:bg-sand-100 text-black text-xs font-semibold px-4 py-2 disabled:opacity-50 transition-colors flex items-center gap-2"
                    >
                      {slackBusy ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : null}
                      {slackBusy
                        ? "Opening Slack…"
                        : (slackInstalls?.length ?? 0) > 0
                        ? "Add another workspace"
                        : "Add to Slack"}
                    </button>
                    {(slackInstalls?.length ?? 0) > 0 && (
                      <Link
                        href="/integrations/slack"
                        className="flex items-center gap-1.5 text-xs text-blue-400 hover:text-blue-300 font-medium transition-colors"
                      >
                        Manage channel bindings <ArrowRight className="w-3.5 h-3.5" />
                      </Link>
                    )}
                  </div>

                  {slackError && (
                    <p className="text-xs text-rose-600 flex items-center gap-1.5">
                      <AlertCircle className="w-3.5 h-3.5 shrink-0" /> {slackError}
                    </p>
                  )}
                </>
              )}
            </div>

            {/* Optional Notification Webhook Section */}
            <div className="p-4 rounded-xl border border-sand-200 bg-sand-50/60 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-sm font-medium text-stone-900">Notification Webhook</h3>
                  <p className="text-xs text-stone-400 mt-0.5">
                    Optional — posts monitor verdicts to a channel, independent of the app install above.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setShowNotificationWebhook((v) => !v)}
                  className="text-xs text-stone-500 hover:text-stone-900 underline transition-colors"
                >
                  {showNotificationWebhook ? "Hide" : status["slack"] ? "Manage webhook" : "Set webhook"}
                </button>
              </div>

              {showNotificationWebhook &&
                integration.fields.map((field) => {
                  const isConnected = status[field.key];
                  const isBusy = savingKey === field.key;
                  const isPwd = (field.type || "password") === "password";
                  const visible = showPassword[field.key] || false;
                  const msg = fieldMsg[field.key];
                  const isErr = fieldErr[field.key];

                  return (
                    <div key={field.key} className="space-y-2 pt-2 border-t border-sand-150">
                      <div className="flex items-center justify-between">
                        <label className="text-xs font-medium text-stone-700">{field.label}</label>
                        {isConnected && (
                          <span className="flex items-center gap-1 text-[11px] text-emerald-400 font-medium">
                            <CheckCircle2 className="w-3 h-3" /> Webhook Active
                          </span>
                        )}
                      </div>

                      <div className="flex gap-2">
                        <div className="relative flex-1">
                          <input
                            type={isPwd && !visible ? "password" : "text"}
                            value={fieldValues[field.key] || ""}
                            onChange={(e) =>
                              setFieldValues((prev) => ({ ...prev, [field.key]: e.target.value }))
                            }
                            placeholder={
                              isConnected
                                ? "•••••••• (paste new URL to replace)"
                                : field.placeholder
                            }
                            className="w-full field text-xs pr-9"
                          />
                          {isPwd && (
                            <button
                              type="button"
                              onClick={() =>
                                setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                              }
                              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-500 hover:text-stone-900 p-1"
                              title={visible ? "Hide URL" : "Show URL"}
                            >
                              {visible ? (
                                <EyeOff className="w-3.5 h-3.5" />
                              ) : (
                                <Eye className="w-3.5 h-3.5" />
                              )}
                            </button>
                          )}
                        </div>

                        <button
                          type="button"
                          onClick={() => handleSaveField(field)}
                          disabled={isBusy || !(fieldValues[field.key] || "").trim()}
                          className="btn-primary text-xs px-3.5 py-1.5 rounded-lg disabled:opacity-50 shrink-0"
                        >
                          {isBusy ? (
                            <Loader2 className="w-3.5 h-3.5 animate-spin" />
                          ) : isConnected ? (
                            "Update"
                          ) : (
                            "Save"
                          )}
                        </button>
                      </div>

                      {field.helpText && (
                        <p className="text-[11px] text-stone-400">{field.helpText}</p>
                      )}

                      {msg && (
                        <div
                          className={`flex items-center gap-1.5 text-xs ${
                            isErr ? "text-rose-600" : "text-emerald-400"
                          }`}
                        >
                          {isErr ? (
                            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                          ) : (
                            <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                          )}
                          <span>{msg}</span>
                        </div>
                      )}
                    </div>
                  );
                })}
            </div>
          </div>
        ) : (
          /* Standard & Multi-Field Integrations */
          <div className="space-y-4">
            {integration.fields.map((field) => {
              const isConnected = status[field.key];
              const isBusy = savingKey === field.key;
              const isPwd = (field.type || "password") === "password";
              const visible = showPassword[field.key] || false;
              const msg = fieldMsg[field.key];
              const isErr = fieldErr[field.key];

              return (
                <div
                  key={field.key}
                  className="p-4 rounded-xl border border-sand-200 bg-sand-50/60 space-y-2.5"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <label className="text-xs font-semibold text-stone-800 uppercase tracking-wide">
                        {field.label}
                      </label>
                      <span className="font-mono text-[10px] text-stone-400">({field.credName})</span>
                    </div>
                    {isConnected ? (
                      <span className="flex items-center gap-1 text-[11px] text-emerald-400 font-medium bg-emerald-500/10 px-2 py-0.5 rounded-full border border-emerald-500/20">
                        <CheckCircle2 className="w-3 h-3" /> Configured
                      </span>
                    ) : (
                      <span className="text-[11px] text-stone-400 font-medium">Not set</span>
                    )}
                  </div>

                  <div className="flex gap-2">
                    <div className="relative flex-1">
                      <input
                        type={isPwd && !visible ? "password" : "text"}
                        value={fieldValues[field.key] || ""}
                        onChange={(e) =>
                          setFieldValues((prev) => ({ ...prev, [field.key]: e.target.value }))
                        }
                        placeholder={isConnected ? "•••••••• (paste to update)" : field.placeholder}
                        className="w-full field text-xs pr-9 font-mono"
                      />
                      {isPwd && (
                        <button
                          type="button"
                          onClick={() =>
                            setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                          }
                          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-500 hover:text-stone-900 p-1"
                          title={visible ? "Hide" : "Show"}
                        >
                          {visible ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
                        </button>
                      )}
                    </div>

                    <button
                      type="button"
                      onClick={() => handleSaveField(field)}
                      disabled={isBusy || !(fieldValues[field.key] || "").trim()}
                      className="btn-primary text-xs px-3.5 py-1.5 rounded-lg disabled:opacity-50 shrink-0"
                    >
                      {isBusy ? (
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                      ) : isConnected ? (
                        "Update"
                      ) : (
                        "Save"
                      )}
                    </button>
                  </div>

                  {msg && (
                    <div
                      className={`flex items-center gap-1.5 text-xs pt-1 ${
                        isErr ? "text-rose-600" : "text-emerald-400"
                      }`}
                    >
                      {isErr ? (
                        <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                      ) : (
                        <CheckCircle2 className="w-3.5 h-3.5 shrink-0" />
                      )}
                      <span>{msg}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Drawer Footer */}
      <div className="p-4 sm:p-6 border-t border-sand-200 bg-stone-900 flex items-center justify-between gap-3">
        <button type="button" onClick={onClose} className="btn-ghost text-xs px-4 py-2 rounded-xl">
          Close
        </button>

        {!integration.isGithubHybrid && !integration.isSlackHybrid && integration.fields.length > 1 && (
          <button
            type="button"
            onClick={handleSaveAllFields}
            disabled={savingKey !== null}
            className="btn-primary text-xs px-5 py-2 rounded-xl"
          >
            {savingKey ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Save All Changes"}
          </button>
        )}
      </div>
    </>
  );
}
