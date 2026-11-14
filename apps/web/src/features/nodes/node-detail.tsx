import { useId, type KeyboardEvent, type ReactNode, type Ref } from "react";
import { Server, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import type { NodeListItemView } from "./types";
import { MetricValue } from "./metric-value";
import { NodeStatusBadge } from "./node-status-badge";
import { formatLastSeen } from "./view-model";

export interface NodeDetailPanelProps {
  /** 当前选中的节点，由调用方每次从最新集合解析后传入；组件自身不缓存。 */
  readonly node: NodeListItemView;
  /** 关闭入口；未提供时不渲染按钮。Esc 同样触发（键盘可用性）。 */
  readonly onClose?: () => void;
  readonly className?: string;
  /** 供包装层在键盘选择后把焦点移入详情区（`tabIndex=-1`，不进入 Tab 序列）。 */
  readonly ref?: Ref<HTMLElement>;
}

function Field({ term, children }: { readonly term: string; readonly children: ReactNode }) {
  return (
    <div className="rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
      <dt className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">{term}</dt>
      <dd className="mt-1.5 text-sm font-medium text-foreground">{children}</dd>
    </div>
  );
}

function UnknownValue() {
  return <span className="text-muted-foreground">未知</span>;
}

/**
 * 只读节点详情。
 *
 * 边界：
 * - 只展示 `NodeListItemView` 已有字段，不新增 IP/地区/凭据/连接诊断等未冻结字段，
 *   也不推断关机、认证或在线原因；未知就是未知。
 * - 指标复用 `describeMetric` 的判定：离线无值、非法/缺失只给原因、陈旧值带可见采样时间，
 *   真实的 0% 与 100% 原样显示。
 * - 非模态、无弹窗库，只有一个关闭操作；不提供编辑/删除/执行等入口。
 */
export function NodeDetailPanel({ node, onClose, className, ref }: NodeDetailPanelProps) {
  const headingId = useId();

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "Escape" || !onClose) return;
    event.stopPropagation();
    onClose();
  };

  return (
    <section
      ref={ref}
      tabIndex={-1}
      aria-labelledby={headingId}
      onKeyDown={handleKeyDown}
      className={cn(
        "relative overflow-hidden rounded-xl border border-primary/25 bg-card shadow-[var(--shadow-card-hover)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        className,
      )}
    >
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-x-0 top-0 h-0.5 bg-gradient-to-r from-[#6658E8] via-[#3984E7] to-[#169B68]"
      />
      <header className="flex flex-wrap items-start justify-between gap-4 border-b border-border/70 bg-gradient-to-b from-primary/[0.03] to-transparent px-6 py-4">
        <div className="flex items-start gap-3">
          <span
            aria-hidden="true"
            className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-xl border border-primary/20 bg-primary-light text-primary"
          >
            <Server className="size-4" />
          </span>
          <div>
            <h2 id={headingId} className="font-semibold text-foreground">
              节点详情
            </h2>
            <p className="mt-1 whitespace-nowrap font-semibold text-foreground">{node.name}</p>
            {node.name === node.id ? null : (
              <p className="whitespace-nowrap font-mono text-xs text-muted-foreground">{node.id}</p>
            )}
          </div>
        </div>
        {onClose ? (
          <Button type="button" onClick={onClose}>
            <X aria-hidden="true" className="size-3.5 text-muted-foreground" />
            关闭详情
          </Button>
        ) : null}
      </header>

      <dl className="grid gap-3.5 px-6 py-5 sm:grid-cols-2 lg:grid-cols-3">
        <Field term="状态">
          <NodeStatusBadge node={node} />
        </Field>
        <Field term="平台">{node.platform ?? <UnknownValue />}</Field>
        <Field term="Agent 版本">{node.agentVersion ?? <UnknownValue />}</Field>
        <Field term="最近在线">{formatLastSeen(node.lastSeenAt) ?? <UnknownValue />}</Field>
        <Field term="CPU 使用率">
          <MetricValue reading={node.cpu} revealReason />
        </Field>
        <Field term="内存使用率">
          <MetricValue reading={node.memory} revealReason />
        </Field>
      </dl>

      <p className="border-t border-border/70 bg-[#FAFBFD] px-6 py-3 text-xs text-muted-foreground">
        只读视图：数据来自当前已加载的节点集合，不在此执行任何操作。
      </p>
    </section>
  );
}
