import React, { useEffect, useState } from "react";
import { ChevronDown, ChevronRight, Copy, Check, FileCode2 } from "lucide-react";
import type { BundledLanguage, ThemedToken } from "shiki";
import type { DiffLine, FileDiffGroup } from "@/lib/toolContent";

/**
 * Rendering a tool call's content the way an editor would.
 *
 * Highlighting uses Shiki (github-light theme for enterprise light UI).
 */

const THEME = "github-light";

type Lines = ThemedToken[][];

function useHighlighted(code: string, lang: string): Lines | null {
  const key = `${lang}\u0000${code}`;
  const [done, setDone] = useState<{ key: string; lines: Lines } | null>(null);

  useEffect(() => {
    if (!code || lang === "text") return;
    let isSubscribed = true;
    import("shiki")
      .then(({ codeToTokens }) =>
        codeToTokens(code, { lang: lang as BundledLanguage, theme: THEME }),
      )
      .then((res) => {
        if (isSubscribed) setDone({ key, lines: res.tokens });
      })
      .catch(() => {
        // Fallback gracefully on unknown grammar
      });
    return () => {
      isSubscribed = false;
    };
  }, [key, code, lang]);

  return done?.key === key ? done.lines : null;
}

function Tokens({ tokens }: { tokens: ThemedToken[] }) {
  return (
    <>
      {tokens.map((t, i) => (
        <span key={i} style={{ color: t.color }}>
          {t.content}
        </span>
      ))}
    </>
  );
}

/**
 * CodeBlock shows a file window with the gutter read_file gave it.
 */
export function CodeBlock({
  code,
  lang,
  startLine = 1,
  note,
}: {
  code: string;
  lang: string;
  startLine?: number;
  note?: string;
}) {
  const highlighted = useHighlighted(code, lang);
  const plain = code.split("\n");
  const count = highlighted?.length ?? plain.length;

  return (
    <div className="mt-1 overflow-hidden rounded-xl border border-sand-200 bg-white shadow-2xs">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse font-mono text-[11px] leading-[1.55]">
          <tbody>
            {Array.from({ length: count }, (_, i) => (
              <tr key={i} className="hover:bg-sand-50/50">
                <td className="select-none border-r border-sand-200 px-2.5 text-right align-top text-stone-400 tabular-nums bg-sand-50/70">
                  {startLine + i}
                </td>
                <td className="whitespace-pre px-3 align-top text-stone-900">
                  {highlighted ? <Tokens tokens={highlighted[i]} /> : plain[i]}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {note && <p className="border-t border-sand-200 px-3 py-1 text-[10px] text-stone-500 bg-sand-50">{note}</p>}
    </div>
  );
}

const ROW: Record<DiffLine["kind"], { bg: string; marker: string; markerClass: string }> = {
  add: { bg: "bg-emerald-50/90", marker: "+", markerClass: "text-emerald-700 font-bold" },
  del: { bg: "bg-rose-50/90", marker: "-", markerClass: "text-rose-700 font-bold" },
  ctx: { bg: "bg-white", marker: " ", markerClass: "text-stone-300" },
};

/**
 * DiffView shows what an edit_file call replaced in clean unified diff style.
 */
export function DiffView({ lines, lang }: { lines: DiffLine[]; lang: string }) {
  const oldCode = lines.filter((l) => l.kind !== "add").map((l) => l.text).join("\n");
  const newCode = lines.filter((l) => l.kind !== "del").map((l) => l.text).join("\n");
  const oldLines = useHighlighted(oldCode, lang);
  const newLines = useHighlighted(newCode, lang);

  let oldIdx = 0;
  let newIdx = 0;

  return (
    <div className="mt-1 overflow-hidden rounded-xl border border-sand-200 bg-white shadow-2xs">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse font-mono text-[11px] leading-[1.55]">
          <tbody>
            {lines.map((line, i) => {
              const style = ROW[line.kind];
              const tokens =
                line.kind === "add" ? newLines?.[newIdx] : oldLines?.[oldIdx];
              if (line.kind !== "add") oldIdx++;
              if (line.kind !== "del") newIdx++;

              return (
                <tr key={i} className={style.bg}>
                  <td className="select-none px-2 text-right align-top text-stone-400 tabular-nums border-r border-sand-150 bg-sand-50/50">
                    {line.oldNo ?? ""}
                  </td>
                  <td className="select-none border-r border-sand-200 px-2 text-right align-top text-stone-400 tabular-nums bg-sand-50/50">
                    {line.newNo ?? ""}
                  </td>
                  <td className={`select-none pl-2 align-top ${style.markerClass}`}>{style.marker}</td>
                  <td className="whitespace-pre pr-3 align-top text-stone-900">
                    {tokens ? <Tokens tokens={tokens} /> : line.text}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/**
 * FileDiffCard displays all edit hunks for a single file inside one cohesive card.
 */
export function FileDiffCard({
  file,
  defaultExpanded = true,
}: {
  file: FileDiffGroup;
  defaultExpanded?: boolean;
}) {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [copied, setCopied] = useState(false);

  // Combine all lines across hunks for syntax highlighting
  const allLines = file.hunks.flatMap((h) => h.lines);
  const oldCode = allLines.filter((l) => l.kind !== "add").map((l) => l.text).join("\n");
  const newCode = allLines.filter((l) => l.kind !== "del").map((l) => l.text).join("\n");
  const oldTokens = useHighlighted(oldCode, file.lang);
  const newTokens = useHighlighted(newCode, file.lang);

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    navigator.clipboard.writeText(file.path).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    });
  };

  let globalOldIdx = 0;
  let globalNewIdx = 0;

  return (
    <div className="overflow-hidden rounded-2xl border border-sand-200 bg-white shadow-2xs transition-all">
      {/* File Header */}
      <div
        onClick={() => setExpanded(!expanded)}
        className="flex items-center justify-between px-4 py-2.5 bg-gradient-to-r from-sand-50/90 via-white to-sand-50/50 hover:bg-sand-100/60 border-b border-sand-200 cursor-pointer select-none transition-colors"
      >
        <div className="flex items-center gap-2.5 min-w-0">
          <button
            type="button"
            className="p-0.5 rounded-md hover:bg-sand-200/70 text-stone-500 transition-colors shrink-0"
          >
            {expanded ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
          </button>

          <FileCode2 className="w-4 h-4 text-stone-500 shrink-0" />

          <span className="font-mono text-xs font-bold text-stone-900 truncate">
            {file.path}
          </span>

          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-sand-100 border border-sand-200 text-stone-600 uppercase font-semibold shrink-0">
            {file.lang}
          </span>

          <button
            type="button"
            onClick={handleCopy}
            title="Copy file path"
            className="p-1 rounded hover:bg-sand-200/70 text-stone-400 hover:text-stone-700 transition-colors"
          >
            {copied ? <Check className="w-3.5 h-3.5 text-emerald-600" /> : <Copy className="w-3.5 h-3.5" />}
          </button>
        </div>

        {/* Change Stats Pill */}
        <div className="flex items-center gap-1.5 shrink-0 font-mono text-xs font-bold">
          {file.additions > 0 && (
            <span className="px-2 py-0.5 rounded-md bg-emerald-50 text-emerald-700 border border-emerald-200 text-[11px]">
              +{file.additions}
            </span>
          )}
          {file.deletions > 0 && (
            <span className="px-2 py-0.5 rounded-md bg-rose-50 text-rose-700 border border-rose-200 text-[11px]">
              -{file.deletions}
            </span>
          )}
          {file.hunks.length > 1 && (
            <span className="text-[10px] font-sans font-medium text-stone-400">
              ({file.hunks.length} hunks)
            </span>
          )}
        </div>
      </div>

      {/* File Hunks Diff Table */}
      {expanded && (
        <div className="overflow-x-auto">
          <table className="w-full border-collapse font-mono text-[11px] leading-[1.55]">
            <tbody>
              {file.hunks.map((hunk, hIdx) => {
                const hunkHeader =
                  hunk.hunkHeader ||
                  `@@ Lines ${hunk.oldStart || hunk.lines[0]?.oldNo || "…"} → ${hunk.lines[hunk.lines.length - 1]?.newNo || hunk.lines[0]?.newNo || "…"} @@`;

                return (
                  <React.Fragment key={hIdx}>
                    {/* Hunk Divider (shown between separate hunks) */}
                    {hIdx > 0 && (
                      <tr className="bg-sand-100/90 border-y border-sand-200 select-none text-[10px] font-mono text-stone-500">
                        <td
                          colSpan={2}
                          className="px-2.5 py-1 text-center bg-sand-200/50 text-stone-400 border-r border-sand-200 select-none font-bold"
                        >
                          ⋯
                        </td>
                        <td
                          colSpan={2}
                          className="px-3 py-1 bg-sand-50 font-mono text-[10px] text-stone-600"
                        >
                          <span className="font-semibold text-indigo-700 bg-indigo-50 border border-indigo-200/70 px-2 py-0.5 rounded shadow-2xs">
                            {hunkHeader}
                          </span>
                        </td>
                      </tr>
                    )}

                    {hunk.lines.map((line, lIdx) => {
                      const style = ROW[line.kind];
                      const tokens =
                        line.kind === "add"
                          ? newTokens?.[globalNewIdx]
                          : oldTokens?.[globalOldIdx];
                      if (line.kind !== "add") globalOldIdx++;
                      if (line.kind !== "del") globalNewIdx++;

                      return (
                        <tr key={`${hIdx}-${lIdx}`} className={style.bg}>
                          <td className="select-none px-2 text-right align-top text-stone-400 tabular-nums border-r border-sand-150 bg-sand-50/50 w-10">
                            {line.oldNo ?? ""}
                          </td>
                          <td className="select-none border-r border-sand-200 px-2 text-right align-top text-stone-400 tabular-nums bg-sand-50/50 w-10">
                            {line.newNo ?? ""}
                          </td>
                          <td className={`select-none pl-2 align-top w-4 ${style.markerClass}`}>
                            {style.marker}
                          </td>
                          <td className="whitespace-pre pr-3 align-top text-stone-900">
                            {tokens ? <Tokens tokens={tokens} /> : line.text}
                          </td>
                        </tr>
                      );
                    })}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {expanded && file.truncated && (
        <div className="border-t border-sand-200 px-4 py-2 text-[10px] font-mono text-stone-500 bg-sand-50">
          Diff truncated — the change is larger than what is recorded here.
        </div>
      )}
    </div>
  );
}

/**
 * GroupedDiffViewer renders all modified files grouped with stats and collapse controls.
 */
export function GroupedDiffViewer({
  files,
}: {
  files: FileDiffGroup[];
}) {
  const [allExpanded, setAllExpanded] = useState(true);
  const [expandKey, setExpandKey] = useState(0);

  const totalAdditions = files.reduce((sum, f) => sum + f.additions, 0);
  const totalDeletions = files.reduce((sum, f) => sum + f.deletions, 0);

  const handleToggleAll = () => {
    setAllExpanded(!allExpanded);
    setExpandKey((k) => k + 1);
  };

  if (files.length === 0) {
    return (
      <div className="p-8 rounded-2xl border border-sand-200 bg-white shadow-2xs text-center text-xs text-stone-400">
        No file edits recorded yet.
      </div>
    );
  }

  return (
    <div className="space-y-4 font-sans">
      {/* Overview stats bar */}
      <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-2.5 rounded-2xl bg-sand-50/90 border border-sand-200 text-xs shadow-2xs">
        <div className="flex items-center gap-3">
          <span className="font-bold text-stone-900">
            {files.length} {files.length === 1 ? "file changed" : "files changed"}
          </span>
          <span className="text-stone-300">|</span>
          <div className="flex items-center gap-2 font-mono font-bold text-[11px]">
            {totalAdditions > 0 && <span className="text-emerald-700">+{totalAdditions} additions</span>}
            {totalDeletions > 0 && <span className="text-rose-700">-{totalDeletions} deletions</span>}
          </div>
        </div>

        {files.length > 1 && (
          <button
            type="button"
            onClick={handleToggleAll}
            className="text-[11px] font-semibold text-stone-700 hover:text-stone-900 cursor-pointer px-2.5 py-1 rounded-xl hover:bg-sand-200/70 border border-sand-200 transition-colors"
          >
            {allExpanded ? "Collapse All Files" : "Expand All Files"}
          </button>
        )}
      </div>

      {/* Files List */}
      <div className="space-y-4">
        {files.map((file) => (
          <FileDiffCard
            key={`${file.path}-${expandKey}`}
            file={file}
            defaultExpanded={allExpanded}
          />
        ))}
      </div>
    </div>
  );
}

