import { cva, type VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors",
  {
    variants: {
      variant: {
        default: "border-primary/20 bg-primary/10 text-primary",
        secondary: "border-border/80 bg-secondary/80 text-secondary-foreground",
        destructive: "border-red-200/90 bg-red-50/90 text-red-700",
        outline: "border-border/80 bg-card/85 text-foreground",
        success: "border-emerald-200/90 bg-emerald-50/90 text-emerald-700",
        warning: "border-amber-200/90 bg-amber-50/90 text-amber-800",
        info: "border-sky-200/90 bg-sky-50/90 text-sky-700",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

export type BadgeTone = "primary" | "success" | "warning" | "danger" | "info" | "neutral";

const TONE_TO_VARIANT: Record<BadgeTone, NonNullable<VariantProps<typeof badgeVariants>["variant"]>> = {
  primary: "default",
  success: "success",
  warning: "warning",
  danger: "destructive",
  info: "info",
  neutral: "secondary",
};

const TONE_DOT_CLASS: Record<BadgeTone, string> = {
  primary: "bg-primary",
  success: "bg-emerald-500",
  warning: "bg-amber-500",
  danger: "bg-red-500",
  info: "bg-sky-500",
  neutral: "bg-slate-400",
};

export type BadgeProps = HTMLAttributes<HTMLSpanElement> &
  VariantProps<typeof badgeVariants> & {
    readonly tone?: BadgeTone;
    readonly dot?: boolean;
  };

export function Badge({ className, variant, tone, dot, children, ...props }: BadgeProps) {
  const resolvedVariant = variant ?? (tone ? TONE_TO_VARIANT[tone] : "default");
  const resolvedTone: BadgeTone =
    tone ??
    (resolvedVariant === "success"
      ? "success"
      : resolvedVariant === "warning"
        ? "warning"
        : resolvedVariant === "destructive"
          ? "danger"
          : resolvedVariant === "info"
            ? "info"
            : resolvedVariant === "secondary" || resolvedVariant === "outline"
              ? "neutral"
              : "primary");

  return (
    <span className={cn(badgeVariants({ variant: resolvedVariant }), className)} {...props}>
      {dot ? <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", TONE_DOT_CLASS[resolvedTone])} /> : null}
      {children}
    </span>
  );
}
