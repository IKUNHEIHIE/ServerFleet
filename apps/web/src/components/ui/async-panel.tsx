import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { ApiError } from "@/lib/api/errors";
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

export function InlineError({ error }: { readonly error: ApiError | null }) {
  if (!error) return null;
  return (
    <p role="alert" className="text-sm font-medium text-destructive">
      {error.code ? `${error.code} · ` : ""}
      {error.kind === "forbidden" ? "服务端拒绝了此操作。" : "操作未完成，请检查输入或稍后重试。"}
      {error.requestId ? <span className="ml-1 font-normal text-muted-foreground">请求 {error.requestId}</span> : null}
    </p>
  );
}

export function FieldErrorList({ error }: { readonly error: ApiError | null }) {
  if (!error || error.fieldErrors.length === 0) return null;
  return (
    <ul role="alert" className="space-y-1 text-xs text-destructive">
      {error.fieldErrors.map(issue => (
        <li key={`${issue.pointer}-${issue.message}`}>
          <span className="font-mono">{issue.pointer}</span>: {issue.message}
        </li>
      ))}
    </ul>
  );
}

export function StatusDot({ tone = "neutral", label }: { readonly tone?: "green" | "amber" | "red" | "neutral"; readonly label: string }) {
  const color =
    tone === "green" ? "bg-emerald-500" : tone === "amber" ? "bg-amber-500" : tone === "red" ? "bg-destructive" : "bg-muted-foreground/60";
  return (
    <span className="inline-flex items-center gap-1.5 whitespace-nowrap text-xs">
      <span aria-hidden="true" className={cn("size-2 rounded-full", color)} />
      {label}
    </span>
  );
}
