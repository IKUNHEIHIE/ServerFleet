import { describe, expect, it } from "vitest";
import type { NodeDetail, NodeSummary } from "@/lib/api/dto";
import { toRichNodes } from "./rich-adapter";

const summaries: NodeSummary[] = [
  { id: "a", name: "Alpha", online: true, cpuPercent: 12, memoryPercent: 34, region: "eu" },
  { id: "b", name: "Beta", online: false, cpuPercent: 90, memoryPercent: 91 },
];

const details: NodeDetail[] = [
  {
    nodeId: "a",
    displayName: "Alpha",
    status: "online",
    revision: 3,
    platform: "linux/amd64",
    agentVersion: "0.9.0",
    lastSeenAt: "2026-10-30T00:00:00Z",
  },
  { nodeId: "b", displayName: "", status: "suspended", revision: 4 },
];

describe("rich node adapter", () => {
  it("uses only real fields and never fabricates a sampling time", () => {
    const items = toRichNodes(summaries, details);
    expect(items).not.toBeNull();
    if (!items) return;
    expect(items.map(item => [item.id, item.name, item.status, item.revision])).toEqual([
      ["a", "Alpha", "online", 3],
      ["b", "b", "suspended", 4],
    ]);
    expect(items[0].cpu).toMatchObject({ quality: "missing", percent: null, reason: "no-timestamp" });
    expect(items[1].cpu).toMatchObject({ quality: "missing", percent: null, reason: "node-not-online" });
  });

  it("fails the whole mapping when a summary has no detail or details duplicate", () => {
    expect(toRichNodes(summaries, [details[0]])).toBeNull();
    expect(toRichNodes(summaries, [details[0], details[1], details[1]])).toBeNull();
    expect(toRichNodes(summaries, [details[0], { ...details[1], nodeId: "a" }])).toBeNull();
  });

  it("preserves summary order and returns an empty list for an empty input", () => {
    expect(toRichNodes([], [])).toEqual([]);
    const reversed = toRichNodes([...summaries].reverse(), [...details].reverse());
    expect(reversed?.map(item => item.id)).toEqual(["b", "a"]);
  });
});
