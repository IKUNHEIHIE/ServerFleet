import { describe, expect, it } from "vitest";
import { toNodeListItem } from "./candidate-adapter";
import {
  FILTERABLE_STATUSES,
  filterNodes,
  hasActiveFilter,
  matchesQuery,
  normaliseQuery,
  summariseVisible,
} from "./node-filters";
import type { NodeListItemView } from "./types";
import type { NodeStatusFilter } from "./node-filters";
import { freshnessPolicy, readyNodes } from "./test/fixtures";

const ids = (nodes: readonly NodeListItemView[]) => nodes.map(node => node.id);
const query = (value: string, status: NodeStatusFilter = "all") => ({ query: value, status });

describe("normaliseQuery / matchesQuery", () => {
  it("裁剪首尾空白并忽略大小写", () => {
    expect(normaliseQuery("  AWS-Tokyo  ")).toBe("aws-tokyo");
    expect(matchesQuery(readyNodes[0], normaliseQuery("  AWS-TOKYO "))).toBe(true);
  });

  it("空搜索词匹配全部，不产生意外过滤", () => {
    expect(matchesQuery(readyNodes[0], normaliseQuery("   "))).toBe(true);
  });

  it("名称与节点 ID 都能命中", () => {
    const node = readyNodes[0];
    expect(matchesQuery(node, normaliseQuery(node.name))).toBe(true);
    expect(matchesQuery(node, normaliseQuery(node.id))).toBe(true);
  });

  it("中文名称可被普通子串命中", () => {
    const chinese = toNodeListItem(
      { nodeId: "node-hk-01", displayName: "香港-入口-01", status: "online", revision: 1 },
      null,
      freshnessPolicy,
    );
    expect(matchesQuery(chinese, normaliseQuery("香港"))).toBe(true);
    expect(matchesQuery(chinese, normaliseQuery("入口"))).toBe(true);
    expect(matchesQuery(chinese, normaliseQuery("东京"))).toBe(false);
  });
});

describe("filterNodes · 搜索", () => {
  it("按名称筛选且保持输入顺序", () => {
    const result = filterNodes(readyNodes, query("gcp"));
    expect(ids(result)).toEqual(["node-online-02", "node-online-03", "node-suspended-01"]);
  });

  it("按 ID 筛选", () => {
    const result = filterNodes(readyNodes, query("node-unknown-07"));
    expect(ids(result)).toEqual(["node-unknown-07"]);
  });

  it("没有命中时返回空数组而不是 null", () => {
    expect(filterNodes(readyNodes, query("zzz-no-such-node"))).toEqual([]);
  });

  it("纯空白搜索词等价于不筛选", () => {
    expect(filterNodes(readyNodes, query("   "))).toHaveLength(readyNodes.length);
  });
});

describe("filterNodes · 状态", () => {
  it("每种状态都能单独筛选", () => {
    expect(filterNodes(readyNodes, query("", "online"))).toHaveLength(4);
    expect(ids(filterNodes(readyNodes, query("", "offline")))).toEqual(["node-offline-01"]);
    expect(ids(filterNodes(readyNodes, query("", "suspended")))).toEqual(["node-suspended-01"]);
    expect(ids(filterNodes(readyNodes, query("", "revoked")))).toEqual(["node-revoked-01"]);
    expect(ids(filterNodes(readyNodes, query("", "unknown")))).toEqual(["node-unknown-07"]);
  });

  it("all 不筛状态，覆盖全部可筛选状态", () => {
    expect(filterNodes(readyNodes, query("", "all"))).toHaveLength(readyNodes.length);
    expect(FILTERABLE_STATUSES).toHaveLength(5);
  });

  it("搜索词与状态同时生效", () => {
    expect(ids(filterNodes(readyNodes, query("gcp", "online")))).toEqual(["node-online-02", "node-online-03"]);
    expect(filterNodes(readyNodes, query("gcp", "offline"))).toEqual([]);
  });
});

describe("filterNodes · 不改动输入", () => {
  it("返回新数组，元素引用与顺序不变，源数组未被修改", () => {
    const frozen = Object.freeze([...readyNodes]) as readonly NodeListItemView[];
    const before = frozen.map(node => node.id);

    const result = filterNodes(frozen, query("gcp"));

    expect(result).not.toBe(frozen);
    expect(frozen.map(node => node.id)).toEqual(before);
    expect(frozen).toHaveLength(8);
    expect(result[0]).toBe(readyNodes[1]);
  });
});

describe("hasActiveFilter / summariseVisible", () => {
  it("空输入不算激活，空格也算用户输入", () => {
    expect(hasActiveFilter({ query: "", status: "all" })).toBe(false);
    expect(hasActiveFilter({ query: "  ", status: "all" })).toBe(true);
    expect(hasActiveFilter({ query: "", status: "online" })).toBe(true);
  });

  it("计数文案区分全量与筛选后", () => {
    expect(summariseVisible(8, 8)).toBe("共 8 个节点");
    expect(summariseVisible(3, 8)).toBe("显示 3 / 共 8 个节点");
    expect(summariseVisible(0, 8)).toBe("显示 0 / 共 8 个节点");
  });

  it("源列表为空时不给计数文案，避免编造总量", () => {
    expect(summariseVisible(0, 0)).toBeNull();
  });
});
