import type { JobSummary } from "./api";

export interface JobFilterOptions {
  status?: string;
  repo?: string;
  query?: string;
}

export type JobSortOption = "newest" | "oldest" | "status";

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
