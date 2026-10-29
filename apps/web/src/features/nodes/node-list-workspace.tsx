import { useState } from "react";
import { cn } from "@/lib/utils";
import type { NodeListItemView, NodeListState } from "./types";
import { NodeListPanel } from "./node-list-panel";
import { NodeDetailPanel } from "./node-detail";

export interface NodeListWorkspaceProps {
  readonly state: NodeListState;
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
  readonly onRetry?: () => void;
}

/**
 * 列表 + 只读详情的工作区包装。
 *
 * 选择边界（W03）：
 * - 只保存选中 ID，不保存节点对象；每次渲染都用调用方本轮传入的 ready 集合重新解析。
 * - 加载中、失败或该 ID 已不在来源集合时，详情立即消失，不会回退展示上一轮的数据，
 *   也不会自动改选其他节点。
 * - 本地搜索/状态筛选只影响表格可见行，不改变授权范围；详情仍按调用方集合解析，
 *   因此“筛掉但仍在已加载集合内”的节点仍可继续查看（隐藏不是权限动作）。
 * - 选中 ID 会保留：若同一 ID 之后又出现在新集合里，详情按新记录重新呈现，而不是旧快照。
 */
export function NodeListWorkspace({
  state,
  title,
  description,
  className,
  onRetry,
}: NodeListWorkspaceProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const selected =
    state.status === "ready" && selectedId !== null
      ? state.nodes.find(node => node.id === selectedId) ?? null
      : null;

  const handleActivate = (node: NodeListItemView) => setSelectedId(node.id);
  const handleClose = () => setSelectedId(null);

  return (
    <div className={cn("flex flex-col gap-4", className)}>
      <NodeListPanel
        state={state}
        title={title}
        description={description}
        onNodeActivate={handleActivate}
        onRetry={onRetry}
        selectedId={selected?.id ?? null}
      />
      {selected ? <NodeDetailPanel node={selected} onClose={handleClose} /> : null}
    </div>
  );
}
