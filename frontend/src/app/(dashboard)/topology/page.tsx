"use client";

import { useMemo } from 'react';
import {
  ReactFlow,
  MiniMap,
  Controls,
  Background,
  useNodesState,
  useEdgesState,
  Position
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { useFleetStore } from '@/store/useFleetStore';

export default function TopologyPage() {
  const { tasks } = useFleetStore();

  // Create mock nodes based on the tasks in the store to form a DAG
  const initialNodes = useMemo(() => {
    return [
      {
        id: 'root',
        position: { x: 400, y: 50 },
        data: { label: 'Feature: Authentication' },
        style: { background: 'rgba(255,255,255,0.05)', color: 'white', border: '1px solid rgba(255,255,255,0.1)', borderRadius: '8px', padding: '10px 20px' }
      },
      ...tasks.slice(0, 5).map((t, i) => ({
        id: t.id,
        position: { x: 150 + (i * 150), y: 150 + (i % 2 === 0 ? 0 : 50) },
        data: { 
          label: (
            <div className="flex flex-col gap-1">
              <span className="text-[10px] uppercase text-zinc-400">{t.phase}</span>
              <span className="text-xs">{t.title}</span>
            </div>
          ) 
        },
        style: { 
          background: t.phase === 'executing' ? 'rgba(59,130,246,0.1)' : t.phase === 'completed' ? 'rgba(34,197,94,0.1)' : 'rgba(255,255,255,0.05)',
          color: 'white', 
          border: `1px solid ${t.phase === 'executing' ? 'rgba(59,130,246,0.3)' : t.phase === 'completed' ? 'rgba(34,197,94,0.3)' : 'rgba(255,255,255,0.1)'}`,
          borderRadius: '8px',
          width: 130
        },
        sourcePosition: Position.Bottom,
        targetPosition: Position.Top
      }))
    ];
  }, [tasks]);

  const initialEdges = useMemo(() => {
    return tasks.slice(0, 5).map((t) => ({
      id: `e-root-${t.id}`,
      source: 'root',
      target: t.id,
      animated: t.phase === 'executing',
      style: { stroke: t.phase === 'executing' ? '#60a5fa' : 'rgba(255,255,255,0.2)' }
    }));
  }, [tasks]);

  const [nodes, , onNodesChange] = useNodesState(initialNodes);
  const [edges, , onEdgesChange] = useEdgesState(initialEdges);

  return (
    <div className="h-full flex flex-col relative">
      <div className="absolute top-8 left-8 z-10 pointer-events-none">
        <h1 className="text-3xl font-light tracking-tight text-white mb-2">Topology Map</h1>
        <p className="text-zinc-400">Live DAG of the Swarm&apos;s task execution strategy.</p>
      </div>

      <div className="flex-1 w-full h-full">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          fitView
          className="bg-[#09090b]"
          colorMode="dark"
        >
          <Controls className="bg-black/40 border border-white/10 fill-white" />
          <MiniMap 
            nodeColor={(n) => {
              if (n.style?.background?.toString().includes('59,130,246')) return '#3b82f6';
              if (n.style?.background?.toString().includes('34,197,94')) return '#22c55e';
              return '#3f3f46';
            }}
            maskColor="rgba(0, 0, 0, 0.7)"
            className="bg-black/60 border border-white/10" 
          />
          <Background gap={12} size={1} color="rgba(255,255,255,0.05)" />
        </ReactFlow>
      </div>
    </div>
  );
}
