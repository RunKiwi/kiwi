import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { buildThread, selectedNode, threadSummary } from "./thread.ts";
import type { JobTask } from "./api.ts";

function task(
  id: string,
  over: Partial<JobTask> = {},
  minutesAgo = 0,
): JobTask {
  return {
    id,
    status: "SUCCEEDED",
    queued_at: new Date(Date.now() - minutesAgo * 60_000).toISOString(),
    attempts: 0,
    root_task_id: id,
    origin: "submit",
    ...over,
  };
}

describe("buildThread", () => {
  it("returns a single node for a task that was never continued", () => {
    const nodes = buildThread([task("t1", { task: "add a health endpoint" })]);
    assert.equal(nodes.length, 1);
    assert.equal(nodes[0].depth, 0);
    assert.equal(nodes[0].task.id, "t1");
  });

  // Oldest first, so the rail reads the way the work happened.
  it("orders a continued thread by when each run was queued", () => {
    const nodes = buildThread([
      task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "pr_comment" }, 5),
      task("t1", {}, 30),
      task("t3", { parent_task_id: "t2", root_task_id: "t1", origin: "pr_comment" }, 1),
    ]);
    assert.deepEqual(
      nodes.map((n) => n.task.id),
      ["t1", "t2", "t3"],
    );
  });

  // A linear thread stays flat: indenting every continuation would make an
  // ordinary conversation look like a tree of decisions it never was.
  it("keeps a linear thread at depth zero", () => {
    const nodes = buildThread([
      task("t1", {}, 30),
      task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "pr_comment" }, 5),
    ]);
    assert.deepEqual(
      nodes.map((n) => n.depth),
      [0, 0],
    );
  });

  // A fork is a second child of one parent. Nothing creates one yet; the rail
  // must already know how to draw it, or forking becomes a rewrite.
  it("indents a fork under the parent it diverged from", () => {
    const nodes = buildThread([
      task("t1", {}, 30),
      task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "pr_comment" }, 10),
      task("t3", { parent_task_id: "t1", root_task_id: "t1", origin: "fork" }, 5),
    ]);
    const byId = Object.fromEntries(nodes.map((n) => [n.task.id, n]));
    assert.equal(byId.t1.depth, 0);
    assert.equal(byId.t2.depth, 0, "the first child continues the line");
    assert.equal(byId.t3.depth, 1, "a second child diverges from it");
  });

  // A task whose parent is not in the payload must still appear. Losing a node
  // because a row was filtered upstream would hide work that really ran.
  it("keeps an orphan rather than dropping it", () => {
    const nodes = buildThread([
      task("t2", { parent_task_id: "missing", root_task_id: "t1", origin: "pr_comment" }),
    ]);
    assert.equal(nodes.length, 1);
    assert.equal(nodes[0].depth, 0);
  });

  it("handles an empty job", () => {
    assert.deepEqual(buildThread([]), []);
  });
});

describe("selectedNode", () => {
  const nodes = buildThread([
    task("t1", {}, 30),
    task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "pr_comment" }, 5),
  ]);

  it("selects the node named in the URL", () => {
    assert.equal(selectedNode(nodes, "t1")?.task.id, "t1");
  });

  // The page is reachable by any member of the thread, so a link to the root
  // must not silently show the newest run — but a link to nothing at all
  // should land somewhere useful rather than blank.
  it("falls back to the newest run when the id is unknown", () => {
    assert.equal(selectedNode(nodes, "nope")?.task.id, "t2");
    assert.equal(selectedNode(nodes, undefined)?.task.id, "t2");
  });

  it("returns undefined for an empty thread", () => {
    assert.equal(selectedNode([], "t1"), undefined);
  });
});

describe("threadSummary", () => {
  it("counts runs and reports the newest status", () => {
    const s = threadSummary(
      buildThread([
        task("t1", {}, 30),
        task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "pr_comment", status: "LEASED" }, 5),
      ]),
    );
    assert.equal(s.runs, 2);
    assert.equal(s.continued, true);
    assert.equal(s.status, "LEASED");
    assert.equal(s.latestOrigin, "pr_comment");
  });

  it("does not call a single run a thread", () => {
    const s = threadSummary(buildThread([task("t1")]));
    assert.equal(s.runs, 1);
    assert.equal(s.continued, false);
  });

  // Regression: continuations used to be counted by checking
  // origin === "pr_comment" specifically, so a thread whose only follow-up
  // came from a Slack reply (origin "slack") silently reported continued:
  // false and hid the "N Execution Runs" badge on /tasks/[id], even though
  // the "View thread" link that led there only appears for a thread with
  // more than one run.
  it("counts a Slack-originated follow-up as a continuation", () => {
    const s = threadSummary(
      buildThread([
        task("t1", {}, 30),
        task("t2", { parent_task_id: "t1", root_task_id: "t1", origin: "slack" }, 5),
      ]),
    );
    assert.equal(s.runs, 2);
    assert.equal(s.continued, true);
    assert.equal(s.latestOrigin, "slack");
  });

  it("survives an empty thread", () => {
    const s = threadSummary([]);
    assert.equal(s.runs, 0);
    assert.equal(s.continued, false);
  });
});
