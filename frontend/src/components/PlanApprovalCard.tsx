"use client";

import React, { useState } from "react";
import { MessageSquare, AlertCircle, Compass, Check } from "lucide-react";
import { api, type JobPlan } from "@/lib/api";
import { KiwiMicroButtonLoader } from "@/components/KiwiLoaders";

export function PlanApprovalCard({
  plan,
  onApproved,
  onRejected,
}: {
  plan: JobPlan;
  onApproved: () => void;
  onRejected: () => void;
  }) {
  const [feedback, setFeedback] = useState("");
  const [showRejectForm, setShowRejectForm] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleApprove = async () => {
    setSubmitting(true);
    setError(null);
    try {
      await api.approveJobPlan(plan.job_id);
      onApproved();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to approve plan");
    } finally {
      setSubmitting(false);
    }
  };

  const handleReject = async () => {
    if (!feedback.trim()) return;
    setSubmitting(true);
    setError(null);
    try {
      await api.rejectJobPlan(plan.job_id, feedback.trim());
      onRejected();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to submit revision feedback");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="rounded-2xl border border-indigo-200/90 bg-white p-4 sm:p-5 shadow-2xs space-y-3 font-sans text-stone-900 select-none">
      <div className="flex items-center justify-between border-b border-indigo-100/80 pb-2.5">
        <div className="flex items-center gap-2">
          <div className="w-8 h-8 rounded-xl bg-indigo-50 border border-indigo-200 flex items-center justify-center text-indigo-700 shrink-0 shadow-2xs">
            <Compass className="w-4 h-4" />
          </div>
          <div>
            <h4 className="text-xs font-bold text-indigo-950">Architect Execution Plan</h4>
            <p className="text-[10px] text-stone-400 font-mono">Model: {plan.architect_model || "Claude 3.7 Sonnet"}</p>
          </div>
        </div>
        <span className="px-2 py-0.5 rounded-lg text-[10px] font-mono font-bold bg-indigo-50 text-indigo-800 border border-indigo-200 flex items-center gap-1.5 shadow-2xs">
          <span className="w-1.5 h-1.5 rounded-full bg-indigo-600 animate-pulse" />
          <span>AWAITING APPROVAL</span>
        </span>
      </div>

      <div className="p-3.5 rounded-xl bg-sand-50/80 text-stone-800 font-mono text-xs max-h-60 overflow-y-auto whitespace-pre-wrap leading-relaxed border border-sand-200 shadow-inner">
        {plan.plan_markdown || "1. Analyze codebase dependencies\n2. Modify schema & implement handlers\n3. Run automated verification suite"}
      </div>

      {error && (
        <div className="p-2.5 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2 font-mono">
          <AlertCircle className="w-4 h-4 shrink-0 text-rose-600" />
          <span>{error}</span>
        </div>
      )}

      {!showRejectForm ? (
        <div className="flex items-center justify-between gap-3 pt-1">
          <button
            type="button"
            onClick={() => setShowRejectForm(true)}
            disabled={submitting}
            className="px-3 py-1.5 rounded-xl border border-sand-200 text-stone-600 hover:text-stone-900 hover:bg-sand-100 text-xs font-semibold flex items-center gap-1.5 transition-all cursor-pointer shadow-2xs"
          >
            <MessageSquare className="w-3.5 h-3.5 text-stone-400" />
            <span>Request Revision...</span>
          </button>

          <button
            type="button"
            onClick={handleApprove}
            disabled={submitting}
            className="px-4 py-2 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white text-xs font-bold flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer active:scale-[0.98]"
          >
            {submitting ? <KiwiMicroButtonLoader /> : <Check className="w-3.5 h-3.5 text-kiwi-400" />}
            <span>Approve &amp; Execute Plan &rarr;</span>
          </button>
        </div>
      ) : (
        <div className="space-y-2 pt-1">
          <textarea
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder="Explain what needs to change in this plan..."
            rows={2}
            className="w-full p-2.5 rounded-xl border border-sand-200 bg-sand-50/80 focus:bg-white text-xs font-sans focus:border-stone-900 focus:outline-none transition-all shadow-2xs"
          />
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowRejectForm(false)}
              className="px-3 py-1.5 rounded-xl border border-sand-200 text-stone-600 hover:bg-sand-100 text-xs font-semibold cursor-pointer"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleReject}
              disabled={submitting || !feedback.trim()}
              className="px-4 py-1.5 rounded-xl bg-rose-600 hover:bg-rose-700 text-white text-xs font-bold flex items-center gap-1.5 shadow-2xs transition-all cursor-pointer disabled:opacity-40"
            >
              {submitting ? <KiwiMicroButtonLoader /> : null}
              <span>Send Revision Feedback</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
