import type { ReactNode } from "react";
import { AlertCircle, AlertTriangle, CheckCircle2, Inbox, Info, Loader2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { describeApiError, isApiError, type ApiError } from "@/lib/api/errors";
import { Button } from "./button";

type Tone = "info" | "success" | "warning" | "error";

const TONES: Record<Tone, string> = {
  info: "border-[#3984E7]/25 bg-[#EFF6FF]/85 text-foreground",
  success: "border-[#169B68]/30 bg-[#ECFDF5]/95 text-emerald-950",
  warning: "border-[#D89B2B]/35 bg-[#FFFBEB]/95 text-amber-950",
  error: "border-[#DE5555]/35 bg-[#FEF2F2]/95 text-foreground",
};

const TONE_ICON: Record<Tone, ReactNode> = {
  info: <Info className="mt-0.5 size-4 shrink-0 text-[#3984E7]" aria-hidden="true" />,
  success: <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-[#169B68]" aria-hidden="true" />,
  warning: <AlertTriangle className="mt-0.5 size-4 shrink-0 text-[#D89B2B]" aria-hidden="true" />,
  error: <AlertCircle className="mt-0.5 size-4 shrink-0 text-[#DE5555]" aria-hidden="true" />,
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
      className={cn(
        "rounded-xl border px-4 py-3 text-sm shadow-[0_1px_2px_rgba(28,32,48,0.03)]",
        TONES[tone],
        className,
      )}
    >
      <div className="flex items-start gap-2.5">
        {TONE_ICON[tone]}
        <div className="min-w-0 flex-1">
          {title ? <p className="font-semibold tracking-tight">{title}</p> : null}
          {children ? (
            <div className={cn("text-muted-foreground", title && "mt-1")}>{children}</div>
          ) : null}
          {actions ? <div className="mt-3 flex flex-wrap gap-2">{actions}</div> : null}
        </div>
      </div>
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
      <div
        role="alert"
        className="rounded-xl border border-[#DE5555]/30 bg-[#FEF2F2]/90 px-4 py-3.5 shadow-[0_1px_2px_rgba(28,32,48,0.03)]"
      >
        <div className="flex items-start gap-2.5">
          <AlertCircle className="mt-0.5 size-4 shrink-0 text-[#DE5555]" aria-hidden="true" />
          <div className="min-w-0">
            <p className="text-sm font-semibold text-destructive">
              {auth ? "需要登录" : forbidden ? "无权限" : title}
            </p>
            <p className="mt-1 text-sm text-muted-foreground">{describeApiError(error)}</p>
            {apiError && apiError.status > 0 ? (
              <p className="mt-1 font-mono text-xs text-muted-foreground">HTTP {apiError.status}</p>
            ) : null}
          </div>
        </div>
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
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin text-[#6658E8]" aria-hidden="true" />
        <p>{label}</p>
      </div>
      <div aria-hidden="true" className="mt-4 space-y-3">
        {[0, 1, 2].map(index => (
          <div key={index} className="h-8 animate-pulse rounded-lg bg-muted" />
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
      <div className="mx-auto mb-2.5 flex size-10 items-center justify-center rounded-full bg-[#F0EEFF] text-[#6658E8]">
        <Inbox className="size-5" aria-hidden="true" />
      </div>
      <p className="font-semibold text-foreground">{title}</p>
      {description ? (
        <p className="mx-auto mt-1 max-w-xl text-sm leading-relaxed text-muted-foreground">
          {description}
        </p>
      ) : null}
      {action ? <div className="mt-4 flex justify-center">{action}</div> : null}
    </div>
  );
}
