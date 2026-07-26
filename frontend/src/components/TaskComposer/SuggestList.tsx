import React, { forwardRef, useEffect, useImperativeHandle, useState } from 'react';

export interface SuggestionItem {
  kind: string;
  value: string;
  label: string;
  status?: string; // For jobs
  sublabel?: string; // E.g., for branches or files
  disabled?: boolean;
}

export interface SuggestListRef {
  onKeyDown: (props: { event: KeyboardEvent }) => boolean;
}

export interface SuggestListProps {
  items: SuggestionItem[];
  command: (item: SuggestionItem) => void;
}

export const SuggestList = forwardRef<SuggestListRef, SuggestListProps>((props, ref) => {
  const [selectedIndex, setSelectedIndex] = useState(0);

  useEffect(() => {
    setSelectedIndex(0);
  }, [props.items]);

  useImperativeHandle(ref, () => ({
    onKeyDown: ({ event }) => {
      if (event.key === 'ArrowUp') {
        event.preventDefault();
        setSelectedIndex((selectedIndex + props.items.length - 1) % props.items.length);
        return true;
      }

      if (event.key === 'ArrowDown') {
        event.preventDefault();
        setSelectedIndex((selectedIndex + 1) % props.items.length);
        return true;
      }

      if (event.key === 'Enter') {
        event.preventDefault();
        const item = props.items[selectedIndex];
        if (item && !item.disabled) {
          props.command(item);
        }
        return true;
      }

      return false;
    },
  }));

  if (!props.items.length) {
    return null;
  }

  return (
    <div className="pr-popover z-50 min-w-[240px] rounded-xl border border-white/10 bg-[#0E1A24]/95 backdrop-blur-xl shadow-[0_24px_60px_-16px_rgba(0,0,0,0.85)] p-1.5 overflow-y-auto max-h-64">
      <div className="flex flex-col gap-0.5">
        {props.items.map((item, index) => (
          <button
            key={index}
            onClick={() => !item.disabled && props.command(item)}
            className={`flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-sm text-left transition-colors ${
              index === selectedIndex ? 'bg-white/[0.07]' : ''
            } ${item.disabled ? 'opacity-50 cursor-not-allowed' : 'hover:bg-white/[0.04]'}`}
          >
            <div className="flex flex-col min-w-0">
              <span className="truncate text-zinc-200">
                {item.label}
              </span>
              {item.sublabel && (
                <span className="truncate text-[11px] text-zinc-500">
                  {item.sublabel}
                </span>
              )}
            </div>
          </button>
        ))}
      </div>
    </div>
  );
});

SuggestList.displayName = "SuggestList";
