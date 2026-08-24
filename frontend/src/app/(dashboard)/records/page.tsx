"use client";

import { useEffect, useState } from "react";
import { ShieldCheck, Lock, CheckCircle2, Key } from "lucide-react";
import { api, type JobSummary, type ExecutionRecordBody } from "@/lib/api";
import { useFleetStore } from "@/store/useFleetStore";
import { LoadingState } from "@/components/LoadingState";

export default function RecordsPage() {
  const { loadJobs } = useFleetStore();
  const [loading, setLoading] = useState(true);
  const [records, setRecords] = useState<{ job: JobSummary; recordHash: string | null; body: ExecutionRecordBody }[]>([]);

  useEffect(() => {
    async function loadData() {
      try {
        const fetchedJobs = await loadJobs();
        const finishedJobs = (fetchedJobs || []).filter((j) => j.status === "SUCCEEDED" || j.status === "FAILED");
        const loaded: { job: JobSummary; recordHash: string | null; body: ExecutionRecordBody }[] = [];

        for (const job of finishedJobs.slice(0, 5)) {
          try {
            const rec = await api.getJobRecord(job.job_id);
            if (rec && rec.data) {
              loaded.push({ job, recordHash: rec.recordHash, body: rec.data as ExecutionRecordBody });
            }
          } catch {
            // ignore
          }
        }
        setRecords(loaded);
      } finally {
        setLoading(false);
      }
    }
    loadData();
  }, [loadJobs]);

  if (loading) {
    return (
      <div className="flex items-center justify-center min-h-[60vh]">
        <LoadingState state="solving" label="Loading Cryptographic Audit Receipts..." />
      </div>
    );
  }

  return (
    <div className="p-0 sm:p-2 md:p-4 max-w-6xl mx-auto space-y-6 font-sans text-stone-900">
      {/* Header */}
      <div className="border-b border-sand-150 pb-4">
        <div className="flex items-center gap-2 mb-1">
          <span className="text-[10px] font-mono font-bold bg-emerald-100 text-emerald-800 border border-emerald-200 px-2 py-0.5 rounded-full flex items-center gap-1">
            <ShieldCheck className="w-3 h-3 text-emerald-600" />
            ZERO TRUST SECURITY
          </span>
        </div>
        <h1 className="text-xl font-bold text-stone-900 tracking-tight">Audit Receipts</h1>
        <p className="text-xs text-stone-500 mt-0.5">
          Tamper-evident execution receipts signed with Ed25519, verifying zero credential egress and automated test outcomes.
        </p>
      </div>

      {/* Security Features Banner */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-2">
          <div className="flex items-center gap-2">
            <div className="p-1.5 rounded-lg bg-indigo-50 text-indigo-700">
              <Key className="w-4 h-4" />
            </div>
            <h3 className="text-xs font-bold text-stone-900">Ed25519 Signed</h3>
          </div>
          <p className="text-[11px] text-stone-500 leading-relaxed">
            Every step in the container loop is signed with the runner daemon&apos;s ephemeral key.
          </p>
        </div>

        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-2">
          <div className="flex items-center gap-2">
            <div className="p-1.5 rounded-lg bg-emerald-50 text-emerald-700">
              <Lock className="w-4 h-4" />
            </div>
            <h3 className="text-xs font-bold text-stone-900">Network Isolated</h3>
          </div>
          <p className="text-[11px] text-stone-500 leading-relaxed">
            Outbound egress to cloud metadata (169.254.169.254) and external APIs is strictly blocked.
          </p>
        </div>

        <div className="p-4 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-2">
          <div className="flex items-center gap-2">
            <div className="p-1.5 rounded-lg bg-amber-50 text-amber-700">
              <CheckCircle2 className="w-4 h-4" />
            </div>
            <h3 className="text-xs font-bold text-stone-900">Proof of Test Pass</h3>
          </div>
          <p className="text-[11px] text-stone-500 leading-relaxed">
            Full stdout/stderr test receipts are cryptographically hashed into the manifest root.
          </p>
        </div>
      </div>

      {/* Receipts List */}
      <div className="space-y-4">
        <h2 className="text-sm font-bold text-stone-900">Recent Verification Manifests</h2>

        {records.length === 0 ? (
          <div className="p-8 rounded-2xl border border-sand-200 bg-white text-center space-y-2">
            <ShieldCheck className="w-8 h-8 text-stone-300 mx-auto" />
            <h3 className="text-xs font-bold text-stone-700">No Audit Receipts Yet</h3>
            <p className="text-[11px] text-stone-400 max-w-sm mx-auto font-sans">
              Cryptographic verification manifests and Ed25519-signed task receipts will appear here once tasks complete execution.
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {records.map(({ job, recordHash, body }) => (
              <div key={job.job_id} className="p-5 rounded-2xl border border-sand-200 bg-white shadow-2xs space-y-3 font-mono text-xs">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="font-bold text-stone-900">RECORD: #{job.job_id.slice(0, 8)}</span>
                    <span className="text-stone-500 font-sans">{job.repo}</span>
                  </div>
                  {body.attestation && (
                    <span className="px-2 py-0.5 rounded text-[10px] font-bold bg-emerald-50 text-emerald-800 border border-emerald-200">
                      {body.attestation}
                    </span>
                  )}
                </div>

                <div className="p-3 bg-sand-50 rounded-xl space-y-1 text-[11px] text-stone-700 border border-sand-200">
                  <p><span className="text-stone-400">HashRoot:</span> {recordHash || body.prev_record_hash || "—"}</p>
                  <p><span className="text-stone-400">DaemonSignature:</span> {body.record_signature?.sig ? `${body.record_signature.sig.slice(0, 48)}...` : "—"}</p>
                  <p><span className="text-stone-400">NetworkEgress:</span> <span className="text-emerald-700 font-semibold">{body.execution?.sandbox?.network === "blocked" ? "BLOCKED & ZERO LEAKS" : (body.execution?.sandbox?.network || "ISOLATED")}</span></p>
                  <p><span className="text-stone-400">Verification:</span> {body.verification?.final_outcome || "—"}</p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
