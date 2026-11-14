import { useId } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

const BEAM_PATHS = [
  "M-380 -189C-380 -189 -312 216 152 343C616 470 684 875 684 875",
  "M-336 -157C-336 -157 -268 248 196 375C660 502 728 907 728 907",
  "M-292 -125C-292 -125 -224 280 240 407C704 534 772 939 772 939",
  "M-248 -93C-248 -93 -180 312 284 439C748 566 816 971 816 971",
  "M-204 -61C-204 -61 -136 344 328 471C792 598 860 1003 860 1003",
  "M-160 -29C-160 -29 -92 376 372 503C836 630 904 1035 904 1035",
] as const;

/**
 * Aceternity UI — BackgroundBeams
 * Subtle flowing SVG light beams tailored for NEUROX Light Theme hero/auth containers.
 */
export function BackgroundBeams({ className }: { readonly className?: string }) {
  const baseId = useId();

  return (
    <div
      aria-hidden="true"
      className={cn(
        "pointer-events-none absolute inset-0 overflow-hidden [mask-image:radial-gradient(ellipse_at_center,white,transparent_82%)]",
        className,
      )}
    >
      <svg
        className="h-full w-full opacity-55"
        viewBox="0 0 696 316"
        fill="none"
        xmlns="http://www.w3.org/2000/svg"
        preserveAspectRatio="none"
      >
        {BEAM_PATHS.map((path, index) => {
          const gradId = `${baseId}-beam-${index}`;
          return (
            <g key={gradId}>
              <path d={path} stroke="rgba(102, 88, 232, 0.08)" strokeWidth="1" />
              <path
                d={path}
                stroke={`url(#${gradId})`}
                strokeWidth="1.5"
                strokeLinecap="round"
              />
            </g>
          );
        })}
        <defs>
          {BEAM_PATHS.map((_, index) => {
            const gradId = `${baseId}-beam-${index}`;
            return (
              <motion.linearGradient
                key={gradId}
                id={gradId}
                initial={{ x1: "0%", x2: "0%", y1: "0%", y2: "0%" }}
                animate={{
                  x1: ["0%", "100%"],
                  x2: ["0%", "92%"],
                  y1: ["0%", "100%"],
                  y2: ["0%", "90%"],
                }}
                transition={{
                  duration: 6 + index * 1.1,
                  repeat: Infinity,
                  repeatDelay: 1.5,
                  delay: index * 0.6,
                  ease: "easeInOut",
                }}
              >
                <stop stopColor="#6658E8" stopOpacity="0" />
                <stop offset="0.35" stopColor="#6658E8" stopOpacity="0.65" />
                <stop offset="0.7" stopColor="#3984E7" stopOpacity="0.55" />
                <stop offset="1" stopColor="#3984E7" stopOpacity="0" />
              </motion.linearGradient>
            );
          })}
        </defs>
      </svg>
    </div>
  );
}
