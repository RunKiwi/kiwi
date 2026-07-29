import type { JobTask } from "./api.ts";

export interface GraphNode {
  id: string;
  workerId: string;
  depth: number;
  row: number;
  task: JobTask;
}

export interface GraphEdge {
  from: string;
  to: string;
  satisfied: boolean;
}

export interface JobGraph {
  nodes: GraphNode[];
  edges: GraphEdge[];
  width: number;
  height: number;
}

export function buildJobGraph(jobId: string, tasks: JobTask[]): JobGraph {
  const prefix = `${jobId}-`;
  
  // 1. Strip <job_id>- prefix to get worker ids
  const nodesByWorkerId = new Map<string, GraphNode>();
  tasks.forEach((t) => {
    let workerId = t.id;
    if (workerId.startsWith(prefix)) {
      workerId = workerId.slice(prefix.length);
    }
    nodesByWorkerId.set(workerId, {
      id: t.id,
      workerId,
      depth: 0,
      row: 0,
      task: t,
    });
  });

  // 2. Resolve depths via longest-path
  // Bound the relaxation at tasks.length passes to avoid hang on cycle
  let changed = true;
  let pass = 0;
  const maxPasses = tasks.length;

  while (changed && pass < maxPasses) {
    changed = false;
    pass++;

    for (const node of nodesByWorkerId.values()) {
      const deps = node.task.depends_on || [];
      let maxDepDepth = -1;
      
      for (const depId of deps) {
        const depNode = nodesByWorkerId.get(depId);
        if (depNode) {
          if (depNode.depth > maxDepDepth) {
            maxDepDepth = depNode.depth;
          }
        }
      }
      
      const newDepth = maxDepDepth === -1 ? 0 : 1 + maxDepDepth;
      if (newDepth > node.depth) {
        node.depth = newDepth;
        changed = true;
      }
    }
  }

  // If there's a cycle, it will still be changing at maxPasses.
  // Malformed input must degrade to a flat row.
  if (changed) {
    for (const node of nodesByWorkerId.values()) {
      node.depth = 0;
    }
  }

  // 3. Stable ordering
  // Within a depth, sort by worker id
  const nodes = Array.from(nodesByWorkerId.values());
  nodes.sort((a, b) => {
    if (a.depth !== b.depth) return a.depth - b.depth;
    return a.workerId.localeCompare(b.workerId);
  });

  // 4. Edges & dangling edges
  const edges: GraphEdge[] = [];
  for (const node of nodes) {
    const deps = node.task.depends_on || [];
    // Sort dependencies to ensure stable edge order for a single node's incoming edges
    const sortedDeps = [...deps].sort((a, b) => a.localeCompare(b));
    for (const depId of sortedDeps) {
      const depNode = nodesByWorkerId.get(depId);
      if (depNode) {
        // Drop dangling edges (only add if depNode exists)
        edges.push({
          from: depNode.id,
          to: node.id,
          satisfied: depNode.task.status === "SUCCEEDED",
        });
      }
    }
  }

  // Assign rows within each depth column
  const depthCounts = new Map<number, number>();
  nodes.forEach(node => {
    const count = depthCounts.get(node.depth) || 0;
    node.row = count;
    depthCounts.set(node.depth, count + 1);
  });

  let maxDepth = -1;
  let maxRow = -1;
  for (const node of nodes) {
    if (node.depth > maxDepth) maxDepth = node.depth;
    if (node.row > maxRow) maxRow = node.row;
  }

  return {
    nodes,
    edges,
    width: nodes.length > 0 ? maxDepth + 1 : 0,
    height: nodes.length > 0 ? maxRow + 1 : 0,
  };
}
