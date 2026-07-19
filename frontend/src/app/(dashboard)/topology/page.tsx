"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ReactFlow, Background, Controls, useNodesState, useEdgesState, type Node, type Edge, MarkerType } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { X } from "lucide-react";
import { client, type Fleet, type Daemon, type JobSummary } from "@/lib/api";

const nodeBase = {
  padding: "10px 14px",
  borderRadius: 12,
  fontSize: 12,
  color: "#fff",
  border: "1px solid rgba(255,255,255,0.12)",
  background: "rgba(20,20,25,0.9)",
  width: 170,
  textAlign: "center" as const,
};

// Detail shown in the side panel when a node is clicked.
interface NodeDetail {
  kind: string;
  title: string;
  lines: [string, string][];
}

export default function TopologyPage() {
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [daemons, setDaemons] = useState<Daemon[]>([]);
  const [jobs, setJobs] = useState<JobSummary[]>([]);
  const [selected, setSelected] = useState<NodeDetail | null>(null);

  const load = () => {
    client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});
    client.listDaemons().then(setDaemons).catch(() => {});
    client.listJobs().then(r => setJobs(r.jobs.slice(0, 6))).catch(() => {});
  };
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t); }, []);

  // Build the graph + a lookup of per-node detail from live data.
  const { computedNodes, computedEdges, meta } = useMemo(() => {
    const computedNodes: Node[] = [];
    const computedEdges: Edge[] = [];
    const meta: Record<string, NodeDetail> = {};

    computedNodes.push({
      id: "cp", position: { x: 420, y: 0 }, data: { label: "Control Plane" },
      style: { ...nodeBase, background: "rgba(147,198,69,0.15)", border: "1px solid #93C645", width: 190 },
      sourcePosition: "bottom" as never, targetPosition: "top" as never,
    });
    meta["cp"] = { kind: "Control Plane", title: "Control Plane", lines: [["Role", "Plans tasks & hands out work"]] };

    const fleetY = 130;
    (fleets.length ? fleets : [{ id: "default", name: "No fleets", type: "self-managed" } as Fleet]).forEach((f, i) => {
      const id = `fleet-${f.id}`;
      const typeLabel = f.type === "byoc" ? "BYOC" : "Managed";
      computedNodes.push({
        id, position: { x: 120 + i * 230, y: fleetY }, data: { label: `${f.name}\n${typeLabel}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: f.type === "byoc" ? "1px solid #60a5fa" : "1px solid #4ade80" },
      });
      computedEdges.push({ id: `e-cp-${id}`, source: "cp", target: id, animated: true, markerEnd: { type: MarkerType.ArrowClosed } });
      if (f.id !== "default") {
        meta[id] = { kind: "Fleet", title: f.name, lines: [["Type", typeLabel], ["Fleet ID", f.id]] };
      }
    });

    const daemonY = 260;
    const fleetNodeExists = (fid?: string) => !!fid && fleets.some(f => f.id === fid);
    daemons.forEach((d, i) => {
      const id = `daemon-${d.id}`;
      const parent = fleetNodeExists(d.fleet_id) ? `fleet-${d.fleet_id}` : "cp";
      const fleetName = d.fleet_id ? (fleets.find(f => f.id === d.fleet_id)?.name ?? d.fleet_id) : "Unassigned";
      computedNodes.push({
        id, position: { x: 120 + i * 200, y: daemonY }, data: { label: `${d.id.slice(0, 14)}…\n${d.online ? "● online" : "○ offline"}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: d.online ? "1px solid #22c55e" : "1px solid #ef4444", color: d.online ? "#fff" : "#aaa" },
      });
      computedEdges.push({ id: `e-${parent}-${id}`, source: parent, target: id, style: { stroke: d.online ? "#22c55e" : "#555" } });
      meta[id] = {
        kind: "Daemon", title: d.id, lines: [
          ["Status", d.online ? "Online" : "Offline"],
          ["Fleet", fleetName],
          ["Last seen", d.last_seen_at ? new Date(d.last_seen_at).toLocaleString() : "Never"],
          ["Registered", new Date(d.created_at).toLocaleString()],
        ],
      };
    });

    const jobY = 390;
    jobs.forEach((j, i) => {
      const id = `job-${j.job_id}`;
      const color = j.status === "SUCCEEDED" ? "#4ade80" : j.status === "FAILED" ? "#ef4444" : j.status === "RUNNING" ? "#60a5fa" : "#a78bfa";
      computedNodes.push({
        id, position: { x: 120 + i * 190, y: jobY }, data: { label: `${j.job_id.slice(0, 12)}…\n${j.status}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: `1px solid ${color}`, fontSize: 11 },
      });
      computedEdges.push({ id: `e-cp-${id}`, source: "cp", target: id, style: { stroke: "#333", strokeDasharray: "4 4" } });
      meta[id] = {
        kind: "Job", title: j.job_id, lines: [
          ["Status", j.status],
          ["Tasks", String(j.task_count)],
          ["Pull requests", String(j.pr_urls?.length ?? 0)],
          ["Created", new Date(j.created_at).toLocaleString()],
        ],
      };
    });

    return { computedNodes, computedEdges, meta };
  }, [fleets, daemons, jobs]);

  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges] = useEdgesState<Edge>([]);

  // Sync live data into the flow. When the set of nodes is unchanged (just a
  // status refresh from polling) we keep any positions the user has dragged and
  // only update labels/styles; when the structure changes we lay it out fresh.
  const sigRef = useRef("");
  useEffect(() => {
    const sig = computedNodes.map(n => n.id).sort().join("|");
    if (sig !== sigRef.current) {
      sigRef.current = sig;
      setNodes(computedNodes);
    } else {
      setNodes(prev => prev.map(p => {
        const c = computedNodes.find(n => n.id === p.id);
        return c ? { ...p, data: c.data, style: c.style } : p;
      }));
    }
    setEdges(computedEdges);
  }, [computedNodes, computedEdges, setNodes, setEdges]);

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-6">
        <h1 className="text-3xl font-light tracking-tight mb-2">Topology</h1>
        <p className="text-zinc-400">Live view: Control Plane → fleets → daemons, and recent jobs. Drag to rearrange; click any node for details.</p>
      </div>
      <div className="glass-panel border border-white/10 rounded-2xl flex-1 min-h-[520px] overflow-hidden relative">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onNodeClick={(_, node) => setSelected(meta[node.id] ?? null)}
          onPaneClick={() => setSelected(null)}
          fitView
          proOptions={{ hideAttribution: true }}
        >
          <Background color="#333" gap={20} />
          <Controls className="!bg-black/50 !border-white/10" />
        </ReactFlow>

        {selected && (
          <div className="absolute top-4 right-4 z-10 w-72 bg-[#0b0b0d]/95 backdrop-blur border border-white/10 rounded-xl shadow-2xl p-4">
            <div className="flex items-start justify-between mb-3">
              <div>
                <div className="text-[10px] uppercase tracking-widest text-zinc-500">{selected.kind}</div>
                <div className="text-sm font-medium text-white break-all">{selected.title}</div>
              </div>
              <button onClick={() => setSelected(null)} className="text-zinc-500 hover:text-white shrink-0" aria-label="Close">
                <X className="w-4 h-4" />
              </button>
            </div>
            <dl className="flex flex-col gap-2">
              {selected.lines.map(([k, v]) => (
                <div key={k} className="flex items-center justify-between gap-3 text-xs">
                  <dt className="text-zinc-500">{k}</dt>
                  <dd className="text-zinc-200 text-right break-all">{v}</dd>
                </div>
              ))}
            </dl>
          </div>
        )}
      </div>
    </div>
  );
}
