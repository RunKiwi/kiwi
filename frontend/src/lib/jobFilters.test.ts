import { describe, it } from "node:test";
import assert from "node:assert";
import { filterJobs, sortJobs, groupJobsByDate } from "./jobFilters.ts";
import type { JobSummary } from "./api.ts";

const mockJobs: JobSummary[] = [
  {
    job_id: "job_001_alpha",
    created_at: new Date().toISOString(), // Today
    task_count: 2,
    status: "RUNNING",
    pr_urls: [],
    task: "Fix authentication header bug",
    repo: "owner/repo-a",
  },
  {
    job_id: "job_002_beta",
    created_at: new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString(), // Yesterday
    task_count: 1,
    status: "SUCCEEDED",
    pr_urls: ["https://github.com/owner/repo-b/pull/1"],
    task: "Add Postgres database migration",
    repo: "owner/repo-b",
  },
  {
    job_id: "job_003_gamma",
    created_at: new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString(), // This week (3 days ago)
    task_count: 4,
    status: "FAILED",
    pr_urls: [],
    task: "Update Docker container entrypoint",
    repo: "owner/repo-a",
  },
  {
    job_id: "job_004_delta",
    created_at: new Date(Date.now() - 10 * 24 * 60 * 60 * 1000).toISOString(), // Older (10 days ago)
    task_count: 1,
    status: "QUEUED",
    pr_urls: [],
    task: "Refactor queue lease handler",
    repo: "owner/repo-c",
  },
];

describe("filterJobs", () => {
  it("returns all jobs when no options provided", () => {
    const result = filterJobs(mockJobs, {});
    assert.strictEqual(result.length, 4);
  });

  it("filters jobs by status case-insensitively", () => {
    const succeeded = filterJobs(mockJobs, { status: "succeeded" });
    assert.strictEqual(succeeded.length, 1);
    assert.strictEqual(succeeded[0].job_id, "job_002_beta");

    const failed = filterJobs(mockJobs, { status: "FAILED" });
    assert.strictEqual(failed.length, 1);
    assert.strictEqual(failed[0].job_id, "job_003_gamma");
  });

  it("filters jobs by repo", () => {
    const repoA = filterJobs(mockJobs, { repo: "owner/repo-a" });
    assert.strictEqual(repoA.length, 2);
    assert.deepStrictEqual(
      repoA.map((j) => j.job_id),
      ["job_001_alpha", "job_003_gamma"]
    );
  });

  it("filters jobs by text query matching id, task, or repo", () => {
    const matchTask = filterJobs(mockJobs, { query: "postgres" });
    assert.strictEqual(matchTask.length, 1);
    assert.strictEqual(matchTask[0].job_id, "job_002_beta");

    const matchId = filterJobs(mockJobs, { query: "004_delta" });
    assert.strictEqual(matchId.length, 1);
    assert.strictEqual(matchId[0].job_id, "job_004_delta");

    const matchRepo = filterJobs(mockJobs, { query: "repo-c" });
    assert.strictEqual(matchRepo.length, 1);
    assert.strictEqual(matchRepo[0].job_id, "job_004_delta");
  });

  it("handles empty jobs input gracefully", () => {
    assert.deepStrictEqual(filterJobs([], { query: "test" }), []);
  });
});

describe("sortJobs", () => {
  it("sorts jobs by newest first by default", () => {
    const sorted = sortJobs(mockJobs, "newest");
    assert.strictEqual(sorted[0].job_id, "job_001_alpha");
    assert.strictEqual(sorted[3].job_id, "job_004_delta");
  });

  it("sorts jobs by oldest first", () => {
    const sorted = sortJobs(mockJobs, "oldest");
    assert.strictEqual(sorted[0].job_id, "job_004_delta");
    assert.strictEqual(sorted[3].job_id, "job_001_alpha");
  });

  it("sorts jobs by status priority (RUNNING -> QUEUED -> FAILED -> SUCCEEDED)", () => {
    const sorted = sortJobs(mockJobs, "status");
    const statusOrder = sorted.map((j) => j.status);
    assert.deepStrictEqual(statusOrder, ["RUNNING", "QUEUED", "FAILED", "SUCCEEDED"]);
  });
});

describe("groupJobsByDate", () => {
  it("groups jobs into today, yesterday, thisWeek, older using reference date", () => {
    const now = new Date();
    const groups = groupJobsByDate(mockJobs, now);

    assert.strictEqual(groups.today.length, 1);
    assert.strictEqual(groups.today[0].job_id, "job_001_alpha");

    assert.strictEqual(groups.yesterday.length, 1);
    assert.strictEqual(groups.yesterday[0].job_id, "job_002_beta");

    assert.strictEqual(groups.thisWeek.length, 1);
    assert.strictEqual(groups.thisWeek[0].job_id, "job_003_gamma");

    assert.strictEqual(groups.older.length, 1);
    assert.strictEqual(groups.older[0].job_id, "job_004_delta");
  });
});
