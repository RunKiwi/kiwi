import { JobTask } from "@/lib/api";
import { buildJobGraph } from "@/lib/jobGraph";
import { statusOf, isRunningStatus } from "@/lib/statusColors";

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
  const nodeById = new Map(graph.nodes.map((n) => [n.id, n]));

  // The spoken description of the picture. Counts use isRunningStatus because a
  // running worker reports LEASED, not RUNNING — matching on the job-level word
  // would announce "0 running" while work was visibly in flight. Only non-zero
  // categories are listed, so the sentence stays short and never pads itself
  // with states that do not apply.
  const counts: [number, string][] = [
    [tasks.filter((t) => t.status === "SUCCEEDED").length, "succeeded"],
    [tasks.filter((t) => isRunningStatus(t.status)).length, "running"],
    [tasks.filter((t) => t.status === "QUEUED").length, "waiting"],
    [tasks.filter((t) => t.status === "FAILED").length, "failed"],
    [tasks.filter((t) => t.status === "CANCELLED").length, "cancelled"],
  ];
  const breakdown = counts
    .filter(([n]) => n > 0)
    .map(([n, label]) => `${n} ${label}`)
    .join(", ");
  const stageWord = graph.width === 1 ? "stage" : "stages";
  const title = `Plan: ${tasks.length} workers in ${graph.width} ${stageWord}. ${breakdown}.`;

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
          // buildJobGraph drops edges whose endpoints have no task row, so both
          // lookups always resolve; going through the map keeps that a lookup
          // rather than a scan per edge, and removes the non-null assertion.
          const fromNode = nodeById.get(edge.from);
          const toNode = nodeById.get(edge.to);
          if (!fromNode || !toNode) return null;

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
          
          // No outline-none on the anchor: globals.css gives every focusable
          // element a visible focus-visible outline, and suppressing it would
          // leave keyboard focus signalled only by a 1px stroke change.
          return (
            <a href={`#task-${node.id}`} key={node.id} className="group">
              <g transform={`translate(${x}, ${y})`}>
                <rect
                  width={NODE_W}
                  height={NODE_H}
                  rx={6}
                  fill={s.wash}
                  stroke={s.border}
                  strokeWidth={1}
                  className="transition-colors group-hover:stroke-white/40 group-focus-visible:stroke-white/60"
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

                {/* foreignObject so the worker's goal can wrap; SVG <text> does
                    not. The div inherits the XHTML namespace from foreignObject,
                    so it needs no xmlns of its own — and React's DOM typings
                    reject that attribute on a div anyway. */}
                <foreignObject x={8} y={22} width={NODE_W - 16} height={NODE_H - 26}>
                  <div style={{
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
