import { useCallback, useEffect, useRef, useState } from "react";
import type { NodeListItemView, NodeListState } from "./types";
import { NodeListWorkspace } from "./node-list-workspace";

/**
 * 固定安全失败文案。
 * 上游异常可能含路径、token 或内部主机名，因此这里既不展示也不记录 `error.message`。
 */
export const LOAD_FAILURE_MESSAGE = "节点数据暂时无法加载，请稍后重试。";

/**
 * 局部只读加载接缝（W05）。
 *
 * 边界：
 * - 这不是公开 API，也不绑定任何未评审的公开 DTO；`load` 直接返回现有局部 ViewModel。
 * - `key` 是稳定、非秘密的来源/作用域标识：身份或授权范围变化时调用方必须更换它。
 *   它是渲染隔离的依据，**不是凭证**，本容器不校验 RBAC，也不能当作缓存许可。
 * - 真实实现（fetch 等）由后续 G2 契约与授权审查后的集成任务提供，本模块不含任何网络代码。
 */
export interface NodeSource {
  readonly key: string;
  load(options: { readonly signal: AbortSignal }): Promise<readonly NodeListItemView[]>;
}

export interface NodeLoaderProps {
  /** 缺少有效来源时按安全失败处理，不默认放行、不返回合成成功。 */
  readonly source: NodeSource | null;
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
}

type Snapshot =
  | { readonly key: string; readonly status: "loading" }
  | { readonly key: string; readonly status: "ready"; readonly nodes: readonly NodeListItemView[] }
  | { readonly key: string; readonly status: "error" };

/**
 * 可注入只读数据源的节点加载容器。
 *
 * 竞态与隔离：
 * - 渲染状态由 `snapshot.key === source.key` 在**渲染期**派生，来源切换后的首个提交
 *   就是 loading，不会先闪一帧旧来源的节点、计数或详情，也不依赖 effect 事后清空。
 * - 每个请求带自增序号；过期响应（成功或失败）即便来源忽略 `AbortSignal` 也会被丢弃。
 * - 卸载、来源切换与重试都会 abort 本容器发起的请求；不做轮询、后台重试或跨用户缓存。
 */
export function NodeLoader({ source, title, description, className }: NodeLoaderProps) {
  const sourceKey = source?.key.trim() ?? "";
  const usable = sourceKey !== "" && typeof source?.load === "function";

  const [snapshot, setSnapshot] = useState<Snapshot>({ key: sourceKey, status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const sequence = useRef(0);
  // 只按 key 重新加载：调用方内联传入的对象不会造成重复请求；key 变化即视为新来源。
  const sourceRef = useRef<NodeSource | null>(source);
  sourceRef.current = source;

  useEffect(() => {
    const active = sourceRef.current;
    if (!usable || !active) return;

    const id = ++sequence.current;
    const controller = new AbortController();

    setSnapshot(current =>
      current.key === active.key.trim() && current.status === "loading"
        ? current
        : { key: active.key.trim(), status: "loading" },
    );

    const settle = (next: Snapshot) => {
      if (id !== sequence.current || controller.signal.aborted) return;
      setSnapshot(next);
    };

    let pending: Promise<readonly NodeListItemView[]>;
    try {
      pending = Promise.resolve(active.load({ signal: controller.signal }));
    } catch {
      settle({ key: active.key.trim(), status: "error" });
      return () => controller.abort();
    }

    pending.then(
      nodes => {
        if (!Array.isArray(nodes)) {
          settle({ key: active.key.trim(), status: "error" });
          return;
        }
        settle({ key: active.key.trim(), status: "ready", nodes });
      },
      () => {
        settle({ key: active.key.trim(), status: "error" });
      },
    );

    return () => controller.abort();
  }, [usable, sourceKey, attempt]);

  const retry = useCallback(() => {
    // 与 attempt 自增同一次提交：重试当帧就回到 loading，不保留成功节点/计数/详情。
    setSnapshot({ key: sourceKey, status: "loading" });
    setAttempt(value => value + 1);
  }, [sourceKey]);

  const current = usable && snapshot.key === sourceKey ? snapshot : null;

  let state: NodeListState;
  if (!usable) state = { status: "error", message: LOAD_FAILURE_MESSAGE };
  else if (!current || current.status === "loading") state = { status: "loading" };
  else if (current.status === "error") state = { status: "error", message: LOAD_FAILURE_MESSAGE };
  else state = { status: "ready", nodes: current.nodes };

  return (
    <NodeListWorkspace
      // 来源/作用域切换时重建工作区：不继承上一个来源的选中项与筛选条件。
      key={sourceKey === "" ? "no-source" : sourceKey}
      state={state}
      title={title}
      description={description}
      className={className}
      onRetry={usable ? retry : undefined}
    />
  );
}
