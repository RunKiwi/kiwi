"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Check, ChevronDown, Search } from "lucide-react";

export interface SelectOption {
  value: string;
  label: string;
  hint?: string; // optional muted trailing text (e.g. "private", "BYOC")
}

interface SelectProps {
  value: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  searchable?: boolean;
  variant?: "field" | "chip";
  /** Small uppercase key shown before the value in chip variant (e.g. "Repo"). */
  label?: string;
  icon?: React.ReactNode;
  /** Extra classes for the trigger. */
  className?: string;
  /** Accessible name when there's no visible label. */
  ariaLabel?: string;
}

// A single custom dropdown used across the dashboard: navy glass menu, green
// accent, keyboard nav, and optional type-to-filter search. Replaces native
// <select> so the menu matches the product design and can be searched.
export function Select({
  value,
  onChange,
  options,
  placeholder = "Select…",
  searchable = false,
  variant = "field",
  label,
  icon,
  className = "",
  ariaLabel,
}: SelectProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  const selected = options.find(o => o.value === value);
  const display = selected?.label ?? placeholder;

  const filtered = useMemo(() => {
    if (!searchable || !query.trim()) return options;
    const q = query.toLowerCase();
    return options.filter(o => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q));
  }, [options, query, searchable]);

  // Close on outside click / Escape; focus the search field on open.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(false); };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    if (searchable) requestAnimationFrame(() => searchRef.current?.focus());
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, searchable]);

  // Open with the highlight already on the current selection (no effect setState).
  const openMenu = () => {
    const idx = options.findIndex(o => o.value === value);
    setActive(idx >= 0 ? idx : 0);
    setQuery("");
    setOpen(true);
  };
  const toggle = () => (open ? setOpen(false) : openMenu());

  // Keep the highlighted row in view.
  useEffect(() => {
    if (!open) return;
    listRef.current?.querySelector<HTMLElement>(`[data-idx="${active}"]`)?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  const pick = (v: string) => { onChange(v); setOpen(false); setQuery(""); };

  const onListKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") { e.preventDefault(); setActive(a => Math.min(a + 1, filtered.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setActive(a => Math.max(a - 1, 0)); }
    else if (e.key === "Enter") { e.preventDefault(); if (filtered[active]) pick(filtered[active].value); }
  };

  const chevron = <ChevronDown className={`w-3.5 h-3.5 text-zinc-400 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} />;

  return (
    <div ref={rootRef} className={`relative ${variant === "field" ? "w-full" : "inline-flex"}`}>
      {variant === "chip" ? (
        <button type="button" aria-label={ariaLabel} aria-expanded={open}
          onClick={toggle}
          className={`chip cursor-pointer ${className}`}>
          {icon}
          {label && <span className="k">{label}</span>}
          <span className="v truncate max-w-[170px]" style={{ color: selected ? undefined : "var(--tx-dim)" }}>{display}</span>
          {chevron}
        </button>
      ) : (
        <button type="button" aria-label={ariaLabel} aria-expanded={open}
          onClick={toggle}
          className={`field flex items-center justify-between gap-2 text-left ${className}`}>
          <span className="flex items-center gap-2 min-w-0">
            {icon}
            <span className={`truncate ${selected ? "text-[var(--tx)]" : "text-[var(--tx-dim)]"}`}>{display}</span>
          </span>
          {chevron}
        </button>
      )}

      {open && (
        <div
          onKeyDown={onListKey}
          className="pr-popover absolute top-full left-0 mt-2 z-50 min-w-[240px] w-max max-w-[340px] rounded-xl border border-white/10 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-16px_rgba(0,0,0,0.85)] p-1.5"
        >
          {searchable && (
            <div className="flex items-center gap-2 px-2.5 py-1.5 mb-1 rounded-lg bg-black/20 border border-white/5">
              <Search className="w-3.5 h-3.5 text-zinc-500 shrink-0" />
              <input
                ref={searchRef}
                value={query}
                onChange={e => { setQuery(e.target.value); setActive(0); }}
                placeholder="Search…"
                className="bg-transparent outline-none border-0 text-sm text-white placeholder:text-zinc-600 w-full"
              />
            </div>
          )}
          <div ref={listRef} className="flex flex-col gap-0.5 max-h-64 overflow-y-auto">
            {filtered.length === 0 ? (
              <div className="px-2.5 py-3 text-xs text-zinc-500 text-center">No matches</div>
            ) : (
              filtered.map((o, i) => {
                const isSel = o.value === value;
                return (
                  <button
                    key={o.value || "__empty"}
                    type="button"
                    data-idx={i}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => pick(o.value)}
                    className={`flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-sm text-left transition-colors ${i === active ? "bg-white/[0.07]" : ""}`}
                  >
                    <Check className={`w-3.5 h-3.5 shrink-0 ${isSel ? "text-[var(--green)]" : "text-transparent"}`} />
                    <span className={`truncate flex-1 ${isSel ? "text-white font-medium" : "text-zinc-200"}`}>{o.label}</span>
                    {o.hint && <span className="text-[11px] text-zinc-500 shrink-0">{o.hint}</span>}
                  </button>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
