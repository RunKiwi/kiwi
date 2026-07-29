import type { JobSummary } from "./api";

export interface JobFilterOptions {
  status?: string;
  repo?: string;
  query?: string;
}

export type JobSortOption = "newest" | "oldest" | "status";

/**
 * The job statuses the board can filter by, in the order they are offered.
 * CANCELLED belongs here: a cancelled job is reachable from the queue and from
 * the card actions, and leaving it out of the filter set would make every job a
 * user called off findable only under "All".
 */
export const FILTERABLE_STATUSES = ["QUEUED", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED"] as const;

const SORT_OPTIONS: readonly JobSortOption[] = ["newest", "oldest", "status"];

/** Coerce a `?status=` value to a filter the UI can render, else "all". */
export function parseStatusParam(raw: string | null | undefined): string {
  if (!raw) return "all";
  const upper = raw.toUpperCase();
  return (FILTERABLE_STATUSES as readonly string[]).includes(upper) ? upper : "all";
}

/** Coerce a `?sort=` value to a known sort, else the default. */
export function parseSortParam(raw: string | null | undefined): JobSortOption {
  return SORT_OPTIONS.includes(raw as JobSortOption) ? (raw as JobSortOption) : "newest";
}

export interface JobGroups {
  today: JobSummary[];
  yesterday: JobSummary[];
  thisWeek: JobSummary[];
  older: JobSummary[];
}

export function filterJobs(jobs: JobSummary[], options: JobFilterOptions = {}): JobSummary[] {
  const { status, repo, query } = options;
  const normalizedQuery = query?.trim().toLowerCase();
  const normalizedStatus = status && status.toLowerCase() !== "all" ? status.toUpperCase() : undefined;

  return jobs.filter((job) => {
    if (normalizedStatus && job.status?.toUpperCase() !== normalizedStatus) {
      return false;
    }
    if (repo && repo !== "all" && job.repo !== repo) {
      return false;
    }
    if (normalizedQuery) {
      const matchId = job.job_id.toLowerCase().includes(normalizedQuery);
      const matchTask = job.task?.toLowerCase().includes(normalizedQuery) ?? false;
      const matchRepo = job.repo?.toLowerCase().includes(normalizedQuery) ?? false;
      if (!matchId && !matchTask && !matchRepo) {
        return false;
      }
    }
    return true;
  });
}

const STATUS_PRIORITY: Record<string, number> = {
  RUNNING: 1,
  QUEUED: 2,
  FAILED: 3,
  SUCCEEDED: 4,
  CANCELLED: 5,
};

export function sortJobs(jobs: JobSummary[], sortBy: JobSortOption = "newest"): JobSummary[] {
  const list = [...jobs];
  list.sort((a, b) => {
    if (sortBy === "oldest") {
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime();
    }
    if (sortBy === "status") {
      const pA = STATUS_PRIORITY[a.status?.toUpperCase()] ?? 99;
      const pB = STATUS_PRIORITY[b.status?.toUpperCase()] ?? 99;
      if (pA !== pB) return pA - pB;
      // Secondary sort by newest first
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    }
    // Default "newest"
    return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
  });
  return list;
}

export function groupJobsByDate(jobs: JobSummary[], referenceDate: Date = new Date()): JobGroups {
  const groups: JobGroups = {
    today: [],
    yesterday: [],
    thisWeek: [],
    older: [],
  };

  const startOfToday = new Date(referenceDate);
  startOfToday.setHours(0, 0, 0, 0);

  const startOfYesterday = new Date(startOfToday);
  startOfYesterday.setDate(startOfYesterday.getDate() - 1);

  const startOfThisWeek = new Date(startOfToday);
  startOfThisWeek.setDate(startOfThisWeek.getDate() - 7);

  for (const job of jobs) {
    const jobDate = new Date(job.created_at);
    if (Number.isNaN(jobDate.getTime())) {
      groups.older.push(job);
      continue;
    }

    if (jobDate >= startOfToday) {
      groups.today.push(job);
    } else if (jobDate >= startOfYesterday) {
      groups.yesterday.push(job);
    } else if (jobDate >= startOfThisWeek) {
      groups.thisWeek.push(job);
    } else {
      groups.older.push(job);
    }
  }

  return groups;
}
