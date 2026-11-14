import { useId, useImperativeHandle, useRef, useState, type ReactNode, type Ref } from "react";
import { Button } from "@/components/ui/button";
import { Select, TextInput } from "@/components/ui/form";
import { SubCard } from "@/components/ui/panel";
import { cn } from "@/lib/utils";
import type { NodeListItemView, NodeListState } from "./types";
import { NodeList, type NodeActivateOrigin, type NodeListHandle } from "./node-list";
import {
  FILTERABLE_STATUSES,
  filterNodes,
  hasActiveFilter,
  summariseVisible,
  type NodeFilterCriteria,
  type NodeStatusFilter,
} from "./node-filters";
import { nodeStatusMeta } from "./view-model";

/** 面板对外的焦点入口：搜索框是筛选场景下的稳定兜底位置。 */
export interface NodeListPanelHandle {
  /** 聚焦搜索框；控件不可用（加载/失败被禁用）时返回 false。 */
  focusSearch(): boolean;
}

export interface NodeListPanelProps {
  /** 与 NodeList 相同的互斥状态；面板不改变加载/失败语义。 */
  readonly state: NodeListState;
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
  readonly onNodeActivate?: (node: NodeListItemView, origin: NodeActivateOrigin) => void;
  readonly onRetry?: () => void;
  /** 透传给 `NodeList` 的选中高亮；面板不解释选择语义。 */
  readonly selectedId?: string | null;
  /** 透传给 `NodeList` 的焦点入口，供包装层按 ID 聚焦行。 */
  readonly listRef?: Ref<NodeListHandle>;
  readonly ref?: Ref<NodeListPanelHandle>;
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
  listRef,
  ref,
}: NodeListPanelProps) {
  const searchId = useId();
  const statusId = useId();
  const searchRef = useRef<HTMLInputElement | null>(null);
  const [criteria, setCriteria] = useState<NodeFilterCriteria>({ query: "", status: "all" });

  useImperativeHandle(
    ref,
    () => ({
      focusSearch() {
        const input = searchRef.current;
        if (!input || !input.isConnected || input.disabled) return false;
        input.focus();
        return document.activeElement === input;
      },
    }),
    [],
  );

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

  let emptyContent: ReactNode;
  if (noMatch) emptyContent = <NoMatches />;

  return (
    <div className={cn("flex min-w-0 flex-col gap-4", className)}>
      <SubCard className="bg-gradient-to-b from-white to-[#FAFBFD] px-6 py-4 shadow-[var(--shadow-card)]">
        <div role="search" aria-label="节点搜索与筛选" className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label htmlFor={searchId} className="block text-xs font-semibold text-muted-foreground">
              搜索节点
            </label>
            <TextInput
              ref={searchRef}
              id={searchId}
              type="search"
              value={criteria.query}
              disabled={controlsDisabled}
              onChange={event => update({ query: event.target.value })}
              placeholder="输入名称或节点 ID"
              className="h-9"
            />
          </div>
          <div>
            <label htmlFor={statusId} className="block text-xs font-semibold text-muted-foreground">
              状态筛选
            </label>
            <Select
              id={statusId}
              value={criteria.status}
              disabled={controlsDisabled}
              onChange={event => update({ status: event.target.value as NodeStatusFilter })}
              className="h-9 w-auto py-1"
            >
              <option value="all">全部状态</option>
              {FILTERABLE_STATUSES.map(status => (
                <option key={status} value={status}>
                  {nodeStatusMeta[status].label}
                </option>
              ))}
            </Select>
          </div>
          <Button type="button" onClick={clear} disabled={!filtering}>
            清空筛选
          </Button>
        </div>
        {summary ? (
          <p role="status" className="mt-3 text-xs font-medium text-foreground/85">
            {summary}
          </p>
        ) : null}
        <p className="mt-2 text-xs text-muted-foreground">
          筛选只作用于当前已加载的节点，不改变任何授权范围。
        </p>
      </SubCard>

      <NodeList
        ref={listRef}
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
