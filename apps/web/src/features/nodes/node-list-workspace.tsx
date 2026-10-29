import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/utils";
import type { NodeListItemView, NodeListState } from "./types";
import { NodeListPanel, type NodeListPanelHandle } from "./node-list-panel";
import type { NodeActivateOrigin, NodeListHandle } from "./node-list";
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
 *
 * 焦点边界（W04）：
 * - 键盘（Enter/Space）激活行后，焦点移入详情区（`tabIndex=-1`，只作程序化焦点，
 *   不进入 Tab 序列）；鼠标点击不搬动焦点。
 * - 关闭按钮或 Esc 关闭时，焦点回到当前仍渲染的来源行；若该行已被本地筛选隐藏，
 *   则回到搜索框；再不行回到列表区域。全程按“此刻 DOM 是否存在”判断，不引用旧节点。
 * - 来源移除 / loading / error / 空列表导致详情消失时，若焦点原本在详情内，
 *   同样落到上面这条链上的第一个可用位置，不为保留焦点而保留旧内容。
 * - 非模态：没有焦点陷阱，Tab 可以照常离开详情；除详情内唯一的关闭按钮外不新增可聚焦控件。
 */
export function NodeListWorkspace({
  state,
  title,
  description,
  className,
  onRetry,
}: NodeListWorkspaceProps) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const listRef = useRef<NodeListHandle>(null);
  const panelRef = useRef<NodeListPanelHandle>(null);
  const detailRef = useRef<HTMLElement>(null);
  const detailHadFocus = useRef(false);
  const focusDetailNext = useRef(false);

  const selected =
    state.status === "ready" && selectedId !== null
      ? state.nodes.find(node => node.id === selectedId) ?? null
      : null;
  const hasSelection = selected !== null;

  /** 兜底顺序：仍存在的来源行 → 搜索框 → 列表区域。全部按当前 DOM 判定。 */
  function restoreFocus(preferredId: string | null) {
    if (preferredId && listRef.current?.focusNode(preferredId)) return;
    if (panelRef.current?.focusSearch()) return;
    listRef.current?.focusList();
  }

  // 键盘选择后把焦点移入详情；只在详情确实渲染出来时执行一次。
  useEffect(() => {
    if (!focusDetailNext.current) return;
    // 先清标志再聚焦：即使详情此刻不存在（已关闭/已失效），也不能把意图留给后台刷新。
    focusDetailNext.current = false;
    detailRef.current?.focus();
  });

  // 详情因来源变化、加载或失败而消失：清掉未执行的聚焦意图，并在需要时落到稳定位置。
  useEffect(() => {
    if (hasSelection) return;
    focusDetailNext.current = false;
    if (!detailHadFocus.current) return;
    detailHadFocus.current = false;
    restoreFocus(selectedId);
  }, [hasSelection, selectedId]);

  const handleActivate = (node: NodeListItemView, origin: NodeActivateOrigin) => {
    const alreadySelected = node.id === selectedId;
    setSelectedId(node.id);

    if (origin !== "keyboard") {
      // 指针操作取代未执行的键盘意图，避免旧请求稍后抢焦点。
      focusDetailNext.current = false;
      return;
    }

    // 同一节点重复激活时 selectedId 不变，React 不会提交更新，effect 也就不会执行；
    // 必须当场聚焦已挂载的详情，否则意图会漂到下一次无关渲染（例如后台刷新）才生效。
    if (alreadySelected && detailRef.current) {
      focusDetailNext.current = false;
      detailRef.current.focus();
      return;
    }

    focusDetailNext.current = true;
  };

  const handleClose = () => {
    const previousId = selectedId;
    focusDetailNext.current = false;
    detailHadFocus.current = false;
    setSelectedId(null);
    // 详情此时仍在 DOM 中，可以立即把焦点交还给仍存在的行或兜底位置。
    restoreFocus(previousId);
  };

  return (
    <div className={cn("flex min-w-0 flex-col gap-4", className)}>
      <NodeListPanel
        ref={panelRef}
        listRef={listRef}
        state={state}
        title={title}
        description={description}
        onNodeActivate={handleActivate}
        onRetry={onRetry}
        selectedId={selected?.id ?? null}
      />
      {selected ? (
        <div
          onFocus={() => {
            detailHadFocus.current = true;
          }}
          onBlur={event => {
            if (!event.currentTarget.contains(event.relatedTarget)) detailHadFocus.current = false;
          }}
        >
          <NodeDetailPanel ref={detailRef} node={selected} onClose={handleClose} />
        </div>
      ) : null}
    </div>
  );
}
