import { describe, expect, it } from "vitest";
import type { CandidateNodeDetail } from "./candidate-adapter";
import { toNodeListItem, toNodeListItems } from "./candidate-adapter";
import { countLabel, formatLastSeen, formatPercent, resolveTitle, summariseNodes } from "./view-model";
import { offlineNodeDetail, onlineNodeDetail } from "./test/fixtures";

const base: CandidateNodeDetail = {
  nodeId: "node-base-01",
  displayName: "base-01",
  status: "online",
  revision: 1,
};

describe("formatPercent", () => {
  it("缺失读数返回 null，交给调用方渲染无有效数据", () => {
    expect(formatPercent({ percent: null, sampledAt: null })).toBeNull();
    expect(formatPercent({ percent: Number.NaN, sampledAt: null })).toBeNull();
    expect(formatPercent({ percent: Number.POSITIVE_INFINITY, sampledAt: null })).toBeNull();
  });

  it("真实 0 与四舍五入后的读数照常显示", () => {
    expect(formatPercent({ percent: 0, sampledAt: null })).toBe("0%");
    expect(formatPercent({ percent: 18.4, sampledAt: null })).toBe("18%");
    expect(formatPercent({ percent: 18.6, sampledAt: null })).toBe("19%");
  });

  it("不在展示层截断越界值，暴露给契约审查", () => {
    expect(formatPercent({ percent: 130, sampledAt: null })).toBe("130%");
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
    const nodes = toNodeListItems([onlineNodeDetail, offlineNodeDetail]);
    expect(summariseNodes(nodes)).toBe("1 个在线 · 1 个未在线");
  });
});

describe("toNodeListItem · 候选 DTO 映射", () => {
  it("离线节点即使带 cpu=0 也映射为无有效数据", () => {
    const item = toNodeListItem(offlineNodeDetail, {
      cpuPercent: 0,
      memoryPercent: 0,
      sampledAt: "2026-10-26T11:05:00Z",
    });

    expect(item.status).toBe("offline");
    expect(item.cpu.percent).toBeNull();
    expect(item.cpu.sampledAt).toBeNull();
    expect(item.memory.percent).toBeNull();
  });

  it("在线节点的真实 0 与缺失分别保留", () => {
    const zero = toNodeListItem(base, { cpuPercent: 0, memoryPercent: 12, sampledAt: "2026-10-29T02:41:00Z" });
    expect(zero.cpu.percent).toBe(0);
    expect(zero.cpu.sampledAt).toBe("2026-10-29T02:41:00Z");

    const missing = toNodeListItem(base, { cpuPercent: Number.NaN });
    expect(missing.cpu.percent).toBeNull();
    expect(missing.cpu.sampledAt).toBeNull();
  });

  it("完全没有指标载荷时不猜测读数", () => {
    const item = toNodeListItem(onlineNodeDetail);
    expect(item.cpu.percent).toBeNull();
    expect(item.memory.percent).toBeNull();
  });

  it("空显示名回退到节点 ID，空白元数据归一为 null", () => {
    const item = toNodeListItem({ ...base, displayName: "  ", platform: "  ", agentVersion: null });
    expect(item.name).toBe("node-base-01");
    expect(item.platform).toBeNull();
    expect(item.agentVersion).toBeNull();
  });

  it("批量映射保持顺序并按 ID 取指标", () => {
    const items = toNodeListItems([onlineNodeDetail, offlineNodeDetail], new Map([
      [onlineNodeDetail.nodeId, { cpuPercent: 18 }],
      [offlineNodeDetail.nodeId, { cpuPercent: 42 }],
    ]));

    expect(items.map(item => item.id)).toEqual([onlineNodeDetail.nodeId, offlineNodeDetail.nodeId]);
    expect(items[0].cpu.percent).toBe(18);
    expect(items[1].cpu.percent).toBeNull();
  });
});
