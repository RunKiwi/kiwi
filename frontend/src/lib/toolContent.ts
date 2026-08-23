import { diffLines, parsePatch } from "diff";

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
  content?: string;
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
      content: str("content"),
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


const HUNK = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@(.*)$/;

/**
 * repairHunkCounts rewrites each hunk header to match the lines that follow it.
 *
 * A diff bounded partway through a hunk still promises the original line counts
 * in its @@ header, and a strict parser rejects the whole patch for it — so the
 * change would vanish from the timeline exactly when it was big enough to be
 * worth reading. Recounting is a normalisation of the header, not a second diff
 * implementation: the hunks themselves are still parsed by jsdiff.
 */
function repairHunkCounts(body: string): string {
  const lines = body.split("\n");
  const out: string[] = [];

  for (let i = 0; i < lines.length; i++) {
    const m = HUNK.exec(lines[i]);
    if (!m) {
      out.push(lines[i]);
      continue;
    }

    let oldCount = 0;
    let newCount = 0;
    let j = i + 1;
    for (; j < lines.length && !HUNK.test(lines[j]); j++) {
      const marker = lines[j][0];
      if (marker === "-") oldCount++;
      else if (marker === "+") newCount++;
      else if (marker === " ") {
        oldCount++;
        newCount++;
      }
      // Anything else — a blank tail, the truncation notice — belongs to
      // neither side and is not counted.
    }

    out.push("@@ -" + m[1] + "," + oldCount + " +" + m[2] + "," + newCount + " @@" + m[3]);
    for (let k = i + 1; k < j; k++) out.push(lines[k]);
    i = j - 1;
  }

  return out.join("\n");
}

export interface DiffHunk {
  hunkHeader?: string;
  oldStart?: number;
  newStart?: number;
  lines: DiffLine[];
}

export interface ParsedDiff {
  path?: string;
  lines: DiffLine[];
  hunks?: DiffHunk[];
  /** The daemon bounded the diff; what is shown is not the whole change. */
  truncated: boolean;
}

/**
 * parseUnifiedDiff reads the diff an edit_file call reported.
 *
 * It has to come from the tool's result rather than from the call's arguments:
 * inputCap bounds a recorded argument at 600 bytes, so a real edit's
 * old_string and new_string arrive cut mid-JSON, unparseable and both
 * incomplete. The daemon therefore computes the diff, where it still has both
 * whole, and sends it.
 *
 * Parsing is jsdiff's parsePatch — the same library that produces the diff for
 * the small-edit path, and the standard implementation of a format with more
 * edge cases than it appears to have.
 */
export function parseUnifiedDiff(detail: string): ParsedDiff | null {
  if (!detail.includes("@@")) return null;

  // The result begins with "edited <path>"; the patch starts at its header.
  const start = detail.indexOf("--- ");
  const body = start >= 0 ? detail.slice(start) : detail.slice(detail.indexOf("@@"));

  let patches;
  try {
    patches = parsePatch(repairHunkCounts(body));
  } catch {
    return null;
  }
  if (!patches.length || !patches[0].hunks.length) return null;

  const patch = patches[0];
  const lines: DiffLine[] = [];
  const hunks: DiffHunk[] = [];

  for (const hunk of patch.hunks) {
    let oldNo = hunk.oldStart;
    let newNo = hunk.newStart;
    const hunkLines: DiffLine[] = [];
    for (const raw of hunk.lines) {
      const marker = raw[0];
      const text = raw.slice(1);
      if (marker === "+") {
        const dl: DiffLine = { kind: "add", text, newNo: newNo++ };
        lines.push(dl);
        hunkLines.push(dl);
      } else if (marker === "-") {
        const dl: DiffLine = { kind: "del", text, oldNo: oldNo++ };
        lines.push(dl);
        hunkLines.push(dl);
      } else if (marker === " ") {
        const dl: DiffLine = { kind: "ctx", text, oldNo: oldNo++, newNo: newNo++ };
        lines.push(dl);
        hunkLines.push(dl);
      }
      // "\\ No newline at end of file" carries no line and is skipped.
    }
    if (hunkLines.length > 0) {
      hunks.push({
        hunkHeader: `@@ -${hunk.oldStart},${hunk.oldLines} +${hunk.newStart},${hunk.newLines} @@`,
        oldStart: hunk.oldStart,
        newStart: hunk.newStart,
        lines: hunkLines,
      });
    }
  }

  if (lines.length === 0) return null;
  return {
    path: patch.newFileName?.replace(/^b\//, "") ?? patch.oldFileName?.replace(/^a\//, ""),
    lines,
    hunks,
    truncated: detail.includes("(diff truncated)"),
  };
}

export interface FileDiffGroup {
  path: string;
  lang: string;
  hunks: DiffHunk[];
  additions: number;
  deletions: number;
  truncated: boolean;
}

export interface RawFileEdit {
  path?: string;
  lines: DiffLine[];
  hunks?: DiffHunk[];
  lang?: string;
  truncated?: boolean;
}

/**
 * groupDiffsByFile aggregates multiple edit operations and diff hunks by file path.
 *
 * Rather than creating multiple cards for the same file, this groups changes
 * cleanly into unified file objects with cumulative addition/deletion counts
 * and structured hunks.
 */
export function groupDiffsByFile(edits: RawFileEdit[]): FileDiffGroup[] {
  const map = new Map<
    string,
    {
      path: string;
      lang: string;
      hunks: DiffHunk[];
      truncated: boolean;
    }
  >();

  for (const edit of edits) {
    const p = edit.path || "unnamed file";
    const lang = edit.lang || languageOf(p);
    let existing = map.get(p);
    if (!existing) {
      existing = {
        path: p,
        lang,
        hunks: [],
        truncated: false,
      };
      map.set(p, existing);
    }

    if (edit.truncated) {
      existing.truncated = true;
    }

    if (edit.hunks && edit.hunks.length > 0) {
      existing.hunks.push(...edit.hunks);
    } else if (edit.lines.length > 0) {
      const oldStart = edit.lines.find((l) => l.oldNo !== undefined)?.oldNo;
      const newStart = edit.lines.find((l) => l.newNo !== undefined)?.newNo;
      existing.hunks.push({
        oldStart,
        newStart,
        lines: edit.lines,
      });
    }
  }

  const results: FileDiffGroup[] = [];
  for (const item of map.values()) {
    let additions = 0;
    let deletions = 0;
    for (const h of item.hunks) {
      for (const l of h.lines) {
        if (l.kind === "add") additions++;
        else if (l.kind === "del") deletions++;
      }
    }
    results.push({
      path: item.path,
      lang: item.lang,
      hunks: item.hunks,
      additions,
      deletions,
      truncated: item.truncated,
    });
  }

  return results;
}
