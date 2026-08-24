import { create } from 'zustand';
import { client, Job, JobSummary, Daemon } from '@/lib/api';
import { capture, markOnce } from '@/lib/analytics';

/**
 * Report each job that has produced a pull request, once ever.
 *
 * There is no push channel for this — the dashboard polls `listJobs`, so the
 * same finished job reappears every few seconds and again on every reload.
 * `markOnce` keys on the job id so the funnel counts pull requests rather than
 * poll ticks.
 */
function reportPullRequests(jobs: JobSummary[]): void {
  for (const job of jobs) {
    const count = job.pr_urls?.length ?? 0;
    if (count > 0 && markOnce(`pr:${job.job_id}`)) {
      capture('pr_opened', { job_id: job.job_id, pr_count: count });
    }
  }
}

export type TaskStatus = "QUEUED" | "LEASED" | "SUCCEEDED" | "FAILED";

export interface ProviderConfig {
  name: "Anthropic" | "Gemini" | "OpenAI";
  isConfigured: boolean;
}

interface FleetState {
  jobs: JobSummary[];
  currentJob: Job | null;
  daemons: Daemon[];
  providers: ProviderConfig[];
  isLoading: boolean;
  error: string | null;
  
  loadJobs: () => Promise<JobSummary[]>;
  loadJob: (jobId: string) => Promise<void>;
  loadDaemons: () => Promise<void>;
}

export const useFleetStore = create<FleetState>((set, get) => ({
  jobs: [],
  currentJob: null,
  daemons: [],
  providers: [
    { name: "Anthropic", isConfigured: false },
    { name: "Gemini", isConfigured: false },
    { name: "OpenAI", isConfigured: false },
  ],
  isLoading: false,
  error: null,
  
  loadJobs: async () => {
    if (get().jobs.length === 0) {
      set({ isLoading: true, error: null });
    }
    try {
      const data = await client.listJobs();
      const jobs = data.jobs || [];
      reportPullRequests(jobs);
      set({ jobs, isLoading: false, error: null });
      return jobs;
    } catch (err) {
      set({ error: (err as Error).message || "Failed to load jobs", isLoading: false });
      return [];
    }
  },
  
  loadJob: async (jobId: string) => {
    set({ error: null });
    try {
      const data = await client.getJob(jobId);
      set({ currentJob: data });
    } catch (err) {
      set({ error: (err as Error).message || "Failed to load job" });
    }
  },
  
  loadDaemons: async () => {
    if (get().daemons.length === 0) {
      set({ isLoading: true, error: null });
    }
    try {
      const data = await client.listDaemons();
      set({ daemons: data || [], isLoading: false, error: null });
    } catch (err) {
      set({ error: (err as Error).message || "Failed to load daemons", isLoading: false });
    }
  }
}));
