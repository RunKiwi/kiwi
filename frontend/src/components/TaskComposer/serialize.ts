import type { PlanRequest } from "@/lib/api";
import type { JSONContent } from "@tiptap/react";

export function serializeTask(doc: JSONContent): Partial<PlanRequest> {
  const result: Partial<PlanRequest> = {
    task: "",
  };

  if (!doc.content) return result;

  // To handle paragraph spacing properly, we'll collect paragraphs
  const paragraphs: string[] = [];

  const walk = (node: JSONContent): string => {
    let text = "";
    if (node.type === "text" && node.text) {
      text += node.text;
    } else if (node.type === "refMention") {
      const attrs = node.attrs || {};
      const val = attrs.value || attrs.label;
      if (attrs.kind === "file") {
        result.files = result.files || [];
        result.files.push(val);
        text += `@${val}`;
      } else if (attrs.kind === "branch") {
        result.ref = val;
        text += `@${val}`;
      } else if (attrs.kind === "symbol") {
        text += `@${val}`;
      } else {
        text += `@${val}`;
      }
    } else if (node.type === "workMention") {
      const attrs = node.attrs || {};
      const val = attrs.value || attrs.label;
      if (attrs.kind === "task") {
        // Only "Reference" mode actually sets the field in v1
        if (attrs.mode === "Reference") {
          result.reference_job_ids = result.reference_job_ids || [];
          result.reference_job_ids.push(val);
          result.reference_mode = "manual";
        }
        text += `#${attrs.label || val}`;
      } else if (attrs.kind === "issue") {
        text += `#${attrs.label || val}`;
      } else {
        text += `#${attrs.label || val}`;
      }
    } else if (node.type === "actionMention") {
      // Actions are directives, not references — they're captured as structured
      // fields, so we deliberately don't echo them into the task prose the planner reads.
      const attrs = node.attrs || {};
      const val = attrs.value || attrs.label;
      if (attrs.kind === "test") {
        result.test_cmd = val;
      } else if (attrs.kind === "model") {
        result.model = val;
      } else if (attrs.kind === "workers") {
        result.max_workers = parseInt(val, 10);
      }
      // post/notify are no-ops in v1 (backend TODO).
    } else if (node.content) {
      text += node.content.map(walk).join("");
    }
    return text;
  };

  for (const node of doc.content) {
    if (node.type === "paragraph") {
      paragraphs.push(node.content ? node.content.map(walk).join("") : "");
    } else {
      paragraphs.push(walk(node));
    }
  }

  // Collapse the runs of whitespace left where action markers were removed.
  result.task = paragraphs
    .map((p) => p.replace(/[ \t]{2,}/g, " ").trim())
    .join("\n\n")
    .trim();
  return result;
}
