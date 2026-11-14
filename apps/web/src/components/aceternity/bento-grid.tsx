import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * Aceternity UI — BentoGrid & BentoGridItem
 */
export function BentoGrid({
  className,
  children,
}: {
  readonly className?: string;
  readonly children?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-1 gap-4 md:grid-cols-3",
        className,
      )}
    >
      {children}
    </div>
  );
}

export function BentoGridItem({
  className,
  title,
  description,
  header,
  icon,
  footer,
}: {
  readonly className?: string;
  readonly title?: ReactNode;
  readonly description?: ReactNode;
  readonly header?: ReactNode;
  readonly icon?: ReactNode;
  readonly footer?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "group/bento row-span-1 flex flex-col justify-between space-y-3 rounded-xl border border-border bg-card p-5 shadow-[var(--shadow-card)] transition duration-200 hover:-translate-y-0.5 hover:border-[#6658E8]/35 hover:shadow-[var(--shadow-card-hover)]",
        className,
      )}
    >
      {header}
      <div className="transition duration-200 group-hover/bento:translate-x-0.5">
        {icon ? <div className="mb-2 text-[#6658E8]">{icon}</div> : null}
        {title ? <div className="font-semibold text-foreground">{title}</div> : null}
        {description ? <div className="mt-1 text-xs text-muted-foreground">{description}</div> : null}
      </div>
      {footer}
    </div>
  );
}
