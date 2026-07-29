import React from "react";
import { JobTask } from "@/lib/api";
import { buildJobGraph } from "@/lib/jobGraph";
import { STATUS, statusOf } from "@/lib/statusColors";

interface JobGraphProps {
  jobId: string;
  tasks: JobTask[];
}

const NODE_W = 150;
const NODE_H = 56;
const H_GUTTER = 28;
const V_GUTTER = 16;

export function JobGraph({ jobId, tasks }: JobGraphProps) {
  const graph = buildJobGraph(jobId, tasks);

  if (tasks.length < 2 || graph.edges.length === 0) {
    return null;
  }

  const naturalWidth = graph.width * (NODE_W + H_GUTTER) - H_GUTTER;
  const naturalHeight = graph.height * (NODE_H + V_GUTTER) - V_GUTTER;

  const succeededCount = tasks.filter((t) => t.status === "SUCCEEDED").length;
  const runningCount = tasks.filter((t) => t.status === "RUNNING").length;
  const waitingCount = tasks.filter((t) => t.status === "QUEUED").length;
  const title = `Plan: ${tasks.length} workers, ${graph.width} stages. ${succeededCount} succeeded, ${runningCount} running, ${waitingCount} waiting.`;

  return (
    <div className="w-full overflow-x-auto overflow-y-hidden mb-6 pb-2 pr-2">
      <svg
        role="img"
        aria-label={title}
        width={naturalWidth}
        height={naturalHeight}
        viewBox={`0 0 ${naturalWidth} ${naturalHeight}`}
        preserveAspectRatio="xMinYMin meet"
        style={{ display: "block" }}
      >
        <title>{title}</title>
        
        {/* Draw edges */}
        {graph.edges.map((edge, i) => {
          const fromNode = graph.nodes.find((n) => n.id === edge.from)!;
          const toNode = graph.nodes.find((n) => n.id === edge.to)!;
          
          const x1 = fromNode.depth * (NODE_W + H_GUTTER) + NODE_W;
          const y1 = fromNode.row * (NODE_H + V_GUTTER) + NODE_H / 2;
          
          const x2 = toNode.depth * (NODE_W + H_GUTTER);
          const y2 = toNode.row * (NODE_H + V_GUTTER) + NODE_H / 2;
          
          const midX = x1 + H_GUTTER / 2;

          const path = `M ${x1} ${y1} H ${midX} V ${y2} H ${x2}`;
          const color = edge.satisfied ? "rgba(147,198,69,0.3)" : "rgba(255,255,255,0.15)";
          const strokeWidth = edge.satisfied ? 2 : 1;

          return (
            <path
              key={`edge-${i}`}
              d={path}
              fill="none"
              stroke={color}
              strokeWidth={strokeWidth}
            />
          );
        })}

        {/* Draw nodes */}
        {graph.nodes.map((node) => {
          const x = node.depth * (NODE_W + H_GUTTER);
          const y = node.row * (NODE_H + V_GUTTER);
          const s = statusOf(node.task.status);

          // Truncate task description
          const taskDesc = node.task.task || "";
          
          return (
            <a href={`#task-${node.id}`} key={node.id} className="group outline-none">
              <g transform={`translate(${x}, ${y})`}>
                <rect
                  width={NODE_W}
                  height={NODE_H}
                  rx={6}
                  fill={s.wash}
                  stroke={s.border}
                  strokeWidth={1}
                  className="transition-colors group-hover:stroke-white/40 group-focus:stroke-white/60"
                />
                
                {/* Eyebrow: worker id and attempts */}
                <text
                  x={8}
                  y={16}
                  fill={s.color}
                  fontSize={10}
                  fontFamily="monospace"
                  fontWeight="bold"
                >
                  {node.workerId}
                </text>
                
                {node.task.attempts > 1 && (
                  <text
                    x={NODE_W - 8}
                    y={16}
                    fill={s.color}
                    fontSize={9}
                    fontFamily="monospace"
                    textAnchor="end"
                  >
                    try {node.task.attempts}
                  </text>
                )}

                {/* Task description (using foreignObject for text wrapping, highly compatible) */}
                <foreignObject x={8} y={22} width={NODE_W - 16} height={NODE_H - 26}>
                  <div xmlns="http://www.w3.org/1999/xhtml" style={{ 
                    fontSize: '11px', 
                    color: 'rgba(255,255,255,0.8)',
                    lineHeight: '1.2',
                    overflow: 'hidden',
                    display: '-webkit-box',
                    WebkitLineClamp: 2,
                    WebkitBoxOrient: 'vertical',
                    wordBreak: 'break-word',
                  }}>
                    {taskDesc}
                  </div>
                </foreignObject>
              </g>
            </a>
          );
        })}
      </svg>
    </div>
  );
}
