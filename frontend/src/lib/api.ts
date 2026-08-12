export interface PlanRequest {
  task: string;
  repo_url: string;
  // Everything below is optional — we're driving an AI agent, so file / test
  // command / ref / model are hints, not hard requirements.
  ref?: string;
  file?: string;
  files?: string[];
  test_cmd?: string;
  // model is the worker model — the Implementer, which runs constantly. It runs
  // on your own provider key.
  model?: string;
  // planner_model is accepted and ignored. Decomposition into a worker DAG is
  // gone: the Architect plans inside the daemon, so nothing is planned here.
  planner_model?: string;
  max_workers?: number;
  fleet_id?: string;
  reference_mode?: string;
  reference_job_ids?: string[];
  // mode is accepted and ignored. The Architect/Implementer session is the only
  // execution loop; the field survives so a client written against the two-mode
  // API still submits rather than failing on an unknown key.
  mode?: string;
  // architect_model plans and reviews. Expected to be more capable than model:
  // the reviewer is called a handful of times per task while the implementer
  // runs constantly. Omitted lets the Control Plane choose — see
  // DefaultArchitectModel in ee/planner.
  architect_model?: string;
}

export interface Fleet {
  id: string;
  org_id: string;
  name: string;
  type: "managed" | "byoc";
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

export interface GithubInstallation {
  installation_id: number;
  org_id: string;
  account_login: string;
  repo_selection: string;
  created_at: string;
  updated_at: string;
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

/** Why a QUEUED task has not started. Mirrors store.Block* in the Go backend. */
export type BlockedReason =
  | "awaiting_runner"
  | "provisioning"
  | "provision_failed"
  | "no_runner"
  | "runner_offline"
  | "concurrency_cap"
  | "compute_cap"
  | "waiting_on_dependencies";

export interface JobTask {
  id: string;
  status: string;
  /** This worker's own goal, from its spec. */
  task?: string;
  depends_on?: string[];
  model?: string;
  files?: string[];
  result_url?: string;
  result_detail?: string;
  queued_at: string;
  started_at?: string;
  // Last write to the task row. On a terminal task this is the completion time
  // — the schema has no completed_at. On a running one it is bumped by lease
  // renewal, so it is only a finish time once the task is terminal.
  updated_at?: string;
  attempts: number;
  leased_by?: string;
  /** Set only while the task is QUEUED; blocked_detail is the sentence to show. */
  blocked_reason?: BlockedReason;
  blocked_detail?: string;

  // Lineage. A review comment on a Kiwi pull request continues the task that
  // opened it, and a continuation reuses its parent's job id — so a job's task
  // list is a thread, and these say which run follows which.
  /** Empty on a task submitted directly. */
  parent_task_id?: string;
  /** The thread this run belongs to; equals its own id on the first run. */
  root_task_id?: string;
  /** submit | pr_comment | fork */
  origin?: string;
}

export interface Job {
  job_id: string;
  /** The overall goal that produced this job, and the repo it targets. */
  task?: string;
  repo?: string;
  tasks: JobTask[];
}

// Mirrors orchestrator.JobLifecycleResponse. tasks_affected is load-bearing: a
// cancel on a job whose tasks all finished a moment earlier succeeds and changes
// nothing, and the caller has to be able to tell the difference.
export interface JobLifecycleResult {
  job_id: string;
  action: string;
  tasks_affected: number;
  message?: string;
}

/**
 * A job's signed execution record. `recordHash` comes from the X-Kiwi-Record-Hash
 * response header rather than the body, so a caller can verify chain continuity
 * without re-canonicalizing the record.
 */
export interface ExecutionRecordResponse {
  recordHash: string | null;
  data: unknown;
}

/**
 * The subset of ver.Record the receipt panel reads. `attestation` is the
 * backend's own word for whether a Control-Plane signature is present
 * ("signed" | "unsigned"); it is not a claim that the client verified anything.
 */
// One phase of the Actor-Critic loop, as recorded by the daemon that ran it.
// Raw test output is never carried — only a digest — because it can contain
// secrets; the Critic's own reasons are quoted, bounded, since they are what
// explains a rejection.
/**
 * One step of a run.
 *
 * Two sources produce this shape and they carry different fields, which is why
 * almost everything below is optional:
 *
 * - The **live progress** feed (`ver.TaskEvent`) carries `detail`, `duration_ms`,
 *   `input_tokens`, `output_tokens` and `cost_usd` — what a step is doing and
 *   what it cost, available while the run is still going.
 * - The **signed record** (`ver.WorkerStep`) carries `reasons` and `detail_hash`
 *   instead, and moves the token and cost totals up to the worker.
 *
 * `phase` for session mode is `actor:<tool>` (e.g. `actor:read_file`), plus the
 * raw `round_start` / `session_end` markers — see `sessionPhase` in
 * pkg/daemon/session_run.go.
 */
export interface RecordStep {
  step: number;
  phase: "initial_test" | "actor" | "critic" | "test" | string;
  outcome: "pass" | "fail" | "proposed" | "approved" | "rejected" | "error" | string;
  // Signed-record only.
  reasons?: string;
  detail_hash?: string;
  // Live-progress only.
  detail?: string;
  duration_ms?: number;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
  // The tool call's arguments, as the model wrote them — the command `run` was
  // given, the path `read_file` was asked for. Live-progress only: the signed
  // record commits to them as `input_hash` instead, for the same reason it
  // hashes `detail`.
  input?: string;
  // When the daemon recorded the event. Durations say how long each phase took;
  // only this says how long the run spent between them.
  at?: string;
}

export interface RecordWorker {
  worker_id?: string;
  actor_model?: string;
  critic_model?: string;
  provider?: string;
  steps?: RecordStep[];
  // Per-worker totals, present on the signed record. The live feed reports the
  // same quantities per step instead, so a running job sums its steps and a
  // finished one reads these.
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
  critic_rejections?: number;
}

export interface ExecutionRecordBody {
  ver?: string;
  record_id?: string;
  job_id?: string;
  prev_record_hash?: string;
  attestation?: string;
  record_signature?: { alg?: string; key?: string; sig?: string };
  verification?: { test_cmd?: string; final_outcome?: string; duration_ms?: number };
  intent?: { submitted_at?: string };
  execution?: {
    sandbox?: { runtime?: string; network?: string };
    workers?: RecordWorker[];
  };
}

export interface JobSummary {
  job_id: string;
  created_at: string;
  task_count: number;
  /**
   * How many of this job's runs came from a review comment. task_count cannot
   * stand in for it: a plan with three workers counts three the same way three
   * runs do, and only one of those is a thread.
   */
  continuation_count?: number;
  /** How the newest run came to exist, so a row says what moved it last. */
  latest_origin?: string;
  status: string;
  pr_urls: string[];
  task?: string;
  repo?: string;
  fleet_id?: string;
  daemon_id?: string;
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

export interface ProviderInfo {
  id: string;
  display: string;
  kind: string;
  connected: boolean;
  kiwi_available: boolean;
}

export interface CatalogModel {
  org_id: string;
  model_id: string;
  provider: string;
  display_name: string;
  description: string;
  input_cost_per_m: number | null;
  output_cost_per_m: number | null;
  context_length: number | null;
  supports_tools: boolean | null;
  tier: "free" | "economy" | "frontier" | "unknown";
  kiwi_provided: boolean;
  selectable: boolean;
}

export interface ValidateResponse {
  user_id: string;
  org_id: string;
  org_name: string;
  activation_state: string;
  plan: string;
  role: string;
  domain_join: boolean;
  primary_domain: string;
}

export interface SpendBucket {
  label: string;
  planner_usd: number;
  worker_usd: number;
  total_usd: number;
}

export interface SpendPoint {
  date: string;
  planner_usd: number;
  worker_usd: number;
}

/** Mirrors orchestrator.SpendResponse. */
export interface SpendResponse {
  from: string;
  to: string;
  cost_usd: number;
  planner_usd: number;
  worker_usd: number;
  agent_minutes: number;
  tokens_in: number;
  tokens_out: number;
  kiwi_tokens_in: number;
  kiwi_tokens_out: number;
  job_count: number;
  /**
   * Jobs with at least one metered task. Below job_count, the total is a floor
   * rather than a measurement — jobs that ran before cost metering shipped have
   * no cost recorded, and their zero must not be shown as one.
   */
  metered_jobs: number;
  daily: SpendPoint[];
  by_repo: SpendBucket[];
  by_model: SpendBucket[];
  by_provider: SpendBucket[];
  allowance?: AllowanceBucket[];
  plan?: string;
  allowance_stale?: boolean;
}

// Billing plans are "plans"; model price bands are "classes". They used to
// share the word "tier", which made "Free tier" mean either the plan a customer
// is on or the band a model sits in — and on the Models page both appeared on
// screen at once. The wire format still says `tier`; only the label changes.
export const MODEL_CLASS_LABEL: Record<string, string> = {
  free: "No-cost",
  economy: "Economy",
  frontier: "Frontier",
  unknown: "Unpriced",
};

// Cheapest first, so the biggest allowance leads and the scarce one reads as
// the exception. Exported so the Models and Spend pages cannot disagree about
// the order — they show the same three bars.
export const CLASS_ORDER = ["free", "economy", "frontier"];

export function modelClassLabel(tier: string): string {
  return MODEL_CLASS_LABEL[tier] ?? tier;
}

// What each class is for, in one line, so the wildly different allowances
// (10M vs 50k tokens) read as deliberate rather than arbitrary.
export const MODEL_CLASS_BLURB: Record<string, string> = {
  free: "Models that cost nothing to run.",
  economy: "Cheap models — the default for most work.",
  frontier: "The most capable models, and the most expensive.",
  unknown: "Price unknown, so Kiwi cannot fund these.",
};

export function planLabel(plan: string): string {
  if (!plan) return "";
  return plan.charAt(0).toUpperCase() + plan.slice(1) + " plan";
}

export function formatTokens(n: number): string {
  if (n < 0) return "Unlimited";
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(n % 1_000_000 === 0 ? 0 : 1) + "M";
  if (n >= 1_000) return (n / 1_000).toFixed(n % 1_000 === 0 ? 0 : 1) + "k";
  return String(n);
}

export interface AllowanceBucket {
  tier: string;
  period: string;
  granted: number;
  used: number;
  remaining: number;
}

export interface UsageResponse {
  plan: string;
  activation_state: string;
  agent_minutes_used: number;
  agent_minutes_limit: number; // 0 = unlimited
  concurrent_jobs_running: number;
  max_concurrent_jobs: number;
  is_super_admin?: boolean;
}

export interface AdminUsageRow {
  model?: string;
  provider: string;
  task_count: number;
  cost_usd: number;
  // kiwi_cost_usd is the subset of cost_usd spent on Kiwi-funded (free tier)
  // work — never billed to anyone, so it is the number to watch for abuse.
  kiwi_cost_usd: number;
  tokens_in: number;
  tokens_out: number;
}

export interface AdminStats {
  total_orgs: number;
  orgs_by_plan: Record<string, number>;
  orgs_by_activation_state: Record<string, number>;
  signups_last_7_days: number;
  signups_last_30_days: number;
  total_agent_minutes: number;
  tasks_by_status: Record<string, number>;
  model_usage: AdminUsageRow[];
  provider_usage: AdminUsageRow[];
}

export interface AdminOrg {
  id: string;
  name: string;
  plan: string;
  activation_state: string;
  domain_join: boolean;
  primary_domain: string;
  // Omitted when an AdminOrg is built from /auth/validate (self-service),
  // which doesn't return it — OrgManagementPanel never displays it.
  created_at?: string;
}

export interface AdminJoinRequest {
  id: string;
  org_id: string;
  user_email: string;
  status: string;
  created_at: string;
}

export interface AdminUser {
  id: string;
  email: string;
  name: string;
  org_id: string;
  role: string;
  created_at: string;
}

export interface AdminAuditLog {
  id: number;
  org_id: string;
  user_id: string;
  user_email: string;
  action: string;
  resource: string;
  resource_id: string;
  details: string;
  client_ip: string;
  created_at: string;
}

export interface AdminProviderConfig {
  org_id: string;
  provider_name: string;
  actor_model: string;
  critic_model: string;
  api_key?: string;
}

export interface AdminAPIKey {
  id: string;
  user_id: string;
  label: string;
  created_at: string;
  expires_at?: string;
  revoked_at?: string;
}

export interface AdminAPIKeyCreated {
  key_id: string;
  key: string;
  label: string;
  user_id: string;
  created_at: string;
  expires_at: string | null;
}

export interface AdminUserUsageRow {
  user_id: string;
  email: string;
  task_count: number;
  succeeded: number;
  failed: number;
  cost_usd: number;
  kiwi_cost_usd: number;
  tokens_in: number;
  tokens_out: number;
}

export interface AdminOrgModelUsage {
  model_usage: AdminUsageRow[];
  provider_usage: AdminUsageRow[];
  tasks_by_status: Record<string, number>;
  per_user: AdminUserUsageRow[];
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

  // A 200 can still have an empty body (e.g. handlers that call
  // w.WriteHeader(http.StatusOK) without writing JSON) — response.json() would
  // throw "Unexpected end of JSON input" on that, so read as text first and
  // only parse when there's something to parse.
  const raw = await response.text();
  return (raw ? JSON.parse(raw) : null) as T;
}

export interface AuthProvidersResponse {
  providers: string[];
}

export const client = {
  getAuthProviders: () => fetchApi<AuthProvidersResponse>("/auth/providers"),
  validate: () => fetchApi<ValidateResponse>("/auth/validate"),
  getUsage: () => fetchApi<UsageResponse>("/api/v1/usage"),

  getSpend: (from: string, to: string, funding?: string) =>
    fetchApi<SpendResponse>(`/api/v1/spend?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}${funding ? `&funding=${encodeURIComponent(funding)}` : ""}`),

  // Admin APIs
  getAdminStats: () => fetchApi<AdminStats>("/admin/stats"),
  listAdminOrgs: () => fetchApi<AdminOrg[]>("/admin/orgs"),
  createAdminOrg: (name: string) => fetchApi<AdminOrg>("/admin/orgs", { method: "POST", body: JSON.stringify({ name }) }),
  setOrgPlan: (orgId: string, plan: string) => fetchApi<void>(`/admin/orgs/${orgId}/plan`, { method: "POST", body: JSON.stringify({ plan }) }),
  grantOrgMinutes: (orgId: string, agent_minutes: number) => fetchApi<void>(`/admin/orgs/${orgId}/grant`, { method: "POST", body: JSON.stringify({ agent_minutes }) }),
  activateOrg: (orgId: string) => fetchApi<void>(`/admin/orgs/${orgId}/activate`, { method: "POST" }),
  suspendOrg: (orgId: string) => fetchApi<void>(`/admin/orgs/${orgId}/suspend`, { method: "POST" }),
  listAdminOrgUsers: (orgId: string) => fetchApi<AdminUser[]>(`/admin/orgs/${orgId}/users`),
  createAdminOrgUser: (orgId: string, email: string, name: string, role: string) => fetchApi<AdminUser>(`/admin/orgs/${orgId}/users`, { method: "POST", body: JSON.stringify({ email, name, role }) }),
  listAdminUserAPIKeys: (orgId: string, userId: string) => fetchApi<AdminAPIKey[]>(`/admin/orgs/${orgId}/users/${userId}/keys`),
  createAdminUserAPIKey: (orgId: string, userId: string, label: string) => fetchApi<AdminAPIKeyCreated>(`/admin/orgs/${orgId}/users/${userId}/keys`, { method: "POST", body: JSON.stringify({ label }) }),
  revokeAdminUserAPIKey: (orgId: string, userId: string, keyId: string) => fetchApi<void>(`/admin/orgs/${orgId}/users/${userId}/keys/${keyId}`, { method: "DELETE" }),
  getAdminOrgAuditLogs: (orgId: string) => fetchApi<AdminAuditLog[]>(`/admin/orgs/${orgId}/audit`),
  getAdminOrgModelUsage: (orgId: string) => fetchApi<AdminOrgModelUsage>(`/admin/orgs/${orgId}/model_usage`),
  getAdminOrgProviderConfig: (orgId: string) => fetchApi<AdminProviderConfig>(`/admin/orgs/${orgId}/provider`),
  setAdminOrgProviderConfig: (orgId: string, config: Partial<AdminProviderConfig>) => fetchApi<AdminProviderConfig>(`/admin/orgs/${orgId}/provider`, { method: "PUT", body: JSON.stringify(config) }),
  listJoinRequests: (orgId: string) => fetchApi<AdminJoinRequest[]>(`/admin/orgs/${orgId}/join_requests`),
  approveJoinRequest: (orgId: string, reqId: string) => fetchApi<void>(`/admin/orgs/${orgId}/join_requests/${reqId}/approve`, { method: "POST" }),
  denyJoinRequest: (orgId: string, reqId: string) => fetchApi<void>(`/admin/orgs/${orgId}/join_requests/${reqId}/deny`, { method: "POST" }),
  setDomainJoin: (orgId: string, domainJoin: boolean) => fetchApi<AdminOrg>(`/admin/orgs/${orgId}/domain_join`, { method: "PUT", body: JSON.stringify({ domain_join: domainJoin }) }),
  renameOrg: (orgId: string, name: string) => fetchApi<AdminOrg>(`/admin/orgs/${orgId}/name`, { method: "PUT", body: JSON.stringify({ name }) }),

  // Starts a Stripe Checkout Session for the Pro upgrade and returns the hosted
  // checkout URL to redirect to. 503 when billing isn't configured.
  createCheckout: () =>
    fetchApi<{ url: string }>("/api/v1/billing/checkout", { method: "POST" }),

  submitPlan: (req: PlanRequest) => 
    fetchApi<PlanResponse>("/api/v1/planner/plan", {
      method: "POST",
      body: JSON.stringify(req),
    }),
    
  getJob: (jobId: string) => 
    fetchApi<Job>(`/api/v1/jobs/${jobId}`),

  // The one endpoint whose response header carries meaning, so it cannot go
  // through fetchApi (which returns only the parsed body). It still has to
  // resolve against the control-plane origin and send the bearer token like
  // every other call — a bare relative fetch would hit the dashboard's own
  // origin unauthenticated.
  // What a job is doing right now. Distinct from getJobRecord: a record is a
  // signed artifact assembled once at the end, this is a mutable view of a run
  // in flight, and it 404s for a job that never started.
  getJobProgress: (jobId: string) =>
    fetchApi<{ tasks: JobProgressTask[] }>(`/api/v1/jobs/${encodeURIComponent(jobId)}/progress`),

  getJobRecord: async (jobId: string): Promise<ExecutionRecordResponse> => {
    const headers = new Headers();
    const token = getToken();
    if (token) headers.set("Authorization", `Bearer ${token}`);

    const res = await fetch(
      `${getBaseUrl()}/api/v1/jobs/${encodeURIComponent(jobId)}/record`,
      { headers },
    );
    if (!res.ok) {
      const text = await res.text().catch(() => "");
      throw new ApiError((text || res.statusText || "Record not found").trim());
    }
    const recordHash = res.headers.get("X-Kiwi-Record-Hash");
    const data = await res.json();
    return { recordHash, data };
  },
    
  listJobs: () =>
    fetchApi<JobsListResponse>("/api/v1/jobs"),

  /** Stop a job. Queued tasks stop at once; a running task stops at its next
   * lease renewal, since the daemon is a separate process the CP cannot reach. */
  cancelJob: (jobId: string) =>
    fetchApi<JobLifecycleResult>(`/api/v1/jobs/${jobId}/cancel`, { method: "POST" }),

  /** Requeue a job's failed and cancelled tasks. Succeeded tasks are left alone. */
  retryJob: (jobId: string) =>
    fetchApi<JobLifecycleResult>(`/api/v1/jobs/${jobId}/retry`, { method: "POST" }),

  /** Remove a job. Cancels first, so no daemon is left working on a deleted row. */
  deleteJob: (jobId: string) =>
    fetchApi<JobLifecycleResult>(`/api/v1/jobs/${jobId}`, { method: "DELETE" }),
    
  listDaemons: () => 
    fetchApi<Daemon[]>("/api/v1/daemons"),
    
  setCredential: (name: string, kind: string, value: string) =>
    fetchApi<void>("/api/v1/credentials", {
      method: "POST",
      body: JSON.stringify({ name, kind, value }),
    }),

  listFleets: () => fetchApi<{ fleets: Fleet[] }>("/api/v1/fleets"),

  createFleet: (name: string, type: "managed" | "byoc") =>
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

  // GitHub App. listGithubInstallations reports which GitHub accounts this org
  // has connected; githubInstallUrl asks the Control Plane for a signed install
  // link.
  //
  // The link is fetched rather than navigated to. This endpoint is behind bearer
  // auth and a top-level navigation carries no Authorization header, so the
  // server hands back the URL as JSON and the caller navigates itself.
  listGithubInstallations: () =>
    fetchApi<{ installations: GithubInstallation[] }>("/api/v1/github/installations"),

  githubInstallUrl: () =>
    fetchApi<{ install_url: string }>("/api/v1/github/install", {
      headers: { Accept: "application/json" },
    }),

  listProviders: () => fetchApi<{ providers: ProviderInfo[] }>("/api/v1/providers"),
  listCatalogModels: () => fetchApi<{ models: CatalogModel[] }>("/api/v1/catalog/models"),

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

// Curated models we recommend, grouped by provider. Shown on the Models page for
// one-click add so people don't have to hand-type ids. (Automatic discovery from
// the org's stored key is a planned follow-up.)
// One worker's live state while it runs. `steps` is the same shape RunTimeline
// renders for a finished job, so the live and final views are one component.
export interface JobProgressTask {
  task_id: string;
  status: string;
  actor_model?: string;
  steps: RecordStep[];
  // What is running right now, for the gap between two steps — which on a slow
  // install or test is most of the elapsed time.
  phase?: string;
  output_tail?: string;
  progress_at?: string;
}

export interface RecommendedModel {
  id: string;
  label: string;
  provider: "anthropic" | "gemini" | "openai";
  note?: string;
}

export const RECOMMENDED_MODELS: RecommendedModel[] = [
  { id: "claude-opus-4-8", label: "Claude Opus 4.8", provider: "anthropic", note: "Most capable" },
  { id: "claude-sonnet-5", label: "Claude Sonnet 5", provider: "anthropic", note: "Balanced" },
  { id: "claude-haiku-4-5-20251001", label: "Claude Haiku 4.5", provider: "anthropic", note: "Fast & cheap" },
  { id: "gemini-flash-latest", label: "Gemini Flash (latest)", provider: "gemini", note: "Fast & cheap" },
  { id: "gemini-2.0-flash", label: "Gemini 2.0 Flash", provider: "gemini" },
  { id: "gpt-5", label: "GPT-5", provider: "openai", note: "Most capable" },
  { id: "gpt-5-mini", label: "GPT-5 mini", provider: "openai", note: "Balanced" },
  { id: "gpt-4.1-mini", label: "GPT-4.1 mini", provider: "openai", note: "Fast & cheap" },
];

// The task form's worker default: a fast, cheap Implementer. The Architect it
// is paired with is chosen by the Control Plane, so the form does not carry a
// second default that could drift from it.
export const DEFAULT_WORKER_MODEL = "claude-haiku-4-5-20251001";

// How each provider id is written for a human. "OpenAI" does not survive CSS
// capitalize (it renders "Openai"), so the label is data, not a text-transform.
export const PROVIDER_LABELS: Record<string, string> = {
  anthropic: "Anthropic",
  gemini: "Gemini",
  openai: "OpenAI",
};

export function providerLabel(id: string): string {
  return PROVIDER_LABELS[id] ?? id;
}

export const SUPPORT_EMAIL = "support@runkiwi.dev";
export const PRO_UPGRADE_MAILTO = "mailto:" + SUPPORT_EMAIL + "?subject=" + encodeURIComponent("Kiwi Pro upgrade");
export const ENTERPRISE_MAILTO = "mailto:" + SUPPORT_EMAIL + "?subject=" + encodeURIComponent("Kiwi Enterprise");
