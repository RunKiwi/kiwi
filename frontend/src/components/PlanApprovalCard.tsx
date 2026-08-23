"use client";

import React, { useState } from "react";
import { CheckSquare, MessageSquare, AlertCircle, Compass, Check } from "lucide-react";
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
    <div className="rounded-2xl border border-indigo-200/90 bg-gradient-to-b from-indigo-50/40 via-white to-white p-5 shadow-sm space-y-4">
      <div className="flex items-center justify-between border-b border-indigo-100 pb-3">
        <div className="flex items-center gap-2">
          <span className="p-1.5 rounded-xl bg-indigo-600 text-white shadow-xs">
            <Compass className="w-4 h-4" />
          </span>
          <div>
            <h4 className="text-xs font-bold text-indigo-950">Architect Execution Plan</h4>
            <p className="text-[11px] text-stone-500 font-mono">Model: {plan.architect_model || "Claude Sonnet 5"}</p>
          </div>
        </div>
        <span className="px-2 py-0.5 rounded-full text-[10px] font-mono font-bold bg-indigo-100 text-indigo-800 border border-indigo-200 flex items-center gap-1.5">
          <span className="w-1.5 h-1.5 rounded-full bg-indigo-600 animate-ping" />
          Awaiting Approval
        </span>
      </div>

      <div className="p-3.5 rounded-xl bg-stone-900 text-stone-100 font-mono text-xs max-h-60 overflow-y-auto whitespace-pre-wrap leading-relaxed border border-stone-800 shadow-inner">
        {plan.plan_markdown || "1. Analyze codebase dependencies\n2. Modify schema & implement handlers\n3. Run automated verification suite"}
      </div>

      {error && (
        <div className="p-2.5 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs flex items-center gap-2">
          <AlertCircle className="w-4 h-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {!showRejectForm ? (
        <div className="flex items-center justify-between gap-3 pt-1">
          <button
            type="button"
            onClick={() => setShowRejectForm(true)}
            disabled={submitting}
            className="px-3.5 py-2 rounded-xl text-stone-600 hover:text-stone-900 hover:bg-sand-100 text-xs font-semibold flex items-center gap-1.5 transition-all"
          >
            <MessageSquare className="w-3.5 h-3.5 text-stone-400" />
            <span>Request Revision...</span>
          </button>

          <button
            type="button"
            onClick={handleApprove}
            disabled={submitting}
            className="px-4 py-2 rounded-xl bg-indigo-600 hover:bg-indigo-700 text-white text-xs font-bold flex items-center gap-2 shadow-sm transition-all"
          >
            {submitting ? <KiwiMicroButtonLoader /> : <Check className="w-4 h-4" />}
            <span>Approve & Execute Plan &rarr;</span>
          </button>
        </div>
      ) : (
        <div className="space-y-2 pt-1">
          <textarea
            value={feedback}
            onChange={(e) => setFeedback(e.target.value)}
            placeholder="Explain what needs to change in this plan..."
            rows={2}
            className="w-full p-2.5 rounded-xl border border-sand-300 text-xs font-sans focus:ring-2 focus:ring-stone-900 focus:outline-none"
          />
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={() => setShowRejectForm(false)}
              className="px-3 py-1.5 rounded-xl text-stone-600 hover:bg-sand-100 text-xs font-semibold"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleReject}
              disabled={submitting || !feedback.trim()}
              className="px-3.5 py-1.5 rounded-xl bg-rose-600 hover:bg-rose-700 disabled:opacity-50 text-white text-xs font-bold flex items-center gap-1.5"
            >
              {submitting ? <KiwiMicroButtonLoader /> : <CheckSquare className="w-3.5 h-3.5" />}
              <span>Send Revision Feedback</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
