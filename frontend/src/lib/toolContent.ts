import { diffLines } from "diff";

/**
 * Reading what a tool call actually did.
 *
 * The timeline has always carried this content — a read_file's lines, an
 * edit_file's before and after — and rendered it as one undifferentiated block
 * of grey monospace. That is the same information a diff viewer shows, minus
 * everything that makes it readable: no syntax colour, no gutter, no sense of
 * what was removed and what replaced it.
 *
 * The parsing lives here rather than in the components because it is the part
 * that can be wrong, and the frontend test runner only reaches .test.ts files.
 * Line diffing itself is jsdiff's job — it is the standard implementation and
 * nothing about this problem is special enough to justify a second one.
 */

export interface ToolArgs {
  path?: string;
  command?: string;
  pattern?: string;
  oldString?: string;
  newString?: string;
}

/**
 * parseToolArgs unpacks the JSON a model emitted for a tool call.
 *
 * Arguments are head-truncated on the way in, so half an object is the normal
 * case rather than a corruption: unparseable input yields no arguments and the
 * caller falls back to showing the raw text.
 */
export function parseToolArgs(input?: string): ToolArgs {
  if (!input) return {};
  try {
    const o = JSON.parse(input) as Record<string, unknown>;
    const str = (k: string) => (typeof o[k] === "string" ? (o[k] as string) : undefined);
    return {
      path: str("path"),
      command: str("command"),
      pattern: str("pattern"),
      oldString: str("old_string"),
      newString: str("new_string"),
    };
  } catch {
    return {};
  }
}

export interface NumberedFile {
  /** The source with its gutter removed, ready to highlight. */
  code: string;
  /** The first line's real number, so the gutter still matches the file. */
  startLine: number;
  /** A truncation notice, when the read did not reach the end of the file. */
  note?: string;
}

// read_file emits "<lineno>\t<text>". Anchored, so a line of source that
// merely contains a tab is not mistaken for a numbered one.
const NUMBERED = /^(\d+)\t([\s\S]*)$/;

/**
 * parseNumberedFile splits read_file output into source and gutter.
 *
 * The numbers are not part of the code. Highlighting them as source colours
 * them as integers and shifts every line one tab to the right, which is
 * exactly the thing that made this output hard to read.
 */
export function parseNumberedFile(detail: string): NumberedFile | null {
  if (!detail.trim()) return null;

  const out: string[] = [];
  let startLine = 0;
  let note: string | undefined;

  for (const line of detail.split("\n")) {
    const m = NUMBERED.exec(line);
    if (m) {
      if (startLine === 0) startLine = Number(m[1]);
      out.push(m[2]);
      continue;
    }
    // The truncation sentence is prose telling the model how to continue
    // reading. It is not code and must not land inside the block.
    if (line.trim().startsWith("... (showing lines")) {
      note = line.trim().replace(/^\.\.\.\s*\(/, "").replace(/\)$/, "");
    }
  }

  if (out.length === 0) return null;
  return { code: out.join("\n"), startLine: startLine || 1, note };
}

// Only extensions this codebase and its customers' repositories actually
// produce. A guess for anything else would colour the wrong tokens, which
// reads as a bug in the file rather than in the viewer.
const LANGUAGES: Record<string, string> = {
  go: "go",
  ts: "typescript",
  tsx: "tsx",
  js: "javascript",
  jsx: "jsx",
  mjs: "javascript",
  py: "python",
  rb: "ruby",
  rs: "rust",
  java: "java",
  php: "php",
  sh: "shell",
  bash: "shell",
  sql: "sql",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  md: "markdown",
  css: "css",
  html: "html",
};

/** Files whose language is their name rather than an extension. */
const BY_NAME: Record<string, string> = {
  dockerfile: "docker",
  makefile: "makefile",
};

export function languageOf(path?: string): string {
  if (!path) return "text";
  const base = path.split("/").pop() ?? "";
  const byName = BY_NAME[base.toLowerCase()];
  if (byName) return byName;
  const ext = base.includes(".") ? base.split(".").pop()!.toLowerCase() : "";
  return LANGUAGES[ext] ?? "text";
}

export interface DiffLine {
  kind: "add" | "del" | "ctx";
  text: string;
  /** Line number on the old side; absent on an added line. */
  oldNo?: number;
  /** Line number on the new side; absent on a removed line. */
  newNo?: number;
}

/**
 * editDiff turns an edit_file call's before and after into diff rows.
 *
 * Both sides are numbered independently, as a diff viewer does: a removed line
 * has no number on the new side and an added line has none on the old, which
 * is what makes the two columns line up against the file they came from.
 */
export function editDiff(oldStr: string, newStr: string): DiffLine[] {
  const parts = diffLines(oldStr ?? "", newStr ?? "");
  const rows: DiffLine[] = [];
  let oldNo = 1;
  let newNo = 1;

  for (const part of parts) {
    const lines = part.value.split("\n");
    // A trailing newline produces an empty final element that is not a line.
    if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop();

    for (const text of lines) {
      if (part.added) {
        rows.push({ kind: "add", text, newNo: newNo++ });
      } else if (part.removed) {
        rows.push({ kind: "del", text, oldNo: oldNo++ });
      } else {
        rows.push({ kind: "ctx", text, oldNo: oldNo++, newNo: newNo++ });
      }
    }
  }
  return rows;
}
