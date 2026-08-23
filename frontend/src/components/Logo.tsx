export type KiwiLogoVariant = "full-color" | "monochrome" | "app-icon" | "wordmark";
export type KiwiPose = "idle" | "vibing" | "dancing" | "flying" | "hacking" | "guarding" | "sleeping";

export interface LogoProps extends React.SVGProps<SVGSVGElement> {
  variant?: KiwiLogoVariant;
  pose?: KiwiPose;
  size?: number | string;
  className?: string;
  animated?: boolean;
}

/**
 * Official Kiwi 8-bit chunky chibi logo mark and mascot variants.
 * Source: https://github.com/RunKiwi/kiwi-brand
 */
export function Logo({
  variant = "full-color",
  pose = "idle",
  size,
  className = "w-5 h-5",
  animated = false,
  style,
  ...props
}: LogoProps) {
  // Animation classes
  const getPoseAnimationClass = () => {
    if (!animated && pose === "idle") return "";
    switch (pose) {
      case "dancing":
        return "animate-kiwi-dance";
      case "flying":
        return "animate-kiwi-float";
      case "sleeping":
        return "animate-kiwi-sleep";
      case "vibing":
      case "hacking":
      case "guarding":
      case "idle":
      default:
        return animated ? "animate-kiwi-bob" : "";
    }
  };

  const rootAnimation = getPoseAnimationClass();

  // 1. App Icon with dark glowing gradient tile
  if (variant === "app-icon") {
    return (
      <svg
        viewBox="0 0 512 512"
        width={size}
        height={size}
        shapeRendering="crispEdges"
        className={`${className} ${rootAnimation}`}
        style={{ imageRendering: "pixelated", ...style }}
        aria-hidden="true"
        {...props}
      >
        <defs>
          <linearGradient id="appTileGrad" x1="0%" y1="0%" x2="0%" y2="100%">
            <stop offset="0%" stopColor="#152433" />
            <stop offset="100%" stopColor="#080E14" />
          </linearGradient>
          <radialGradient id="appGlow" cx="50%" cy="35%" r="60%">
            <stop offset="0%" stopColor="#93C645" stopOpacity="0.3" />
            <stop offset="100%" stopColor="#080E14" stopOpacity="0" />
          </radialGradient>
        </defs>

        <rect x="8" y="8" width="496" height="496" rx="112" fill="url(#appTileGrad)" />
        <rect x="8" y="8" width="496" height="496" rx="112" fill="url(#appGlow)" />
        <rect x="8" y="8" width="496" height="496" rx="112" fill="none" stroke="#93C645" strokeWidth="3" strokeOpacity="0.4" />

        <g transform="translate(96, 96) scale(20)">
          <rect x="5" y="2" width="5" height="1" fill="#88BC38" />
          <rect x="3" y="3" width="8" height="1" fill="#93C645" />
          <rect x="2" y="4" width="9" height="1" fill="#93C645" />
          <rect x="2" y="5" width="9" height="1" fill="#93C645" />
          <rect x="1" y="6" width="10" height="1" fill="#93C645" />
          <rect x="1" y="7" width="10" height="1" fill="#93C645" />
          <rect x="2" y="8" width="9" height="1" fill="#88BC38" />
          <rect x="2" y="9" width="9" height="1" fill="#78A832" />
          <rect x="3" y="10" width="7" height="1" fill="#6A962A" />
          <rect x="4" y="11" width="5" height="1" fill="#5A8222" />
          <rect x="7" y="4" width="2" height="2" fill="#111816" />
          <rect x="7" y="4" width="1" height="1" fill="#FFFFFF" />
          <rect x="11" y="5" width="3" height="2" fill="#FFAA28" />
          <rect x="14" y="6" width="1" height="1" fill="#E89115" />
          <rect x="4" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="3" y="13" width="3" height="1" fill="#FFAA28" />
          <rect x="8" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="7" y="13" width="3" height="1" fill="#FFAA28" />
        </g>
      </svg>
    );
  }

  // 2. Monochrome Mark
  if (variant === "monochrome") {
    return (
      <svg
        viewBox="0 0 16 16"
        width={size}
        height={size}
        className={`${className} ${rootAnimation}`}
        fill="currentColor"
        shapeRendering="crispEdges"
        style={{ imageRendering: "pixelated", ...style }}
        aria-hidden="true"
        {...props}
      >
        <rect x="5" y="2" width="5" height="1" opacity={0.8} />
        <rect x="3" y="3" width="8" height="1" opacity={0.9} />
        <rect x="2" y="4" width="5" height="1" opacity={1.0} />
        <rect x="9" y="4" width="2" height="1" opacity={1.0} />
        <rect x="2" y="5" width="9" height="1" opacity={1.0} />
        <rect x="1" y="6" width="10" height="1" opacity={0.95} />
        <rect x="1" y="7" width="10" height="1" opacity={0.95} />
        <rect x="2" y="8" width="9" height="1" opacity={0.85} />
        <rect x="2" y="9" width="9" height="1" opacity={0.8} />
        <rect x="3" y="10" width="7" height="1" opacity={0.7} />
        <rect x="4" y="11" width="5" height="1" opacity={0.6} />
        <rect x="11" y="5" width="3" height="2" opacity={0.95} />
        <rect x="14" y="6" width="1" height="1" opacity={0.8} />
        <rect x="4" y="12" width="2" height="2" opacity={0.9} />
        <rect x="3" y="13" width="3" height="1" opacity={0.9} />
        <rect x="8" y="12" width="2" height="2" opacity={0.9} />
        <rect x="7" y="13" width="3" height="1" opacity={0.9} />
        <rect x="7" y="4" width="1" height="1" fill="#FFFFFF" opacity={0.95} />
      </svg>
    );
  }

  // 3. Wordmark (Chibi Mark + "kiwi" typographic wordmark)
  if (variant === "wordmark") {
    return (
      <div className={`inline-flex items-center gap-2 select-none ${className}`}>
        <svg
          viewBox="0 0 16 16"
          width={size ?? 22}
          height={size ?? 22}
          shapeRendering="crispEdges"
          className={rootAnimation}
          style={{ imageRendering: "pixelated" }}
          aria-hidden="true"
        >
          <rect x="5" y="2" width="5" height="1" fill="#88BC38" />
          <rect x="3" y="3" width="8" height="1" fill="#93C645" />
          <rect x="2" y="4" width="9" height="1" fill="#93C645" />
          <rect x="2" y="5" width="9" height="1" fill="#93C645" />
          <rect x="1" y="6" width="10" height="1" fill="#93C645" />
          <rect x="1" y="7" width="10" height="1" fill="#93C645" />
          <rect x="2" y="8" width="9" height="1" fill="#88BC38" />
          <rect x="2" y="9" width="9" height="1" fill="#78A832" />
          <rect x="3" y="10" width="7" height="1" fill="#6A962A" />
          <rect x="4" y="11" width="5" height="1" fill="#5A8222" />
          <rect x="7" y="4" width="2" height="2" fill="#111816" />
          <rect x="7" y="4" width="1" height="1" fill="#FFFFFF" />
          <rect x="11" y="5" width="3" height="2" fill="#FFAA28" />
          <rect x="14" y="6" width="1" height="1" fill="#E89115" />
          <rect x="4" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="3" y="13" width="3" height="1" fill="#FFAA28" />
          <rect x="8" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="7" y="13" width="3" height="1" fill="#FFAA28" />
        </svg>
        <span className="font-mono font-bold text-stone-900 tracking-tight text-sm leading-none">
          kiwi
        </span>
      </div>
    );
  }

  // 4. Default: Official Multi-Color 8-Bit Chibi Logo with pose accessories and animations
  return (
    <svg
      viewBox="0 0 16 16"
      width={size}
      height={size}
      shapeRendering="crispEdges"
      className={`${className} ${rootAnimation}`}
      style={{ imageRendering: "pixelated", ...style }}
      aria-hidden="true"
      {...props}
    >
      {/* Body Greens */}
      <rect x="5" y="2" width="5" height="1" fill="#88BC38" />
      <rect x="3" y="3" width="8" height="1" fill="#93C645" />
      <rect x="2" y="4" width="9" height="1" fill="#93C645" />
      <rect x="2" y="5" width="9" height="1" fill="#93C645" />
      <rect x="1" y="6" width="10" height="1" fill="#93C645" />
      <rect x="1" y="7" width="10" height="1" fill="#93C645" />
      <rect x="2" y="8" width="9" height="1" fill="#88BC38" />
      <rect x="2" y="9" width="9" height="1" fill="#78A832" />
      <rect x="3" y="10" width="7" height="1" fill="#6A962A" />
      <rect x="4" y="11" width="5" height="1" fill="#5A8222" />

      {/* Eyes based on pose */}
      {pose === "sleeping" ? (
        <rect x="7" y="5" width="3" height="1" fill="#111816" />
      ) : pose === "dancing" ? (
        <>
          <rect x="7" y="4" width="3" height="1" fill="#111816" />
          <rect x="6" y="5" width="1" height="1" fill="#111816" />
          <rect x="10" y="5" width="1" height="1" fill="#111816" />
        </>
      ) : (
        <>
          <rect x="7" y="4" width="2" height="2" fill="#111816" />
          <rect x="7" y="4" width="1" height="1" fill="#FFFFFF" />
        </>
      )}

      {/* Beak */}
      <rect x="11" y="5" width="3" height="2" fill="#FFAA28" />
      <rect x="14" y="6" width="1" height="1" fill="#E89115" />

      {/* Feet based on pose */}
      {pose === "dancing" ? (
        <>
          <rect x="3" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="2" y="13" width="3" height="1" fill="#FFAA28" />
          <rect x="9" y="11" width="2" height="2" fill="#FFAA28" />
          <rect x="9" y="12" width="3" height="1" fill="#FFAA28" />
        </>
      ) : pose === "flying" ? (
        <>
          <rect x="4" y="11" width="3" height="1" fill="#FFAA28" />
          <rect x="8" y="11" width="3" height="1" fill="#FFAA28" />
        </>
      ) : (
        <>
          <rect x="4" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="3" y="13" width="3" height="1" fill="#FFAA28" />
          <rect x="8" y="12" width="2" height="2" fill="#FFAA28" />
          <rect x="7" y="13" width="3" height="1" fill="#FFAA28" />
        </>
      )}

      {/* Pose Accessories with individual micro-animations */}
      {pose === "vibing" && (
        <g className={animated ? "animate-kiwi-visor" : ""}>
          <rect x="4" y="1" width="5" height="1" fill="#FF4D6D" />
          <rect x="3" y="2" width="1" height="3" fill="#FF4D6D" />
          <rect x="9" y="2" width="1" height="3" fill="#FF4D6D" />
          <rect x="2" y="4" width="1" height="3" fill="#00E5FF" />
          <rect x="9" y="4" width="1" height="3" fill="#00E5FF" />
          <rect x="6" y="4" width="4" height="2" fill="#090D0B" />
          <rect x="6" y="4" width="1" height="1" fill="#00E5FF" />
          <rect x="8" y="4" width="1" height="1" fill="#00E5FF" />
        </g>
      )}

      {pose === "hacking" && (
        <g className={animated ? "animate-kiwi-hack" : ""}>
          <rect x="7" y="3" width="3" height="3" fill="none" stroke="#FFAA28" strokeWidth={0.8} />
          <rect x="10" y="6" width="1" height="1" fill="#FFAA28" />
        </g>
      )}

      {pose === "guarding" && (
        <g className={animated ? "animate-kiwi-shield" : ""}>
          <rect x="0" y="4" width="3" height="5" fill="#4FB477" />
          <rect x="1" y="5" width="1" height="3" fill="#FFFFFF" />
        </g>
      )}

      {pose === "flying" && (
        <g>
          <rect x="0" y="5" width="2" height="2" fill="#3D5A12" />
          <g className={animated ? "animate-kiwi-flame" : ""}>
            <rect x="0" y="7" width="1" height="3" fill="#FF4D6D" />
            <rect x="1" y="7" width="1" height="2" fill="#FFAA28" />
            <rect x="0" y="10" width="1" height="2" fill="#FFB703" />
          </g>
        </g>
      )}
    </svg>
  );
}
