import type { ComponentProps } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center rounded-md border px-2 py-1 text-xs font-medium",
  { variants: { variant: {
    default: "border-transparent bg-primary text-primary-foreground",
    outline: "text-foreground",
  } }, defaultVariants: { variant: "default" } },
);

export function Badge({ className, variant, ...props }:
  ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />;
}
