import { useId, useState, type ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { NodeListItemView, NodeListState } from "./types";
import { NodeList } from "./node-list";
import {
  FILTERABLE_STATUSES,
  filterNodes,
  hasActiveFilter,
  summariseVisible,
  type NodeFilterCriteria,
  type NodeStatusFilter,
} from "./node-filters";
import { nodeStatusMeta } from "./view-model";

export interface NodeListPanelProps {
  /** 与 NodeList 相同的互斥状态；面板不改变加载/失败语义。 */
  readonly state: NodeListState;
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
  readonly onNodeActivate?: (node: NodeListItemView) => void;
  readonly onRetry?: () => void;
  /** 透传给 `NodeList` 的选中高亮；面板不解释选择语义。 */
  readonly selectedId?: string | null;
}

const NO_NODES: readonly NodeListItemView[] = [];

function NoMatches() {
  return (
    <div className="px-6 py-12 text-center">
      <p className="font-medium">没有匹配结果</p>
      <p className="mt-1 text-sm text-muted-foreground">
        当前筛选条件下没有节点。可更换关键词或状态，或使用“清空筛选”。
      </p>
    </div>
  );
}

/**
 * 节点列表面板：在已验收的 `NodeList` 之上加本地搜索、状态筛选与清空。
 *
 * 边界：
 * - 只过滤调用方已传入的节点集合，不发起任何请求，也不把筛选当作权限控制；
 *   可见性由上游决定，本地筛选既不授予也不收回任何权限。
 * - 不修改输入数组与元素，顺序保持原样；新鲜度等指标质量已由适配层定好，这里不再重算。
 * - “源列表为空”与“筛选后无匹配”是两种不同状态：前者沿用 NodeList 的“暂无节点”，
 *   后者用“没有匹配结果”并在标题副行说明，不会把搜索结果说成空系统。
 */
export function NodeListPanel({
  state,
  title,
  description,
  className,
  onNodeActivate,
  onRetry,
  selectedId,
}: NodeListPanelProps) {
  const searchId = useId();
  const statusId = useId();
  const [criteria, setCriteria] = useState<NodeFilterCriteria>({ query: "", status: "all" });

  const available = state.status === "ready";
  const sourceNodes = state.status === "ready" ? state.nodes : NO_NODES;
  const visibleNodes = filterNodes(sourceNodes, criteria);
  const total = sourceNodes.length;
  const visible = visibleNodes.length;
  const filtering = hasActiveFilter(criteria);
  const noMatch = available && total > 0 && visible === 0;

  const listState: NodeListState = state.status === "ready" ? { status: "ready", nodes: visibleNodes } : state;
  const summary = available ? summariseVisible(visible, total) : null;
  const controlsDisabled = !available;

  const update = (patch: Partial<NodeFilterCriteria>) => {
    setCriteria(current => ({ ...current, ...patch }));
  };

  const clear = () => setCriteria({ query: "", status: "all" });

  const controlClassName =
    "mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm transition-colors " +
    "placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring " +
    "disabled:cursor-not-allowed disabled:opacity-50";

  let emptyContent: ReactNode;
  if (noMatch) emptyContent = <NoMatches />;

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <div className="rounded-xl border bg-card px-6 py-4">
        <div role="search" aria-label="节点搜索与筛选" className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label htmlFor={searchId} className="block text-xs font-medium text-muted-foreground">
              搜索节点
            </label>
            <input
              id={searchId}
              type="search"
              value={criteria.query}
              disabled={controlsDisabled}
              onChange={event => update({ query: event.target.value })}
              placeholder="输入名称或节点 ID"
              className={cn(controlClassName, "w-full")}
            />
          </div>
          <div>
            <label htmlFor={statusId} className="block text-xs font-medium text-muted-foreground">
              状态筛选
            </label>
            <select
              id={statusId}
              value={criteria.status}
              disabled={controlsDisabled}
              onChange={event => update({ status: event.target.value as NodeStatusFilter })}
              className={controlClassName}
            >
              <option value="all">全部状态</option>
              {FILTERABLE_STATUSES.map(status => (
                <option key={status} value={status}>
                  {nodeStatusMeta[status].label}
                </option>
              ))}
            </select>
          </div>
          <button
            type="button"
            onClick={clear}
            disabled={!filtering}
            className="inline-flex h-9 items-center rounded-md border border-input bg-card px-3 text-sm font-medium transition-colors hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
          >
            清空筛选
          </button>
        </div>
        {summary ? (
          <p role="status" className="mt-3 text-xs text-muted-foreground">
            {summary}
          </p>
        ) : null}
        <p className="mt-2 text-xs text-muted-foreground">
          筛选只作用于当前已加载的节点，不改变任何授权范围。
        </p>
      </div>

      <NodeList
        state={listState}
        title={title}
        description={noMatch ? null : description}
        emptyContent={emptyContent}
        onNodeActivate={onNodeActivate}
        onRetry={onRetry}
        selectedId={selectedId}
      />
    </div>
  );
}
