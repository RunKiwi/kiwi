import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { buildJobGraph } from "./jobGraph.ts";
import type { JobTask } from "./api.ts";

function mockTask(id: string, depends_on: string[] = [], status = "QUEUED"): JobTask {
  return {
    id,
    status,
    queued_at: new Date().toISOString(),
    attempts: 0,
    depends_on,
  };
}

describe("buildJobGraph", () => {
  it("handles empty tasks", () => {
    const graph = buildJobGraph("job1", []);
    assert.deepEqual(graph.nodes, []);
    assert.deepEqual(graph.edges, []);
    assert.equal(graph.width, 0);
    assert.equal(graph.height, 0);
  });

  it("handles a single task", () => {
    const tasks = [mockTask("job1-worker1")];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.length, 1);
    assert.equal(graph.nodes[0].depth, 0);
    assert.equal(graph.nodes[0].row, 0);
    assert.equal(graph.edges.length, 0);
    assert.equal(graph.width, 1);
    assert.equal(graph.height, 1);
  });

  it("handles a pure chain", () => {
    const tasks = [
      mockTask("job1-A"),
      mockTask("job1-B", ["A"]),
      mockTask("job1-C", ["B"]),
    ];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.find(n => n.workerId === "A")?.depth, 0);
    assert.equal(graph.nodes.find(n => n.workerId === "B")?.depth, 1);
    assert.equal(graph.nodes.find(n => n.workerId === "C")?.depth, 2);
    assert.equal(graph.edges.length, 2);
    assert.equal(graph.width, 3);
  });

  it("handles a pure fan-out", () => {
    const tasks = [
      mockTask("job1-A"),
      mockTask("job1-B", ["A"]),
      mockTask("job1-C", ["A"]),
    ];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.find(n => n.workerId === "A")?.depth, 0);
    assert.equal(graph.nodes.find(n => n.workerId === "B")?.depth, 1);
    assert.equal(graph.nodes.find(n => n.workerId === "C")?.depth, 1);
    assert.equal(graph.edges.length, 2);
    assert.equal(graph.width, 2);
    assert.equal(graph.height, 2);
  });

  it("handles a diamond (longest path)", () => {
    const tasks = [
      mockTask("job1-A"),
      mockTask("job1-B", ["A"]),
      mockTask("job1-C", ["A"]),
      mockTask("job1-D", ["A", "B", "C"]),
    ];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.find(n => n.workerId === "A")?.depth, 0);
    assert.equal(graph.nodes.find(n => n.workerId === "B")?.depth, 1);
    assert.equal(graph.nodes.find(n => n.workerId === "C")?.depth, 1);
    assert.equal(graph.nodes.find(n => n.workerId === "D")?.depth, 2);
  });

  it("handles dangling depends_on", () => {
    const tasks = [
      mockTask("job1-A", ["missing"]),
    ];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.find(n => n.workerId === "A")?.depth, 0);
    assert.equal(graph.edges.length, 0);
  });

  it("terminates and falls back to flat row on cycles", () => {
    const tasks = [
      mockTask("job1-A", ["C"]),
      mockTask("job1-B", ["A"]),
      mockTask("job1-C", ["B"]),
    ];
    const graph = buildJobGraph("job1", tasks);
    assert.equal(graph.nodes.every(n => n.depth === 0), true);
    assert.equal(graph.width, 1);
    assert.equal(graph.height, 3);
  });

  it("produces stable ordering with shuffled input", () => {
    const tasks1 = [
      mockTask("job1-Z", ["A"]),
      mockTask("job1-M", ["A"]),
      mockTask("job1-A"),
    ];
    const tasks2 = [
      mockTask("job1-A"),
      mockTask("job1-M", ["A"]),
      mockTask("job1-Z", ["A"]),
    ];
    const graph1 = buildJobGraph("job1", tasks1);
    const graph2 = buildJobGraph("job1", tasks2);
    
    assert.equal(graph1.nodes[1].workerId, "M");
    assert.equal(graph1.nodes[2].workerId, "Z");

    assert.deepEqual(graph1.nodes.map(n => n.workerId), graph2.nodes.map(n => n.workerId));
    assert.deepEqual(graph1.edges, graph2.edges);
  });
});
