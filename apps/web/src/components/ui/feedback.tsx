import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { describeApiError, isApiError, type ApiError } from "@/lib/api/errors";
import { Button } from "./button";

type Tone = "info" | "success" | "warning" | "error";

const TONES: Record<Tone, string> = {
  info: "border-primary/25 bg-primary/5 text-foreground",
  success: "border-emerald-600/30 bg-emerald-50 text-emerald-900",
  warning: "border-amber-600/30 bg-amber-50 text-amber-900",
  error: "border-destructive/35 bg-destructive/5 text-foreground",
};

export function Notice({
  tone = "info",
  title,
  children,
  actions,
  className,
}: {
  readonly tone?: Tone;
  readonly title?: ReactNode;
  readonly children?: ReactNode;
  readonly actions?: ReactNode;
  readonly className?: string;
}) {
  return (
    <div
      role={tone === "error" ? "alert" : "status"}
      className={cn("rounded-lg border px-4 py-3 text-sm", TONES[tone], className)}
    >
      {title ? <p className="font-medium">{title}</p> : null}
      {children ? <div className={cn("text-muted-foreground", title && "mt-1")}>{children}</div> : null}
      {actions ? <div className="mt-3 flex flex-wrap gap-2">{actions}</div> : null}
    </div>
  );
}

export function ErrorPanel({
  error,
  onRetry,
  retryLabel = "重新加载",
  title = "加载失败",
  compact = false,
}: {
  readonly error: unknown;
  readonly onRetry?: () => void;
  readonly retryLabel?: string;
  readonly title?: string;
  readonly compact?: boolean;
}) {
  const apiError: ApiError | null = isApiError(error) ? error : null;
  const forbidden = apiError?.kind === "forbidden";
  const auth = apiError?.kind === "auth";
  return (
    <div className={cn("px-5 py-4", !compact && "sm:px-6")} aria-live="polite">
      <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3">
        <p className="text-sm font-medium text-destructive">
          {auth ? "需要登录" : forbidden ? "无权限" : title}
        </p>
        <p className="mt-1 text-sm text-muted-foreground">{describeApiError(error)}</p>
        {apiError && apiError.status > 0 ? (
          <p className="mt-1 text-xs text-muted-foreground">HTTP {apiError.status}</p>
        ) : null}
      </div>
      {onRetry && !forbidden && !auth ? (
        <Button type="button" onClick={onRetry} className="mt-3">
          {retryLabel}
        </Button>
      ) : null}
    </div>
  );
}

export function LoadingPanel({ label = "正在加载…" }: { readonly label?: string }) {
  return (
    <div role="status" className="px-5 py-8" aria-busy="true">
      <p className="text-sm text-muted-foreground">{label}</p>
      <div aria-hidden="true" className="mt-4 space-y-3">
        {[0, 1, 2].map(index => (
          <div key={index} className="h-8 animate-pulse rounded-md bg-muted" />
        ))}
      </div>
    </div>
  );
}

export function EmptyState({
  title,
  description,
  action,
}: {
  readonly title: string;
  readonly description?: ReactNode;
  readonly action?: ReactNode;
}) {
  return (
    <div className="px-6 py-10 text-center">
      <p className="font-medium">{title}</p>
      {description ? <p className="mx-auto mt-1 max-w-xl text-sm text-muted-foreground">{description}</p> : null}
      {action ? <div className="mt-4 flex justify-center">{action}</div> : null}
    </div>
  );
}
