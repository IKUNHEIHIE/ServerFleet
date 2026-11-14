import { useId } from "react";
import { cn } from "@/lib/utils";

interface SpotlightProps {
  readonly className?: string;
  readonly fill?: string;
}

/**
 * Aceternity UI — Spotlight
 * Renders an ambient directional SVG spotlight beam tailored for Light & Dark surfaces.
 */
export function Spotlight({ className, fill = "#6658E8" }: SpotlightProps) {
  const filterId = useId();
  const gradId = useId();

  return (
    <svg
      aria-hidden="true"
      className={cn(
        "pointer-events-none absolute z-[1] h-[140%] w-[130%] opacity-60 transition-opacity duration-700",
        className,
      )}
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 3787 2842"
      fill="none"
    >
      <g filter={`url(#${filterId})`}>
        <ellipse
          cx="1924.71"
          cy="273.501"
          rx="1924.71"
          ry="273.501"
          transform="matrix(-0.822377 -0.568943 -0.568943 0.822377 3631.88 2291.09)"
          fill={`url(#${gradId})`}
          fillOpacity="0.26"
        />
      </g>
      <defs>
        <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor={fill} stopOpacity="0.85" />
          <stop offset="60%" stopColor="#3984E7" stopOpacity="0.45" />
          <stop offset="100%" stopColor={fill} stopOpacity="0" />
        </linearGradient>
        <filter
          id={filterId}
          x="0.860352"
          y="0.838989"
          width="3785.16"
          height="2840.26"
          filterUnits="userSpaceOnUse"
          colorInterpolationFilters="sRGB"
        >
          <feFlood floodOpacity="0" result="BackgroundImageFix" />
          <feBlend mode="normal" in="SourceGraphic" in2="BackgroundImageFix" result="shape" />
          <feGaussianBlur stdDeviation="151" result="effect1_foregroundBlur" />
        </filter>
      </defs>
    </svg>
  );
}
