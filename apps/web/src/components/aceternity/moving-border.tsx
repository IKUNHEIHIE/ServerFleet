import type { ReactNode } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

interface MovingBorderProps {
  readonly children: ReactNode;
  readonly className?: string;
  readonly containerClassName?: string;
  readonly borderClassName?: string;
  readonly duration?: number;
}

/**
 * Aceternity UI — MovingBorder
 * Renders a travelling luminous highlight along a rounded container border.
 */
export function MovingBorder({
  children,
  className,
  containerClassName,
  borderClassName,
  duration = 5,
}: MovingBorderProps) {
  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-2xl p-[1.5px] bg-border/80",
        containerClassName,
      )}
    >
      <motion.div
        aria-hidden="true"
        animate={{ rotate: 360 }}
        transition={{ duration, repeat: Infinity, ease: "linear" }}
        className={cn(
          "pointer-events-none absolute -inset-[100%] bg-[conic-gradient(from_0deg,transparent_0_290deg,#6658E8_330deg,#3984E7_360deg)] opacity-75",
          borderClassName,
        )}
      />
      <div className={cn("relative rounded-[calc(1rem-1.5px)] bg-card", className)}>
        {children}
      </div>
    </div>
  );
}
