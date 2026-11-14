import { useState, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { cn } from "@/lib/utils";

export interface TooltipItem {
  readonly id: string;
  readonly name: string;
  readonly designation?: string;
  readonly badge?: ReactNode;
}

/**
 * Aceternity UI — AnimatedTooltip
 * Spring-animated hover tooltip pills for node/member/gateway badges.
 */
export function AnimatedTooltip({
  items,
  className,
}: {
  readonly items: readonly TooltipItem[];
  readonly className?: string;
}) {
  const [hoveredId, setHoveredId] = useState<string | null>(null);

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {items.map(item => (
        <div
          key={item.id}
          className="group relative"
          onMouseEnter={() => setHoveredId(item.id)}
          onMouseLeave={() => setHoveredId(null)}
        >
          <AnimatePresence>
            {hoveredId === item.id && (
              <motion.div
                initial={{ opacity: 0, y: 8, scale: 0.92 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, y: 6, scale: 0.94 }}
                transition={{ type: "spring", stiffness: 320, damping: 22 }}
                className="pointer-events-none absolute -top-11 left-1/2 z-50 -translate-x-1/2 whitespace-nowrap rounded-lg border border-[#6658E8]/25 bg-foreground px-2.5 py-1 text-[11px] text-white shadow-lg"
              >
                <div className="font-semibold">{item.name}</div>
                {item.designation ? (
                  <div className="text-[10px] text-white/75">{item.designation}</div>
                ) : null}
              </motion.div>
            )}
          </AnimatePresence>
          <span className="inline-flex items-center gap-1 rounded-full border border-border bg-secondary/70 px-2.5 py-0.5 text-xs font-medium text-foreground transition-colors hover:border-[#6658E8]/40 hover:bg-[#F0EEFF] hover:text-[#6658E8]">
            {item.badge}
            {item.name}
          </span>
        </div>
      ))}
    </div>
  );
}
