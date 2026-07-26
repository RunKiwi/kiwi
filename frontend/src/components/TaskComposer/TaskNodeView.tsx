import { NodeViewWrapper, NodeViewProps } from '@tiptap/react';
import { useState, useRef, useEffect } from 'react';

export const TaskNodeView = ({ node, updateAttributes }: NodeViewProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const popoverRef = useRef<HTMLDivElement>(null);
  
  const { kind, value, label, status, mode } = node.attrs;
  
  const modes = ["Reference", "Continue", "After", "Avoid"];
  
  // Close popover when clicking outside
  useEffect(() => {
    if (!isOpen) return;
    const handleClick = (e: MouseEvent) => {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setIsOpen(false);
      }
    };
    document.addEventListener('mousedown', handleClick);
    return () => document.removeEventListener('mousedown', handleClick);
  }, [isOpen]);

  const isTask = kind === "task";

  // Chip base style (amber)
  const chipStyle = {
    display: "inline-flex",
    alignItems: "center",
    gap: "4px",
    padding: "0px 6px",
    borderRadius: "9999px",
    background: "rgba(232,161,83,0.1)",
    border: "1px solid rgba(232,161,83,0.3)",
    color: "#E8A153",
    fontSize: "0.85em",
    fontFamily: "monospace",
    userSelect: "none" as const,
    margin: "0 2px",
    cursor: isTask ? "pointer" : "default",
  };

  const statusColor = status === "SUCCEEDED" ? "#93C645" : 
                      status === "FAILED" ? "#EF6060" : 
                      status === "RUNNING" ? "#5A9DF5" : 
                      "#E8A153"; // default QUEUED/unknown

  return (
    <NodeViewWrapper style={{ display: 'inline-block', position: 'relative' }}>
      <span 
        style={chipStyle} 
        onClick={() => { if (isTask) setIsOpen(!isOpen); }}
      >
        {isTask && (
          <span style={{
            width: "6px", height: "6px", borderRadius: "50%", background: statusColor, display: "inline-block"
          }} />
        )}
        #{label || value}
        {isTask && mode && mode !== "Reference" && (
          <span style={{ fontSize: "0.8em", opacity: 0.8, marginLeft: "2px" }}>({mode})</span>
        )}
      </span>

      {isOpen && isTask && (
        <div 
          ref={popoverRef}
          contentEditable={false}
          className="pr-popover absolute top-full left-0 mt-1 z-50 w-32 rounded-xl border border-white/10 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-16px_rgba(0,0,0,0.85)] p-1.5"
        >
          <div className="flex flex-col gap-0.5">
            {modes.map(m => (
              <button
                key={m}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  updateAttributes({ mode: m });
                  setIsOpen(false);
                }}
                className={`flex items-center px-2.5 py-1.5 rounded-lg text-xs text-left transition-colors ${m === mode ? "bg-white/[0.07] text-white" : "text-zinc-400 hover:bg-white/[0.04] hover:text-zinc-200"}`}
              >
                {m}
              </button>
            ))}
          </div>
        </div>
      )}
    </NodeViewWrapper>
  );
};
