import { LEGACY_FORMAT_ERROR, type LegacyAdaptResult, type LegacyMetric, type LegacyNodeRow } from "./legacy-types";

function nonEmptyString(value: unknown): string | null {
  return typeof value === "string" && value.trim() !== "" ? value : null;
}

/**
 * 旧列表 DTO 校验。
 *
 * 身份/权威字段错误（非对象、坏 id、坏 online、重复 id、items 非数组）整次失败，
 * 不返回部分表格或计数；纯展示字段（name/region/provider/指标）按固定规则降级。
 */
export function adaptLegacyNodes(payload: unknown): LegacyAdaptResult {
  if (typeof payload !== "object" || payload === null || !Array.isArray((payload as { items?: unknown }).items)) {
    return { ok: false, message: LEGACY_FORMAT_ERROR };
  }

  const seen = new Set<string>();
  const rows: LegacyNodeRow[] = [];

  for (const item of (payload as { items: unknown[] }).items) {
    if (typeof item !== "object" || item === null) return { ok: false, message: LEGACY_FORMAT_ERROR };
    const record = item as Record<string, unknown>;

    if (typeof record.id !== "string" || record.id.trim() === "") return { ok: false, message: LEGACY_FORMAT_ERROR };
    if (typeof record.online !== "boolean") return { ok: false, message: LEGACY_FORMAT_ERROR };
    if (seen.has(record.id)) return { ok: false, message: LEGACY_FORMAT_ERROR };
    seen.add(record.id);

    let name = "";
    let nameIssue: LegacyNodeRow["nameIssue"] = null;
    if (typeof record.name === "string") {
      if (record.name.trim() === "") nameIssue = "empty";
      else name = record.name;
    } else {
      nameIssue = "invalid";
    }

    rows.push({
      id: record.id,
      name: nameIssue ? record.id : name,
      nameIssue,
      online: record.online,
      region: nonEmptyString(record.region),
      provider: nonEmptyString(record.provider),
      cpu: adaptMetric(record.cpuPercent, record.online),
      memory: adaptMetric(record.memoryPercent, record.online),
    });
  }

  return { ok: true, rows };
}

/**
 * 先看 online 再看数值：离线行即便带有历史百分比也不展示当前读数。
 * 有限 0–100（含 0/100）保留数值；不夹取、不以 0 补缺、不补造采样时间。
 */
export function adaptMetric(raw: unknown, online: boolean): LegacyMetric {
  if (!online) return { kind: "unavailable", reason: "offline" };
  if (raw === null || raw === undefined) return { kind: "absent" };
  if (typeof raw !== "number" || !Number.isFinite(raw)) return { kind: "invalid", reason: "non-finite" };
  if (raw < 0 || raw > 100) return { kind: "invalid", reason: "out-of-range" };
  return { kind: "value", percent: raw };
}
