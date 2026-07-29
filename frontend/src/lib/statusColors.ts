import { Activity, CheckCircle2, XCircle, Loader2, Ban } from "lucide-react";

export interface StatusPresentation {
  label: string;
  Icon: typeof Activity;
  color: string;
  border: string;
  wash: string;
  glow: string;
  spin?: boolean;
}

// Job-level and task-level statuses name the same state differently: a job is
// RUNNING, while the task executing it is LEASED (store.TaskLeased — the lease
// is what makes it run). This map serves both surfaces, so it has to answer to
// both words or a running worker renders as though it were still queued.
const RUNNING: StatusPresentation = {
  label: "Running",
  Icon: Activity,
  color: "#5A9DF5",
  border: "rgba(59,130,246,0.34)",
  wash: "rgba(59,130,246,0.15)",
  glow: "rgba(59,130,246,0.12)",
};

export const STATUS: Record<string, StatusPresentation> = {
  QUEUED: {
    label: "Queued",
    Icon: Loader2,
    color: "#E8A153",
    border: "rgba(232,161,83,0.32)",
    wash: "rgba(232,161,83,0.14)",
    glow: "rgba(232,161,83,0.10)",
    spin: true,
  },
  RUNNING,
  LEASED: RUNNING,
  SUCCEEDED: {
    label: "Succeeded",
    Icon: CheckCircle2,
    color: "#93C645",
    border: "rgba(147,198,69,0.30)",
    wash: "rgba(147,198,69,0.13)",
    glow: "rgba(147,198,69,0.09)",
  },
  FAILED: {
    label: "Failed",
    Icon: XCircle,
    color: "#EF6060",
    border: "rgba(239,68,68,0.30)",
    wash: "rgba(239,68,68,0.14)",
    glow: "rgba(239,68,68,0.09)",
  },
  // Cancelled is deliberately the quietest state on the board: it is not a
  // failure, and it should not compete for attention with one.
  CANCELLED: {
    label: "Cancelled",
    Icon: Ban,
    color: "#A0A0A0",
    border: "rgba(160,160,160,0.30)",
    wash: "rgba(160,160,160,0.10)",
    glow: "rgba(160,160,160,0.06)",
  },
};

// Neutral near-black card base — not navy, which muddies the status tint into
// grey — so a flat whole-card colour wash reads true.
export const CARD_BASE = "#0C0D10";

export const statusOf = (s: string) => STATUS[s] ?? STATUS.QUEUED;

/** True for either spelling of "work is happening right now". */
export const isRunningStatus = (s: string) => s === "RUNNING" || s === "LEASED";
