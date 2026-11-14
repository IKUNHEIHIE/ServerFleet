import { useId, useMemo, useState } from "react";
import { Search, Server, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { TextInput } from "@/components/ui/form";
import { cn } from "@/lib/utils";
import { LegacyMetricCell } from "./metric-display";
import type { LegacyNodeRow } from "./legacy-types";

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

function NameCell({ row }: { readonly row: LegacyNodeRow }) {
  return (
    <th scope="row" className="px-4 py-3.5 text-left font-normal">
      <div className="flex items-start gap-2.5">
        <span
          aria-hidden="true"
          className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-lg border border-border/80 bg-[#F6F7FB] text-primary"
        >
          <Server className="size-3.5" />
        </span>
        <div>
          <span className="font-semibold text-foreground">{row.name}</span>
          {row.nameIssue ? (
            <span className="mt-0.5 block text-xs text-amber-700">
              {row.nameIssue === "empty" ? "名称为空，显示节点 ID" : "名称字段非法，显示节点 ID"}
            </span>
          ) : null}
          {row.name === row.id ? null : (
            <span className="mt-0.5 block break-all font-mono text-xs text-muted-foreground">{row.id}</span>
          )}
        </div>
      </div>
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
            <tr className="border-b border-border/70 bg-[#F6F7FB]/90">
              {["节点", "状态", "地区", "云厂商", "CPU", "内存"].map((label, index) => (
                <th
                  key={label}
                  scope="col"
                  className={cn(
                    "whitespace-nowrap px-4 py-3 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground",
                    index >= 4 ? "text-right" : "text-left",
                  )}
                >
                  {label}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-border/60">
            {visible.map(row => (
              <tr key={row.id} className="align-top transition-colors hover:bg-primary/[0.025]">
                <NameCell row={row} />
                <td className="px-4 py-3.5">
                  <span
                    className={cn(
                      "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-semibold",
                      row.online
                        ? "border-emerald-500/25 bg-emerald-500/10 text-emerald-800"
                        : "border-border bg-secondary text-muted-foreground",
                    )}
                  >
                    <span
                      aria-hidden="true"
                      className={cn(
                        "size-1.5 rounded-full",
                        row.online ? "bg-emerald-500" : "bg-muted-foreground/60",
                      )}
                    />
                    {row.online ? "在线" : "离线"}
                  </span>
                </td>
                <td className="px-4 py-3.5">{row.region ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3.5">{row.provider ?? <span className="text-muted-foreground">缺失</span>}</td>
                <td className="px-4 py-3.5 text-right">
                  <LegacyMetricCell metric={row.cpu} />
                </td>
                <td className="px-4 py-3.5 text-right">
                  <LegacyMetricCell metric={row.memory} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <section
      aria-labelledby={titleId}
      className={cn("overflow-hidden rounded-xl border border-border/80 bg-card shadow-[var(--shadow-card)]", className)}
    >
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border/70 bg-gradient-to-b from-white to-[#FAFBFD] px-5 py-4">
        <div>
          <h2 id={titleId} className="font-semibold text-foreground">
            {title}
          </h2>
          <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">
            {description ??
              "来自兼容接口 GET /api/v1/nodes。旧 DTO 没有状态、版本与采样时间：离线行不显示历史百分比，在线数值只标“无采样时间”。"}
          </p>
        </div>
        <Badge tone="primary" role="status" className="py-1">
          {countLabel}
        </Badge>
      </header>
      <div className="border-b border-border/70 bg-[#FAFBFD] px-5 py-3.5">
        <div role="search" aria-label="兼容节点列表搜索" className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label htmlFor={searchId} className="block text-xs font-semibold text-muted-foreground">
              搜索名称或节点 ID
            </label>
            <div className="relative mt-1">
              <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <TextInput
                id={searchId}
                type="search"
                value={query}
                onChange={event => setQuery(event.target.value)}
                placeholder="仅搜索已加载的节点"
                disabled={total === 0}
                className="mt-0 h-9 pl-8"
              />
            </div>
          </div>
          <Button
            type="button"
            onClick={() => setQuery("")}
            disabled={!filtering}
          >
            <X aria-hidden="true" className="size-3.5 text-muted-foreground" />
            清空搜索
          </Button>
        </div>
      </div>
      {body}
      <p className="border-t border-border/70 bg-[#FAFBFD] px-5 py-3 text-xs leading-5 text-muted-foreground">
        兼容视图不提供详情或操作；计数只描述当前参数集与搜索命中数，不推断全局或隐藏节点。真正的授权与状态由 Controller 返回。
      </p>
    </section>
  );
}
