import type { ReactNode } from "react";
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
    return <div className="px-6 py-10 text-center text-sm text-muted-foreground">{empty ?? "暂无记录"}</div>;
  }
  return (
    <div className={cn("overflow-x-auto [contain:paint]", className)}>
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="bg-secondary/60">
            {columns.map(column => (
              <th
                key={column.key}
                scope="col"
                className={cn(
                  "whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground",
                  column.align === "right" ? "text-right" : "text-left",
                )}
              >
                {column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={rowKey(row, index)} className="border-t align-top">
              {columns.map(column => (
                <td
                  key={column.key}
                  className={cn(
                    "px-4 py-3",
                    column.align === "right" ? "text-right" : "text-left",
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
        <div key={item.term}>
          <dt className="text-xs text-muted-foreground">{item.term}</dt>
          <dd className="mt-1 break-words text-sm">{item.value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function CodeBlock({ children, label }: { readonly children: string; readonly label?: string }) {
  return (
    <div className="min-w-0">
      {label ? <p className="mb-1 text-xs text-muted-foreground">{label}</p> : null}
      <pre className="max-h-80 overflow-auto rounded-md border bg-secondary/40 p-3 text-xs leading-5">
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
  return (
    <div role="tablist" aria-label={label} className="flex flex-wrap gap-1 border-b px-2">
      {tabs.map(tab => (
        <button
          key={tab.id}
          type="button"
          role="tab"
          aria-selected={active === tab.id}
          disabled={tab.disabled}
          onClick={() => onChange(tab.id)}
          className={cn(
            "-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            active === tab.id
              ? "border-primary text-foreground"
              : "border-transparent text-muted-foreground hover:text-foreground",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        >
          {tab.label}
        </button>
      ))}
    </div>
  );
}
