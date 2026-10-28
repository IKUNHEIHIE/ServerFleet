import { describe, expect, it } from "vitest";
import { toNodeListItems } from "./candidate-adapter";
import type { MetricQualityReason, MetricReading } from "./types";
import { countLabel, describeMetric, formatLastSeen, resolveTitle, summariseNodes } from "./view-model";
import { freshnessPolicy, offlineNodeDetail, onlineNodeDetail } from "./test/fixtures";

const reading = (patch: Partial<MetricReading>): MetricReading => ({
  quality: "fresh",
  percent: null,
  sampledAt: null,
  reason: null,
  ageMs: null,
  ...patch,
});

describe("describeMetric · 有数值的情况", () => {
  it("新鲜样本只给数值，不加任何附加说明", () => {
    const metric = describeMetric(reading({ percent: 37, sampledAt: "2026-10-29T02:59:30Z", ageMs: 30_000 }));

    expect(metric.kind).toBe("value");
    expect(metric.value).toBe("37%");
    expect(metric.note).toBeNull();
    expect(metric.stale).toBe(false);
  });

  it("新鲜 0% 与 100% 原样保留", () => {
    expect(describeMetric(reading({ percent: 0 })).value).toBe("0%");
    expect(describeMetric(reading({ percent: 100 })).value).toBe("100%");
  });

  it("陈旧样本保留数值，并强制给出含采样时间的可见说明", () => {
    const metric = describeMetric(
      reading({ quality: "stale", percent: 55, sampledAt: "2026-10-29T02:41:00Z", ageMs: 19 * 60 * 1000 }),
    );

    expect(metric.kind).toBe("value");
    expect(metric.value).toBe("55%");
    expect(metric.stale).toBe(true);
    expect(metric.note).toBe("陈旧 · 采样于 2026-10-29 02:41 UTC");
  });

  it("陈旧但时间不可渲染时仍标注陈旧，不静默降级为普通数值", () => {
    const metric = describeMetric(reading({ quality: "stale", percent: 55, sampledAt: "broken" }));

    expect(metric.note).toBe("陈旧 · 采样时间未知");
    expect(metric.stale).toBe(true);
  });

  it("小数按四舍五入展示，不在展示层夹取", () => {
    expect(describeMetric(reading({ percent: 18.4 })).value).toBe("18%");
    expect(describeMetric(reading({ percent: 18.6 })).value).toBe("19%");
  });
});

describe("describeMetric · 无数值的情况", () => {
  const reasons: ReadonlyArray<[MetricQualityReason, string]> = [
    ["node-not-online", "无有效数据（节点不在线或状态未确认）"],
    ["absent", "无有效数据（未上报）"],
    ["non-finite", "无有效数据（读数非法）"],
    ["out-of-range", "无有效数据（超出 0–100 范围）"],
    ["no-timestamp", "无有效数据（缺少采样时间）"],
    ["invalid-timestamp", "无有效数据（采样时间无法解析）"],
    ["future-timestamp", "无有效数据（采样时间晚于当前时间）"],
  ];

  it("每种原因都给出可读说明，且不产生数字", () => {
    for (const [reason, expected] of reasons) {
      const metric = describeMetric(reading({ quality: "invalid", percent: null, reason }));
      expect(metric.kind, reason).toBe("empty");
      expect(metric.value, reason).toBeNull();
      expect(metric.reason, reason).toBe(expected);
      expect(metric.stale, reason).toBe(false);
    }
  });

  it("没有原因时退化为通用文案", () => {
    expect(describeMetric(reading({ percent: null })).reason).toBe("无有效数据");
  });
});

describe("formatLastSeen", () => {
  it("按 UTC 输出确定格式，不受运行时区影响", () => {
    expect(formatLastSeen("2026-10-29T02:41:00Z")).toBe("2026-10-29 02:41 UTC");
  });

  it("空值与非法时间都返回 null", () => {
    expect(formatLastSeen(null)).toBeNull();
    expect(formatLastSeen("")).toBeNull();
    expect(formatLastSeen("not-a-date")).toBeNull();
  });
});

describe("countLabel / resolveTitle / summariseNodes", () => {
  it("各状态的数量文案互不混淆", () => {
    expect(countLabel({ status: "loading" })).toBe("加载中");
    expect(countLabel({ status: "error" })).toBe("无法加载");
    expect(countLabel({ status: "ready", nodes: [] })).toBe("0 个节点");
  });

  it("标题为空时回退到默认名称", () => {
    expect(resolveTitle(undefined)).toBe("节点列表");
    expect(resolveTitle("   ")).toBe("节点列表");
    expect(resolveTitle("生产节点")).toBe("生产节点");
  });

  it("空列表与有数据时给出不同摘要", () => {
    expect(summariseNodes([])).toBe("已授权节点接入后会显示在这里");
    const nodes = toNodeListItems(
      [onlineNodeDetail, offlineNodeDetail],
      undefined,
      freshnessPolicy,
    );
    expect(summariseNodes(nodes)).toBe("1 个在线 · 1 个未在线");
  });
});
