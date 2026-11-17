import { useEffect, useRef, useState, type MouseEvent, type ReactNode } from "react";
import { cn } from "@/lib/utils";

export const glowTones = {
  violet: "rgba(102, 88, 232, 0.16)",
  blue: "rgba(57, 132, 231, 0.17)",
  green: "rgba(22, 155, 104, 0.16)",
  amber: "rgba(216, 155, 43, 0.17)",
  rose: "rgba(225, 90, 120, 0.15)",
  cyan: "rgba(20, 160, 190, 0.16)",
} as const;

export type GlowTone = keyof typeof glowTones;

interface GlowingEffectProps {
  readonly children?: ReactNode;
  readonly className?: string;
  readonly glowColor?: string;
  readonly tone?: GlowTone;
  readonly disabled?: boolean;
}

const baseTones: Record<GlowTone, string> = {
  violet: "from-[#6658E8]/10 via-transparent to-[#3984E7]/8",
  blue: "from-[#3984E7]/12 via-transparent to-[#22B8CF]/8",
  green: "from-[#169B68]/12 via-transparent to-[#8CC63F]/8",
  amber: "from-[#D89B2B]/14 via-transparent to-[#F0A34A]/8",
  rose: "from-[#E15A78]/12 via-transparent to-[#F08CA4]/8",
  cyan: "from-[#14A0BE]/13 via-transparent to-[#3984E7]/8",
};

/**
 * Aceternity UI — GlowingEffect
 * Cursor-aware radial highlight for NEUROX cards. Overlay mode listens on the
 * nearest positioned parent so cards only need `group relative overflow-hidden`.
 */
export function GlowingEffect({
  children,
  className,
  glowColor,
  tone = "violet",
  disabled = false,
}: GlowingEffectProps) {
  const overlayRef = useRef<HTMLDivElement | null>(null);
  const [coords, setCoords] = useState<{ x: number; y: number; active: boolean }>({
    x: 0,
    y: 0,
    active: false,
  });
  const color = glowColor ?? glowTones[tone];

  useEffect(() => {
    if (children !== undefined || disabled) return;
    const parent = overlayRef.current?.parentElement;
    if (!parent) return;
    const move = (event: PointerEvent) => {
      const rect = parent.getBoundingClientRect();
      setCoords({ x: event.clientX - rect.left, y: event.clientY - rect.top, active: true });
    };
    const leave = () => setCoords(current => ({ ...current, active: false }));
    parent.addEventListener("pointermove", move);
    parent.addEventListener("pointerleave", leave);
    return () => {
      parent.removeEventListener("pointermove", move);
      parent.removeEventListener("pointerleave", leave);
    };
  }, [children, disabled]);

  if (children === undefined) {
    if (disabled) return null;
    return (
      <div ref={overlayRef} aria-hidden="true" className="pointer-events-none absolute inset-0 z-0 overflow-hidden">
        <div className={cn("absolute inset-0 bg-gradient-to-br opacity-70", baseTones[tone])} />
        <div
          className="absolute inset-0 transition-opacity duration-200"
          style={{
            opacity: coords.active ? 1 : 0,
            background: `radial-gradient(320px circle at ${coords.x}px ${coords.y}px, ${color}, transparent 72%)`,
          }}
        />
      </div>
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
        <>
          <div aria-hidden="true" className={cn("pointer-events-none absolute inset-0 z-0 bg-gradient-to-br opacity-80", baseTones[tone])} />
          <div
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 z-0 transition-opacity duration-300"
            style={{
              opacity: coords.active ? 1 : 0,
              background: `radial-gradient(360px circle at ${coords.x}px ${coords.y}px, ${color}, transparent 70%)`,
            }}
          />
        </>
      )}
      <div className="relative z-10 h-full">{children}</div>
    </div>
  );
}