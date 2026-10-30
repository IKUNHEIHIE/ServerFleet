import { describe, expect, it } from "vitest";
import { adaptLegacyNodes, adaptMetric } from "./legacy-adapter";
import { LEGACY_FORMAT_ERROR } from "./legacy-types";

function adapt(items: unknown[]) {
  return adaptLegacyNodes({ items });
}

const base = { id: "n-1", name: "Tokyo", online: true };

describe("legacy node adapter", () => {
  it("fails the whole response for malformed envelopes", () => {
    for (const payload of [null, {}, { items: null }, { items: {} }, [], "x"]) {
      expect(adaptLegacyNodes(payload)).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
    }
  });

  it("fails the whole response for non-object rows, bad ids, bad online or duplicate ids", () => {
    expect(adapt([null])).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
    expect(adapt([{ ...base, id: 1 }])).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
    expect(adapt([{ ...base, id: "   " }])).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
    expect(adapt([{ ...base, online: 1 }])).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
    expect(adapt([base, { ...base, name: "Other" }])).toEqual({ ok: false, message: LEGACY_FORMAT_ERROR });
  });

  it("preserves exact ids and order, and does not merge ids that only differ by whitespace", () => {
    const result = adapt([{ ...base, id: " n-1 " }, { ...base, id: "n-1" }, { ...base, id: "n-2" }]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.rows.map(row => row.id)).toEqual([" n-1 ", "n-1", "n-2"]);
  });

  it("keeps a row when only display fields are wrong and marks the name fallback", () => {
    const result = adapt([
      { ...base, name: "   ", region: "", provider: 7, cpuPercent: null, memoryPercent: null },
      { ...base, id: "n-2", name: null },
    ]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.rows[0]).toMatchObject({
      id: "n-1",
      name: "n-1",
      nameIssue: "empty",
      region: null,
      provider: null,
      cpu: { kind: "absent" },
      memory: { kind: "absent" },
    });
    expect(result.rows[1]).toMatchObject({ name: "n-2", nameIssue: "invalid" });
  });

  it("ignores unknown fields and keeps a legal name verbatim", () => {
    const result = adapt([{ ...base, name: "  Tokyo  ", status: "online", revision: 9, sampledAt: "2026-01-01T00:00:00Z" }]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.rows[0].name).toBe("  Tokyo  ");
    expect(result.rows[0]).not.toHaveProperty("revision");
  });

  it("never shows historical percentages for offline nodes", () => {
    for (const value of [0, 23, 100, null, undefined, 101]) {
      expect(adaptMetric(value, false)).toEqual({ kind: "unavailable", reason: "offline" });
    }
  });

  it("distinguishes 0/100, missing, non-finite and out-of-range values", () => {
    expect(adaptMetric(0, true)).toEqual({ kind: "value", percent: 0 });
    expect(adaptMetric(100, true)).toEqual({ kind: "value", percent: 100 });
    expect(adaptMetric(99.5, true)).toEqual({ kind: "value", percent: 99.5 });
    expect(adaptMetric(null, true)).toEqual({ kind: "absent" });
    expect(adaptMetric(undefined, true)).toEqual({ kind: "absent" });
    expect(adaptMetric(Number.NaN, true)).toEqual({ kind: "invalid", reason: "non-finite" });
    expect(adaptMetric(Number.POSITIVE_INFINITY, true)).toEqual({ kind: "invalid", reason: "non-finite" });
    expect(adaptMetric("42", true)).toEqual({ kind: "invalid", reason: "non-finite" });
    expect(adaptMetric(-1, true)).toEqual({ kind: "invalid", reason: "out-of-range" });
    expect(adaptMetric(101, true)).toEqual({ kind: "invalid", reason: "out-of-range" });
  });

  it("returns an empty successful list for an empty server set", () => {
    expect(adapt([])).toEqual({ ok: true, rows: [] });
  });

  it("maps a mixed legal response deterministically", () => {
    const result = adapt([
      { id: "b", name: "Beta", online: false, cpuPercent: 50, memoryPercent: 50, region: "eu", provider: "x" },
      { id: "a", name: "Alpha", online: true, cpuPercent: 0, memoryPercent: 100 },
    ]);
    expect(result.ok).toBe(true);
    if (!result.ok) return;
    expect(result.rows.map(row => row.id)).toEqual(["b", "a"]);
    expect(result.rows[0].cpu).toEqual({ kind: "unavailable", reason: "offline" });
    expect(result.rows[1].cpu).toEqual({ kind: "value", percent: 0 });
    expect(result.rows[1].memory).toEqual({ kind: "value", percent: 100 });
  });
});
