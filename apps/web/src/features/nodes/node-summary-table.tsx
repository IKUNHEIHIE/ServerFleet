import type { ReactNode } from "react";
import { Server } from "lucide-react";
import { cn } from "@/lib/utils";
import type { NodeSummary } from "@/lib/api/dto";
import { adaptMetric } from "./legacy-adapter";
import { LegacyMetricCell } from "./metric-display";

export interface NodeSummaryTableProps {
  readonly rows: readonly NodeSummary[];
  readonly caption: string;
  readonly empty?: ReactNode;
  readonly selectedIds?: ReadonlySet<string>;
  readonly onToggle?: (nodeId: string, checked: boolean) => void;
}

/** 服务端已过滤的节点结果表；只展示已授权字段，不推断缺失指标。 */
export function NodeSummaryTable({ rows, caption, empty, selectedIds, onToggle }: NodeSummaryTableProps) {
  if (rows.length === 0) {
    return <div className="px-6 py-10 text-center text-sm text-muted-foreground">{empty ?? "没有可见节点。"}</div>;
  }
  const selectable = Boolean(onToggle);
  return (
    <div className="overflow-x-auto [contain:paint]">
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">{caption}</caption>
        <thead>
          <tr className="border-b border-border/70 bg-[#F6F7FB]/90">
            {selectable ? (
              <th scope="col" className="w-10 px-3 py-3">
                <span className="sr-only">选择</span>
              </th>
            ) : null}
            {["节点", "状态", "地区", "云厂商", "CPU", "内存"].map((label, index) => (
              <th
                key={label}
                scope="col"
                className={`whitespace-nowrap px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground ${index >= 4 ? "text-right" : "text-left"}`}
              >
                {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-border/60">
          {rows.map(node => {
            const name = node.name.trim() ? node.name : node.id;
            const isSelected = selectedIds?.has(node.id) ?? false;
            return (
              <tr
                key={node.id}
                draggable={selectable}
                onDragStart={
                  selectable
                    ? event => {
                        event.dataTransfer.setData("text/plain", node.id);
                        event.dataTransfer.effectAllowed = "copyMove";
                      }
                    : undefined
                }
                className={cn(
                  "align-top transition-colors hover:bg-primary/[0.025]",
                  isSelected && "bg-primary/[0.04]",
                )}
              >
                {selectable ? (
                  <td className="px-3 py-3.5">
                    <input
                      type="checkbox"
                      aria-label={`选择节点 ${name}`}
                      checked={isSelected}
                      onChange={event => onToggle?.(node.id, event.target.checked)}
                      className="size-4 accent-[var(--primary)]"
                    />
                  </td>
                ) : null}
                <th scope="row" className="px-4 py-3.5 text-left font-normal">
                  <div className="flex items-start gap-2.5">
                    <span
                      aria-hidden="true"
                      className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg border border-border/80 bg-[#F6F7FB] text-primary"
                    >
                      <Server className="size-3.5" />
                    </span>
                    <div>
                      <span className="font-semibold text-foreground">{name}</span>
                      {node.name.trim() ? null : <span className="mt-0.5 block text-xs text-amber-700">名称为空，显示节点 ID</span>}
                      {name === node.id ? null : (
                        <span className="mt-0.5 block break-all font-mono text-xs text-muted-foreground">{node.id}</span>
                      )}
                      {node.ip ? <span className="mt-0.5 block font-mono text-xs text-muted-foreground">{node.ip}</span> : null}
                    </div>
                  </div>
                </th>
                <td className="px-4 py-3.5">
                  <span
                    className={cn(
                      "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold",
                      node.online
                        ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-800"
                        : "border-border bg-secondary text-muted-foreground",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn("size-1.5 rounded-full", node.online ? "bg-emerald-500" : "bg-muted-foreground/60")}
                    />
                    {node.online ? "在线" : "离线"}
                  </span>
                </td>
                <td className="px-4 py-3.5">{node.region ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3.5">{node.provider ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3.5 text-right">
                  <LegacyMetricCell metric={adaptMetric(node.cpuPercent, node.online)} />
                </td>
                <td className="px-4 py-3.5 text-right">
                  <LegacyMetricCell metric={adaptMetric(node.memoryPercent, node.online)} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
