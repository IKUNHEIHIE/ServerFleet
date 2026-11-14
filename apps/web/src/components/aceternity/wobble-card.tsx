import { useState, type MouseEvent, type ReactNode } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

/**
 * Aceternity UI — WobbleCard
 * Interactive spring-translated highlight card for featured operational modules.
 */
export function WobbleCard({
  children,
  containerClassName,
  className,
}: {
  readonly children: ReactNode;
  readonly containerClassName?: string;
  readonly className?: string;
}) {
  const [mousePosition, setMousePosition] = useState({ x: 0, y: 0 });
  const [isHovering, setIsHovering] = useState(false);

  function handleMouseMove(event: MouseEvent<HTMLElement>) {
    const { clientX, clientY } = event;
    const rect = event.currentTarget.getBoundingClientRect();
    const x = (clientX - (rect.left + rect.width / 2)) / 24;
    const y = (clientY - (rect.top + rect.height / 2)) / 24;
    setMousePosition({ x, y });
  }

  return (
    <motion.section
      onMouseMove={handleMouseMove}
      onMouseEnter={() => setIsHovering(true)}
      onMouseLeave={() => {
        setIsHovering(false);
        setMousePosition({ x: 0, y: 0 });
      }}
      style={{
        transform: isHovering
          ? `translate3d(${mousePosition.x}px, ${mousePosition.y}px, 0) scale3d(1, 1, 1)`
          : "translate3d(0px, 0px, 0) scale3d(1, 1, 1)",
        transition: "transform 0.15s ease-out",
      }}
      className={cn(
        "relative mx-auto w-full overflow-hidden rounded-2xl border border-[#6658E8]/20 bg-gradient-to-br from-white via-[#F6F7FB] to-[#F0EEFF]/70 shadow-[var(--shadow-card)]",
        containerClassName,
      )}
    >
      <div
        className={cn("relative h-full px-5 py-5 sm:px-6", className)}
      >
        {children}
      </div>
    </motion.section>
  );
}
