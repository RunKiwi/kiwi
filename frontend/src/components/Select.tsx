"use client";

import { useEffect, useMemo, useRef, useState, useId } from "react";
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
  /**
   * Renders a detail panel under the list for whichever option is currently
   * highlighted — by hover, by arrow key, or by being the current selection
   * when the menu opens.
   *
   * A callback rather than a field on SelectOption so the caller only builds
   * the panel for the one row being looked at. With a hundred-odd models that
   * is the difference between one node and a hundred.
   */
  renderDetail?: (option: SelectOption) => React.ReactNode;
}

// A single custom dropdown used across the dashboard: sand/white popover menu,
// kiwi accent, keyboard nav, and optional type-to-filter search. Replaces
// native <select> so the menu matches the product design and can be searched.
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
    return options.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q));
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
    <ChevronDown className={`w-3.5 h-3.5 text-stone-400 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} />
  );

  return (
    <div ref={rootRef} className={`relative ${variant === "field" ? "w-full" : "inline-flex"}`}>
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
          className={`flex items-center gap-1.5 px-2.5 py-1 rounded-xl bg-white hover:bg-sand-50 border border-sand-200 text-xs font-medium text-stone-700 shadow-2xs transition-all cursor-pointer ${className}`}
        >
          {icon}
          {label && <span className="text-[10px] font-mono uppercase text-stone-400 font-bold">{label}</span>}
          <span className={`truncate max-w-[170px] ${selected ? "text-stone-900" : "text-stone-400"}`}>{display}</span>
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
          className={`w-full px-3.5 py-2.5 rounded-xl bg-sand-50/90 hover:bg-white hover:border-sand-300 border border-sand-200 text-left flex items-center justify-between gap-2 shadow-xs transition-all ${className}`}
        >
          <span className="flex items-center gap-2 min-w-0">
            {icon}
            <span className={`truncate text-sm ${selected ? "text-stone-900 font-medium" : "text-stone-400"}`}>{display}</span>
          </span>
          {chevron}
        </button>
      )}

      {open && (
        <div
          id={listboxId}
          role="listbox"
          aria-label={ariaLabel || label || placeholder}
          onKeyDown={onListKey}
          className="absolute top-full left-0 mt-1.5 z-50 min-w-[240px] w-max max-w-[340px] rounded-2xl border border-sand-200 bg-white shadow-popover p-1.5"
        >
          {searchable && (
            <div className="flex items-center gap-2 px-2.5 py-1.5 mb-1 rounded-xl bg-sand-50/60 border border-sand-200">
              <Search className="w-3.5 h-3.5 text-stone-400 shrink-0" />
              <input
                ref={searchRef}
                value={query}
                onChange={(e) => {
                  setQuery(e.target.value);
                  setActive(0);
                }}
                placeholder="Search…"
                className="bg-transparent outline-none border-0 text-sm text-stone-900 placeholder:text-stone-400 w-full"
              />
            </div>
          )}
          <div ref={listRef} className="flex flex-col gap-0.5 max-h-64 overflow-y-auto">
            {filtered.length === 0 ? (
              <div className="px-2.5 py-3 text-xs text-stone-400 text-center">No matches</div>
            ) : (
              filtered.map((o, i) => {
                const isSel = o.value === value;
                return (
                  <button
                    key={o.value || "__empty"}
                    type="button"
                    role="option"
                    aria-selected={isSel}
                    data-idx={i}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => pick(o.value)}
                    className={`flex items-center gap-2.5 px-2.5 py-2 rounded-xl text-sm text-left transition-colors ${
                      i === active ? "bg-sand-100" : ""
                    }`}
                  >
                    <Check className={`w-3.5 h-3.5 shrink-0 ${isSel ? "text-kiwi-600" : "text-transparent"}`} />
                    <span className={`truncate flex-1 ${isSel ? "text-stone-900 font-semibold" : "text-stone-700"}`}>{o.label}</span>
                    {o.hint && <span className="text-[11px] text-stone-400 shrink-0">{o.hint}</span>}
                  </button>
                );
              })
            )}
          </div>
          {renderDetail && filtered[active] && (
            <div className="mt-1 pt-2.5 border-t border-sand-200">{renderDetail(filtered[active])}</div>
          )}
        </div>
      )}
    </div>
  );
}
