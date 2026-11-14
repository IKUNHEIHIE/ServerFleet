import type { ReactNode } from "react";
import { Sparkles } from "lucide-react";
import { cn } from "@/lib/utils";

export function Panel({
  title,
  description,
  actions,
  children,
  className,
  headingLevel = 2,
}: {
  readonly title?: ReactNode;
  readonly description?: ReactNode;
  readonly actions?: ReactNode;
  readonly children: ReactNode;
  readonly className?: string;
  readonly headingLevel?: 2 | 3;
}) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <section
      className={cn(
        "group/panel relative min-w-0 overflow-hidden rounded-2xl border border-border/90 bg-card text-card-foreground shadow-[var(--shadow-card)] transition-all duration-200 hover:border-[#6658E8]/25",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-0 h-[2px] bg-gradient-to-r from-transparent via-[#6658E8]/35 to-transparent opacity-70 transition-opacity duration-300 group-hover/panel:opacity-100"
      />
      {title !== undefined || actions !== undefined ? (
        <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border/80 bg-gradient-to-b from-[#FAFBFD] to-card px-5 py-4">
          <div className="min-w-0">
            {title !== undefined ? (
              <Heading className="font-semibold tracking-tight text-foreground">{title}</Heading>
            ) : null}
            {description !== undefined ? (
              <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
            ) : null}
          </div>
          {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
        </header>
      ) : null}
      {children}
    </section>
  );
}

export function PageHeader({
  title,
  description,
  actions,
  badge,
}: {
  readonly title: string;
  readonly description?: ReactNode;
  readonly actions?: ReactNode;
  readonly badge?: ReactNode;
}) {
  return (
    <div className="relative overflow-hidden rounded-2xl border border-border/90 bg-gradient-to-br from-white via-white to-[#F0EEFF]/55 px-5 py-4 shadow-[var(--shadow-card)]">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute -top-20 -right-16 h-48 w-48 rounded-full bg-[#6658E8]/8 blur-2xl"
      />
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-0 h-[2px] bg-gradient-to-r from-[#6658E8]/60 via-[#3984E7]/45 to-transparent"
      />
      <div className="relative z-10 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="mb-1 flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.14em] text-[#6658E8]">
            <Sparkles className="size-3" aria-hidden="true" />
            <span>Server Fleet · NEUROX Control Plane</span>
          </div>
          <div className="flex flex-wrap items-center gap-2.5">
            <h1
              id="page-title"
              tabIndex={-1}
              className="text-2xl font-bold tracking-tight text-foreground focus:outline-none"
            >
              {title}
            </h1>
            {badge}
          </div>
          {description ? (
            <p className="mt-1.5 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p>
          ) : null}
        </div>
        {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
      </div>
    </div>
  );
}
