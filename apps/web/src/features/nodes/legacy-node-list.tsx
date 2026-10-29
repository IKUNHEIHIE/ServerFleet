import { useId, useMemo, useState } from "react";
import { cn } from "@/lib/utils";
import type { LegacyMetric, LegacyNodeRow } from "./legacy-types";

export function normaliseLegacyQuery(raw: string): string {
  return raw.trim().toLowerCase();
}

export function filterLegacyRows(rows: readonly LegacyNodeRow[], query: string): readonly LegacyNodeRow[] {
  const normalised = normaliseLegacyQuery(query);
  if (!normalised) return rows;
  return rows.filter(
    row => row.name.toLowerCase().includes(normalised) || row.id.toLowerCase().includes(normalised),
  );
}

function formatPercent(percent: number): string {
  return `${percent.toFixed(1).replace(/\.0$/, "")}%`;
}

function MetricCell({ metric }: { readonly metric: LegacyMetric }) {
  if (metric.kind === "value") {
    return (
      <span className="inline-flex flex-col items-end gap-0.5">
        <span className="tabular-nums">{formatPercent(metric.percent)}</span>
        <span className="text-xs font-medium text-amber-700">无采样时间</span>
      </span>
    );
  }
  const text =
    metric.kind === "unavailable"
      ? "离线，无当前读数"
      : metric.kind === "absent"
        ? "无读数"
        : metric.reason === "out-of-range"
          ? "读数超出 0–100"
          : "读数非法";
  return <span className="text-xs text-muted-foreground">{text}</span>;
}

function NameCell({ row }: { readonly row: LegacyNodeRow }) {
  return (
    <th scope="row" className="px-4 py-3 text-left font-normal">
      <span className="font-medium">{row.name}</span>
      {row.nameIssue ? (
        <span className="mt-0.5 block text-xs text-amber-700">
          {row.nameIssue === "empty" ? "名称为空，显示节点 ID" : "名称字段非法，显示节点 ID"}
        </span>
      ) : null}
      {row.name === row.id ? null : (
        <span className="mt-0.5 block break-all font-mono text-xs text-muted-foreground">{row.id}</span>
      )}
    </th>
  );
}

export interface LegacyNodeListProps {
  readonly rows: readonly LegacyNodeRow[];
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
}

/**
 * W07 兼容旧列表：只读、非交互行、本地搜索与可见计数。
 *
 * 筛选仅作用于已传入的行，不查询后端、不代表权限变更，也不推断全局节点数。
 * 行内没有详情/执行入口；搜索与清空是唯一控件。
 */
export function LegacyNodeList({ rows, title = "兼容旧列表", description, className }: LegacyNodeListProps) {
  const [query, setQuery] = useState("");
  const searchId = useId();
  const titleId = useId();
  const total = rows.length;
  const visible = useMemo(() => filterLegacyRows(rows, query), [rows, query]);
  const filtering = query.trim() !== "";
  const noMatch = total > 0 && visible.length === 0;
  const countLabel = total === 0 ? "0 个节点" : visible.length === total ? `共 ${total} 个节点` : `显示 ${visible.length} / 共 ${total} 个节点`;

  let body;
  if (total === 0) {
    body = (
      <div className="px-6 py-10 text-center">
        <p className="font-medium">暂无可显示节点</p>
        <p className="mt-1 text-sm text-muted-foreground">
          服务端返回了空集合；这不代表系统没有节点，只代表当前身份可见范围为 0。
        </p>
      </div>
    );
  } else if (noMatch) {
    body = (
      <div className="px-6 py-10 text-center">
        <p className="font-medium">没有匹配结果</p>
        <p className="mt-1 text-sm text-muted-foreground">
          当前搜索词没有命中已加载的 {total} 条记录；节点数据没有被修改。
        </p>
      </div>
    );
  } else {
    body = (
      <div className="overflow-x-auto [contain:paint]">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">兼容旧节点列表</caption>
          <thead>
            <tr className="bg-secondary/60">
              {["节点", "状态", "地区", "云厂商", "CPU", "内存"].map((label, index) => (
                <th
                  key={label}
                  scope="col"
                  className={cn(
                    "whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground",
                    index >= 4 ? "text-right" : "text-left",
                  )}
                >
                  {label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visible.map(row => (
              <tr key={row.id} className="border-t align-top">
                <NameCell row={row} />
                <td className="px-4 py-3">{row.online ? "在线" : "离线"}</td>
                <td className="px-4 py-3">{row.region ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3">{row.provider ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3 text-right">
                  <MetricCell metric={row.cpu} />
                </td>
                <td className="px-4 py-3 text-right">
                  <MetricCell metric={row.memory} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <section aria-labelledby={titleId} className={cn("overflow-hidden rounded-xl border bg-card", className)}>
      <header className="flex flex-wrap items-start justify-between gap-3 border-b px-5 py-4">
        <div>
          <h2 id={titleId} className="font-semibold">
            {title}
          </h2>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
            {description ??
              "来自兼容接口 GET /api/v1/nodes。旧 DTO 没有状态、版本与采样时间：离线行不显示历史百分比，在线数值只标“无采样时间”。"}
          </p>
        </div>
        <span className="rounded-md border px-2 py-1 text-xs" role="status">{countLabel}</span>
      </header>
      <div className="border-b px-5 py-3">
        <div role="search" aria-label="兼容节点列表搜索" className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label htmlFor={searchId} className="block text-xs font-medium text-muted-foreground">
              搜索名称或节点 ID
            </label>
            <input
              id={searchId}
              type="search"
              value={query}
              onChange={event => setQuery(event.target.value)}
              placeholder="仅搜索已加载的节点"
              disabled={total === 0}
              className="mt-1 h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            />
          </div>
          <button
            type="button"
            onClick={() => setQuery("")}
            disabled={!filtering}
            className="h-9 rounded-md border border-input bg-card px-3 text-sm font-medium hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          >
            清空搜索
          </button>
        </div>
      </div>
      {body}
      <p className="border-t px-5 py-3 text-xs leading-5 text-muted-foreground">
        兼容视图不提供详情或操作；计数只描述当前参数集与搜索命中数，不推断全局或隐藏节点。真正的授权与状态由 Controller 返回。
      </p>
    </section>
  );
}
