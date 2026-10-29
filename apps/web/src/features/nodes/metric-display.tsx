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
export function LegacyMetricCell({ metric }: { readonly metric: LegacyMetric }) {
  if (metric.kind === "value") {
    return (
      <span className="inline-flex flex-col items-end gap-0.5">
        <span className="tabular-nums">{formatLegacyPercent(metric.percent)}</span>
        <span className="text-xs font-medium text-amber-700">无采样时间</span>
      </span>
    );
  }
  return <span className="text-xs text-muted-foreground">{legacyMetricText(metric)}</span>;
}
