"use client";

import { useEffect, useMemo, useRef, useState, useId } from "react";
import { Check, ChevronDown, Search } from "lucide-react";

export interface SelectOption {
  value: string;
  label: string;
  hint?: string; // optional muted trailing text (e.g. "private", "BYOC")
  badge?: React.ReactNode;
  icon?: React.ReactNode;
  sublabel?: string;
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
  /**
   * Renders a detail panel under the list for whichever option is currently
   * highlighted — by hover, by arrow key, or by being the current selection
   * when the menu opens.
   */
  renderDetail?: (option: SelectOption) => React.ReactNode;
}

// A custom dropdown used across the dashboard: frosted popover menu,
// kiwi accent, keyboard nav, and optional type-to-filter search.
export function Select({
  value,
  onChange,
  options,
  placeholder = "Select…",
  searchable = false,
  variant = "field",
  label,
  icon,
  renderDetail,
  className = "",
  ariaLabel,
}: SelectProps) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const listboxId = useId();

  const selected = options.find((o) => o.value === value);
  const display = selected?.label ?? placeholder;

  const filtered = useMemo(() => {
    if (!searchable || !query.trim()) return options;
    const q = query.toLowerCase();
    return options.filter(
      (o) =>
        o.label.toLowerCase().includes(q) ||
        o.value.toLowerCase().includes(q) ||
        (o.sublabel && o.sublabel.toLowerCase().includes(q))
    );
  }, [options, query, searchable]);

  // Close on outside click / Escape; focus the search field on open.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    if (searchable) requestAnimationFrame(() => searchRef.current?.focus());
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, searchable]);

  // Open with the highlight already on the current selection.
  const openMenu = () => {
    const idx = options.findIndex((o) => o.value === value);
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

  const pick = (v: string) => {
    onChange(v);
    setOpen(false);
    setQuery("");
    triggerRef.current?.focus();
  };

  const onTriggerKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      openMenu();
    }
  };

  const onListKey = (e: React.KeyboardEvent) => {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (filtered[active]) pick(filtered[active].value);
    }
  };

  const chevron = (
    <ChevronDown className={`w-4 h-4 text-stone-400 shrink-0 transition-transform duration-200 ${open ? "rotate-180 text-kiwi-600" : ""}`} />
  );

  return (
    <div ref={rootRef} className={`relative ${variant === "field" ? "w-full" : "inline-flex"} ${open ? "z-50" : ""}`}>
      {variant === "chip" ? (
        <button
          ref={triggerRef}
          type="button"
          role="combobox"
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={listboxId}
          aria-label={ariaLabel || label || placeholder}
          onClick={toggle}
          onKeyDown={onTriggerKeyDown}
          className={`flex items-center gap-1.5 px-3 py-1.5 rounded-xl bg-white hover:bg-sand-50 border border-sand-200 text-xs font-medium text-stone-800 shadow-2xs hover:border-kiwi-300 transition-all cursor-pointer ${className}`}
        >
          {icon}
          {label && <span className="text-[10px] font-mono uppercase text-stone-400 font-bold">{label}</span>}
          <span className={`truncate max-w-[170px] ${selected ? "text-stone-900 font-semibold" : "text-stone-400"}`}>{display}</span>
          {chevron}
        </button>
      ) : (
        <button
          ref={triggerRef}
          type="button"
          role="combobox"
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={listboxId}
          aria-label={ariaLabel || label || placeholder}
          onClick={toggle}
          onKeyDown={onTriggerKeyDown}
          className={`w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 hover:bg-white hover:border-kiwi-300 focus:bg-white focus:border-kiwi-500 focus:ring-2 focus:ring-kiwi-100 border border-sand-200 text-left flex items-center justify-between gap-2 shadow-2xs transition-all cursor-pointer group ${className}`}
        >
          <span className="flex items-center gap-2.5 min-w-0">
            {selected?.icon || icon}
            <span className={`truncate text-xs ${selected ? "text-stone-900 font-bold" : "text-stone-400 font-medium"}`}>{display}</span>
          </span>
          <div className="flex items-center gap-1.5 shrink-0">
            {selected?.badge}
            {chevron}
          </div>
        </button>
      )}

      {open && (
        <div
          id={listboxId}
          role="listbox"
          aria-label={ariaLabel || label || placeholder}
          onKeyDown={onListKey}
          className={`absolute top-full left-0 mt-1.5 z-[999] rounded-2xl border border-sand-300 bg-white shadow-2xl p-2 space-y-2 ring-1 ring-black/10 animate-in fade-in zoom-in-95 duration-150 ${
            variant === "field" ? "w-full min-w-0" : "min-w-[280px] w-max max-w-[380px]"
          }`}
        >
          {searchable && (
            <div className="flex items-center gap-2 px-3 py-1.5 rounded-xl bg-sand-50 border border-sand-200 focus-within:border-kiwi-400 focus-within:bg-white focus-within:ring-2 focus-within:ring-kiwi-100 transition-all">
              <Search className="w-3.5 h-3.5 text-stone-400 shrink-0" />
              <input
                ref={searchRef}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setActive(0);
                }}
                placeholder="Search by name or provider…"
                className="bg-transparent outline-none border-0 text-xs text-stone-900 placeholder:text-stone-400 w-full font-medium"
              />
            </div>
          )}

          <div ref={listRef} className="flex flex-col gap-1 h-44 overflow-y-auto pr-1">
            {filtered.length === 0 ? (
              <div className="flex items-center justify-center h-full text-xs text-stone-400 font-mono bg-sand-50/50 rounded-xl border border-sand-150 p-4">
                No matching models found.
              </div>
            ) : (
              filtered.map((o, i) => {
                const isSel = o.value === value;
                const isHover = i === active;
                return (
                  <button
                    key={o.value || "__empty"}
                    type="button"
                    role="option"
                    aria-selected={isSel}
                    data-idx={i}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => pick(o.value)}
                    className={`flex items-center justify-between gap-2.5 px-3 py-2 rounded-xl text-xs text-left transition-all cursor-pointer border ${
                      isSel
                        ? "bg-kiwi-50/90 text-kiwi-950 font-bold border-kiwi-300 shadow-2xs"
                        : isHover
                        ? "bg-sand-100/90 text-stone-900 border-sand-200"
                        : "bg-transparent text-stone-700 border-transparent hover:bg-sand-50"
                    }`}
                  >
                    <div className="flex items-center gap-2 min-w-0 flex-1">
                      <div className="w-3.5 h-3.5 flex items-center justify-center shrink-0">
                        {isSel ? (
                          <Check className="w-3.5 h-3.5 text-kiwi-700 stroke-[3]" />
                        ) : (
                          o.icon || <div className="w-1.5 h-1.5 rounded-full bg-sand-300" />
                        )}
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className={`truncate ${isSel ? "text-kiwi-950 font-bold" : "text-stone-900 font-semibold"}`}>
                          {o.label}
                        </div>
                        {o.sublabel && (
                          <div className="text-[10px] text-stone-400 font-mono truncate leading-none mt-0.5">{o.sublabel}</div>
                        )}
                      </div>
                    </div>

                    <div className="flex items-center gap-1.5 shrink-0">
                      {o.badge}
                      {o.hint && (
                        <span className="text-[10px] font-mono font-semibold px-1.5 py-0.5 rounded bg-sand-100 text-stone-600 border border-sand-200">
                          {o.hint}
                        </span>
                      )}
                    </div>
                  </button>
                );
              })
            )}
          </div>

          {renderDetail && (
            <div className="pt-2 border-t border-sand-200 bg-sand-50/80 -mx-2 -mb-2 p-2.5 rounded-b-2xl">
              {filtered[active] ? (
                renderDetail(filtered[active])
              ) : (
                <div className="text-[10px] text-stone-400 font-mono text-center py-1">
                  Select a model to view capabilities
                </div>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  );
}
