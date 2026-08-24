"use client";

import { useFleetStore } from "@/store/useFleetStore";
import { useEffect, useState, useMemo } from "react";
import {
  Server,
  Plus,
  Cloud,
  Building2,
  KeyRound,
  Copy,
  Check,
  AlertCircle,
  ShieldCheck,
  Terminal,
  Lock,
  RefreshCw,
  Layers,
} from "lucide-react";
import { client, type Fleet, type UsageResponse, type Daemon } from "@/lib/api";
import { LoadingState } from "@/components/LoadingState";
import { Logo } from "@/components/Logo";
import { UpgradeButton } from "@/components/UpgradeButton";

import { usePolling } from "@/hooks/usePolling";

export default function FleetPage() {
  const { daemons, loadDaemons, jobs, loadJobs } = useFleetStore();
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [u, setU] = useState<UsageResponse | null>(null);
  const [usageLoaded, setUsageLoaded] = useState(false);
  const [filter, setFilter] = useState<"all" | "managed" | "byoc">("all");

  // Fleet creation state
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [name, setName] = useState("");
  const [type, setType] = useState<"managed" | "byoc">("byoc");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");

  // BYOC Join token state
  const [selectedFleetId, setSelectedFleetId] = useState<string>("");
  const [token, setToken] = useState<{ fleetId: string; value: string } | null>(null);
  const [minting, setMinting] = useState(false);
  const [copied, setCopied] = useState(false);

  const loadFleets = async () => {
    try {
      const r = await client.listFleets();
      setFleets(r.fleets || []);
    } catch {
      // ignore
    }
  };

  const activeTasksCount = jobs.filter((j) => j.status === "LEASED" || j.status === "RUNNING").length;

  usePolling(
    async () => {
      await Promise.all([
        loadDaemons().catch(() => {}),
        loadJobs().catch(() => {}),
        loadFleets().catch(() => {}),
        client
          .getUsage()
          .then(setU)
          .catch(() => setU(null))
          .finally(() => setUsageLoaded(true)),
      ]);
    },
    {
      activeIntervalMs: 3000,
      idleIntervalMs: 10000,
      isIdle: activeTasksCount === 0,
    }
  );

  const isFree = u?.plan === "free";
  const hasCap = (u?.agent_minutes_limit ?? 0) > 0;
  const daemonsOnline = daemons.filter((d) => d.online).length;
  const managedFleets = useMemo(() => fleets.filter((f) => f.type === "managed"), [fleets]);
  const byocFleets = useMemo(() => fleets.filter((f) => f.type === "byoc"), [fleets]);

  const managedDaemons = useMemo(
    () => daemons.filter((d) => !d.fleet_id || managedFleets.some((f) => f.id === d.fleet_id)),
    [daemons, managedFleets]
  );
  const byocDaemons = useMemo(
    () => daemons.filter((d) => d.fleet_id && byocFleets.some((f) => f.id === d.fleet_id)),
    [daemons, byocFleets]
  );

  // Mint token for selected or specific fleet
  const mintToken = async (fleetId?: string) => {
    setMinting(true);
    try {
      const targetId = fleetId || selectedFleetId || (byocFleets[0]?.id ?? "");
      const r = await client.mintJoinToken(targetId || undefined);
      setToken({ fleetId: targetId, value: r.join_token });
    } catch {
      // ignore
    } finally {
      setMinting(false);
    }
  };

  // Auto-mint a join token for BYOC when Pro/Enterprise loads and has BYOC fleets
  useEffect(() => {
    if (!isFree && usageLoaded && !token) {
      const target = byocFleets[0]?.id;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      mintToken(target);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isFree, usageLoaded, byocFleets.length]);

  const copyCommand = () => {
    const cmd = token?.value
      ? `curl -fsSL https://get.runkiwi.dev/install.sh | sh && kiwidaemon join --token ${token.value} --vpc-strict`
      : `curl -fsSL https://get.runkiwi.dev/install.sh | sh && kiwidaemon join --token kw_sec_token --vpc-strict`;
    navigator.clipboard?.writeText(cmd);
    setCopied(true);
    setTimeout(() => setCopied(false), 1800);
  };

  const handleCreateFleet = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    if (!name.trim()) {
      setErr("Please provide a name for the fleet.");
      return;
    }
    setBusy(true);
    try {
      const created = await client.createFleet(name.trim(), type);
      setName("");
      setShowCreateModal(false);
      await loadFleets();
      if (created.type === "byoc") {
        setSelectedFleetId(created.id);
        await mintToken(created.id);
      }
    } catch (error) {
      setErr(error instanceof Error ? error.message : "Failed to create fleet");
    } finally {
      setBusy(false);
    }
  };

  if (!usageLoaded) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <LoadingState state="connecting" label="Loading execution fleets & runner capacity..." />
      </div>
    );
  }

  return (
    <div className="p-0 sm:p-2 md:p-4 max-w-7xl mx-auto space-y-6 font-sans text-stone-900">
      {/* ================= HEADER & QUICK ACTION ROW ================= */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-sand-200 pb-4">
        <div>
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono font-bold bg-kiwi-100 text-kiwi-800 border border-kiwi-200 px-2 py-0.5 rounded uppercase">
              FLEET RUNNERS
            </span>
            <h1 className="text-xl font-bold text-stone-900 tracking-tight">Execution Fleets & Capacity</h1>
          </div>
          <p className="text-xs text-stone-500 mt-0.5">
            Manage ephemeral runners hosted on Kiwi Cloud and private self-hosted daemons (BYOC) in your own VPC.
          </p>
        </div>

        <div className="flex items-center gap-2">
          <div className="px-3 py-1.5 rounded-xl bg-white border border-sand-200 text-stone-700 text-xs font-semibold shadow-2xs flex items-center gap-2">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500" />
            </span>
            <span>Kiwi Cloud Pool</span>
          </div>

          {!isFree && (
            <button
              onClick={() => {
                setType("byoc");
                setShowCreateModal(true);
              }}
              className="px-3.5 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white text-xs font-semibold shadow-xs flex items-center gap-1.5 transition-all"
            >
              <Plus className="w-3.5 h-3.5 text-kiwi-400 stroke-[2.5]" />
              <span>+ Create Fleet</span>
            </button>
          )}
        </div>
      </div>

      {/* ================= CATEGORY FILTER PILLS & TELEMETRY SUMMARY ================= */}
      <div className="flex flex-wrap items-center justify-between gap-3 bg-sand-50/70 p-2.5 rounded-2xl border border-sand-200 shadow-2xs">
        <div className="flex items-center gap-1.5 text-xs">
          <button
            onClick={() => setFilter("all")}
            className={`px-3 py-1 rounded-xl font-semibold text-xs transition-all ${
              filter === "all" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
            }`}
          >
            All Runners ({daemons.length})
          </button>
          <button
            onClick={() => setFilter("managed")}
            className={`px-3 py-1 rounded-xl font-semibold text-xs transition-all flex items-center gap-1.5 ${
              filter === "managed" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
            }`}
          >
            <Logo className="w-3.5 h-3.5" />
            <span>Kiwi Hosted</span>
            <span
              className={`text-[10px] font-mono font-bold px-1.5 py-0.2 rounded ${
                filter === "managed" ? "bg-stone-800 text-kiwi-300" : "bg-kiwi-100 text-kiwi-800"
              }`}
            >
              {isFree ? (daemonsOnline > 0 ? "1" : "0") : managedDaemons.length || managedFleets.length}
            </span>
          </button>
          <button
            onClick={() => setFilter("byoc")}
            className={`px-3 py-1 rounded-xl font-semibold text-xs transition-all flex items-center gap-1.5 ${
              filter === "byoc" ? "bg-stone-900 text-white shadow-2xs" : "text-stone-600 hover:bg-sand-150"
            }`}
          >
            <Building2 className="w-3.5 h-3.5" />
            <span>Self-Hosted (BYOC)</span>
            <span
              className={`text-[10px] font-mono font-bold px-1.5 py-0.2 rounded ${
                filter === "byoc" ? "bg-stone-800 text-indigo-300" : "bg-indigo-100 text-indigo-800"
              }`}
            >
              {isFree ? "0" : byocDaemons.length || byocFleets.length}
            </span>
          </button>
        </div>

        <div className="flex items-center gap-3 text-[11px] font-mono text-stone-500">
          <span>
            Online Daemons:{" "}
            <strong className="text-emerald-700">
              {daemonsOnline} / {daemons.length}
            </strong>
          </span>
          <span>•</span>
          <span>
            Active Leases:{" "}
            <strong className="text-stone-900">
              {activeTasksCount} {activeTasksCount === 1 ? "task" : "tasks"}
            </strong>
          </span>
        </div>
      </div>

      {/* ================= CATEGORY 1: KIWI HOSTED CLOUD FLEET ================= */}
      {(filter === "all" || filter === "managed") && (
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-6 h-6 rounded-lg bg-kiwi-100 border border-kiwi-200 text-kiwi-800 flex items-center justify-center font-bold text-xs shadow-2xs">
                <Logo className="w-3.5 h-3.5 text-kiwi-700" />
              </div>
              <div>
                <h3 className="text-xs font-bold text-stone-900">Kiwi Hosted Cloud Fleet (Serverless & Ephemeral)</h3>
                <p className="text-[11px] text-stone-500">
                  Zero infrastructure setup. Ephemeral gVisor MicroVMs managed by Kiwi with auto-scaling.
                </p>
              </div>
            </div>
            <span className="text-[10px] font-mono font-bold text-kiwi-800 bg-kiwi-50 border border-kiwi-200 px-2 py-0.5 rounded-full">
              MANAGED BY KIWI
            </span>
          </div>

          {isFree ? (
            /* Free Tier Managed Runtime Card */
            <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <span className="relative flex h-2.5 w-2.5">
                    {daemonsOnline > 0 && (
                      <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                    )}
                    <span
                      className={`relative inline-flex rounded-full h-2.5 w-2.5 ${
                        daemonsOnline > 0 ? "bg-emerald-500" : "bg-amber-500"
                      }`}
                    />
                  </span>
                  <span className="font-bold text-stone-900 text-sm">Kiwi Shared Compute Pool</span>
                  <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-sand-100 text-stone-600 font-semibold border border-sand-200">
                    Shared Free Runtime
                  </span>
                </div>
                <span
                  className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded-full border ${
                    daemonsOnline > 0
                      ? "bg-emerald-50 text-emerald-800 border-emerald-200"
                      : "bg-amber-50 text-amber-800 border-amber-200"
                  }`}
                >
                  {daemonsOnline > 0 ? "DAEMON ONLINE" : "IDLE · RUNS ON DEMAND"}
                </span>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-3 gap-3 p-3 bg-sand-50/70 rounded-xl border border-sand-200 text-xs font-mono">
                <div>
                  <span className="text-stone-400 block text-[10px]">Monthly Compute</span>
                  <span className="font-bold text-stone-900">
                    {u?.agent_minutes_used.toFixed(1)} {hasCap ? `/ ${u?.agent_minutes_limit}` : ""} min
                  </span>
                </div>
                <div>
                  <span className="text-stone-400 block text-[10px]">Sandbox Runtime</span>
                  <span className="font-bold text-emerald-700">gVisor Container Isolation</span>
                </div>
                <div>
                  <span className="text-stone-400 block text-[10px]">Dedicated VPC</span>
                  <UpgradeButton plan={u?.plan || "free"} variant="minimal" />
                </div>
              </div>
            </div>
          ) : (
            /* Pro / Enterprise Managed Fleets */
            <div className="space-y-3">
              {managedFleets.length === 0 ? (
                <div className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-2">
                      <span className="w-2 h-2 rounded-full bg-emerald-500" />
                      <span className="font-bold text-stone-900 text-xs font-mono">kiwi-cloud-managed-pool</span>
                    </div>
                    <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-emerald-50 text-emerald-700 border border-emerald-200">
                      ONLINE
                    </span>
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 text-xs font-mono text-stone-600">
                    <div className="flex justify-between p-2 rounded-lg bg-sand-50">
                      <span>Daemons:</span>
                      <strong className="text-stone-900">{managedDaemons.length || 1} Ready</strong>
                    </div>
                    <div className="flex justify-between p-2 rounded-lg bg-sand-50">
                      <span>Runtime:</span>
                      <strong className="text-kiwi-700">gVisor MicroVM</strong>
                    </div>
                    <div className="flex justify-between p-2 rounded-lg bg-sand-50">
                      <span>Active Tasks:</span>
                      <strong className="text-stone-900">{activeTasksCount} leased</strong>
                    </div>
                  </div>
                </div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                  {managedFleets.map((f) => {
                    const fleetDaemons = daemons.filter((d) => d.fleet_id === f.id);
                    const onlineCount = fleetDaemons.filter((d) => d.online).length;
                    return (
                      <div key={f.id} className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
                        <div className="flex items-center justify-between">
                          <div className="flex items-center gap-2">
                            <Cloud className="w-4 h-4 text-kiwi-600" />
                            <span className="font-bold text-stone-900 text-xs font-mono">{f.name}</span>
                          </div>
                          <span className="px-2 py-0.5 rounded-full text-[9px] font-mono font-bold bg-kiwi-50 text-kiwi-800 border border-kiwi-200">
                            MANAGED
                          </span>
                        </div>
                        <div className="space-y-1 text-[11px] font-mono text-stone-600">
                          <div className="flex justify-between">
                            <span>Daemons Enrolled:</span>
                            <strong className="text-stone-900">{fleetDaemons.length}</strong>
                          </div>
                          <div className="flex justify-between">
                            <span>Online Health:</span>
                            <strong className={onlineCount > 0 ? "text-emerald-700" : "text-stone-500"}>
                              {onlineCount} / {fleetDaemons.length || 1} Online
                            </strong>
                          </div>
                          <div className="flex justify-between">
                            <span>Fleet ID:</span>
                            <code className="text-stone-400">{f.id.slice(0, 8)}</code>
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* ================= CATEGORY 2: SELF-HOSTED RUNNERS (BYOC) ================= */}
      {(filter === "all" || filter === "byoc") && (
        <div className="space-y-4 pt-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-6 h-6 rounded-lg bg-indigo-100 border border-indigo-200 text-indigo-800 flex items-center justify-center font-bold text-xs shadow-2xs">
                <Building2 className="w-3.5 h-3.5 text-indigo-700" />
              </div>
              <div>
                <h3 className="text-xs font-bold text-stone-900">Self-Hosted Runners (BYOC — Private Cloud VPC)</h3>
                <p className="text-[11px] text-stone-500">
                  Private daemons executing inside your AWS VPC, GCP, or Kubernetes cluster. Zero code egress.
                </p>
              </div>
            </div>
            <span className="text-[10px] font-mono font-bold text-indigo-800 bg-indigo-50 border border-indigo-200 px-2 py-0.5 rounded-full flex items-center gap-1">
              <ShieldCheck className="w-3 h-3 text-indigo-600" />
              CUSTOMER VPC ISOLATED
            </span>
          </div>

          {isFree ? (
            /* LOCKED GATED CARD (FREE TIER) */
            <div className="p-8 rounded-3xl border-2 border-dashed border-sand-300 bg-gradient-to-b from-sand-50/80 to-white space-y-4 text-center max-w-2xl mx-auto shadow-2xs">
              <div className="w-12 h-12 rounded-2xl bg-amber-100 border border-amber-200 text-amber-800 flex items-center justify-center mx-auto shadow-2xs">
                <Lock className="w-6 h-6" />
              </div>

              <div className="space-y-1">
                <span className="text-[10px] font-mono font-bold bg-amber-100 text-amber-900 px-2 py-0.5 rounded-full border border-amber-200">
                  PRO & ENTERPRISE EXCLUSIVE
                </span>
                <h3 className="text-base font-bold text-stone-900">Self-Hosted Runners (BYOC) are Locked on Free Tier</h3>
                <p className="text-xs text-stone-600 max-w-md mx-auto">
                  Connect your private cloud infrastructure so autonomous AI agents run safely inside your own AWS/GCP VPC with zero data egress.
                </p>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2 text-left text-xs text-stone-700 bg-white p-3.5 rounded-2xl border border-sand-200 font-medium">
                <div className="flex items-center gap-2">
                  <ShieldCheck className="w-4 h-4 text-emerald-600 shrink-0" />
                  <span>Execute tests behind your corporate VPC firewall</span>
                </div>
                <div className="flex items-center gap-2">
                  <Server className="w-4 h-4 text-indigo-600 shrink-0" />
                  <span>Hardware acceleration with local Firecracker / KVM</span>
                </div>
              </div>

              {u?.plan === "free" && (
                <div>
                  <UpgradeButton label="Upgrade to Pro to Connect BYOC Runners" />
                </div>
              )}
            </div>
          ) : (
            /* UNLOCKED PRO/ENTERPRISE BYOC SECTION */
            <div className="space-y-4">
              {/* Terminal Quick-Connect Box */}
              <div className="p-4 rounded-2xl border border-sand-200 bg-sand-50/70 space-y-3 shadow-2xs">
                <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                  <span className="font-bold text-stone-800 flex items-center gap-1.5">
                    <Terminal className="w-4 h-4 text-stone-700" />
                    <span>Deploy a New Self-Hosted Runner in your VPC</span>
                  </span>

                  <div className="flex items-center gap-2">
                    {byocFleets.length > 1 && (
                      <select
                        value={selectedFleetId}
                        onChange={(e) => {
                          setSelectedFleetId(e.target.value);
                          mintToken(e.target.value);
                        }}
                        aria-label="Target BYOC Fleet"
                        className="bg-white border border-sand-200 rounded-lg text-[11px] font-mono px-2 py-1 text-stone-700"
                      >
                        {byocFleets.map((f) => (
                          <option key={f.id} value={f.id}>
                            Fleet: {f.name}
                          </option>
                        ))}
                      </select>
                    )}
                    <button
                      onClick={() => mintToken(selectedFleetId)}
                      disabled={minting}
                      className="text-stone-500 hover:text-stone-900 text-[11px] font-mono underline flex items-center gap-1"
                    >
                      <RefreshCw className={`w-3 h-3 ${minting ? "animate-spin" : ""}`} />
                      <span>Regenerate Token</span>
                    </button>
                  </div>
                </div>

                <div className="flex items-center justify-between bg-stone-950 p-3 rounded-xl border border-stone-800 font-mono text-xs text-stone-200 overflow-x-auto gap-2">
                  <code className="text-kiwi-300 truncate select-all">
                    curl -fsSL https://get.runkiwi.dev/install.sh | sh && kiwidaemon join --token{" "}
                    {token?.value || "kw_sec_minting..."} --vpc-strict
                  </code>
                  <button
                    onClick={copyCommand}
                    className="px-3 py-1.5 rounded-lg bg-stone-800 hover:bg-stone-700 text-stone-200 text-[11px] font-sans font-semibold shrink-0 transition-all flex items-center gap-1.5"
                  >
                    {copied ? (
                      <>
                        <Check className="w-3.5 h-3.5 text-emerald-400" />
                        <span className="text-emerald-300">Copied</span>
                      </>
                    ) : (
                      <>
                        <Copy className="w-3.5 h-3.5" />
                        <span>Copy</span>
                      </>
                    )}
                  </button>
                </div>
                <p className="text-[10px] text-stone-500 leading-tight">
                  Run this command inside any Linux EC2/GCE instance or Kubernetes node with Docker/KVM access. The daemon
                  securely leases jobs and runs sandboxes locally.
                </p>
              </div>

              {/* BYOC Fleets List */}
              {byocFleets.length > 0 && (
                <div className="space-y-2">
                  <h4 className="text-xs font-bold text-stone-900 flex items-center gap-1.5">
                    <Layers className="w-3.5 h-3.5 text-stone-600" />
                    <span>Configured BYOC Fleets ({byocFleets.length})</span>
                  </h4>
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                    {byocFleets.map((f) => {
                      const fleetDaemons = daemons.filter((d) => d.fleet_id === f.id);
                      const onlineCount = fleetDaemons.filter((d) => d.online).length;
                      return (
                        <div key={f.id} className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3">
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-2">
                              <Building2 className="w-4 h-4 text-indigo-600" />
                              <span className="font-bold text-stone-900 text-xs font-mono">{f.name}</span>
                            </div>
                            <span className="px-2 py-0.5 rounded-full text-[9px] font-mono font-bold bg-indigo-50 text-indigo-800 border border-indigo-200">
                              BYOC
                            </span>
                          </div>
                          <div className="space-y-1 text-[11px] font-mono text-stone-600">
                            <div className="flex justify-between">
                              <span>Enrolled Daemons:</span>
                              <strong className="text-stone-900">{fleetDaemons.length}</strong>
                            </div>
                            <div className="flex justify-between">
                              <span>Online Status:</span>
                              <strong className={onlineCount > 0 ? "text-emerald-700" : "text-stone-500"}>
                                {onlineCount} Online
                              </strong>
                            </div>
                          </div>
                          <button
                            onClick={() => {
                              setSelectedFleetId(f.id);
                              mintToken(f.id);
                            }}
                            className="w-full py-1.5 px-2 rounded-lg bg-sand-50 hover:bg-sand-100 border border-sand-200 text-[10px] font-mono font-semibold text-stone-700 flex items-center justify-center gap-1.5 transition-all"
                          >
                            <KeyRound className="w-3 h-3 text-stone-500" />
                            <span>Mint Join Token</span>
                          </button>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}

              {/* Enrolled Daemons Grid */}
              <div className="space-y-2">
                <h4 className="text-xs font-bold text-stone-900 flex items-center gap-1.5">
                  <Server className="w-3.5 h-3.5 text-stone-600" />
                  <span>Enrolled Daemon Instances ({daemons.length})</span>
                </h4>

                {daemons.length === 0 ? (
                  <div className="p-8 rounded-2xl border border-sand-200 bg-white text-center space-y-2 shadow-2xs">
                    <Server className="w-8 h-8 text-stone-400 mx-auto" />
                    <h5 className="text-xs font-bold text-stone-800">No Private Daemons Enrolled Yet</h5>
                    <p className="text-[11px] text-stone-500 max-w-sm mx-auto">
                      Run the terminal command above on any server or cloud VM to connect your first private runner daemon.
                    </p>
                  </div>
                ) : (
                  <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                    {daemons.map((d: Daemon) => {
                      const assignedFleet = fleets.find((f) => f.id === d.fleet_id);
                      return (
                        <div
                          key={d.id}
                          className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3 hover:border-sand-300 transition-all font-mono text-xs"
                        >
                          <div className="flex items-center justify-between">
                            <div className="flex items-center gap-1.5 min-w-0">
                              <span className="relative flex h-2 w-2 shrink-0">
                                {d.online && (
                                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75" />
                                )}
                                <span
                                  className={`relative inline-flex rounded-full h-2 w-2 ${
                                    d.online ? "bg-emerald-500" : "bg-rose-500"
                                  }`}
                                />
                              </span>
                              <span className="font-bold text-stone-900 truncate" title={d.id}>
                                #{d.id.slice(0, 12)}
                              </span>
                            </div>
                            <span
                              className={`px-2 py-0.5 rounded-full text-[9px] font-bold ${
                                d.online
                                  ? "bg-emerald-50 text-emerald-800 border border-emerald-200"
                                  : "bg-rose-50 text-rose-800 border border-rose-200"
                              }`}
                            >
                              {d.online ? "ONLINE" : "OFFLINE"}
                            </span>
                          </div>

                          <div className="space-y-1 text-[11px] text-stone-600 border-t border-sand-150 pt-2.5">
                            <div className="flex justify-between">
                              <span className="text-stone-400">Fleet:</span>
                              <span className="font-bold text-stone-800 truncate max-w-[120px]">
                                {assignedFleet ? assignedFleet.name : d.fleet_id ? d.fleet_id.slice(0, 8) : "Default Pool"}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-stone-400">Last Heartbeat:</span>
                              <span className="text-stone-700">
                                {d.last_seen_at ? new Date(d.last_seen_at).toLocaleTimeString() : "Never"}
                              </span>
                            </div>
                            <div className="flex justify-between">
                              <span className="text-stone-400">Registered:</span>
                              <span className="text-stone-700">
                                {d.created_at ? new Date(d.created_at).toLocaleDateString() : "—"}
                              </span>
                            </div>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            </div>
          )}
        </div>
      )}

      {/* ================= CREATE FLEET MODAL ================= */}
      {showCreateModal && (
        <div
          className="fixed inset-0 bg-stone-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50"
          onClick={() => setShowCreateModal(false)}
        >
          <div
            className="bg-white rounded-3xl border border-sand-200 p-6 max-w-md w-full space-y-4 shadow-popover"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b border-sand-200 pb-3">
              <div className="flex items-center gap-2">
                <div className="p-1.5 rounded-lg bg-sand-100 text-stone-800">
                  <Plus className="w-4 h-4" />
                </div>
                <h3 className="text-sm font-bold text-stone-900">Create New Execution Fleet</h3>
              </div>
              <button
                onClick={() => setShowCreateModal(false)}
                className="text-stone-400 hover:text-stone-700 p-1 rounded-lg text-xs"
              >
                ✕
              </button>
            </div>

            <form onSubmit={handleCreateFleet} className="space-y-4 text-xs">
              <div>
                <label className="block text-[10px] font-bold text-stone-500 uppercase tracking-wider mb-1">
                  Fleet Name
                </label>
                <input
                  type="text"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. prod-vpc-us-east, k8s-europe-cluster"
                  className="w-full field text-xs"
                  autoFocus
                />
              </div>

              <div>
                <label className="block text-[10px] font-bold text-stone-500 uppercase tracking-wider mb-1">
                  Fleet Type
                </label>
                <div className="grid grid-cols-2 gap-2">
                  <button
                    type="button"
                    onClick={() => setType("byoc")}
                    className={`p-3 rounded-2xl border text-left flex flex-col gap-1 transition-all ${
                      type === "byoc"
                        ? "bg-indigo-50/60 border-indigo-500 text-indigo-950 font-bold shadow-2xs"
                        : "bg-sand-50 border-sand-200 text-stone-600 hover:bg-sand-100"
                    }`}
                  >
                    <div className="flex items-center gap-1.5">
                      <Building2 className="w-3.5 h-3.5 text-indigo-600" />
                      <span>Self-Hosted (BYOC)</span>
                    </div>
                    <p className="text-[10px] font-normal text-stone-500">Run in your private VPC</p>
                  </button>

                  <button
                    type="button"
                    onClick={() => setType("managed")}
                    className={`p-3 rounded-2xl border text-left flex flex-col gap-1 transition-all ${
                      type === "managed"
                        ? "bg-kiwi-50/60 border-kiwi-500 text-kiwi-950 font-bold shadow-2xs"
                        : "bg-sand-50 border-sand-200 text-stone-600 hover:bg-sand-100"
                    }`}
                  >
                    <div className="flex items-center gap-1.5">
                      <Cloud className="w-3.5 h-3.5 text-kiwi-700" />
                      <span>Managed Cloud</span>
                    </div>
                    <p className="text-[10px] font-normal text-stone-500">Run on Kiwi Cloud</p>
                  </button>
                </div>
              </div>

              {err && (
                <div className="flex items-center gap-1.5 text-rose-600 text-[11px] p-2 bg-rose-50 border border-rose-200 rounded-xl">
                  <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                  <span>{err}</span>
                </div>
              )}

              <div className="flex items-center justify-end gap-2 pt-2 border-t border-sand-150">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="px-3 py-1.5 rounded-xl border border-sand-200 text-stone-600 hover:bg-sand-100 font-semibold text-xs"
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={busy}
                  className="px-4 py-1.5 rounded-xl bg-stone-900 hover:bg-stone-800 text-white font-semibold text-xs shadow-xs disabled:opacity-50 flex items-center gap-1.5"
                >
                  {busy ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
                  <span>Create Fleet</span>
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
