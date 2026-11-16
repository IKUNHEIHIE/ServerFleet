import { formatDateTime } from "@/lib/format";
import type { LegacyMetric } from "./legacy-types";

export function formatLegacyPercent(percent: number): string {
  return `${percent.toFixed(1).replace(/\.0$/, "")}%`;
}

export function legacyMetricText(metric: LegacyMetric): string {
  if (metric.kind === "value") return formatLegacyPercent(metric.percent);
  if (metric.kind === "unavailable") return "离线，无当前读数";
  if (metric.kind === "absent") return "无读数";
  return metric.reason === "out-of-range" ? "读数超出 0–100" : "读数非法";
}

/** 旧 DTO 指标：有数值必须同时露出“无采样时间”；离线不显示历史百分比。 */
export function LegacyMetricCell({ metric, sampledAt }: { readonly metric: LegacyMetric; readonly sampledAt?: string | null }) {
  if (metric.kind === "value") {
    const clamped = Math.max(0, Math.min(100, metric.percent));
    const barColor =
      clamped >= 85 ? "bg-[#DE5555]" : clamped >= 65 ? "bg-[#D89B2B]" : "bg-[#6658E8]";
    return (
      <span className="inline-flex flex-col items-end gap-1">
        <span className="inline-flex items-center gap-2">
          <span
            aria-hidden="true"
            className="hidden h-1.5 w-14 overflow-hidden rounded-full bg-secondary sm:inline-block"
          >
            <span
              className={`block h-full rounded-full transition-all duration-500 ${barColor}`}
              style={{ width: `${clamped}%` }}
            />
          </span>
          <span className="font-semibold tabular-nums text-foreground">{formatLegacyPercent(metric.percent)}</span>
        </span>
        {sampledAt && Number.isFinite(Date.parse(sampledAt)) ? (
          <span className="text-[11px] text-muted-foreground">采样 {formatDateTime(sampledAt)}</span>
        ) : (
          <span className="rounded bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-800">无采样时间</span>
        )}
      </span>
    );
  }
  return <span className="text-xs text-muted-foreground">{legacyMetricText(metric)}</span>;
}
