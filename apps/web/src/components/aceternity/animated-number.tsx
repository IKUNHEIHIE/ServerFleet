import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";

interface AnimatedNumberProps {
  readonly value: number;
  readonly suffix?: string;
  readonly prefix?: string;
  readonly decimals?: number;
  readonly className?: string;
}

/**
 * NEUROX KPI AnimatedNumber
 * Animates numeric changes smoothly using requestAnimationFrame on the visual layer
 * while keeping accessible/testable textContent immediately accurate.
 */
export function AnimatedNumber({
  value,
  suffix = "",
  prefix = "",
  decimals = 0,
  className,
}: AnimatedNumberProps) {
  const formattedTarget = `${prefix}${value.toFixed(decimals)}${suffix}`;
  const [visualText, setVisualText] = useState(formattedTarget);
  const previousValueRef = useRef(value);

  useEffect(() => {
    const startValue = previousValueRef.current;
    previousValueRef.current = value;

    const prefersReducedMotion =
      typeof window === "undefined" ||
      typeof window.matchMedia !== "function" ||
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    if (prefersReducedMotion || startValue === value) {
      setVisualText(`${prefix}${value.toFixed(decimals)}${suffix}`);
      return;
    }

    let frameId = 0;
    const duration = 520;
    const startTime = performance.now();

    const step = (now: number) => {
      const elapsed = now - startTime;
      const progress = Math.min(elapsed / duration, 1);
      // easeOutExpo
      const eased = progress === 1 ? 1 : 1 - Math.pow(2, -10 * progress);
      const current = startValue + (value - startValue) * eased;
      setVisualText(`${prefix}${current.toFixed(decimals)}${suffix}`);
      if (progress < 1) {
        frameId = requestAnimationFrame(step);
      }
    };

    frameId = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frameId);
  }, [value, prefix, suffix, decimals]);

  return (
    <span className={cn("tabular-nums tracking-tight", className)} data-value={value}>
      {visualText}
    </span>
  );
}
