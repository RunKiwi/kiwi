import { Activity, CheckCircle2, XCircle, Loader2, Ban } from "lucide-react";

export const STATUS: Record<
  string,
  { label: string; Icon: typeof Activity; color: string; border: string; wash: string; glow: string; spin?: boolean }
> = {
  QUEUED: {
    label: "Queued",
    Icon: Loader2,
    color: "#E8A153",
    border: "rgba(232,161,83,0.32)",
    wash: "rgba(232,161,83,0.14)",
    glow: "rgba(232,161,83,0.10)",
    spin: true,
  },
  RUNNING: {
    label: "Running",
    Icon: Activity,
    color: "#5A9DF5",
    border: "rgba(59,130,246,0.34)",
    wash: "rgba(59,130,246,0.15)",
    glow: "rgba(59,130,246,0.12)",
  },
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
  CANCELLED: {
    label: "Cancelled",
    Icon: Ban,
    color: "#A0A0A0",
    border: "rgba(160,160,160,0.30)",
    wash: "rgba(160,160,160,0.10)",
    glow: "rgba(160,160,160,0.06)",
  },
};

export const CARD_BASE = "#0C0D10";

export const statusOf = (s: string) => STATUS[s] ?? STATUS.QUEUED;
