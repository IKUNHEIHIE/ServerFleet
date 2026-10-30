import type { ReactNode } from "react";
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
    <section className={cn("min-w-0 rounded-xl border bg-card", className)}>
      {title !== undefined || actions !== undefined ? (
        <header className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4">
          <div className="min-w-0">
            {title !== undefined ? <Heading className="font-semibold">{title}</Heading> : null}
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
    <div className="flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h1 id="page-title" tabIndex={-1} className="text-2xl font-semibold tracking-tight focus:outline-none">
            {title}
          </h1>
          {badge}
        </div>
        {description ? <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  );
}
