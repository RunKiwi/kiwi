import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { deriveTitle, jobTitle } from "./taskTitle.ts";
import type { TitleSource } from "./taskTitle.ts";

// The task that prompted this: a full issue report pasted as the task, which
// rendered verbatim into an <h2> and pushed the run itself below the fold.
const LONG_TASK =
  "ClickHouse creates excessive one-row parts for singleton event deliveries. " +
  "## Problem Most `BulkInsertEvents` deliveries contain one event but still use " +
  "the batch API, producing one-row ClickHouse parts. In production we observed " +
  "roughly 99,000 one-row parts within ten minutes and CPU throttling during about " +
  "70% of scheduling periods. ## Proposed fix Route singleton bulk deliveries " +
  "through `AsyncInsert` with `wait=true`. Implementation: #2270";

describe("deriveTitle", () => {
  it("takes the sentence a report leads with, dropping the body", () => {
    assert.equal(
      deriveTitle(LONG_TASK),
      "ClickHouse creates excessive one-row parts for singleton event deliveries",
    );
  });

  it("leaves a task that is already short alone", () => {
    assert.equal(deriveTitle("Fix the divide by zero"), "Fix the divide by zero");
  });

  it("uses a leading markdown heading when there is one", () => {
    assert.equal(
      deriveTitle("# Retry flaky uploads\n\nSome background about why."),
      "Retry flaky uploads",
    );
  });

  it("stops at the first line break", () => {
    assert.equal(deriveTitle("Add a health endpoint\nIt should return 200."), "Add a health endpoint");
  });

  // A sentence split must not fire on an abbreviation or a version number, or
  // the title truncates mid-thought — worse than not splitting at all.
  it("does not split on a decimal or an abbreviation", () => {
    assert.equal(deriveTitle("Bump Go to 1.25 across the build"), "Bump Go to 1.25 across the build");
    assert.equal(deriveTitle("Handle e.g. empty payloads"), "Handle e.g. empty payloads");
  });

  it("truncates a long unbroken sentence rather than returning all of it", () => {
    const long = "a".repeat(200);
    const got = deriveTitle(long);
    assert.ok(got.length <= 101, `got ${got.length} chars`);
    assert.ok(got.endsWith("…"));
  });

  it("returns empty for empty input, so callers can fall back", () => {
    assert.equal(deriveTitle(""), "");
    assert.equal(deriveTitle("   \n  "), "");
  });
});

describe("jobTitle", () => {
  const planned = (detail: string): TitleSource[] => [
    { phase: "test", outcome: "fail", detail: "go test ./..." },
    { phase: "critic", outcome: "proposed", detail },
  ];

  it("prefers the Architect's objective once round 0 has produced one", () => {
    const got = jobTitle(LONG_TASK, planned("Route singleton deliveries through AsyncInsert"));
    assert.equal(got.title, "Route singleton deliveries through AsyncInsert");
    assert.equal(got.fromArchitect, true);
  });

  it("falls back to the task's own first sentence before planning finishes", () => {
    const got = jobTitle(LONG_TASK, [{ phase: "test", outcome: "fail", detail: "go test ./..." }]);
    assert.equal(got.title, "ClickHouse creates excessive one-row parts for singleton event deliveries");
    assert.equal(got.fromArchitect, false);
  });

  // Review verdicts share the "critic" phase with the plan — sessionPhase maps
  // both onto it. Only the plan carries "proposed", and taking a review's
  // rationale instead would retitle the job every round.
  it("ignores review events, which share the critic phase with the plan", () => {
    const events: TitleSource[] = [
      { phase: "critic", outcome: "proposed", detail: "The real objective" },
      { phase: "critic", outcome: "revise", detail: "This round missed the error path" },
    ];
    assert.equal(jobTitle(LONG_TASK, events).title, "The real objective");
  });

  it("ignores an empty objective rather than showing a blank header", () => {
    const got = jobTitle(LONG_TASK, planned("   "));
    assert.equal(got.fromArchitect, false);
    assert.ok(got.title.startsWith("ClickHouse creates"));
  });

  it("has something to show even when the task is empty", () => {
    assert.equal(jobTitle("", []).title, "Job Details");
  });

  it("reports whether the original differs, so the toggle can be hidden", () => {
    assert.equal(jobTitle("Fix the divide by zero", []).truncated, false);
    assert.equal(jobTitle(LONG_TASK, []).truncated, true);
  });
});
