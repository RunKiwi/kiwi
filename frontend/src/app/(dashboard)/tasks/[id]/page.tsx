"use client";

import { Suspense, use, useEffect, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { client, type Job } from "@/lib/api";
import { usePolling } from "@/hooks/usePolling";
import { buildThread, selectedNode, threadSummary } from "@/lib/thread";
import { ThreadRail } from "@/components/ThreadRail";
import { RunDetail } from "@/components/RunDetail";
import { LoadingState } from "@/components/LoadingState";

/**
 * A task's thread: the run that was submitted and every run a review comment
 * continued it with.
 *
 * The route parameter is the job id. The design said "any member of the
 * thread", and the job id is exactly that and better — a continuation reuses
 * its parent's job id, which is what keeps it on the same branch, so one job
 * id names the whole thread and it is what the API is already keyed by. Which
 * run is selected is a query parameter, so a single node stays linkable too.
 *
 * This is a page rather than part of TaskDrawer because a thread is something
 * you return to, link to a teammate, and read while it is still moving. The
 * drawer also has no room: a rail plus live output does not fit beside it.
 */

/** Terminal statuses — polling backs off once every run has reached one. */
const TERMINAL = new Set(["SUCCEEDED", "FAILED", "CANCELLED"]);

function ThreadPage({ jobId }: { jobId: string }) {
  const router = useRouter();
  const params = useSearchParams();
  const runParam = params.get("run") ?? undefined;

  const [job, setJob] = useState<Job | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Initial load. Settled in a promise callback with a subscription guard
  // rather than synchronously in the effect body — the same shape TaskDrawer
  // uses for the record, and what React expects of an effect.
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

  // Refresh while a run is in flight. usePolling already pauses on a hidden
  // tab, backs off once everything is terminal, and cannot start two timer
  // chains — none of which a bare setInterval here would do.
  usePolling(
    async () => {
      try {
        setJob(await client.getJob(jobId));
      } catch {
        // A failed poll is a blip; the thread on screen stays as it was rather
        // than collapsing into an error.
      }
    },
    { enabled: !!job, isIdle: idle, activeIntervalMs: 2500, idleIntervalMs: 15000 },
  );

  if (error) {
    return <p className="p-8 text-[12px] text-rose-300">{error}</p>;
  }
  if (!job) {
    return <LoadingState label="Loading this task" />;
  }

  const nodes = buildThread(job.tasks);
  const selected = selectedNode(nodes, runParam);
  const summary = threadSummary(nodes);

  return (
    <div className="mx-auto flex h-full max-w-6xl flex-col p-3 md:p-8">
      <header className="mb-4">
        <Link href="/" className="mb-2 inline-flex items-center gap-1 text-[11px] text-zinc-500 hover:text-zinc-300">
          <ArrowLeft className="h-3 w-3" /> Tasks
        </Link>
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-[15px] text-zinc-100">{job.task || job.job_id}</h1>
          {summary.continued && (
            <span className="rounded-full bg-white/6 px-2 py-0.5 text-[10px] text-zinc-400">
              {summary.runs} runs
            </span>
          )}
          {job.repo && <span className="text-[11px] text-zinc-600">{job.repo}</span>}
        </div>
      </header>

      <div className="flex min-h-0 flex-1 gap-2">
        <ThreadRail
          nodes={nodes}
          selectedId={selected?.task.id}
          onSelect={(taskId) => router.replace(`/tasks/${jobId}?run=${encodeURIComponent(taskId)}`)}
        />
        {selected ? (
          <RunDetail task={selected.task} />
        ) : (
          <p className="pl-4 text-[12px] text-zinc-500">This task has no runs yet.</p>
        )}
      </div>
    </div>
  );
}

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  return (
    <Suspense fallback={<LoadingState label="Loading this task" />}>
      <ThreadPage jobId={id} />
    </Suspense>
  );
}
