"use client";

import React, { useMemo } from "react";
import { ThinkingOrb as BaseThinkingOrb } from "thinking-orbs";
import type { OrbState } from "@/lib/orbState";

export interface ThinkingOrbProps {
  state?: OrbState;
  size?: number;
  className?: string;
  theme?: "kiwi" | "mono" | "default";
  glow?: boolean;
  "aria-hidden"?: boolean;
  "aria-label"?: string;
}

export function ThinkingOrb({
  state = "working",
  size = 64,
  className = "",
  theme = "kiwi",
  glow = true,
  "aria-hidden": ariaHiddenProp,
  "aria-label": ariaLabel,
}: ThinkingOrbProps) {
  const ariaHidden = ariaHiddenProp !== undefined ? ariaHiddenProp : !ariaLabel;
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

  const showGlow = glow && theme !== "mono";

  return (
    <div
      role={ariaLabel ? "img" : undefined}
      aria-label={ariaLabel}
      className={`relative inline-flex items-center justify-center ${className}`}
    >
      {showGlow && (
        <div
          className="absolute rounded-full bg-lime-400/25 blur-xl pointer-events-none animate-pulse"
          style={{
            width: size * 1.5,
            height: size * 1.5,
          }}
        />
      )}
      <div
        className={`relative z-10 inline-flex shrink-0 items-center justify-center ${themeClass}`}
        style={style}
        aria-hidden={ariaHidden}
      >
        <BaseThinkingOrb state={state} size={basePreset} aria-hidden={ariaHidden} />
      </div>
    </div>
  );
}
