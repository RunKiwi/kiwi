import { ReactRenderer } from "@tiptap/react";
import { SuggestList, SuggestionItem } from "./SuggestList";
import { computePosition, flip, shift, offset } from "@floating-ui/dom";
import type { Editor } from "@tiptap/core";
import type { JobSummary, GithubRepo } from "@/lib/api";

export interface SuggestionContext {
  repos: GithubRepo[];
  jobs: JobSummary[];
  models: string[];
  repoSelected: boolean;
}

export function createSuggestion(
  trigger: string,
  getContext: () => SuggestionContext
) {
  return {
    char: trigger,
    items: ({ query }: { query: string }): SuggestionItem[] => {
      const q = query.toLowerCase();

      if (trigger === "@") {
        if (!getContext().repoSelected) {
          return [{ kind: "error", label: "Select a repo first", value: "", disabled: true }];
        }
        // Since we don't have real github repo tree API hooked up here,
        // we'll just allow free-form via a static option if query is typed,
        // or some placeholders.
        if (!q) {
          return [
            { kind: "file", label: "Type a file path...", value: "", disabled: true },
            { kind: "branch", label: "Type a branch name...", value: "", disabled: true },
            { kind: "symbol", label: "Type a symbol name...", value: "", disabled: true },
          ];
        }
        return [
          { kind: "file", label: `${query}`, sublabel: "File", value: query },
          { kind: "branch", label: `${query}`, sublabel: "Branch", value: query },
          { kind: "symbol", label: `${query}`, sublabel: "Symbol", value: query },
        ];
      }

      if (trigger === "#") {
        // filter jobs
        const jobs = getContext().jobs
          .filter(j => j.task?.toLowerCase().includes(q) || j.job_id.toLowerCase().includes(q))
          .slice(0, 5)
          .map(j => ({
            kind: "task",
            label: j.job_id.substring(0, 8),
            sublabel: j.task || "No description",
            value: j.job_id,
            status: j.status,
          }));

        const issues = query
          ? [{ kind: "issue", label: `${query}`, sublabel: "Issue", value: query }]
          : [{ kind: "issue", label: "Type an issue number...", value: "", disabled: true }];

        return [...jobs, ...issues];
      }

      if (trigger === "/") {
        const actions: SuggestionItem[] = [
          { kind: "test", label: "go test ./...", value: "go test ./..." },
          { kind: "test", label: "go test ./pkg/...", value: "go test ./pkg/..." },
          { kind: "test", label: "npm test", value: "npm test" },
          { kind: "test", label: "pytest -q", value: "pytest -q" },
          ...getContext().models.map(m => ({ kind: "model", label: m, value: m, sublabel: "Model" })),
          { kind: "workers", label: "1 worker", value: "1" },
          { kind: "workers", label: "2 workers", value: "2" },
          { kind: "workers", label: "4 workers", value: "4" },
          { kind: "post", label: "post to Slack", value: "post" },
          { kind: "notify", label: "notify reviewers", value: "notify" },
        ];

        let filtered = actions.filter(a => a.label.toLowerCase().includes(q) || a.value.toLowerCase().includes(q));
        if (filtered.length === 0 && q) {
          // freeform test command fallback
          filtered = [{ kind: "test", label: query, sublabel: "Custom test command", value: query }];
        }
        return filtered.slice(0, 10);
      }

      return [];
    },

    render: () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let reactRenderer: any;
      let popup: HTMLElement | null = null;

      return {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        onStart: (props: any) => {
          if (!props.clientRect) {
            return;
          }
          
          reactRenderer = new ReactRenderer(SuggestList, {
            props,
            editor: props.editor as Editor,
          });

          popup = document.createElement("div");
          popup.style.position = "absolute";
          popup.style.zIndex = "50";
          document.body.appendChild(popup);
          
          if (reactRenderer.element) {
            popup.appendChild(reactRenderer.element);
          }
          
          const rect = props.clientRect();
          if (rect) {
            computePosition(
              { getBoundingClientRect: () => rect },
              popup,
              {
                placement: "bottom-start",
                middleware: [offset(4), flip(), shift({ padding: 8 })],
              }
            ).then(({ x, y }) => {
              Object.assign(popup!.style, {
                left: `${x}px`,
                top: `${y}px`,
              });
            });
          }
        },
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        onUpdate: (props: any) => {
          reactRenderer?.updateProps(props);
          
          if (!props.clientRect || !popup) {
            return;
          }
          
          const rect = props.clientRect();
          if (rect) {
            computePosition(
              { getBoundingClientRect: () => rect },
              popup,
              {
                placement: "bottom-start",
                middleware: [offset(4), flip(), shift({ padding: 8 })],
              }
            ).then(({ x, y }) => {
              Object.assign(popup!.style, {
                left: `${x}px`,
                top: `${y}px`,
              });
            });
          }
        },
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        onKeyDown: (props: any) => {
          if (props.event.key === "Escape") {
            reactRenderer?.destroy();
            popup?.remove();
            return true;
          }
          return reactRenderer?.ref?.onKeyDown(props);
        },
        onExit: () => {
          reactRenderer?.destroy();
          popup?.remove();
        },
      };
    },
  };
}
