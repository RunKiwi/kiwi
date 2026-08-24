"use client";

import { Suspense, use, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { ArrowLeft, GitBranch } from "lucide-react";
import { client, type Job } from "@/lib/api";
import { usePolling } from "@/hooks/usePolling";
import { buildThread, selectedNode, threadSummary } from "@/lib/thread";
import { ThreadRail } from "@/components/ThreadRail";
import { RunDetail } from "@/components/RunDetail";
import { LoadingState } from "@/components/LoadingState";
import { Logo } from "@/components/Logo";

/** Terminal statuses — polling backs off once every run has reached one. */
const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

function ThreadPage({ jobId }: { jobId: string }) {
  const router = useRouter();
  const params = useSearchParams();
  const runParam = params.get("run") ?? undefined;

  const [job, setJob] = useState<Job | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let isSubscribed = true;
    client
      .getJob(jobId)
      .then((res) => {
        if (!isSubscribed) return;
        setJob(res);
        setError(null);
      })
      .catch((err) => {
        if (!isSubscribed) return;
        setError(err instanceof Error ? err.message : "Could not load this task");
      });
    return () => {
      isSubscribed = false;
    };
  }, [jobId]);

  const idle = job ? job.tasks.every((t) => TERMINAL.has(t.status)) : false;

  usePolling(
    async () => {
      try {
        setJob(await client.getJob(jobId));
      } catch {
        // non-fatal
      }
    },
    { enabled: !!job, isIdle: idle, activeIntervalMs: 2500, idleIntervalMs: 15000 },
  );

  if (error) {
    return (
      <div className="p-8 text-center text-xs font-mono text-rose-700 bg-rose-50 rounded-2xl border border-rose-200">
        {error}
      </div>
    );
  }
  if (!job) {
    return <LoadingState label="Loading task thread..." />;
  }

  const nodes = buildThread(job.tasks);
  const selected = selectedNode(nodes, runParam);
  const summary = threadSummary(nodes);

  return (
    <div className="mx-auto flex h-full max-w-6xl flex-col gap-4 font-sans text-stone-900 select-none">
      
      {/* Header with Navigation and Modern Swiss Styling */}
      <div className="p-4 rounded-2xl border border-sand-200/90 bg-white shadow-2xs space-y-2">
        <div className="flex items-center justify-between">
          <Link
            href="/"
            className="inline-flex items-center gap-1.5 text-xs font-semibold text-stone-500 hover:text-stone-900 transition-colors"
          >
            <ArrowLeft className="w-3.5 h-3.5" />
            <span>Back to Dashboard</span>
          </Link>

          <span className="text-[10px] font-mono font-bold bg-sand-100 text-stone-700 px-2 py-0.5 rounded border border-sand-200 uppercase tracking-wider">
            TASK THREAD
          </span>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
          <div className="flex items-center gap-3 min-w-0">
            <div className="w-10 h-10 rounded-xl bg-sand-50 border border-sand-200/90 shadow-2xs flex items-center justify-center shrink-0">
              <Logo variant="full-color" pose="hacking" animated={true} className="w-6 h-6" />
            </div>
            <div className="min-w-0">
              <h1 className="text-sm sm:text-base font-bold text-stone-900 truncate">
                {job.task || job.job_id}
              </h1>
              <div className="flex items-center gap-2.5 text-[11px] font-mono text-stone-400 flex-wrap mt-0.5">
                <span>Job ID: <strong className="text-stone-700">{job.job_id.slice(0, 12)}</strong></span>
                {job.repo && (
                  <>
                    <span>•</span>
                    <span className="flex items-center gap-1 text-stone-700">
                      <GitBranch className="w-3 h-3 text-stone-400" />
                      <span>{job.repo}</span>
                    </span>
                  </>
                )}
              </div>
            </div>
          </div>

          {summary.continued && (
            <span className="rounded-full bg-sand-100 border border-sand-200 px-2.5 py-0.5 text-xs font-mono font-bold text-stone-700">
              {summary.runs} Execution Runs
            </span>
          )}
        </div>
      </div>

      {/* Thread Rail & Run Detail Split Container */}
      <div className="flex min-h-0 flex-1 gap-3 overflow-hidden">
        <ThreadRail
          nodes={nodes}
          selectedId={selected?.task.id}
          onSelect={(taskId) => router.replace(`/tasks/${jobId}?run=${encodeURIComponent(taskId)}`)}
        />
        {selected ? (
          <div className="flex-1 min-w-0 overflow-y-auto rounded-2xl bg-white border border-sand-200/90 shadow-2xs p-4 sm:p-5">
            <RunDetail task={selected.task} />
          </div>
        ) : (
          <div className="flex-1 p-8 text-center text-xs font-mono text-stone-400 bg-sand-50/40 rounded-2xl border border-sand-200">
            This task has no execution runs yet.
          </div>
        )}
      </div>
    </div>
  );
}

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return (
    <Suspense fallback={<LoadingState label="Loading task thread..." />}>
      <ThreadPage jobId={id} />
    </Suspense>
  );
}
