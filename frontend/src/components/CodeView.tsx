"use client";

import { useEffect, useState } from "react";
import type { BundledLanguage, ThemedToken } from "shiki";
import type { DiffLine } from "@/lib/toolContent";

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

