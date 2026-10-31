import { describe, expect, it } from "vitest";
import type { CandidateNodeDetail, CandidateNodeMetrics } from "./candidate-adapter";
import { PERCENT_MAX, PERCENT_MIN, toNodeListItem, toNodeListItems } from "./candidate-adapter";
import type { FreshnessPolicy } from "./types";
import { FIXED_NOW, MAX_SAMPLE_AGE_MS, freshnessPolicy, offlineNodeDetail } from "./test/fixtures";

const online: CandidateNodeDetail = {
  nodeId: "node-probe-01",
  displayName: "probe-01",
  status: "online",
  revision: 1,
};

/** 以固定的 FIXED_NOW 为基准构造采样时间，测试不读机器时钟。 */
const sampledAtAgo = (msAgo: number) => new Date(FIXED_NOW - msAgo).toISOString();

const cpuOf = (metrics: CandidateNodeMetrics, policy: FreshnessPolicy = freshnessPolicy) =>
  toNodeListItem(online, metrics, policy).cpu;

describe("采样时效：新鲜 / 陈旧", () => {
  it("阈值内的样本判为新鲜并保留数值", () => {
    const reading = cpuOf({ cpuPercent: 37, sampledAt: sampledAtAgo(30_000) });

    expect(reading.quality).toBe("fresh");
    expect(reading.percent).toBe(37);
    expect(reading.reason).toBeNull();
    expect(reading.ageMs).toBe(30_000);
  });

  it("刚好等于最大年龄仍算新鲜（边界包含）", () => {
    const reading = cpuOf({ cpuPercent: 37, sampledAt: sampledAtAgo(MAX_SAMPLE_AGE_MS) });

    expect(reading.quality).toBe("fresh");
    expect(reading.ageMs).toBe(MAX_SAMPLE_AGE_MS);
  });

  it("超过最大年龄 1 毫秒即降级为陈旧，但保留数值与采样时间", () => {
    const sampledAt = sampledAtAgo(MAX_SAMPLE_AGE_MS + 1);
    const reading = cpuOf({ cpuPercent: 37, sampledAt });

    expect(reading.quality).toBe("stale");
    expect(reading.percent).toBe(37);
    expect(reading.sampledAt).toBe(sampledAt);
    expect(reading.ageMs).toBe(MAX_SAMPLE_AGE_MS + 1);
  });

  it("很久以前的在线样本不会显示成普通当前值", () => {
    const reading = cpuOf({ cpuPercent: 55, sampledAt: sampledAtAgo(19 * 60 * 1000) });

    expect(reading.quality).toBe("stale");
    expect(reading.percent).toBe(55);
  });
});

describe("采样时间缺失或非法", () => {
  it("没有采样时间不假定为刚刚采样", () => {
    const reading = cpuOf({ cpuPercent: 37 });

    expect(reading.quality).toBe("missing");
    expect(reading.percent).toBeNull();
    expect(reading.reason).toBe("no-timestamp");
  });

  it("只有空白字符的采样时间视作缺失", () => {
    expect(cpuOf({ cpuPercent: 37, sampledAt: "   " }).reason).toBe("no-timestamp");
  });

  it("无法解析的采样时间不保留数值", () => {
    const reading = cpuOf({ cpuPercent: 37, sampledAt: "not-a-time" });

    expect(reading.quality).toBe("missing");
    expect(reading.percent).toBeNull();
    expect(reading.reason).toBe("invalid-timestamp");
  });

  it("晚于当前时间的样本判为无效，不当作有效当前数据", () => {
    const reading = cpuOf({ cpuPercent: 37, sampledAt: sampledAtAgo(-1) });

    expect(reading.quality).toBe("invalid");
    expect(reading.percent).toBeNull();
    expect(reading.reason).toBe("future-timestamp");
  });
});

describe("百分比域：0–100 之外一律无效", () => {
  it("负值与大于 100 的值都不夹取、不显示", () => {
    for (const bad of [-0.1, -10, 100.001, 150, 1e6]) {
      const reading = cpuOf({ cpuPercent: bad, sampledAt: sampledAtAgo(1_000) });
      expect(reading.quality, `value ${bad}`).toBe("invalid");
      expect(reading.percent, `value ${bad}`).toBeNull();
      expect(reading.reason, `value ${bad}`).toBe("out-of-range");
    }
  });

  it("边界值 0 与 100 是合法读数", () => {
    const zero = cpuOf({ cpuPercent: PERCENT_MIN, sampledAt: sampledAtAgo(1_000) });
    const full = cpuOf({ cpuPercent: PERCENT_MAX, sampledAt: sampledAtAgo(1_000) });

    expect(zero.quality).toBe("fresh");
    expect(zero.percent).toBe(0);
    expect(full.quality).toBe("fresh");
    expect(full.percent).toBe(100);
  });

  it("非有限数按非法处理，且不影响真实 0", () => {
    for (const bad of [Number.NaN, Number.POSITIVE_INFINITY, Number.NEGATIVE_INFINITY]) {
      const reading = cpuOf({ cpuPercent: bad, sampledAt: sampledAtAgo(1_000) });
      expect(reading.quality, `value ${bad}`).toBe("missing");
      expect(reading.percent, `value ${bad}`).toBeNull();
      expect(reading.reason, `value ${bad}`).toBe("non-finite");
    }
    expect(cpuOf({ cpuPercent: 0, sampledAt: sampledAtAgo(1_000) }).percent).toBe(0);
  });

  it("非数值类型（上游脏数据）也按非法处理", () => {
    const dirty = { cpuPercent: "37" as unknown as number, sampledAt: sampledAtAgo(1_000) };
    expect(cpuOf(dirty).reason).toBe("non-finite");
  });

  it("完全没有数值时不猜测读数", () => {
    expect(cpuOf({ sampledAt: sampledAtAgo(1_000) }).reason).toBe("absent");
    expect(cpuOf({ cpuPercent: null, sampledAt: sampledAtAgo(1_000) }).reason).toBe("absent");
  });
});

describe("节点状态与指标可用性", () => {
  it("离线节点即便上游给了新鲜样本也不展示读数", () => {
    const item = toNodeListItem(
      offlineNodeDetail,
      { cpuPercent: 0, memoryPercent: 0, sampledAt: sampledAtAgo(1_000) },
      freshnessPolicy,
    );

    expect(item.status).toBe("offline");
    expect(item.cpu.quality).toBe("missing");
    expect(item.cpu.percent).toBeNull();
    expect(item.cpu.reason).toBe("node-not-online");
    expect(item.memory.reason).toBe("node-not-online");
  });

  it("暂停/吊销/未知状态同样不展示读数", () => {
    for (const status of ["suspended", "revoked", "unknown"] as const) {
      const item = toNodeListItem({ ...online, status }, { cpuPercent: 12, sampledAt: sampledAtAgo(1_000) }, freshnessPolicy);
      expect(item.cpu.reason, status).toBe("node-not-online");
      expect(item.cpu.percent, status).toBeNull();
    }
  });
});

describe("策略注入异常时的失败安全", () => {
  it("阈值为 0 或非有限数时，任何样本都不会被判为新鲜", () => {
    const sampledAt = sampledAtAgo(1_000);
    const policies: FreshnessPolicy[] = [
      { now: FIXED_NOW, maxSampleAgeMs: 0 },
      { now: FIXED_NOW, maxSampleAgeMs: Number.NaN },
      { now: Number.NaN, maxSampleAgeMs: MAX_SAMPLE_AGE_MS },
    ];

    for (const policy of policies) {
      const reading = cpuOf({ cpuPercent: 37, sampledAt }, policy);
      expect(reading.quality, JSON.stringify(policy)).toBe("stale");
      expect(reading.percent, JSON.stringify(policy)).toBe(37);
      expect(reading.ageMs, JSON.stringify(policy)).toBeNull();
    }
  });
});

describe("不变量：数值存在时必须带质量标签", () => {
  it("percent 非 null 只可能来自 fresh 或 stale", () => {
    const samples = [0, 18.4, 100, -1, 150, Number.NaN];
    const timestamps = [undefined, "not-a-time", sampledAtAgo(1_000), sampledAtAgo(-1), sampledAtAgo(60 * 60 * 1000)];

    for (const cpuPercent of samples) {
      for (const sampledAt of timestamps) {
        const reading = cpuOf({ cpuPercent, sampledAt });
        if (reading.percent !== null) {
          expect(["fresh", "stale"]).toContain(reading.quality);
          expect(reading.percent).toBeGreaterThanOrEqual(PERCENT_MIN);
          expect(reading.percent).toBeLessThanOrEqual(PERCENT_MAX);
          expect(reading.sampledAt).not.toBeNull();
        } else {
          expect(reading.reason).not.toBeNull();
        }
      }
    }
  });
});

describe("toNodeListItems · 批量映射", () => {
  it("保持顺序、按 ID 取指标并应用同一策略", () => {
    const items = toNodeListItems(
      [
        online,
        { ...online, nodeId: "node-probe-02", displayName: "probe-02" },
      ],
      new Map([
        ["node-probe-01", { cpuPercent: 18, sampledAt: sampledAtAgo(1_000) }],
        ["node-probe-02", { cpuPercent: 42, sampledAt: sampledAtAgo(60 * 60 * 1000) }],
      ]),
      freshnessPolicy,
    );

    expect(items.map(item => item.id)).toEqual(["node-probe-01", "node-probe-02"]);
    expect(items[0].cpu.quality).toBe("fresh");
    expect(items[1].cpu.quality).toBe("stale");
  });

  it("空显示名回退到节点 ID，空白元数据归一为 null", () => {
    const item = toNodeListItem({ ...online, displayName: "  ", platform: "  ", agentVersion: null }, null, freshnessPolicy);

    expect(item.name).toBe("node-probe-01");
    expect(item.platform).toBeNull();
    expect(item.agentVersion).toBeNull();
  });
});
