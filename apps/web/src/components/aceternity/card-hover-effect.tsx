import { useState, type MouseEvent, type ReactNode } from "react";
import { AnimatePresence, motion } from "motion/react";
import { cn } from "@/lib/utils";

export interface HoverCardItem {
  readonly id: string;
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly badge?: ReactNode;
  readonly icon?: ReactNode;
  readonly footer?: ReactNode;
  readonly href?: string;
  readonly onClick?: () => void;
}

/**
 * Aceternity UI — CardHoverEffect
 * Shared layoutId magnetic hover highlight across a grid of cards.
 */
export function CardHoverEffect({
  items,
  className,
}: {
  readonly items: readonly HoverCardItem[];
  readonly className?: string;
}) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);

  return (
    <div className={cn("grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3", className)}>
      {items.map((item, idx) => {
        const Wrapper = item.href ? "a" : "div";
        return (
          <Wrapper
            key={item.id}
            {...(item.href ? { href: item.href } : {})}
            onClick={item.onClick}
            onMouseEnter={() => setHoveredIndex(idx)}
            onMouseLeave={() => setHoveredIndex(null)}
            className="group relative block h-full w-full p-1.5"
          >
            <AnimatePresence>
              {hoveredIndex === idx && (
                <motion.span
                  layoutId="aceternity-card-hover-bg"
                  className="pointer-events-none absolute inset-0 block h-full w-full rounded-2xl bg-[#F0EEFF]/90 border border-[#6658E8]/25"
                  initial={{ opacity: 0 }}
                  animate={{ opacity: 1, transition: { duration: 0.18 } }}
                  exit={{ opacity: 0, transition: { duration: 0.15, delay: 0.08 } }}
                />
              )}
            </AnimatePresence>
            <div className="relative z-10 flex h-full flex-col justify-between rounded-xl border border-border bg-card p-4 shadow-[var(--shadow-card)] transition-all duration-200 group-hover:border-[#6658E8]/35">
              <div>
                <div className="flex items-start justify-between gap-2">
                  <div className="flex items-center gap-2.5">
                    {item.icon ? (
                      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-[#F0EEFF] text-[#6658E8]">
                        {item.icon}
                      </span>
                    ) : null}
                    <div className="font-semibold text-foreground">{item.title}</div>
                  </div>
                  {item.badge}
                </div>
                {item.description ? (
                  <div className="mt-2 text-xs leading-relaxed text-muted-foreground">
                    {item.description}
                  </div>
                ) : null}
              </div>
              {item.footer ? <div className="mt-3 border-t pt-2.5">{item.footer}</div> : null}
            </div>
          </Wrapper>
        );
      })}
    </div>
  );
}

/**
 * NEUROX & Aceternity inspired 3D Tilt Card with cursor spotlight reflection.
 */
export function TiltCard({
  children,
  className,
  intensity = 5,
}: {
  readonly children: ReactNode;
  readonly className?: string;
  readonly intensity?: number;
}) {
  const [transform, setTransform] = useState("perspective(1000px) rotateX(0deg) rotateY(0deg) translateZ(0px)");
  const [glare, setGlare] = useState({ x: 50, y: 50, opacity: 0 });

  function onMouseMove(event: MouseEvent<HTMLDivElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const relX = (event.clientX - rect.left) / rect.width;
    const relY = (event.clientY - rect.top) / rect.height;
    const rotateY = (relX - 0.5) * intensity * 2;
    const rotateX = (0.5 - relY) * intensity * 2;
    setTransform(
      `perspective(1000px) rotateX(${rotateX.toFixed(2)}deg) rotateY(${rotateY.toFixed(2)}deg) translateY(-2px)`,
    );
    setGlare({ x: relX * 100, y: relY * 100, opacity: 1 });
  }

  function onMouseLeave() {
    setTransform("perspective(1000px) rotateX(0deg) rotateY(0deg) translateY(0px)");
    setGlare(current => ({ ...current, opacity: 0 }));
  }

  return (
    <div
      onMouseMove={onMouseMove}
      onMouseLeave={onMouseLeave}
      style={{ transform, transition: "transform 0.22s cubic-bezier(0.22, 1, 0.36, 1)" }}
      className={cn(
        "group relative overflow-hidden rounded-xl border border-border bg-card shadow-[var(--shadow-card)] hover:border-[#6658E8]/35 hover:shadow-[var(--shadow-card-hover)]",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 z-0 transition-opacity duration-300"
        style={{
          opacity: glare.opacity,
          background: `radial-gradient(280px circle at ${glare.x}% ${glare.y}%, rgba(102, 88, 232, 0.09), transparent 70%)`,
        }}
      />
      <div className="relative z-10 h-full">{children}</div>
    </div>
  );
}
