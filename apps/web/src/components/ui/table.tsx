import { useId, type ReactNode } from "react";
import { motion } from "motion/react";
import { cn } from "@/lib/utils";

export interface Column<T> {
  readonly key: string;
  readonly header: ReactNode;
  readonly render: (row: T) => ReactNode;
  readonly align?: "left" | "right";
  readonly className?: string;
}

export function DataTable<T>({
  caption,
  columns,
  rows,
  rowKey,
  empty,
  className,
}: {
  readonly caption: string;
  readonly columns: readonly Column<T>[];
  readonly rows: readonly T[];
  readonly rowKey: (row: T, index: number) => string;
  readonly empty?: ReactNode;
  readonly className?: string;
}) {
  if (rows.length === 0) {
    return (
      <div className="px-6 py-10 text-center text-sm text-muted-foreground">
        {empty ?? "暂无记录"}
      </div>
    );
  }
  return (
    <div className={cn("overflow-x-auto [contain:paint]", className)}>
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="border-b border-border/80 bg-[#F6F7FB]/90">
            {columns.map(column => (
              <th
                key={column.key}
                scope="col"
                className={cn(
                  "whitespace-nowrap px-4 py-2.5 text-xs font-semibold text-muted-foreground",
                  column.align === "right" ? "text-right" : "text-left",
                )}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border/70">
          {rows.map((row, index) => (
            <tr
              key={rowKey(row, index)}
              className="group/row border-t align-top transition-colors duration-150 hover:bg-[#F0EEFF]/45"
            >
              {columns.map(column => (
                <td
                  key={column.key}
                  className={cn(
                    "px-4 py-3 text-foreground",
                    column.align === "right" ? "text-right tabular-nums" : "text-left",
                    column.className,
                  )}
                >
                  {column.render(row)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function DefinitionList({
  items,
  className,
}: {
  readonly items: readonly { readonly term: string; readonly value: ReactNode }[];
  readonly className?: string;
}) {
  return (
    <dl className={cn("grid gap-x-6 gap-y-4 px-5 py-5 sm:grid-cols-2 lg:grid-cols-3", className)}>
      {items.map(item => (
        <div
          key={item.term}
          className="rounded-xl border border-border/75 bg-[#F6F7FB]/65 px-3.5 py-2.5 transition-colors hover:border-[#6658E8]/30 hover:bg-card"
        >
          <dt className="text-xs font-medium text-muted-foreground">{item.term}</dt>
          <dd className="mt-1 break-words text-sm font-medium text-foreground">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function CodeBlock({
  children,
  label,
}: {
  readonly children: string;
  readonly label?: string;
}) {
  return (
    <div className="min-w-0">
      {label ? <p className="mb-1 text-xs font-medium text-muted-foreground">{label}</p> : null}
      <pre className="max-h-80 overflow-auto rounded-xl border border-border bg-[#1C2030] p-3.5 font-mono text-xs leading-5 text-slate-100 shadow-inner">
        <code>{children}</code>
      </pre>
    </div>
  );
}

export function TabBar<T extends string>({
  tabs,
  active,
  onChange,
  label,
}: {
  readonly tabs: readonly { readonly id: T; readonly label: ReactNode; readonly disabled?: boolean }[];
  readonly active: T;
  readonly onChange: (id: T) => void;
  readonly label: string;
}) {
  const pillId = useId();
  return (
    <div
      role="tablist"
      aria-label={label}
      className="flex flex-wrap gap-1.5 rounded-xl border border-border bg-card p-1.5 shadow-[var(--shadow-card)]"
    >
      {tabs.map(tab => {
        const selected = active === tab.id;
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            aria-selected={selected}
            disabled={tab.disabled}
            onClick={() => onChange(tab.id)}
            className={cn(
              "relative rounded-lg px-3.5 py-1.5 text-sm font-medium transition-colors duration-150",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              selected
                ? "text-white"
                : "text-muted-foreground hover:bg-[#F6F7FB] hover:text-foreground",
              "disabled:cursor-not-allowed disabled:opacity-50",
            )}
          >
            {selected ? (
              <motion.span
                layoutId={pillId}
                transition={{ type: "spring", stiffness: 380, damping: 30 }}
                className="pointer-events-none absolute inset-0 rounded-lg bg-gradient-to-r from-[#6658E8] to-[#5647DC] shadow-[0_2px_10px_-2px_rgba(102,88,232,0.45)]"
              />
            ) : null}
            <span className="relative z-10">{tab.label}</span>
          </button>
        );
      })}
    </div>
  );
}
