import { describe, expect, it } from "vitest";
import { ApiError } from "./errors";
import {
  normalizeDnsProviders,
  normalizeInstalledPlugin,
  normalizeListPage,
  normalizePluginList,
  normalizeTaskExecutions,
  normalizeTransferItem,
  normalizeZoneSync,
  requireTaskIdentifier,
} from "./compat";

function identity<T>(value: unknown): T {
  return value as T;
}

describe("normalizeListPage", () => {
  it("accepts the candidate page meta shape", () => {
    const result = normalizeListPage(
      { items: [{ id: "a" }], page: { nextCursor: "7", limit: 50, hasMore: true } },
      identity,
      "test",
    );
    expect(result.items).toEqual([{ id: "a" }]);
    expect(result.page).toEqual({ nextCursor: "7", limit: 50, hasMore: true });
  });

  it("accepts the real controller flat cursor shape", () => {
    const result = normalizeListPage({ items: [{ id: "a" }], nextCursor: "12" }, identity, "test");
    expect(result.items).toEqual([{ id: "a" }]);
    expect(result.page).toEqual({ nextCursor: "12", limit: null, hasMore: true });
  });

  it("treats an absent cursor as no more data without inventing one", () => {
    const result = normalizeListPage({ items: [] }, identity, "test");
    expect(result.page).toEqual({ nextCursor: null, limit: null, hasMore: false });
  });

  it("rejects non-object payloads and non-array items instead of faking an empty list", () => {
    expect(() => normalizeListPage(null, identity, "test")).toThrow(ApiError);
    expect(() => normalizeListPage("nope", identity, "test")).toThrow(ApiError);
    expect(() => normalizeListPage({ items: "not-an-array" }, identity, "test")).toThrow(ApiError);
    expect(() => normalizeListPage({ page: { hasMore: false } }, identity, "test")).toThrow(ApiError);
  });

  it("rejects malformed candidate meta and malformed cursors", () => {
    expect(() => normalizeListPage({ items: [], page: { hasMore: "yes" } }, identity, "test")).toThrow(ApiError);
    expect(() => normalizeListPage({ items: [], nextCursor: 42 }, identity, "test")).toThrow(ApiError);
  });

  it("fails the whole page when any item fails validation", () => {
    expect(() =>
      normalizeListPage({ items: [{ ok: true }, { invalid: true }] }, item => {
        if (typeof item === "object" && item !== null && "ok" in item) return item;
        throw new TypeError("bad item");
      }, "test"),
    ).toThrow(ApiError);
  });
});

describe("requireTaskIdentifier", () => {
  it("accepts candidate taskId and real batch jobId", () => {
    expect(requireTaskIdentifier({ taskId: "t-1" }, "x")).toBe("t-1");
    expect(requireTaskIdentifier({ jobId: "t-2" }, "x")).toBe("t-2");
    expect(() => requireTaskIdentifier({ id: "t-3" }, "x")).toThrow(ApiError);
    expect(() => requireTaskIdentifier("nope", "x")).toThrow(ApiError);
  });
});

describe("normalizeTaskExecutions", () => {
  it("accepts the real minimal entry shape and keeps missing fields absent", () => {
    const result = normalizeTaskExecutions({ items: [{ nodeId: "n-1", state: "queued" }] }, "executions");
    expect(result.items).toEqual([{ nodeId: "n-1", state: "queued" }]);
    expect(result.items[0]).not.toHaveProperty("executionId");
    expect(result.items[0]).not.toHaveProperty("lastSequence");
  });

  it("keeps candidate-only extensions when present", () => {
    const result = normalizeTaskExecutions(
      {
        items: [{ executionId: "e-1", nodeId: "n-1", state: "succeeded", exitCode: 0, lastSequence: 4, outputTruncated: false }],
        nextCursor: null,
      },
      "executions",
    );
    expect(result.items[0]).toMatchObject({ executionId: "e-1", exitCode: 0, lastSequence: 4, outputTruncated: false });
  });

  it("rejects entries without nodeId/state and invalid sequences", () => {
    expect(() => normalizeTaskExecutions({ items: [{ state: "queued" }] }, "executions")).toThrow(ApiError);
    expect(() => normalizeTaskExecutions({ items: [{ nodeId: "n-1", state: 3 }] }, "executions")).toThrow(ApiError);
    expect(() => normalizeTaskExecutions({ items: [{ nodeId: "n-1", state: "queued", lastSequence: -1 }] }, "executions")).toThrow(ApiError);
  });
});

describe("normalizeDnsProviders", () => {
  it("maps the real {code,name} shape to displayName", () => {
    const result = normalizeDnsProviders({ items: [{ code: "cloudflare", name: "Cloudflare" }] }, "providers");
    expect(result.items).toEqual([{ code: "cloudflare", displayName: "Cloudflare" }]);
    expect(result.items[0]).not.toHaveProperty("supported");
    expect(result.items[0]).not.toHaveProperty("capabilities");
  });

  it("keeps the candidate shape intact", () => {
    const result = normalizeDnsProviders(
      { items: [{ code: "cloudflare", displayName: "Cloudflare", supported: true, capabilities: { supportsZoneSync: true } }] },
      "providers",
    );
    expect(result.items[0]).toEqual({ code: "cloudflare", displayName: "Cloudflare", supported: true, capabilities: { supportsZoneSync: true } });
  });

  it("rejects providers with neither displayName nor name", () => {
    expect(() => normalizeDnsProviders({ items: [{ code: "cloudflare" }] }, "providers")).toThrow(ApiError);
  });
});

describe("normalizeZoneSync", () => {
  const isZone = (value: unknown) => typeof value === "object" && value !== null && "id" in value;

  it("accepts the candidate items list", () => {
    expect(normalizeZoneSync({ items: [{ id: "z-1" }] }, isZone, "zones")).toEqual({ items: [{ id: "z-1" }] });
  });

  it("wraps the real single-zone object as a one-item list", () => {
    expect(normalizeZoneSync({ id: "z-2", name: "x" }, isZone, "zones")).toEqual({ items: [{ id: "z-2", name: "x" }] });
  });

  it("rejects everything else", () => {
    expect(() => normalizeZoneSync(null, isZone, "zones")).toThrow(ApiError);
    expect(() => normalizeZoneSync({ items: [{}] }, isZone, "zones")).toThrow(ApiError);
  });
});

describe("normalizeTransferItem", () => {
  it("maps real sourcePath/status/bytesCopied fields", () => {
    expect(normalizeTransferItem({ sourcePath: "/a", status: "copied", bytesCopied: 12 }, "t")).toEqual({
      path: "/a",
      state: "copied",
      bytes: 12,
    });
  });

  it("keeps candidate path/state/bytes fields", () => {
    expect(normalizeTransferItem({ path: "/a", state: "copied", bytes: 3, error: "x" }, "t")).toEqual({
      path: "/a",
      state: "copied",
      bytes: 3,
      error: "x",
    });
  });

  it("rejects contradictory transfer aliases instead of silently picking one", () => {
    for (const item of [
      { path: "/a", sourcePath: "/b" },
      { state: "copied", status: "failed" },
      { bytes: 12, bytesCopied: 24 },
    ]) expect(() => normalizeTransferItem(item, "transfer")).toThrow(ApiError);
    expect(normalizeTransferItem({ path: "/a", sourcePath: "/a", bytes: 12, bytesCopied: 12 }, "transfer"))
      .toEqual({ path: "/a", bytes: 12 });
  });

  it("drops invalid bytes and rejects non-object entries", () => {
    expect(normalizeTransferItem({ sourcePath: "/a", status: "copied", bytesCopied: "many" }, "t")).toEqual({ path: "/a", state: "copied" });
    expect(() => normalizeTransferItem("nope", "t")).toThrow(ApiError);
  });
});

describe("normalizeInstalledPlugin", () => {
  it("maps the real controller lifecycle phase onto an honest status", () => {
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", installed: true, enabled: false, phase: "installed_disabled", revision: 3 },
      "plugin",
    )).toMatchObject({ id: "xray-manager", version: "1.0.0", status: "installed_disabled", enabled: false, revision: 3 });
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", installed: true, enabled: false, phase: "disabled" },
      "plugin",
    )).toMatchObject({ status: "installed_disabled" });
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", installed: true, enabled: true, phase: "enabled", source: "github.com/serverfleet/xray-plugin" },
      "plugin",
    )).toMatchObject({ status: "enabled", enabled: true, source: "github.com/serverfleet/xray-plugin" });
  });

  it("keeps the candidate status enum untouched", () => {
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "2.0.0", status: "enabled", enabled: true, sourceCommit: "abc123" },
      "plugin",
    )).toEqual({ id: "xray-manager", version: "2.0.0", status: "enabled", enabled: true, source: "abc123" });
  });

  it("carries transitional and unknown runtime phases verbatim", () => {
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", enabled: true, phase: "stopping" }, "plugin",
    ).status).toBe("stopping");
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", enabled: true, phase: "runtime_unknown" }, "plugin",
    ).status).toBe("runtime_unknown");
    expect(normalizeInstalledPlugin(
      { id: "xray-manager", version: "1.0.0", enabled: false, phase: "stop_unknown" }, "plugin",
    ).status).toBe("stop_unknown");
  });

  it("rejects unknown shapes instead of inventing a status", () => {
    expect(() => normalizeInstalledPlugin({ id: "x", version: "1", enabled: true, phase: "warp_drive" }, "plugin")).toThrow(ApiError);
    expect(() => normalizeInstalledPlugin({ id: "x", version: "1", enabled: true }, "plugin")).toThrow(ApiError);
    expect(() => normalizeInstalledPlugin({ id: "x", enabled: true, phase: "enabled" }, "plugin")).toThrow(ApiError);
    expect(() => normalizeInstalledPlugin({ id: "x", version: "1", phase: "enabled" }, "plugin")).toThrow(ApiError);
    expect(() => normalizeInstalledPlugin(null, "plugin")).toThrow(ApiError);
  });
});

describe("normalizePluginList", () => {
  it("normalizes each installed entry", () => {
    const result = normalizePluginList(
      { items: [{ id: "xray-manager", version: "1", enabled: false, phase: "installed_disabled" }] },
      "installed plugins",
    );
    expect(result.items).toHaveLength(1);
    expect(result.items[0].status).toBe("installed_disabled");
  });

  it("rejects a list with any invalid entry", () => {
    expect(() => normalizePluginList({ items: [{ id: "x", version: "1", enabled: true }] }, "installed plugins")).toThrow(ApiError);
    expect(() => normalizePluginList({ items: "all" }, "installed plugins")).toThrow(ApiError);
    expect(() => normalizePluginList(null, "installed plugins")).toThrow(ApiError);
  });
});
