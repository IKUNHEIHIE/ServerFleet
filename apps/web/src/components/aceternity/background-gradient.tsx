import type { ReactNode } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

interface BackgroundGradientProps {
  readonly children: ReactNode;
  readonly className?: string;
  readonly containerClassName?: string;
  readonly animate?: boolean;
}

/**
 * Aceternity UI — BackgroundGradient & BackgroundGradientAnimation
 * Surrounds important cards or hero panels with an animated multi-stop aura border.
 */
export function BackgroundGradient({
  children,
  className,
  containerClassName,
  animate = true,
}: BackgroundGradientProps) {
  return (
    <div className={cn("group relative p-[1.5px] rounded-2xl", containerClassName)}>
      <motion.div
        aria-hidden="true"
        initial={animate ? { backgroundPosition: "0% 50%" } : undefined}
        animate={animate ? { backgroundPosition: ["0% 50%", "100% 50%", "0% 50%"] } : undefined}
        transition={
          animate
            ? { duration: 7, repeat: Infinity, ease: "linear" }
            : undefined
        }
        style={{ backgroundSize: animate ? "250% 250%" : undefined }}
        className="pointer-events-none absolute inset-0 rounded-2xl bg-[linear-gradient(120deg,rgba(102,88,232,0.45),rgba(57,132,231,0.35),rgba(22,155,104,0.28),rgba(102,88,232,0.45))] opacity-60 blur-[2px] transition-opacity duration-300 group-hover:opacity-100"
      />
      <div className={cn("relative rounded-[calc(1rem-1.5px)] bg-card", className)}>{children}</div>
    </div>
  );
}

export function BackgroundGradientAnimation({
  children,
  className,
}: {
  readonly children?: ReactNode;
  readonly className?: string;
}) {
  return (
    <div
      className={cn(
        "relative overflow-hidden bg-[#F6F7FB]",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-32 -left-32 h-96 w-96 rounded-full bg-[#6658E8]/12 blur-3xl"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute top-1/4 -right-28 h-80 w-80 rounded-full bg-[#3984E7]/10 blur-3xl"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -bottom-24 left-1/3 h-72 w-72 rounded-full bg-[#169B68]/8 blur-3xl"
      />
      <div className="relative z-10">{children}</div>
    </div>
  );
}
