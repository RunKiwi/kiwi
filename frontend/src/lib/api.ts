export interface PlanRequest {
  task: string;
  repo_url: string;
  // Everything below is optional — we're driving an AI agent, so file / test
  // command / ref / model are hints, not hard requirements.
  ref?: string;
  file?: string;
  files?: string[];
  test_cmd?: string;
  model?: string;
  max_workers?: number;
  fleet_id?: string;
}

export interface Fleet {
  id: string;
  org_id: string;
  name: string;
  type: "self-managed" | "byoc";
  created_at: string;
}

export interface ModelEntry {
  id: string;
  name: string;
  provider: string;
  created_at: string;
}

export interface Integration {
  key: string;
  kind: string;
  connected: boolean;
}

export interface GithubRepo {
  full_name: string;
  url: string;
  private: boolean;
  default_branch: string;
}

export interface PlanResponse {
  manifest_id: string;
  job_id: string;
  task_ids: string[];
  summary: string;
}

export interface JobTask {
  id: string;
  status: string;
  result_url?: string;
  result_detail?: string;
}

export interface Job {
  job_id: string;
  tasks: JobTask[];
}

export interface JobSummary {
  job_id: string;
  created_at: string;
  task_count: number;
  status: string;
  pr_urls: string[];
}

export interface JobsListResponse {
  jobs: JobSummary[];
}

export interface Daemon {
  id: string;
  fleet_id?: string;
  online: boolean;
  last_seen_at?: string;
  created_at: string;
}

export interface ValidateResponse {
  user_id: string;
  org_id: string;
  org_name: string;
  activation_state: string;
  plan: string;
}

const getBaseUrl = () => {
  return process.env.NEXT_PUBLIC_KIWI_API_URL || "http://localhost:8080";
};

const getToken = () => {
  if (typeof window !== "undefined") {
    return localStorage.getItem("kiwi_token");
  }
  return null;
};

class ApiError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ApiError";
  }
}

async function fetchApi<T>(path: string, options?: RequestInit): Promise<T> {
  const url = `${getBaseUrl()}${path}`;
  const headers = new Headers(options?.headers);
  
  const token = getToken();
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  if (options?.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const response = await fetch(url, {
    ...options,
    headers,
  });

  if (!response.ok) {
    if (response.status === 202) {
      // 202 Accepted is valid for our planner endpoint
      return response.json() as Promise<T>;
    }
    
    let errorMessage = response.statusText;
    try {
      const raw = await response.text();
      if (raw) {
        // Handlers return either JSON {error|message} or a plain-text body
        // (Go's http.Error). Surface whichever we get so the real reason —
        // e.g. "Anthropic rejected this credential" — reaches the user.
        try {
          const parsed = JSON.parse(raw);
          errorMessage = parsed?.error || parsed?.message || raw;
        } catch {
          errorMessage = raw;
        }
      }
    } catch {
      // Body unreadable — fall back to statusText.
    }
    throw new ApiError(errorMessage.trim());
  }

  if (response.status === 204) {
    return null as unknown as T;
  }

  return response.json() as Promise<T>;
}

export const client = {
  validate: () => fetchApi<ValidateResponse>("/auth/validate"),
  
  submitPlan: (req: PlanRequest) => 
    fetchApi<PlanResponse>("/api/v1/planner/plan", {
      method: "POST",
      body: JSON.stringify(req),
    }),
    
  getJob: (jobId: string) => 
    fetchApi<Job>(`/api/v1/jobs/${jobId}`),
    
  listJobs: () => 
    fetchApi<JobsListResponse>("/api/v1/jobs"),
    
  listDaemons: () => 
    fetchApi<Daemon[]>("/api/v1/daemons"),
    
  setCredential: (name: string, kind: string, value: string) =>
    fetchApi<void>("/api/v1/credentials", {
      method: "POST",
      body: JSON.stringify({ name, kind, value }),
    }),

  listFleets: () => fetchApi<{ fleets: Fleet[] }>("/api/v1/fleets"),

  createFleet: (name: string, type: "self-managed" | "byoc") =>
    fetchApi<Fleet>("/api/v1/fleets", {
      method: "POST",
      body: JSON.stringify({ name, type }),
    }),

  listModels: () => fetchApi<{ models: ModelEntry[] }>("/api/v1/models"),

  createModel: (name: string, provider: string) =>
    fetchApi<ModelEntry>("/api/v1/models", {
      method: "POST",
      body: JSON.stringify({ name, provider }),
    }),

  deleteModel: (id: string) =>
    fetchApi<void>(`/api/v1/models/${id}`, { method: "DELETE" }),

  listIntegrations: () =>
    fetchApi<{ integrations: Integration[] }>("/api/v1/integrations"),

  listGithubRepos: () =>
    fetchApi<{ repos: GithubRepo[] }>("/api/v1/github/repos"),

  // Mint a single-use daemon join token. Pass a fleetId to bind the daemon to
  // that fleet (so it leases only that fleet's tasks); omit it for the org's
  // unassigned pool.
  mintJoinToken: (fleetId?: string) =>
    fetchApi<{ join_token: string; expires_in: number; fleet_id: string }>("/api/v1/daemon/join-token", {
      method: "POST",
      body: JSON.stringify({ fleet_id: fleetId ?? "" }),
    }),
};

// Built-in model ids offered even before an org adds custom ones. The daemon
// routes gemini-* to Gemini, else Anthropic.
export const BUILTIN_MODELS = [
  "claude-opus-4-8",
  "claude-haiku-4-5-20251001",
  "gemini-2.0-flash",
];
