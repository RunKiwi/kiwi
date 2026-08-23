"use client";

import React, { useMemo } from "react";
import { ThinkingOrb as BaseThinkingOrb } from "thinking-orbs";
import type { OrbState } from "@/lib/orbState";

export interface ThinkingOrbProps {
  state?: OrbState;
  size?: number;
  className?: string;
  theme?: "kiwi" | "mono" | "default";
  "aria-hidden"?: boolean;
}

export function ThinkingOrb({
  state = "working",
  size = 64,
  className = "",
  theme = "kiwi",
  "aria-hidden": ariaHidden = true,
}: ThinkingOrbProps) {
  const basePreset = size <= 32 ? 20 : 64;
  const scale = size / basePreset;

  const style = useMemo<React.CSSProperties>(() => {
    if (size === 20 || size === 64) {
      return { width: size, height: size };
    }
    return {
      width: size,
      height: size,
      display: "inline-flex",
      alignItems: "center",
      justifyContent: "center",
      transform: `scale(${scale})`,
      transformOrigin: "center center",
    };
  }, [size, scale]);

  const themeClass = useMemo(() => {
    switch (theme) {
      case "kiwi":
        return "filter drop-shadow-[0_0_10px_rgba(101,163,13,0.3)]";
      case "mono":
        return "grayscale contrast-125";
      default:
        return "";
    }
  }, [theme]);

  return (
    <div
      className={`inline-flex shrink-0 items-center justify-center ${themeClass} ${className}`}
      style={style}
      aria-hidden={ariaHidden}
    >
      <BaseThinkingOrb state={state} size={basePreset} aria-hidden={ariaHidden} />
    </div>
  );
}
