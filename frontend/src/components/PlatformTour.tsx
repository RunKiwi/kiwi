"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import { useSearchParams, useRouter, usePathname } from "next/navigation";
import {
  Sparkles,
  ChevronRight,
  ChevronLeft,
  X,
  CheckCircle2,
  LayoutGrid,
  Radar,
  Server,
  Receipt,
  Plus,
  ArrowRight,
} from "lucide-react";
import { Logo, type KiwiPose } from "@/components/Logo";

export interface TourStep {
  id: string;
  title: string;
  subtitle: string;
  description: string;
  badge: string;
  targetQuery?: string;
  preferredPlacement?: "right" | "bottom" | "top" | "left" | "center";
  icon: React.ReactNode;
  pose?: KiwiPose;
  actionLabel?: string;
  actionHref?: string;
  highlights: string[];
}

const TOUR_STEPS: TourStep[] = [
  {
    id: "tasks-queue",
    title: "Autonomous Swarm Queue",
    subtitle: "Real-time task execution and testing",
    description:
      "Your central execution board. Watch autonomous agent swarms plan changes, edit code, and verify tests inside isolated sandboxes with cryptographically verifiable audit receipts.",
    badge: "CORE ENGINE",
    targetQuery: '[data-tour="tasks-queue"]',
    preferredPlacement: "bottom",
    icon: <LayoutGrid className="w-4 h-4 text-emerald-600" />,
    pose: "vibing",
    highlights: ["Live agent tool calls", "Interactive unified git diffs", "Cryptographic task receipts"],
  },
  {
    id: "nav-monitors",
    title: "PR Watchdogs & Monitors",
    subtitle: "Automated PR triage and continuous review",
    description:
      "Kiwi can autonomously monitor pull requests across your repositories. When test failures or reviews occur, watchdogs automatically investigate, fix regressions, and pass CI.",
    badge: "CONTINUOUS REVIEW",
    targetQuery: '[data-tour="nav-monitors"]',
    preferredPlacement: "right",
    icon: <Radar className="w-4 h-4 text-sky-600" />,
    pose: "guarding",
    actionLabel: "View Watchdogs",
    actionHref: "/monitors",
    highlights: ["Background repo monitoring", "Automatic test failure fixes", "GitHub PR auto-commentary"],
  },
  {
    id: "nav-fleet",
    title: "Execution Fleets & Runners",
    subtitle: "Ephemeral cloud sandboxes & BYOC private runners",
    description:
      "Run agents in managed Kiwi Cloud pools or connect your own private daemons inside your secure VPC using `kiwidaemon join`. Inspect CPU/RAM load and real-time hardware meters.",
    badge: "HYBRID COMPUTE",
    targetQuery: '[data-tour="nav-fleet"]',
    preferredPlacement: "right",
    icon: <Server className="w-4 h-4 text-purple-600" />,
    pose: "hacking",
    actionLabel: "Explore Fleets",
    actionHref: "/fleet",
    highlights: ["Micro-VM sandbox isolation", "BYOC private VPC runners", "Live server-rack meters"],
  },
  {
    id: "nav-spend",
    title: "Spend & Model Intelligence",
    subtitle: "Token quotas, model intelligence & spend caps",
    description:
      "Track compute minutes and token usage in real time. Kiwi provides generous platform quotas for frontier models (Claude 3.7 Sonnet, GPT-4.5, Gemini 2.0) with strict spend cap protection.",
    badge: "TOKEN GOVERNANCE",
    targetQuery: '[data-tour="nav-spend"]',
    preferredPlacement: "right",
    icon: <Receipt className="w-4 h-4 text-amber-600" />,
    pose: "vibing",
    actionLabel: "View Spend",
    actionHref: "/spend",
    highlights: ["Platform model quotas", "Custom budget spend caps", "Cost-per-run telemetry"],
  },
  {
    id: "new-task-btn",
    title: "Launch Autonomous Tasks",
    subtitle: "Compose goals with Architect & Worker models",
    description:
      "Ready to build? Click '+ New Task' or press ⌘K anywhere. Pair high-reasoning Architect models with lightning-fast Worker models to execute complex features.",
    badge: "TASK COMPOSER",
    targetQuery: '[data-tour="new-task-btn"]',
    preferredPlacement: "right",
    icon: <Plus className="w-4 h-4 text-kiwi-600" />,
    pose: "dancing",
    actionLabel: "Open Composer",
    actionHref: "/composer",
    highlights: ["Architect + Worker pairing", "Human-in-the-loop plan review", "Instant GitHub branch creation"],
  },
];

export function PlatformTour() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const [isOpen, setIsOpen] = useState(() => {
    if (typeof window !== "undefined") {
      const params = new URLSearchParams(window.location.search);
      return params.get("tour") === "true";
    }
    return false;
  });
  const [currentStepIndex, setCurrentStepIndex] = useState(0);
  const [targetRect, setTargetRect] = useState<DOMRect | null>(null);
  const [windowSize, setWindowSize] = useState({ width: 1200, height: 800 });

  const currentStep = TOUR_STEPS[currentStepIndex];

  const handleClose = useCallback(() => {
    setIsOpen(false);
    if (typeof window !== "undefined") {
      localStorage.setItem("kiwi_tour_completed", "1");
    }
  }, []);

  const handleNext = useCallback(() => {
    if (currentStepIndex < TOUR_STEPS.length - 1) {
      setCurrentStepIndex((prev) => prev + 1);
    } else {
      handleClose();
    }
  }, [currentStepIndex, handleClose]);

  const handlePrev = useCallback(() => {
    if (currentStepIndex > 0) {
      setCurrentStepIndex((prev) => prev - 1);
    }
  }, [currentStepIndex]);

  // Clean URL query ?tour=true on mount or navigation if present
  useEffect(() => {
    if (searchParams.get("tour") === "true") {
      const nextUrl = window.location.pathname;
      window.history.replaceState({}, "", nextUrl);
    }
  }, [searchParams]);

  // Listen for global custom event to trigger tour anytime (e.g. from ⌘K or Help)
  useEffect(() => {
    const handleStartTour = () => {
      if (pathname !== "/") {
        router.push("/?tour=true");
      } else {
        setIsOpen(true);
        setCurrentStepIndex(0);
      }
    };

    window.addEventListener("kiwi:start-tour", handleStartTour);
    return () => window.removeEventListener("kiwi:start-tour", handleStartTour);
  }, [pathname, router]);

  // Update spotlight target element rectangle and window size
  const updateSpotlight = useCallback(() => {
    if (typeof window !== "undefined") {
      setWindowSize({ width: window.innerWidth, height: window.innerHeight });
    }
    if (!isOpen || !currentStep?.targetQuery) {
      setTargetRect(null);
      return;
    }
    const el = document.querySelector(currentStep.targetQuery);
    if (el) {
      const rect = el.getBoundingClientRect();
      setTargetRect(rect);
    } else {
      setTargetRect(null);
    }
  }, [isOpen, currentStep]);

  useEffect(() => {
    const rafId = requestAnimationFrame(updateSpotlight);
    window.addEventListener("resize", updateSpotlight);
    window.addEventListener("scroll", updateSpotlight);
    return () => {
      cancelAnimationFrame(rafId);
      window.removeEventListener("resize", updateSpotlight);
      window.removeEventListener("scroll", updateSpotlight);
    };
  }, [updateSpotlight]);

  // Keyboard navigation for tour
  useEffect(() => {
    if (!isOpen) return;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        handleClose();
      } else if (e.key === "ArrowRight") {
        e.preventDefault();
        handleNext();
      } else if (e.key === "ArrowLeft") {
        e.preventDefault();
        handlePrev();
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, handleClose, handleNext, handlePrev]);

  // Calculate anchored popover placement and arrow alignment
  const popoverStyle = useMemo(() => {
    const isMobile = windowSize.width < 768;
    const cardWidth = Math.min(420, windowSize.width - 32);

    if (isMobile || !targetRect) {
      // Mobile: anchored to bottom sheet
      return {
        cardStyle: {
          position: "fixed" as const,
          bottom: "16px",
          left: "16px",
          right: "16px",
          maxWidth: "480px",
          margin: "0 auto",
          zIndex: 60,
        },
        arrowSide: null as null | "left" | "top" | "bottom" | "right",
        arrowTop: 0,
      };
    }

    const preferred = currentStep.preferredPlacement || "right";

    if (preferred === "right") {
      const left = Math.min(targetRect.right + 14, windowSize.width - cardWidth - 16);
      const top = Math.min(
        Math.max(16, targetRect.top - 20),
        windowSize.height - 380
      );
      const arrowTop = Math.max(
        24,
        Math.min(targetRect.top + targetRect.height / 2 - top, 340)
      );

      return {
        cardStyle: {
          position: "fixed" as const,
          left: `${left}px`,
          top: `${top}px`,
          width: `${cardWidth}px`,
          zIndex: 60,
        },
        arrowSide: "left" as const,
        arrowTop,
      };
    }

    if (preferred === "bottom") {
      const top = Math.min(targetRect.bottom + 14, windowSize.height - 380);
      const left = Math.min(
        Math.max(16, targetRect.left + (targetRect.width - cardWidth) / 2),
        windowSize.width - cardWidth - 16
      );

      return {
        cardStyle: {
          position: "fixed" as const,
          left: `${left}px`,
          top: `${top}px`,
          width: `${cardWidth}px`,
          zIndex: 60,
        },
        arrowSide: "top" as const,
        arrowTop: 0,
      };
    }

    // Default Fallback
    return {
      cardStyle: {
        position: "fixed" as const,
        left: "50%",
        top: "50%",
        transform: "translate(-50%, -50%)",
        width: `${cardWidth}px`,
        zIndex: 60,
      },
      arrowSide: null,
      arrowTop: 0,
    };
  }, [windowSize, targetRect, currentStep]);

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 overflow-hidden font-sans select-none pointer-events-auto">
      {/* SVG Masked Dimming Overlay: 0% blur, crystal clear cutout over target */}
      <svg
        className="fixed inset-0 w-full h-full pointer-events-auto z-40 transition-all duration-300"
        onClick={handleClose}
      >
        <defs>
          <mask id="tour-spotlight-mask">
            {/* White base = dimmed area */}
            <rect x="0" y="0" width="100%" height="100%" fill="white" />
            {/* Black rectangle = 100% transparent cutout hole for target element */}
            {targetRect && (
              <rect
                x={Math.max(4, targetRect.left - 6)}
                y={Math.max(4, targetRect.top - 6)}
                width={targetRect.width + 12}
                height={targetRect.height + 12}
                rx="12"
                ry="12"
                fill="black"
              />
            )}
          </mask>
        </defs>
        {/* Soft, non-blurred dark fill with masked cutout */}
        <rect
          x="0"
          y="0"
          width="100%"
          height="100%"
          fill="rgba(15, 15, 14, 0.40)"
          mask="url(#tour-spotlight-mask)"
        />
      </svg>

      {/* Target Element Spotlight Ring & Pulse Halo */}
      {targetRect && (
        <div
          className="fixed pointer-events-none transition-all duration-300 ease-out z-50 rounded-xl border-2 border-kiwi-400 ring-4 ring-kiwi-400/30 shadow-[0_0_20px_rgba(147,198,69,0.45)]"
          style={{
            top: `${Math.max(4, targetRect.top - 6)}px`,
            left: `${Math.max(4, targetRect.left - 6)}px`,
            width: `${targetRect.width + 12}px`,
            height: `${targetRect.height + 12}px`,
          }}
        />
      )}

      {/* Anchored Tour Popover Card */}
      <div
        style={popoverStyle.cardStyle}
        className="bg-white border border-sand-200/90 rounded-2xl shadow-popover p-5 sm:p-6 animate-in fade-in zoom-in-95 duration-200 flex flex-col relative"
      >
        {/* Pointer Arrow if anchored to element */}
        {popoverStyle.arrowSide === "left" && (
          <div
            className="absolute -left-2 w-4 h-4 bg-white border-l border-b border-sand-200/90 transform rotate-45 pointer-events-none"
            style={{ top: `${popoverStyle.arrowTop}px` }}
          />
        )}
        {popoverStyle.arrowSide === "top" && (
          <div className="absolute -top-2 left-8 w-4 h-4 bg-white border-l border-t border-sand-200/90 transform rotate-45 pointer-events-none" />
        )}

        {/* Top Header Strip */}
        <div className="flex items-center justify-between gap-2 pb-3 mb-3 border-b border-sand-200/80">
          <div className="flex items-center gap-2">
            <span className="text-[9px] font-mono font-bold uppercase tracking-wider text-kiwi-800 bg-kiwi-50 border border-kiwi-200 px-2 py-0.5 rounded flex items-center gap-1">
              <Sparkles className="w-3 h-3 text-kiwi-600" />
              <span>{currentStep.badge}</span>
            </span>
            <span className="text-[11px] font-mono text-stone-400 font-semibold">
              {currentStepIndex + 1} / {TOUR_STEPS.length}
            </span>
          </div>

          <button
            onClick={handleClose}
            className="p-1 rounded-lg text-stone-400 hover:text-stone-700 hover:bg-sand-100 transition-all cursor-pointer"
            title="Skip Tour (Esc)"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Title, Subtitle, & Reactive Mascot */}
        <div className="flex items-start gap-3.5 mb-3">
          <div className="w-11 h-11 rounded-xl bg-sand-50 border border-sand-200/90 flex items-center justify-center shrink-0 shadow-2xs">
            <Logo
              variant="full-color"
              pose={currentStep.pose || "vibing"}
              animated={true}
              className="w-6 h-6"
            />
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-1.5 mb-0.5">
              <div className="p-1 rounded-md bg-sand-100 border border-sand-200/80 shrink-0">
                {currentStep.icon}
              </div>
              <h3 className="text-sm sm:text-base font-bold text-stone-900 tracking-tight truncate">
                {currentStep.title}
              </h3>
            </div>
            <p className="text-[11px] font-mono text-stone-500 truncate">
              {currentStep.subtitle}
            </p>
          </div>
        </div>

        {/* Description Body */}
        <p className="text-xs text-stone-600 leading-relaxed font-sans mb-3">
          {currentStep.description}
        </p>

        {/* Key Feature Highlights Pill List */}
        <div className="space-y-1.5 p-2.5 rounded-xl bg-sand-50/70 border border-sand-200/80 mb-4 text-[11px]">
          {currentStep.highlights.map((h, i) => (
            <div key={i} className="flex items-center gap-1.5 text-stone-700">
              <div className="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0" />
              <span className="truncate">{h}</span>
            </div>
          ))}
        </div>

        {/* Direct Action Shortcut if available */}
        {currentStep.actionHref && (
          <div className="mb-3">
            <button
              onClick={() => {
                handleClose();
                if (currentStep.actionHref) router.push(currentStep.actionHref);
              }}
              className="w-full py-1.5 px-3 rounded-xl border border-sand-200 hover:bg-sand-100 text-stone-700 font-semibold text-xs transition-all flex items-center justify-between cursor-pointer"
            >
              <span className="text-[11px] font-mono">{currentStep.actionLabel}</span>
              <ArrowRight className="w-3.5 h-3.5 text-stone-400" />
            </button>
          </div>
        )}

        {/* Step Progress Indicators & Footer Controls */}
        <div className="flex items-center justify-between gap-2 pt-3 border-t border-sand-200/80">
          {/* Step Dots */}
          <div className="flex items-center gap-1">
            {TOUR_STEPS.map((step, idx) => (
              <button
                key={step.id}
                onClick={() => setCurrentStepIndex(idx)}
                className={`h-1.5 rounded-full transition-all cursor-pointer ${
                  idx === currentStepIndex
                    ? "w-5 bg-stone-900"
                    : idx < currentStepIndex
                    ? "w-2 bg-emerald-500"
                    : "w-2 bg-sand-200 hover:bg-sand-300"
                }`}
                title={`Jump to step ${idx + 1}: ${step.title}`}
              />
            ))}
          </div>

          {/* Nav Buttons */}
          <div className="flex items-center gap-2">
            {currentStepIndex > 0 && (
              <button
                onClick={handlePrev}
                className="px-2.5 py-1.5 rounded-xl border border-sand-200 hover:bg-sand-100 text-stone-700 font-semibold text-xs transition-all flex items-center gap-1 cursor-pointer"
              >
                <ChevronLeft className="w-3.5 h-3.5" />
                <span>Back</span>
              </button>
            )}

            <button
              onClick={handleNext}
              className="px-3.5 py-1.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-1.5 active:scale-[0.98] cursor-pointer"
            >
              <span>{currentStepIndex === TOUR_STEPS.length - 1 ? "Finish Tour" : "Next"}</span>
              {currentStepIndex === TOUR_STEPS.length - 1 ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-kiwi-400" />
              ) : (
                <ChevronRight className="w-3.5 h-3.5 text-kiwi-400" />
              )}
            </button>
          </div>
        </div>

        {/* Keyboard shortcut footer hint */}
        <div className="mt-2 text-center">
          <span className="text-[10px] font-mono text-stone-400">
            Use <kbd className="px-1 py-0.5 rounded bg-sand-100 border border-sand-200">←</kbd> <kbd className="px-1 py-0.5 rounded bg-sand-100 border border-sand-200">→</kbd> to navigate, <kbd className="px-1 py-0.5 rounded bg-sand-100 border border-sand-200">Esc</kbd> to skip
          </span>
        </div>
      </div>
    </div>
  );
}
