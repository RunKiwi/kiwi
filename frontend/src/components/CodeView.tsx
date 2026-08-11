"use client";

import { useEffect, useState } from "react";
import type { BundledLanguage, ThemedToken } from "shiki";
import type { DiffLine } from "@/lib/toolContent";

/**
 * Rendering a tool call's content the way an editor would.
 *
 * The timeline already carried this — what a read_file returned, what an
 * edit_file replaced — and showed it as one block of grey monospace. The
 * information was there and unreadable.
 *
 * Highlighting is Shiki's, which is the grammar engine VS Code itself uses, so
 * the colours match what these files look like in an editor rather than
 * approximating them. It is imported dynamically: the full grammar bundle is
 * large, nobody needs it until a tool row is expanded, and loading it lazily
 * keeps it out of the dashboard's initial payload entirely.
 *
 * Tokens are rendered as React elements rather than Shiki's HTML string. That
 * avoids dangerouslySetInnerHTML on content a model produced — the one place
 * in this view where untrusted text meets the DOM.
 */

// GitHub's own dark theme, so a diff here looks like the diff on the pull
// request it came from.
const THEME = "github-dark-default";

type Lines = ThemedToken[][];

/**
 * useHighlighted tokenises code, returning null until the grammar has loaded
 * and if the language is unsupported. Callers render plain text in that case,
 * so content is always visible — colour arrives, or it does not, but the code
 * never waits on it.
 */
function useHighlighted(code: string, lang: string): Lines | null {
  // Keyed by what was highlighted, so stale tokens are discarded by comparing
  // during render rather than by clearing state in an effect. Clearing was the
  // obvious way to write this and is the one thing an effect must not do — it
  // sets state synchronously, which cascades a render.
  const key = `${lang}\u0000${code}`;
  const [done, setDone] = useState<{ key: string; lines: Lines } | null>(null);

  useEffect(() => {
    if (!code || lang === "text") return;
    let isSubscribed = true;
    import("shiki")
      .then(({ codeToTokens }) =>
        // languageOf only ever produces ids Shiki bundles, and the catch below
        // covers the case where that stops being true — an unknown grammar
        // renders as plain text rather than throwing into the component tree.
        codeToTokens(code, { lang: lang as BundledLanguage, theme: THEME }),
      )
      .then((res) => {
        if (isSubscribed) setDone({ key, lines: res.tokens });
      })
      .catch(() => {
        // An unknown grammar is not an error worth showing. Leaving the state
        // alone means the key will not match and the plain-text fallback
        // renders — a worse render, not a broken one.
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
 * CodeBlock shows a file window with the gutter read_file gave it, so a line
 * number here is the line number in the file.
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
    <div className="mt-1 overflow-hidden rounded-md border border-white/10 bg-black/40">
      <div className="overflow-x-auto">
        <table className="w-full border-collapse font-mono text-[11px] leading-[1.55]">
          <tbody>
            {Array.from({ length: count }, (_, i) => (
              <tr key={i}>
                <td className="select-none border-r border-white/8 px-2 text-right align-top text-zinc-600 tabular-nums">
                  {startLine + i}
                </td>
                <td className="whitespace-pre px-3 align-top text-zinc-300">
                  {highlighted ? <Tokens tokens={highlighted[i]} /> : plain[i]}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {note && <p className="border-t border-white/8 px-3 py-1 text-[10px] text-zinc-500">{note}</p>}
    </div>
  );
}

// Backgrounds tint the row; the +/- marker carries the same meaning in text,
// so a colourblind reader and a screenshot both still say which side a line is
// on.
const ROW: Record<DiffLine["kind"], { bg: string; marker: string; markerClass: string }> = {
  add: { bg: "bg-emerald-500/10", marker: "+", markerClass: "text-emerald-400" },
  del: { bg: "bg-rose-500/10", marker: "-", markerClass: "text-rose-400" },
  ctx: { bg: "", marker: " ", markerClass: "text-zinc-700" },
};

/**
 * DiffView shows what an edit_file call replaced, in the shape of the pull
 * request it will end up in.
 *
 * Both sides are highlighted in the file's own language rather than as "diff",
 * which colours a whole line one colour and loses the code inside it. The two
 * sides are tokenised separately because they are two different texts; rows
 * are then interleaved in diff order.
 */
export function DiffView({ lines, lang }: { lines: DiffLine[]; lang: string }) {
  const oldCode = lines.filter((l) => l.kind !== "add").map((l) => l.text).join("\n");
  const newCode = lines.filter((l) => l.kind !== "del").map((l) => l.text).join("\n");
  const oldLines = useHighlighted(oldCode, lang);
  const newLines = useHighlighted(newCode, lang);

  // Walk each side's tokens in step with the rows that came from it.
  let oldIdx = 0;
  let newIdx = 0;

  return (
    <div className="mt-1 overflow-hidden rounded-md border border-white/10 bg-black/40">
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
                  <td className="select-none px-2 text-right align-top text-zinc-600 tabular-nums">
                    {line.oldNo ?? ""}
                  </td>
                  <td className="select-none border-r border-white/8 px-2 text-right align-top text-zinc-600 tabular-nums">
                    {line.newNo ?? ""}
                  </td>
                  <td className={`select-none pl-2 align-top ${style.markerClass}`}>{style.marker}</td>
                  <td className="whitespace-pre pr-3 align-top text-zinc-300">
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
