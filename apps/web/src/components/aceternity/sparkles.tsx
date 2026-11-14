import { useId, useMemo } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

interface SparklesCoreProps {
  readonly id?: string;
  readonly className?: string;
  readonly background?: string;
  readonly minSize?: number;
  readonly maxSize?: number;
  readonly particleDensity?: number;
  readonly particleColor?: string;
  readonly speed?: number;
}

/**
 * Aceternity UI — SparklesCore
 * Lightweight deterministic SVG particle field with transform/opacity motion,
 * zero canvas overhead, and full prefers-reduced-motion compatibility.
 */
export function SparklesCore({
  id,
  className,
  background = "transparent",
  minSize = 1.2,
  maxSize = 3.2,
  particleDensity = 28,
  particleColor = "#6658E8",
  speed = 1,
}: SparklesCoreProps) {
  const generatedId = useId();
  const containerId = id ?? generatedId;

  const particles = useMemo(() => {
    const count = Math.min(Math.max(particleDensity, 8), 64);
    return Array.from({ length: count }, (_, index) => {
      const seed = (index + 1) * 9301 + 49297;
      const x = ((seed * 17) % 980) / 10 + 1;
      const y = ((seed * 31) % 960) / 10 + 2;
      const size = minSize + (((seed * 13) % 100) / 100) * (maxSize - minSize);
      const duration = (2.6 + ((seed * 7) % 30) / 10) / Math.max(speed, 0.25);
      const delay = ((seed * 11) % 25) / 10;
      return { id: `${containerId}-p-${index}`, x, y, size, duration, delay };
    });
  }, [containerId, minSize, maxSize, particleDensity, speed]);

  return (
    <div
      id={containerId}
      aria-hidden="true"
      style={{ background }}
      className={cn("pointer-events-none relative h-full w-full overflow-hidden", className)}
    >
      <svg className="h-full w-full" viewBox="0 0 100 100" preserveAspectRatio="none">
        {particles.map(particle => (
          <motion.circle
            key={particle.id}
            cx={particle.x}
            cy={particle.y}
            r={particle.size * 0.22}
            fill={particleColor}
            initial={{ opacity: 0.15, scale: 0.75 }}
            animate={{ opacity: [0.15, 0.7, 0.15], scale: [0.75, 1.2, 0.75] }}
            transition={{
              duration: particle.duration,
              delay: particle.delay,
              repeat: Infinity,
              ease: "easeInOut",
            }}
          />
        ))}
      </svg>
    </div>
  );
}
