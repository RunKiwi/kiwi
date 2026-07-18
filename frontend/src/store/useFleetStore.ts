import { create } from 'zustand';
import { client, Job, JobSummary, Daemon } from '@/lib/api';

export type TaskStatus = "QUEUED" | "LEASED" | "SUCCEEDED" | "FAILED";

export interface ProviderConfig {
  name: "Anthropic" | "Gemini" | "Codex";
  isConfigured: boolean;
}

interface FleetState {
  jobs: JobSummary[];
  currentJob: Job | null;
  daemons: Daemon[];
  providers: ProviderConfig[];
  isLoading: boolean;
  error: string | null;
  
  loadJobs: () => Promise<void>;
  loadJob: (jobId: string) => Promise<void>;
  loadDaemons: () => Promise<void>;
}

export const useFleetStore = create<FleetState>((set) => ({
  jobs: [],
  currentJob: null,
  daemons: [],
  providers: [
    { name: "Anthropic", isConfigured: false },
    { name: "Gemini", isConfigured: false },
    { name: "Codex", isConfigured: false },
  ],
  isLoading: false,
  error: null,
  
  loadJobs: async () => {
    set({ isLoading: true, error: null });
    try {
      const data = await client.listJobs();
      set({ jobs: data.jobs || [], isLoading: false });
    } catch (err) {
      set({ error: (err as Error).message || "Failed to load jobs", isLoading: false });
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
    set({ isLoading: true, error: null });
    try {
      const data = await client.listDaemons();
      set({ daemons: data || [], isLoading: false });
    } catch (err) {
      set({ error: (err as Error).message || "Failed to load daemons", isLoading: false });
    }
  }
}));
