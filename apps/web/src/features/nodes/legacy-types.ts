/**
 * W07 兼容旧列表的局部展示类型。
 *
 * 旧 `GET /api/v1/nodes` 只有 `{items}` 和 `id/name/online`（可选地区/厂商/指标），
 * 没有 status/revision/sampledAt。这里不把旧 DTO 强转成富化 ViewModel，
 * 也不补造任何状态、版本或采样时间。
 */

export type LegacyMetric =
  | { readonly kind: "value"; readonly percent: number }
  | { readonly kind: "absent" }
  | { readonly kind: "unavailable"; readonly reason: "offline" }
  | { readonly kind: "invalid"; readonly reason: "non-finite" | "out-of-range" };

export interface LegacyNodeRow {
  /** 原样保留，不去空格、不改写。 */
  readonly id: string;
  /** 显示名；空白/非法时由界面回退到 id，并给出固定提示。 */
  readonly name: string;
  readonly nameIssue: "empty" | "invalid" | null;
  readonly online: boolean;
  readonly region: string | null;
  readonly provider: string | null;
  readonly cpu: LegacyMetric;
  readonly memory: LegacyMetric;
}

export type LegacyAdaptResult =
  | { readonly ok: true; readonly rows: readonly LegacyNodeRow[] }
  | { readonly ok: false; readonly message: string };

export const LEGACY_FORMAT_ERROR =
  "节点数据格式不符合兼容约定，已停止显示。请确认 Controller 返回 {items:[…]}，且每行 id/online 合法、id 唯一。";
