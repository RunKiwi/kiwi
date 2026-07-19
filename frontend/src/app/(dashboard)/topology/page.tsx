"use client";

import { useEffect, useMemo, useState } from "react";
import { ReactFlow, Background, Controls, type Node, type Edge, MarkerType } from "@xyflow/react";
import "@xyflow/react/dist/style.css";
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

export default function TopologyPage() {
  const [fleets, setFleets] = useState<Fleet[]>([]);
  const [daemons, setDaemons] = useState<Daemon[]>([]);
  const [jobs, setJobs] = useState<JobSummary[]>([]);

  const load = () => {
    client.listFleets().then(r => setFleets(r.fleets)).catch(() => {});
    client.listDaemons().then(setDaemons).catch(() => {});
    client.listJobs().then(r => setJobs(r.jobs.slice(0, 6))).catch(() => {});
  };
  useEffect(() => { load(); const t = setInterval(load, 5000); return () => clearInterval(t); }, []);

  const { nodes, edges } = useMemo(() => {
    const nodes: Node[] = [];
    const edges: Edge[] = [];

    // Control plane (root)
    nodes.push({
      id: "cp", position: { x: 420, y: 0 }, data: { label: "Control Plane" },
      style: { ...nodeBase, background: "rgba(147,198,69,0.15)", border: "1px solid #93C645", width: 190 },
      sourcePosition: "bottom" as never, targetPosition: "top" as never,
    });

    // Fleets row
    const fleetY = 130;
    (fleets.length ? fleets : [{ id: "default", name: "No fleets", type: "self-managed" } as Fleet]).forEach((f, i) => {
      const id = `fleet-${f.id}`;
      nodes.push({
        id, position: { x: 120 + i * 230, y: fleetY }, data: { label: `${f.name}\n${f.type === "byoc" ? "BYOC" : "Self-managed"}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: f.type === "byoc" ? "1px solid #60a5fa" : "1px solid #4ade80" },
      });
      edges.push({ id: `e-cp-${id}`, source: "cp", target: id, animated: true, markerEnd: { type: MarkerType.ArrowClosed } });
    });

    // Daemons row. A daemon hangs off its fleet (that's what it leases work
    // from); an unassigned daemon hangs off the Control Plane directly.
    const daemonY = 260;
    const fleetNodeExists = (fid?: string) => !!fid && fleets.some(f => f.id === fid);
    daemons.forEach((d, i) => {
      const id = `daemon-${d.id}`;
      const parent = fleetNodeExists(d.fleet_id) ? `fleet-${d.fleet_id}` : "cp";
      nodes.push({
        id, position: { x: 120 + i * 200, y: daemonY }, data: { label: `${d.id.slice(0, 14)}…\n${d.online ? "● online" : "○ offline"}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: d.online ? "1px solid #22c55e" : "1px solid #ef4444", color: d.online ? "#fff" : "#aaa" },
      });
      edges.push({ id: `e-${parent}-${id}`, source: parent, target: id, style: { stroke: d.online ? "#22c55e" : "#555" } });
    });

    // Recent jobs
    const jobY = 390;
    jobs.forEach((j, i) => {
      const id = `job-${j.job_id}`;
      const color = j.status === "SUCCEEDED" ? "#4ade80" : j.status === "FAILED" ? "#ef4444" : j.status === "RUNNING" ? "#60a5fa" : "#a78bfa";
      nodes.push({
        id, position: { x: 120 + i * 190, y: jobY }, data: { label: `${j.job_id.slice(0, 12)}…\n${j.status}` },
        style: { ...nodeBase, whiteSpace: "pre-line", border: `1px solid ${color}`, fontSize: 11 },
      });
      edges.push({ id: `e-cp-${id}`, source: "cp", target: id, style: { stroke: "#333", strokeDasharray: "4 4" } });
    });

    return { nodes, edges };
  }, [fleets, daemons, jobs]);

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <div className="mb-6">
        <h1 className="text-3xl font-light tracking-tight mb-2">Topology</h1>
        <p className="text-zinc-400">Live view: Control Plane → fleets → daemons, and recent jobs.</p>
      </div>
      <div className="glass-panel border border-white/10 rounded-2xl flex-1 min-h-[520px] overflow-hidden">
        <ReactFlow nodes={nodes} edges={edges} fitView proOptions={{ hideAttribution: true }}>
          <Background color="#333" gap={20} />
          <Controls className="!bg-black/50 !border-white/10" />
        </ReactFlow>
      </div>
    </div>
  );
}
