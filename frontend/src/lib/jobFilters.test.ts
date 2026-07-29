import { describe, it } from "node:test";
import assert from "node:assert";
import {
  filterJobs,
  sortJobs,
  groupJobsByDate,
  parseStatusParam,
  parseSortParam,
} from "./jobFilters.ts";
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

  it("returns empty groups for empty input", () => {
    const groups = groupJobsByDate([], new Date());
    assert.deepStrictEqual(groups, { today: [], yesterday: [], thisWeek: [], older: [] });
  });

  it("puts a job created exactly at midnight into today, not yesterday", () => {
    const reference = new Date("2026-07-29T13:00:00");
    const midnight = new Date("2026-07-29T00:00:00");
    const groups = groupJobsByDate(
      [{ ...mockJobs[0], job_id: "job_midnight", created_at: midnight.toISOString() }],
      reference,
    );
    assert.strictEqual(groups.today.length, 1);
    assert.strictEqual(groups.yesterday.length, 0);
  });

  it("puts a job one millisecond before midnight into yesterday", () => {
    const reference = new Date("2026-07-29T13:00:00");
    const justBefore = new Date("2026-07-28T23:59:59.999");
    const groups = groupJobsByDate(
      [{ ...mockJobs[0], job_id: "job_edge", created_at: justBefore.toISOString() }],
      reference,
    );
    assert.strictEqual(groups.today.length, 0);
    assert.strictEqual(groups.yesterday.length, 1);
  });

  it("files a job with an unparseable timestamp under older rather than dropping it", () => {
    const groups = groupJobsByDate(
      [{ ...mockJobs[0], job_id: "job_bad_date", created_at: "not-a-date" }],
      new Date(),
    );
    assert.strictEqual(groups.older.length, 1);
    assert.strictEqual(groups.older[0].job_id, "job_bad_date");
  });
});

describe("sortJobs", () => {
  it("returns an empty array for empty input", () => {
    assert.deepStrictEqual(sortJobs([], "newest"), []);
  });

  it("does not mutate the input array", () => {
    const input = [...mockJobs];
    const order = input.map(j => j.job_id);
    sortJobs(input, "oldest");
    assert.deepStrictEqual(input.map(j => j.job_id), order);
  });
});

describe("filterJobs", () => {
  it("matches CANCELLED as a filterable status", () => {
    const cancelled = { ...mockJobs[0], job_id: "job_cancelled", status: "CANCELLED" };
    const result = filterJobs([...mockJobs, cancelled], { status: "CANCELLED" });
    assert.strictEqual(result.length, 1);
    assert.strictEqual(result[0].job_id, "job_cancelled");
  });
});

describe("parseStatusParam", () => {
  it("accepts a known status in any case", () => {
    assert.strictEqual(parseStatusParam("failed"), "FAILED");
    assert.strictEqual(parseStatusParam("CANCELLED"), "CANCELLED");
  });

  it("falls back to all for unknown, empty, or missing values", () => {
    assert.strictEqual(parseStatusParam("garbage"), "all");
    assert.strictEqual(parseStatusParam(""), "all");
    assert.strictEqual(parseStatusParam(null), "all");
    assert.strictEqual(parseStatusParam(undefined), "all");
  });
});

describe("parseSortParam", () => {
  it("accepts the known sort options", () => {
    assert.strictEqual(parseSortParam("oldest"), "oldest");
    assert.strictEqual(parseSortParam("status"), "status");
  });

  it("falls back to newest for anything else", () => {
    assert.strictEqual(parseSortParam("sideways"), "newest");
    assert.strictEqual(parseSortParam(null), "newest");
  });
});
