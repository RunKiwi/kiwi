/* eslint-disable react-hooks/refs */
"use client";

import { useEditor, EditorContent } from "@tiptap/react";
import Document from "@tiptap/extension-document";
import Paragraph from "@tiptap/extension-paragraph";
import Text from "@tiptap/extension-text";
import Placeholder from "@tiptap/extension-placeholder";
import { useEffect, useRef, useMemo } from "react";
import type { JobSummary, PlanRequest, GithubRepo } from "@/lib/api";

import { RefMention } from "./extensions/refMention";
import { WorkMention } from "./extensions/workMention";
import { ActionMention } from "./extensions/actionMention";
import { createSuggestion } from "./suggest";
import { serializeTask } from "./serialize";

export interface TaskComposerProps {
  value: string;
  onChange: (partial: Partial<PlanRequest>) => void;
  repos: GithubRepo[];
  jobs: JobSummary[];
  models: string[];
  repoSelected: boolean;
}

export function TaskComposer({ value, onChange, repos, jobs, models, repoSelected }: TaskComposerProps) {
  // Pass suggestion context as ref to keep Tiptap options up to date without recreation
  const contextRef = useRef({ repos, jobs, models, repoSelected });
  
  useEffect(() => {
    contextRef.current = { repos, jobs, models, repoSelected };
  }, [repos, jobs, models, repoSelected]);

  const PLACEHOLDER = "Describe what to build or fix, e.g. “The /api/report endpoint returns stale data — fix it and add a test.”";

  const extensions = useMemo(() => [
    Document,
    Paragraph,
    Text,
    Placeholder.configure({ placeholder: PLACEHOLDER }),
    RefMention.configure({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      suggestion: createSuggestion("@", () => contextRef.current) as any,
    }),
    WorkMention.configure({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      suggestion: createSuggestion("#", () => contextRef.current) as any,
    }),
    ActionMention.configure({
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      suggestion: createSuggestion("/", () => contextRef.current) as any,
    }),
  ], []);

  const editor = useEditor({
    immediatelyRender: false,
    extensions,
    // value is plain text; escape it so a task containing "<" doesn't misparse as HTML.
    content: value ? `<p>${escapeHtml(value)}</p>` : "",
    onUpdate: ({ editor }) => {
      const doc = editor.getJSON();
      const partial = serializeTask(doc);
      onChange(partial);
    },
    editorProps: {
      attributes: {
        class: "field border-0 bg-transparent rounded-lg px-2 py-1.5 min-h-[76px] text-base leading-relaxed focus:shadow-none focus:outline-none",
      },
    },
  });

  // Reset the editor when the parent clears the task (e.g. after a successful
  // submit). useEditor reads `content` only once at mount, so without this the
  // text and chips would linger — and get resubmitted on the next Launch.
  useEffect(() => {
    if (editor && value === "" && !editor.isEmpty) {
      editor.commands.clearContent();
    }
  }, [value, editor]);

  return (
    <div className="relative w-full cursor-text" onClick={() => editor?.commands.focus()}>
      <EditorContent editor={editor} />
    </div>
  );
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}
