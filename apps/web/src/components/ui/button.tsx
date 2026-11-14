import type { ComponentProps, Ref } from "react";
import { cn } from "@/lib/utils";

type Variant = "primary" | "secondary" | "outline" | "ghost" | "danger";
type Size = "sm" | "md";

const VARIANTS: Record<Variant, string> = {
  primary:
    "border-[#6658E8] bg-gradient-to-b from-[#7265F0] to-[#6658E8] text-primary-foreground shadow-[0_2px_10px_-2px_rgba(102,88,232,0.45)] hover:from-[#6658E8] hover:to-[#5647DC] hover:shadow-[0_4px_16px_-2px_rgba(102,88,232,0.55)]",
  secondary:
    "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80",
  outline:
    "border-input bg-card text-foreground shadow-[0_1px_2px_rgba(28,32,48,0.04)] hover:border-[#6658E8]/35 hover:bg-[#F6F7FB] hover:text-[#6658E8]",
  ghost:
    "border-transparent bg-transparent text-foreground hover:bg-[#F0EEFF]/70 hover:text-[#6658E8]",
  danger:
    "border-[#DE5555] bg-gradient-to-b from-[#E66060] to-[#DE5555] text-white shadow-[0_2px_8px_-2px_rgba(222,85,85,0.4)] hover:from-[#DE5555] hover:to-[#C94444]",
};

const SIZES: Record<Size, string> = {
  sm: "h-8 px-2.5 text-xs",
  md: "h-9 px-3.5 text-sm",
};

export interface ButtonProps extends ComponentProps<"button"> {
  readonly variant?: Variant;
  readonly size?: Size;
  readonly loading?: boolean;
  readonly ref?: Ref<HTMLButtonElement>;
}

export function Button({
  variant = "outline",
  size = "md",
  loading = false,
  className,
  children,
  disabled,
  ...props
}: ButtonProps) {
  return (
    <button
      {...props}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-lg border font-medium transition-all duration-150 active:scale-[0.985]",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        "disabled:cursor-not-allowed disabled:opacity-50",
        VARIANTS[variant],
        SIZES[size],
        className,
      )}
    >
      {loading ? (
        <span
          aria-hidden="true"
          className="size-3 animate-spin rounded-full border-2 border-current border-t-transparent"
        />
      ) : null}
      {children}
    </button>
  );
}
