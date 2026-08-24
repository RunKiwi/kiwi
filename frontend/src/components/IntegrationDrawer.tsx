"use client";

import React, { useEffect, useRef, useState, useId } from "react";
import {
  X,
  CheckCircle2,
  AlertCircle,
  ExternalLink,
  Eye,
  EyeOff,
  ShieldCheck,
  Lock,
  ArrowRight,
  Hash,
  BookOpen,
} from "lucide-react";
import Link from "next/link";
import { client, type GithubInstallation, type SlackInstallation } from "@/lib/api";
import { parseActionableError } from "@/lib/errors";
import { capture } from "@/lib/analytics";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";

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
        className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs z-40 transition-opacity animate-in fade-in duration-200"
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
        className="fixed inset-y-0 right-0 w-full sm:w-[560px] max-w-full bg-white border-l border-sand-200 shadow-popover z-50 flex flex-col outline-none animate-in slide-in-from-right duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] font-sans text-stone-900"
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
      {/* ================= DRAWER HEADER (LIGHT THEME) ================= */}
      <div className="flex items-start justify-between p-5 sm:p-6 border-b border-sand-200 bg-white">
        <div className="flex items-center gap-3.5 min-w-0">
          <div
            className={`w-12 h-12 rounded-2xl flex items-center justify-center shrink-0 border border-sand-200 shadow-2xs ${integration.iconBg}`}
          >
            <Icon className={`w-6 h-6 ${integration.iconColor}`} />
          </div>
          <div className="min-w-0">
            <div className="flex items-center gap-2 flex-wrap">
              <h2 id={headingId} className="text-lg font-bold text-stone-900 tracking-tight">
                {integration.name}
              </h2>
              <span className="text-[10px] font-mono uppercase font-bold tracking-wider px-2 py-0.5 rounded-md bg-sand-100 text-stone-600 border border-sand-200">
                {integration.categoryLabel}
              </span>
              {isAllConnected && (
                <span className="flex items-center gap-1 text-[10px] font-mono font-bold text-emerald-800 px-2 py-0.5 rounded-md bg-emerald-50 border border-emerald-200">
                  <CheckCircle2 className="w-3 h-3 text-emerald-600" /> Connected
                </span>
              )}
            </div>
            <p className="text-xs text-stone-500 mt-1 line-clamp-2 leading-relaxed">{integration.description}</p>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1.5 hover:bg-sand-100 rounded-xl transition-colors text-stone-400 hover:text-stone-700 shrink-0 ml-2"
          title="Close drawer (Esc)"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* ================= DRAWER CONTENT ================= */}
      <div className="flex-1 overflow-y-auto p-5 sm:p-6 space-y-5">
        {/* Security Banner */}
        <div className="flex items-start gap-3 p-3.5 rounded-2xl border border-sand-200 bg-sand-50/70 text-xs text-stone-600">
          <Lock className="w-4 h-4 text-stone-500 shrink-0 mt-0.5" />
          <div className="leading-relaxed">
            <span className="text-stone-900 font-bold">Encrypted &amp; Sealed: </span>
            Credentials are encrypted at rest and sealed to runner runtimes. They are never exposed in the browser.
          </div>
        </div>

        {/* Docs Reference Link */}
        {integration.docUrl && (
          <div className="flex items-center justify-between text-xs p-3.5 rounded-2xl border border-sand-200 bg-sand-50/70">
            <span className="text-stone-600 flex items-center gap-1.5">
              <BookOpen className="w-3.5 h-3.5 text-stone-400" />
              <span>Need help creating credentials?</span>
            </span>
            <a
              href={integration.docUrl}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-1 text-xs font-semibold text-kiwi-700 hover:underline transition-colors"
            >
              <span>{integration.docLabel || "Documentation"}</span>
              <ExternalLink className="w-3.5 h-3.5" />
            </a>
          </div>
        )}

        {/* GitHub Hybrid Flow */}
        {integration.isGithubHybrid ? (
          <div className="space-y-4">
            {/* GitHub App Section */}
            <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/60 space-y-3.5">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Kiwi GitHub App</h3>
                    <span className="text-[10px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-800 border border-emerald-200 font-mono">
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
                <div className="flex items-center gap-2 text-xs text-stone-500 py-2">
                  <KiwiMicroButtonLoader /> Loading installations…
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
                          className="flex items-center justify-between gap-3 text-xs rounded-xl border border-sand-200 bg-white px-3.5 py-2.5 shadow-2xs"
                        >
                          <span className="flex items-center gap-2 min-w-0">
                            <ShieldCheck className="w-4 h-4 text-emerald-600 shrink-0" />
                            <span className="truncate text-stone-900 font-mono font-bold">{i.account_login}</span>
                            <span className="text-stone-400 shrink-0 text-[11px] font-mono">
                              ({i.repo_selection === "all" ? "all repos" : "selected repos"})
                            </span>
                          </span>
                          <a
                            href="https://github.com/settings/installations"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-1 text-[11px] font-semibold text-stone-500 hover:text-stone-900 shrink-0 transition-colors"
                          >
                            <span>Manage</span>
                            <ExternalLink className="w-3 h-3 text-stone-400" />
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
                      className="rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-bold px-4 py-2 disabled:opacity-50 transition-colors flex items-center gap-2 shadow-2xs"
                    >
                      {githubBusy ? <KiwiMicroButtonLoader /> : null}
                      <span>
                        {githubBusy
                          ? "Opening GitHub…"
                          : (installs?.length ?? 0) > 0
                          ? "Install on another account"
                          : "Install GitHub App"}
                      </span>
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
            <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/60 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Personal Access Token (PAT)</h3>
                  <p className="text-xs text-stone-400 mt-0.5">
                    Alternative connection if you cannot install the GitHub App.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setShowFallbackToken((v) => !v)}
                  className="text-xs text-stone-600 hover:text-stone-900 font-semibold underline transition-colors"
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
                    <div key={field.key} className="space-y-2 pt-2 border-t border-sand-200">
                      <div className="flex items-center justify-between">
                        <label className="text-xs font-bold text-stone-700">{field.label}</label>
                        {isConnected && (
                          <span className="flex items-center gap-1 text-[11px] font-mono text-emerald-700 font-bold bg-emerald-50 px-1.5 py-0.5 rounded border border-emerald-200">
                            <CheckCircle2 className="w-3 h-3 text-emerald-600" /> Token Active
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
                            className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 pr-9 shadow-2xs"
                          />
                          {isPwd && (
                            <button
                              type="button"
                              onClick={() =>
                                setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                              }
                              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-700 p-1"
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
                          className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs disabled:opacity-50 shrink-0 transition-all"
                        >
                          {isBusy ? (
                            <KiwiMicroButtonLoader />
                          ) : isConnected ? (
                            "Update"
                          ) : (
                            "Save"
                          )}
                        </button>
                      </div>

                      {msg && (
                        <div
                          className={`flex items-center gap-1.5 text-xs pt-0.5 ${
                            isErr ? "text-rose-600" : "text-emerald-700 font-medium"
                          }`}
                        >
                          {isErr ? (
                            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                          ) : (
                            <CheckCircle2 className="w-3.5 h-3.5 shrink-0 text-emerald-600" />
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
          <div className="space-y-4">
            {/* Slack App Section */}
            <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/60 space-y-3.5">
              <div className="flex items-start justify-between gap-2">
                <div>
                  <div className="flex items-center gap-2">
                    <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Kiwi Slack App</h3>
                    <span className="text-[10px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-800 border border-emerald-200 font-mono">
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
                <div className="flex items-center gap-2 text-xs text-stone-500 py-2">
                  <KiwiMicroButtonLoader /> Loading workspaces…
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
                          className="flex items-center justify-between gap-3 text-xs rounded-xl border border-sand-200 bg-white px-3.5 py-2.5 shadow-2xs"
                        >
                          <span className="flex items-center gap-2 min-w-0">
                            <ShieldCheck className="w-4 h-4 text-emerald-600 shrink-0" />
                            <span className="truncate text-stone-900 font-mono font-bold">{s.team_name || s.team_id}</span>
                          </span>

                          <span className="text-[10px] font-mono text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200 font-bold">
                            Installed
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}

                  <div className="flex items-center justify-between flex-wrap gap-2 pt-1">
                    <button
                      type="button"
                      onClick={handleConnectSlack}
                      disabled={slackBusy}
                      className="rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-bold px-4 py-2 disabled:opacity-50 transition-colors flex items-center gap-2 shadow-2xs"
                    >
                      {slackBusy ? <KiwiMicroButtonLoader /> : null}
                      <span>
                        {slackBusy
                          ? "Opening Slack…"
                          : (slackInstalls?.length ?? 0) > 0
                          ? "Add another workspace"
                          : "Add to Slack"}
                      </span>
                    </button>

                    {(slackInstalls?.length ?? 0) > 0 && (
                      <Link
                        href="/integrations/slack"
                        onClick={onClose}
                        className="flex items-center gap-1.5 text-xs text-kiwi-700 hover:underline font-bold transition-colors"
                      >
                        <Hash className="w-3.5 h-3.5 text-kiwi-600" />
                        <span>Manage channel bindings</span>
                        <ArrowRight className="w-3.5 h-3.5" />
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
            <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/60 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="text-xs font-bold text-stone-900 uppercase tracking-wider">Notification Webhook</h3>
                  <p className="text-xs text-stone-400 mt-0.5">
                    Optional — posts monitor verdicts to a channel, independent of the app install above.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setShowNotificationWebhook((v) => !v)}
                  className="text-xs text-stone-600 hover:text-stone-900 font-semibold underline transition-colors"
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
                    <div key={field.key} className="space-y-2 pt-2 border-t border-sand-200">
                      <div className="flex items-center justify-between">
                        <label className="text-xs font-bold text-stone-700">{field.label}</label>
                        {isConnected && (
                          <span className="flex items-center gap-1 text-[11px] font-mono text-emerald-700 font-bold bg-emerald-50 px-1.5 py-0.5 rounded border border-emerald-200">
                            <CheckCircle2 className="w-3 h-3 text-emerald-600" /> Webhook Active
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
                            className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 pr-9 shadow-2xs"
                          />
                          {isPwd && (
                            <button
                              type="button"
                              onClick={() =>
                                setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                              }
                              className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-700 p-1"
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
                          className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs disabled:opacity-50 shrink-0 transition-all"
                        >
                          {isBusy ? (
                            <KiwiMicroButtonLoader />
                          ) : isConnected ? (
                            "Update"
                          ) : (
                            "Save"
                          )}
                        </button>
                      </div>

                      {msg && (
                        <div
                          className={`flex items-center gap-1.5 text-xs pt-0.5 ${
                            isErr ? "text-rose-600" : "text-emerald-700 font-medium"
                          }`}
                        >
                          {isErr ? (
                            <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                          ) : (
                            <CheckCircle2 className="w-3.5 h-3.5 shrink-0 text-emerald-600" />
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
          /* Standard Provider Credentials Form (LLM, Datadog, Prometheus, Git) */
          <div className="space-y-4">
            {integration.fields.map((field) => {
              const isConnected = status[field.key];
              const isBusy = savingKey === field.key;
              const isPwd = (field.type || "password") === "password";
              const visible = showPassword[field.key] || false;
              const msg = fieldMsg[field.key];
              const isErr = fieldErr[field.key];

              return (
                <div key={field.key} className="p-4 rounded-2xl border border-sand-200 bg-sand-50/60 space-y-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <label className="text-xs font-bold text-stone-900 block">{field.label}</label>
                      {field.helpText && (
                        <p className="text-[11px] text-stone-400 mt-0.5">{field.helpText}</p>
                      )}
                    </div>
                    {isConnected && (
                      <span className="flex items-center gap-1 text-[10px] font-mono text-emerald-700 font-bold bg-emerald-50 px-2 py-0.5 rounded-md border border-emerald-200">
                        <CheckCircle2 className="w-3 h-3 text-emerald-600" /> Active
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
                            ? "•••••••• (paste new key to replace)"
                            : field.placeholder
                        }
                        className="w-full bg-white border border-sand-200 rounded-xl px-3 py-2 text-xs font-mono focus:outline-none focus:border-stone-800 pr-9 shadow-2xs"
                      />
                      {isPwd && (
                        <button
                          type="button"
                          onClick={() =>
                            setShowPassword((p) => ({ ...p, [field.key]: !p[field.key] }))
                          }
                          className="absolute right-2.5 top-1/2 -translate-y-1/2 text-stone-400 hover:text-stone-700 p-1"
                          title={visible ? "Hide credential" : "Show credential"}
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
                      className="px-4 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs disabled:opacity-50 shrink-0 transition-all"
                    >
                      {isBusy ? (
                        <KiwiMicroButtonLoader />
                      ) : isConnected ? (
                        "Update"
                      ) : (
                        "Save"
                      )}
                    </button>
                  </div>

                  {msg && (
                    <div
                      className={`flex items-center gap-1.5 text-xs pt-0.5 ${
                        isErr ? "text-rose-600" : "text-emerald-700 font-medium"
                      }`}
                    >
                      {isErr ? (
                        <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                      ) : (
                        <CheckCircle2 className="w-3.5 h-3.5 shrink-0 text-emerald-600" />
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

      {/* ================= DRAWER FOOTER (LIGHT THEME) ================= */}
      <div className="p-4 sm:p-5 border-t border-sand-200 bg-sand-50/80 flex items-center justify-between gap-3">
        <button
          type="button"
          onClick={onClose}
          className="px-4 py-2 rounded-xl bg-white hover:bg-sand-100 border border-sand-200 text-stone-700 font-semibold text-xs transition-colors shadow-2xs"
        >
          Close
        </button>

        {!integration.isGithubHybrid && !integration.isSlackHybrid && integration.fields.length > 1 && (
          <button
            type="button"
            onClick={handleSaveAllFields}
            disabled={savingKey !== null}
            className="px-5 py-2 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-bold text-xs shadow-2xs transition-all disabled:opacity-50"
          >
            {savingKey ? <KiwiMicroButtonLoader /> : "Save All Changes"}
          </button>
        )}
      </div>
    </>
  );
}
