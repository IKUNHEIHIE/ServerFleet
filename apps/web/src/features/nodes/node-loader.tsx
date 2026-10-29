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
 *   身份按**精确字符串**比较，不 trim：`"scope-a"` 与 `" scope-a "` 是两个不同的来源；
 *   带首尾空白的 key 视为无效来源（安全失败），不会被静默规范化。
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
  | { readonly epoch: number; readonly key: string; readonly status: "loading" }
  | {
      readonly epoch: number;
      readonly key: string;
      readonly status: "ready";
      readonly nodes: readonly NodeListItemView[];
    }
  | { readonly epoch: number; readonly key: string; readonly status: "error" };

/**
 * 可注入只读数据源的节点加载容器。
 *
 * 竞态与隔离：
 * - 渲染状态由「精确 key + 世代编号」在**渲染期**派生：来源切换，或来源失效后恢复，
 *   首个提交就是 loading，不会先闪一帧旧世代的节点、计数或详情，也不依赖 effect 事后清空。
 * - 每个请求带自增序号；过期响应（成功或失败）即便来源忽略 `AbortSignal` 也会被丢弃。
 * - 卸载、来源切换与重试都会 abort 本容器发起的请求；不做轮询、后台重试或跨用户缓存。
 */
export function NodeLoader({ source, title, description, className }: NodeLoaderProps) {
  // 身份用精确 key：不做 trim，避免把 `scope-a` 与 ` scope-a ` 静默合并成同一作用域。
  const sourceKey = source?.key ?? "";
  const keyValid = sourceKey !== "" && sourceKey.trim() === sourceKey;
  const usable = keyValid && typeof source?.load === "function";

  // 世代编号由「精确 key + 可用性」共同决定：key 变化（含 A → 无效来源 → A）、
  // 或同 key 的 load 从函数变成非函数再恢复，都会开启新世代。上一世代的 ready 快照
  // 因此立即失效，恢复来源后的首个提交不会重新显示旧节点/计数/详情。
  const identityKey = `${sourceKey}::${usable ? "valid" : "invalid"}`;
  const [identity, setIdentity] = useState({ key: identityKey, epoch: 0 });
  if (identity.key !== identityKey) {
    // 渲染期同步调整自身 state：React 会在提交前立即重渲染，本次提交已用新世代。
    setIdentity({ key: identityKey, epoch: identity.epoch + 1 });
  }
  const epoch = identity.key === identityKey ? identity.epoch : identity.epoch + 1;

  const [snapshot, setSnapshot] = useState<Snapshot>({ epoch, key: sourceKey, status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const sequence = useRef(0);
  // 只按精确 key 与世代重新加载：调用方内联传入的对象不会造成重复请求。
  const sourceRef = useRef<NodeSource | null>(source);
  sourceRef.current = source;

  useEffect(() => {
    const active = sourceRef.current;
    if (!usable || !active) return;

    const id = ++sequence.current;
    const controller = new AbortController();

    setSnapshot(current =>
      current.epoch === epoch && current.key === sourceKey && current.status === "loading"
        ? current
        : { epoch, key: sourceKey, status: "loading" },
    );

    const settle = (next: Snapshot) => {
      if (id !== sequence.current || controller.signal.aborted) return;
      setSnapshot(next);
    };

    let pending: Promise<readonly NodeListItemView[]>;
    try {
      pending = Promise.resolve(active.load({ signal: controller.signal }));
    } catch {
      settle({ epoch, key: sourceKey, status: "error" });
      return () => controller.abort();
    }

    pending.then(
      nodes => {
        if (!Array.isArray(nodes)) {
          settle({ epoch, key: sourceKey, status: "error" });
          return;
        }
        settle({ epoch, key: sourceKey, status: "ready", nodes });
      },
      () => {
        settle({ epoch, key: sourceKey, status: "error" });
      },
    );

    return () => controller.abort();
  }, [usable, epoch, sourceKey, attempt]);

  const retry = useCallback(() => {
    // 与 attempt 自增同一次提交：重试当帧就回到 loading，不保留成功节点/计数/详情。
    setSnapshot({ epoch, key: sourceKey, status: "loading" });
    setAttempt(value => value + 1);
  }, [epoch, sourceKey]);

  const current =
    usable && snapshot.key === sourceKey && snapshot.epoch === epoch ? snapshot : null;

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
