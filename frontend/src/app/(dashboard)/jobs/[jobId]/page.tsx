"use client";

import { TaskDrawer } from "@/components/TaskDrawer";
import { useRouter } from "next/navigation";
import { use } from "react";

export default function JobPage({ params }: { params: Promise<{ jobId: string }> }) {
  const router = useRouter();
  const resolvedParams = use(params);

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col">
      <div className="mb-8">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Job Details</h1>
        <p className="text-zinc-400">Viewing job {resolvedParams.jobId}</p>
      </div>
      
      {/* We render the TaskDrawer, which will overlay this page */}
      <TaskDrawer taskId={resolvedParams.jobId} onClose={() => router.push("/")} />
    </div>
  );
}
