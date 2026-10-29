import type { ReactNode } from "react";
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
          <tr className="bg-secondary/60">
            {selectable ? (
              <th scope="col" className="w-10 px-3 py-2.5">
                <span className="sr-only">选择</span>
              </th>
            ) : null}
            {["节点", "状态", "地区", "云厂商", "CPU", "内存"].map((label, index) => (
              <th
                key={label}
                scope="col"
                className={`whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground ${index >= 4 ? "text-right" : "text-left"}`}
              >
                {label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map(node => {
            const name = node.name.trim() ? node.name : node.id;
            return (
              <tr key={node.id} className="border-t align-top">
                {selectable ? (
                  <td className="px-3 py-3">
                    <input
                      type="checkbox"
                      aria-label={`选择节点 ${name}`}
                      checked={selectedIds?.has(node.id) ?? false}
                      onChange={event => onToggle?.(node.id, event.target.checked)}
                      className="size-4 accent-[var(--primary)]"
                    />
                  </td>
                ) : null}
                <th scope="row" className="px-4 py-3 text-left font-normal">
                  <span className="font-medium">{name}</span>
                  {node.name.trim() ? null : <span className="mt-0.5 block text-xs text-amber-700">名称为空，显示节点 ID</span>}
                  {name === node.id ? null : (
                    <span className="mt-0.5 block break-all font-mono text-xs text-muted-foreground">{node.id}</span>
                  )}
                  {node.ip ? <span className="mt-0.5 block font-mono text-xs text-muted-foreground">{node.ip}</span> : null}
                </th>
                <td className="px-4 py-3">{node.online ? "在线" : "离线"}</td>
                <td className="px-4 py-3">{node.region ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3">{node.provider ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3 text-right">
                  <LegacyMetricCell metric={adaptMetric(node.cpuPercent, node.online)} />
                </td>
                <td className="px-4 py-3 text-right">
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
