"use client";

import React, { useState, useEffect, useCallback } from "react";
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
} from "lucide-react";
import { Logo, type KiwiPose } from "@/components/Logo";

export interface TourStep {
  id: string;
  title: string;
  subtitle: string;
  description: string;
  targetQuery?: string;
  icon: React.ReactNode;
  pose?: KiwiPose;
  actionLabel?: string;
  actionHref?: string;
}

const TOUR_STEPS: TourStep[] = [
  {
    id: "tasks-queue",
    title: "Autonomous Swarm Queue",
    subtitle: "Real-time task execution and testing",
    description:
      "This is your command center. Watch autonomous agent swarms plan changes, edit code, and verify tests inside isolated sandboxes. Click any card to inspect full execution logs, live tool calls, and unified git diffs.",
    targetQuery: '[data-tour="tasks-queue"]',
    icon: <LayoutGrid className="w-4 h-4 text-emerald-600" />,
    pose: "vibing",
  },
  {
    id: "nav-monitors",
    title: "PR Watchdogs & Monitors",
    subtitle: "Automated PR triage and continuous review",
    description:
      "Kiwi can autonomously monitor pull requests across your GitHub repositories. When test failures or reviews occur, watchdogs automatically investigate, fix regressions, and pass CI.",
    targetQuery: '[data-tour="nav-monitors"]',
    icon: <Radar className="w-4 h-4 text-sky-600" />,
    pose: "guarding",
    actionLabel: "View Watchdogs",
    actionHref: "/monitors",
  },
  {
    id: "nav-fleet",
    title: "Execution Fleets & Runners",
    subtitle: "Ephemeral cloud sandboxes & BYOC private runners",
    description:
      "Run agents in managed Kiwi Cloud pools or connect your own private daemons inside your secure VPC using `kiwidaemon join`. Inspect CPU/RAM load and real-time hardware meters.",
    targetQuery: '[data-tour="nav-fleet"]',
    icon: <Server className="w-4 h-4 text-purple-600" />,
    pose: "hacking",
    actionLabel: "Explore Fleets",
    actionHref: "/fleet",
  },
  {
    id: "nav-spend",
    title: "Spend & Model Intelligence",
    subtitle: "Token quotas, model intelligence & spend caps",
    description:
      "Track compute minutes and token usage in real time. Kiwi provides generous platform quotas for frontier models (Claude 3.7 Sonnet, GPT-4.5, Gemini 2.0) with strict spend cap protection.",
    targetQuery: '[data-tour="nav-spend"]',
    icon: <Receipt className="w-4 h-4 text-amber-600" />,
    pose: "vibing",
    actionLabel: "View Spend",
    actionHref: "/spend",
  },
  {
    id: "new-task-btn",
    title: "Launch Autonomous Tasks",
    subtitle: "Compose goals with Architect & Worker models",
    description:
      "Ready to build? Click '+ New Task' or press ⌘K to open the Task Composer. Pair high-reasoning Architect models with fast Worker models to ship features with cryptographically verifiable proofs.",
    targetQuery: '[data-tour="new-task-btn"]',
    icon: <Plus className="w-4 h-4 text-kiwi-600" />,
    pose: "dancing",
    actionLabel: "Open Composer",
    actionHref: "/composer",
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

  // Update spotlight target element rectangle
  const updateSpotlight = useCallback(() => {
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

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 overflow-hidden flex items-center justify-center p-4 sm:p-6 select-none font-sans">
      {/* Dark backdrop with smooth fade */}
      <div
        className="fixed inset-0 bg-stone-950/60 backdrop-blur-xs transition-opacity animate-in fade-in duration-200"
        onClick={handleClose}
      />

      {/* Spotlight cutout border overlay if element is found */}
      {targetRect && (
        <div
          className="fixed pointer-events-none transition-all duration-300 ease-out border-2 border-kiwi-400 rounded-xl shadow-[0_0_0_9999px_rgba(15,15,14,0.45),0_0_20px_rgba(147,198,69,0.4)] z-50"
          style={{
            top: `${Math.max(8, targetRect.top - 6)}px`,
            left: `${Math.max(8, targetRect.left - 6)}px`,
            width: `${targetRect.width + 12}px`,
            height: `${targetRect.height + 12}px`,
          }}
        />
      )}

      {/* Tour Dialog Card */}
      <div className="relative z-50 bg-white border border-sand-200/90 rounded-2xl shadow-popover max-w-lg w-full p-6 sm:p-7 animate-in zoom-in-95 duration-200 flex flex-col">
        {/* Header Strip */}
        <div className="flex items-center justify-between gap-3 mb-4 pb-3 border-b border-sand-200/80">
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono font-bold uppercase tracking-wider text-kiwi-800 bg-kiwi-50 border border-kiwi-200 px-2 py-0.5 rounded-md flex items-center gap-1">
              <Sparkles className="w-3 h-3 text-kiwi-600" />
              <span>PLATFORM TOUR</span>
            </span>
            <span className="text-xs font-mono text-stone-400 font-semibold">
              Step {currentStepIndex + 1} of {TOUR_STEPS.length}
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

        {/* Mascot & Main Feature Content */}
        <div className="flex items-start gap-4 mb-5">
          <div className="w-12 h-12 rounded-xl bg-sand-50 border border-sand-200/90 flex items-center justify-center shrink-0 shadow-2xs">
            <Logo
              variant="full-color"
              pose={currentStep.pose || "vibing"}
              animated={true}
              className="w-7 h-7"
            />
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 mb-0.5">
              <div className="p-1 rounded-md bg-sand-100 border border-sand-200/80">
                {currentStep.icon}
              </div>
              <h3 className="text-base font-bold text-stone-900 tracking-tight">
                {currentStep.title}
              </h3>
            </div>
            <p className="text-xs font-medium text-stone-500 mb-2 font-mono">
              {currentStep.subtitle}
            </p>
            <p className="text-xs text-stone-600 leading-relaxed font-sans">
              {currentStep.description}
            </p>
          </div>
        </div>

        {/* Step Progress Indicators */}
        <div className="flex items-center justify-center gap-1.5 my-2">
          {TOUR_STEPS.map((step, idx) => (
            <button
              key={step.id}
              onClick={() => setCurrentStepIndex(idx)}
              className={`h-1.5 rounded-full transition-all cursor-pointer ${
                idx === currentStepIndex
                  ? "w-6 bg-stone-900"
                  : idx < currentStepIndex
                  ? "w-2 bg-kiwi-500"
                  : "w-2 bg-sand-200 hover:bg-sand-300"
              }`}
              title={`Jump to step ${idx + 1}: ${step.title}`}
            />
          ))}
        </div>

        {/* Footer Navigation Buttons */}
        <div className="flex items-center justify-between gap-3 pt-4 border-t border-sand-200/80 mt-2">
          <button
            onClick={handleClose}
            className="text-xs font-semibold text-stone-400 hover:text-stone-700 transition-colors cursor-pointer"
          >
            Skip tour
          </button>

          <div className="flex items-center gap-2">
            {currentStepIndex > 0 && (
              <button
                onClick={handlePrev}
                className="px-3 py-1.5 rounded-xl border border-sand-200 hover:bg-sand-100 text-stone-700 font-semibold text-xs transition-all flex items-center gap-1 cursor-pointer"
              >
                <ChevronLeft className="w-3.5 h-3.5" />
                <span>Back</span>
              </button>
            )}

            <button
              onClick={handleNext}
              className="px-4 py-1.5 rounded-xl bg-charcoal-900 hover:bg-charcoal-800 text-white font-semibold text-xs shadow-sm transition-all flex items-center gap-1.5 active:scale-[0.98] cursor-pointer"
            >
              <span>{currentStepIndex === TOUR_STEPS.length - 1 ? "Finish Tour" : "Next Step"}</span>
              {currentStepIndex === TOUR_STEPS.length - 1 ? (
                <CheckCircle2 className="w-3.5 h-3.5 text-kiwi-400" />
              ) : (
                <ChevronRight className="w-3.5 h-3.5 text-kiwi-400" />
              )}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
