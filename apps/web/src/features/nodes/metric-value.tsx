import { cn } from "@/lib/utils";
import type { MetricReading } from "./types";
import { describeMetric } from "./view-model";

/**
 * 缺失值标记。
 * 视觉上沿用原型的破折号，但给读屏器一个明确原因，
 * 避免“—”、“无有效数据”、“0”在同一列里语义不清。
 */
export function MissingValue({ label, className }: { readonly label: string; readonly className?: string }) {
  return (
    <span className={cn("text-muted-foreground", className)}>
      <span aria-hidden="true">—</span>
      <span className="sr-only">{label}</span>
    </span>
  );
}

export interface MetricValueProps {
  readonly reading: MetricReading;
  /**
   * 详情区等有空间的场景把不可用原因直接显示出来；
   * 表格里保持破折号 + 读屏原因，避免每格都堆长句。
   */
  readonly revealReason?: boolean;
  readonly className?: string;
}

/**
 * 指标数值。
 *
 * 只有新鲜或陈旧样本才有数字；陈旧样本必须同时渲染可见的“陈旧 + 采样时间”，
 * 不使用 title/悬停承载时效信息，键盘与触摸用户同样能看到。
 */
export function MetricValue({ reading, revealReason = false, className }: MetricValueProps) {
  const metric = describeMetric(reading);

  if (metric.kind === "empty") {
    const reason = metric.reason ?? "无有效数据";
    if (revealReason) return <span className={cn("text-muted-foreground", className)}>{reason}</span>;
    return <MissingValue label={reason} className={className} />;
  }

  const clamped = reading.percent !== null ? Math.max(0, Math.min(100, Math.round(reading.percent))) : 0;
  const barColor = metric.stale
    ? "bg-amber-500"
    : clamped >= 85
      ? "bg-[#DE5555]"
      : clamped >= 65
        ? "bg-[#D89B2B]"
        : "bg-[#6658E8]";

  return (
    <span className={cn("inline-flex flex-col gap-1", className)}>
      <span className="inline-flex items-center gap-2">
        <span
          aria-hidden="true"
          className="hidden h-1.5 w-14 overflow-hidden rounded-full bg-secondary sm:inline-block"
        >
          <span
            className={cn("block h-full rounded-full transition-all duration-500", barColor)}
            style={{ width: `${clamped}%` }}
          />
        </span>
        <span className="font-semibold tabular-nums text-foreground">{metric.value}</span>
      </span>
      {metric.note ? (
        <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-800">
          {metric.note}
        </span>
      ) : null}
    </span>
  );
}
