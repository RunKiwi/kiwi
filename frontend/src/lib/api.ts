export interface PlanRequest {
  task: string;
  repo_url: string;
  ref: string;
  file: string;
  test_cmd: string;
  model: string;
  max_workers: number;
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
  online: boolean;
  last_seen_at?: string;
  created_at: string;
}

export interface ValidateResponse {
  user_id: string;
  org_id: string;
  org_name: string;
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
      const errorData = await response.json();
      if (errorData && errorData.error) {
        errorMessage = errorData.error;
      }
    } catch {
      // If we can't parse the JSON, fall back to statusText
    }
    throw new ApiError(errorMessage);
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
};
