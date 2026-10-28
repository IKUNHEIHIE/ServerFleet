import { useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { MetricReading, NodeListItemView, NodeListState } from "./types";
import {
  countLabel,
  describeMetric,
  formatLastSeen,
  nodeStatusMeta,
  resolveTitle,
  summariseNodes,
} from "./view-model";

export interface NodeListProps {
  /** 互斥状态：加载 / 失败 / 就绪（就绪但为空即空列表）。 */
  readonly state: NodeListState;
  readonly title?: string;
  readonly description?: string;
  readonly className?: string;
  /** 行被激活（键盘 Enter/Space 或鼠标点击）时触发；未提供时行不可激活。 */
  readonly onNodeActivate?: (node: NodeListItemView) => void;
  /** 失败状态下的重试入口；未提供时不渲染按钮，避免出现无效操作。 */
  readonly onRetry?: () => void;
}

interface Column {
  readonly key: string;
  readonly label: string;
  readonly align: "left" | "right";
}

const COLUMNS: readonly Column[] = [
  { key: "node", label: "节点", align: "left" },
  { key: "status", label: "状态", align: "left" },
  { key: "platform", label: "平台", align: "left" },
  { key: "agent", label: "Agent 版本", align: "left" },
  { key: "cpu", label: "CPU 使用率", align: "right" },
  { key: "memory", label: "内存使用率", align: "right" },
  { key: "lastSeen", label: "最近在线", align: "left" },
];

/**
 * 缺失值标记。
 * 视觉上沿用原型的破折号，但给读屏器一个明确原因，
 * 避免“—”、“无有效数据”、“0”在同一列里语义不清。
 */
function MissingValue({ label }: { readonly label: string }) {
  return (
    <span className="text-muted-foreground">
      <span aria-hidden="true">—</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}

/**
 * 指标单元格。
 *
 * 只有新鲜或陈旧样本才有数字；陈旧样本必须同时渲染可见的“陈旧 + 采样时间”，
 * 不使用 title/悬停承载时效信息，键盘与触摸用户同样能看到。
 */
function MetricCell({ reading }: { readonly reading: MetricReading }) {
  const metric = describeMetric(reading);
  if (metric.kind === "empty") return <MissingValue label={metric.reason ?? "无有效数据"} />;
  return (
    <span className="inline-flex flex-col items-end gap-0.5">
      <span className="tabular-nums">{metric.value}</span>
      {metric.note ? (
        <span className="text-xs font-medium text-amber-700">{metric.note}</span>
      ) : null}
    </span>
  );
}

function NodeStatusBadge({ node }: { readonly node: NodeListItemView }) {
  const meta = nodeStatusMeta[node.status];
  return (
    <Badge variant="outline" className={cn("gap-1.5 whitespace-nowrap", meta.badgeClassName)}>
      <span aria-hidden="true" className={cn("size-1.5 rounded-full", meta.dotClassName)} />
      {meta.label}
    </Badge>
  );
}

interface NodeTableProps {
  readonly labelledBy: string;
  readonly nodes: readonly NodeListItemView[];
  readonly onNodeActivate?: (node: NodeListItemView) => void;
}

/**
 * 表格主体。
 * 键盘交互采用 roving tabindex：只有一行进入 Tab 序列，其余行用方向键移动，
 * Home/End 跳到首尾，Enter/Space 激活——与“每行都是可聚焦按钮”相比不会让
 * 键盘用户连续按十几次 Tab 才能越过列表。
 */
function NodeTable({ labelledBy, nodes, onNodeActivate }: NodeTableProps) {
  const [activeId, setActiveId] = useState<string | null>(nodes[0]?.id ?? null);
  const rowRefs = useRef<Array<HTMLTableRowElement | null>>([]);
  const foundIndex = nodes.findIndex(node => node.id === activeId);
  const activeIndex = foundIndex === -1 ? 0 : foundIndex;

  function focusRow(index: number) {
    const target = nodes[index];
    if (!target) return;
    setActiveId(target.id);
    rowRefs.current[index]?.focus();
  }

  function handleKeyDown(event: KeyboardEvent<HTMLTableRowElement>, index: number) {
    const lastIndex = nodes.length - 1;
    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        focusRow(Math.min(index + 1, lastIndex));
        return;
      case "ArrowUp":
        event.preventDefault();
        focusRow(Math.max(index - 1, 0));
        return;
      case "Home":
        event.preventDefault();
        focusRow(0);
        return;
      case "End":
        event.preventDefault();
        focusRow(lastIndex);
        return;
      case "Enter":
      case " ": {
        if (!onNodeActivate) return;
        event.preventDefault();
        const target = nodes[index];
        if (target) onNodeActivate(target);
        return;
      }
      default:
    }
  }

  return (
    <div className="overflow-x-auto">
      <table aria-labelledby={labelledBy} className="w-full border-collapse text-sm">
        <thead>
          <tr className="bg-secondary/60">
            {COLUMNS.map(column => (
              <th
                key={column.key}
                scope="col"
                className={cn(
                  "whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground",
                  column.align === "right" ? "text-right" : "text-left",
                )}
              >
                {column.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {nodes.map((node, index) => (
            <tr
              key={node.id}
              ref={element => {
                rowRefs.current[index] = element;
              }}
              tabIndex={index === activeIndex ? 0 : -1}
              onClick={onNodeActivate ? () => onNodeActivate(node) : undefined}
              onKeyDown={event => handleKeyDown(event, index)}
              className={cn(
                "border-t align-top transition-colors",
                onNodeActivate && "cursor-pointer hover:bg-secondary/40",
                index === activeIndex && "bg-secondary/30",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset",
              )}
            >
              <th scope="row" className="px-4 py-3 text-left font-normal">
                <span className="whitespace-nowrap font-medium">{node.name}</span>
                {node.name === node.id ? null : (
                  <span className="mt-0.5 block whitespace-nowrap font-mono text-xs text-muted-foreground">
                    {node.id}
                  </span>
                )}
              </th>
              <td className="px-4 py-3">
                <NodeStatusBadge node={node} />
              </td>
              <td className="px-4 py-3">
                {node.platform ?? <MissingValue label="未知" />}
              </td>
              <td className="px-4 py-3">
                {node.agentVersion ?? <MissingValue label="未知" />}
              </td>
              <td className="px-4 py-3 text-right">
                <MetricCell reading={node.cpu} />
              </td>
              <td className="px-4 py-3 text-right">
                <MetricCell reading={node.memory} />
              </td>
              <td className="px-4 py-3 whitespace-nowrap">
                {formatLastSeen(node.lastSeenAt) ?? <MissingValue label="未知" />}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function LoadingState() {
  return (
    <div role="status" className="px-6 py-8">
      <p className="text-sm text-muted-foreground">正在加载节点…</p>
      <div aria-hidden="true" className="mt-4 space-y-3">
        {[0, 1, 2].map(index => (
          <div key={index} className="h-9 animate-pulse rounded-md bg-muted" />
        ))}
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="px-6 py-12 text-center">
      <p className="font-medium">暂无节点</p>
      <p className="mt-1 text-sm text-muted-foreground">节点经授权并接入后，状态与指标会显示在这里。</p>
    </div>
  );
}

function ErrorState({ message, onRetry }: { readonly message?: string; readonly onRetry?: () => void }) {
  return (
    <div className="px-6 py-8">
      <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3">
        <p className="text-sm font-medium text-destructive">节点列表加载失败</p>
        <p className="mt-1 text-sm text-muted-foreground">
          {message?.trim() || "未能从 Controller 读取节点列表，请稍后重试。"}
        </p>
      </div>
      {onRetry ? (
        <button
          type="button"
          onClick={onRetry}
          className="mt-4 inline-flex items-center rounded-md border border-input bg-card px-3 py-1.5 text-sm font-medium transition-colors hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          重新加载
        </button>
      ) : null}
    </div>
  );
}

function MetricFootnote() {
  return (
    <p className="border-t px-6 py-3 text-xs text-muted-foreground">
      仅新鲜样本显示百分比；缺失、非法、未来时间或超出 0–100 范围的读数显示“无有效数据”，陈旧读数带“陈旧”与采样时间（UTC）；均不按 0 计。
    </p>
  );
}

/**
 * 节点列表。
 * 纯展示：不发起任何请求，数据与状态由调用方注入（本轮不接入 App，接入口由集成负责人统一完成）。
 */
export function NodeList({ state, title, description, className, onNodeActivate, onRetry }: NodeListProps) {
  const headingId = useId();
  const subtitle = description ?? (state.status === "ready" ? summariseNodes(state.nodes) : null);

  let body: ReactNode;
  if (state.status === "loading") body = <LoadingState />;
  else if (state.status === "error") body = <ErrorState message={state.message} onRetry={onRetry} />;
  else if (state.nodes.length === 0) body = <EmptyState />;
  else body = <NodeTable labelledBy={headingId} nodes={state.nodes} onNodeActivate={onNodeActivate} />;

  const showFootnote = state.status === "ready" && state.nodes.length > 0;

  return (
    <section
      aria-labelledby={headingId}
      aria-busy={state.status === "loading"}
      className={cn("overflow-hidden rounded-xl border bg-card", className)}
    >
      <header className="flex flex-wrap items-start justify-between gap-4 border-b px-6 py-4">
        <div>
          <h2 id={headingId} className="font-semibold">
            {resolveTitle(title)}
          </h2>
          {subtitle ? <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p> : null}
        </div>
        <Badge
          variant="outline"
          className={cn(state.status === "error" && "border-destructive/40 text-destructive")}
        >
          {countLabel(state)}
        </Badge>
      </header>
      {body}
      {showFootnote ? <MetricFootnote /> : null}
    </section>
  );
}
