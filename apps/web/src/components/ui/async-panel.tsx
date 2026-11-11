import type { ReactNode } from "react";
import type { ResourceState } from "@/lib/data/use-resource";
import { EmptyState, ErrorPanel, LoadingPanel } from "./feedback";

export interface AsyncPanelProps<T> {
  readonly state: ResourceState<T>;
  readonly onRetry?: () => void;
  readonly loadingLabel?: string;
  readonly isEmpty?: (data: T) => boolean;
  readonly empty?: ReactNode;
  readonly children: (data: T) => ReactNode;
  readonly errorTitle?: string;
}

/** 统一加载/失败/空/就绪分支；失败分支没有数据，避免“出错仍展示旧表”。 */
export function AsyncPanel<T>({
  state,
  onRetry,
  loadingLabel,
  isEmpty,
  empty,
  children,
  errorTitle,
}: AsyncPanelProps<T>) {
  if (state.status === "idle") return <EmptyState title="尚未加载" description="当前作用域未请求数据。" />;
  if (state.status === "loading") return <LoadingPanel label={loadingLabel} />;
  if (state.status === "error") return <ErrorPanel error={state.error} onRetry={onRetry} title={errorTitle} />;
  if (isEmpty?.(state.data)) return <>{empty ?? <EmptyState title="暂无数据" />}</>;
  return (
    <>
      {state.refreshError ? (
        <div className="border-b px-5 py-2 text-xs text-amber-800">
          最近一次刷新失败：{state.refreshError.code ?? state.refreshError.kind}。以下内容为上次成功结果，可能已过期。
        </div>
      ) : null}
      {children(state.data)}
    </>
  );
}
