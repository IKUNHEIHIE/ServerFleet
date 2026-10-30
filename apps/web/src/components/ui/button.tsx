import type { ComponentProps, Ref } from "react";
import { cn } from "@/lib/utils";

type Variant = "primary" | "secondary" | "outline" | "ghost" | "danger";
type Size = "sm" | "md";

const VARIANTS: Record<Variant, string> = {
  primary: "bg-primary text-primary-foreground hover:bg-primary/90 border-transparent",
  secondary: "bg-secondary text-secondary-foreground hover:bg-secondary/80 border-transparent",
  outline: "bg-card text-foreground hover:bg-secondary border-input",
  ghost: "bg-transparent text-foreground hover:bg-secondary border-transparent",
  danger: "bg-destructive text-white hover:bg-destructive/90 border-transparent",
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

export function Button({ variant = "outline", size = "md", loading = false, className, children, disabled, ...props }: ButtonProps) {
  return (
    <button
      {...props}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={cn(
        "inline-flex items-center justify-center gap-1.5 rounded-md border font-medium transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background",
        "disabled:cursor-not-allowed disabled:opacity-50",
        VARIANTS[variant],
        SIZES[size],
        className,
      )}
    >
      {loading ? <span aria-hidden="true" className="size-3 animate-spin rounded-full border-2 border-current border-t-transparent" /> : null}
      {children}
    </button>
  );
}
