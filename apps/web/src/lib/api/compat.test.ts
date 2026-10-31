import { describe, expect, it } from "vitest";
import { ApiError } from "./errors";
import {
  normalizeDnsProviders,
  normalizeListPage,
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

  it("drops invalid bytes and rejects non-object entries", () => {
    expect(normalizeTransferItem({ sourcePath: "/a", status: "copied", bytesCopied: "many" }, "t")).toEqual({ path: "/a", state: "copied" });
    expect(() => normalizeTransferItem("nope", "t")).toThrow(ApiError);
  });
});
