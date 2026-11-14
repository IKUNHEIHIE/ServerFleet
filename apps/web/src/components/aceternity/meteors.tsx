import { useMemo } from "react";
import { cn } from "@/lib/utils";

/**
 * Aceternity UI — Meteors
 * Subtle diagonal light streaks for hero & AI workspace cards.
 */
export function Meteors({
  number = 10,
  className,
}: {
  readonly number?: number;
  readonly className?: string;
}) {
  const meteors = useMemo(() => {
    return Array.from({ length: number }, (_, idx) => {
      const seed = (idx + 1) * 41;
      const left = ((seed * 29) % 90) + 5;
      const delay = ((seed * 13) % 40) / 10;
      const duration = 3 + ((seed * 19) % 40) / 10;
      return { idx, left, delay, duration };
    });
  }, [number]);

  return (
    <div aria-hidden="true" className="pointer-events-none absolute inset-0 overflow-hidden">
      {meteors.map(meteor => (
        <span
          key={meteor.idx}
          style={{
            top: "-8%",
            left: `${meteor.left}%`,
            animationDelay: `${meteor.delay}s`,
            animationDuration: `${meteor.duration}s`,
          }}
          className={cn(
            "absolute h-0.5 w-14 rotate-[215deg] rounded-full bg-gradient-to-r from-[#6658E8]/55 via-[#3984E7]/30 to-transparent opacity-60",
            className,
          )}
        />
      ))}
    </div>
  );
}
