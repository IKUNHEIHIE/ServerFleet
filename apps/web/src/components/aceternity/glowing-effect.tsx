import { useState, type MouseEvent, type ReactNode } from "react";
import { cn } from "@/lib/utils";

interface GlowingEffectProps {
  readonly children?: ReactNode;
  readonly className?: string;
  readonly glowColor?: string;
  readonly disabled?: boolean;
}

/**
 * Aceternity UI — GlowingEffect
 * Cursor-aware radial border & surface highlight inspired by NEUROX cards.
 * Supports both wrapper mode (`<GlowingEffect>{children}</GlowingEffect>`) and
 * self-closing overlay mode (`<GlowingEffect />` inside a `group relative` card).
 */
export function GlowingEffect({
  children,
  className,
  glowColor = "rgba(102, 88, 232, 0.16)",
  disabled = false,
}: GlowingEffectProps) {
  const [coords, setCoords] = useState<{ x: number; y: number; active: boolean }>({
    x: 0,
    y: 0,
    active: false,
  });

  if (children === undefined) {
    if (disabled) return null;
    return (
      <div
        aria-hidden="true"
        className={cn(
          "pointer-events-none absolute inset-0 z-0 opacity-0 transition-opacity duration-300 group-hover:opacity-100",
          className,
        )}
        style={{
          background: `radial-gradient(280px circle at 85% 15%, ${glowColor}, transparent 70%)`,
        }}
      />
    );
  }

  function handleMouseMove(event: MouseEvent<HTMLDivElement>) {
    if (disabled) return;
    const rect = event.currentTarget.getBoundingClientRect();
    setCoords({
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
      active: true,
    });
  }

  function handleMouseLeave() {
    if (disabled) return;
    setCoords(current => ({ ...current, active: false }));
  }

  return (
    <div
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
      className={cn(
        "group/glow relative overflow-hidden rounded-xl border border-border bg-card transition-all duration-300 hover:border-[#6658E8]/35 hover:shadow-[var(--shadow-card-hover)]",
        className,
      )}
    >
      {!disabled && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 z-0 transition-opacity duration-300"
          style={{
            opacity: coords.active ? 1 : 0,
            background: `radial-gradient(360px circle at ${coords.x}px ${coords.y}px, ${glowColor}, transparent 70%)`,
          }}
        />
      )}
      <div className="relative z-10 h-full">{children}</div>
    </div>
  );
}
