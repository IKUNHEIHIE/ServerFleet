import type { ReactNode } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

/**
 * Aceternity UI — TracingBeam
 * Vertical glowing progress & step beam for Agent execution runs, Task steps, and Audit timelines.
 */
export function TracingBeam({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string;
}) {
  return (
    <div className={cn("relative w-full pl-6 sm:pl-8", className)}>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute top-2 bottom-2 left-2.5 w-[2px] overflow-hidden rounded-full bg-border"
      >
        <motion.div
          initial={{ y: "-100%" }}
          animate={{ y: ["-100%", "200%"] }}
          transition={{ duration: 3.6, repeat: Infinity, ease: "linear" }}
          className="h-1/2 w-full bg-gradient-to-b from-transparent via-[#6658E8] to-[#3984E7]"
        />
      </div>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute top-2 left-[5px] flex size-3 items-center justify-center rounded-full border border-[#6658E8] bg-card shadow-[0_0_8px_rgba(102,88,232,0.45)]"
      >
        <span className="size-1.5 rounded-full bg-[#6658E8]" />
      </div>
      <div className="relative">{children}</div>
    </div>
  );
}
