import { afterEach, expect, it, vi } from "vitest";
import { systemApi } from "./endpoints";
import { apiClient } from "./client";
import { ApiError } from "./errors";
const checks = { database: "unavailable", authority: "ready", scheduler: "ready", agentTransport: "not_configured", pki: "not_configured" };
afterEach(() => vi.unstubAllGlobals());
it("preserves classified readiness checks on HTTP 503 and ignores extra fields", async () => {
  vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ status: "not_ready", checks: { ...checks, privateDetail: "must not be exposed" } }), { status: 503, headers: { "Content-Type": "application/json" } }));
  await expect(systemApi.readiness()).resolves.toEqual({ status: 503, data: { status: "not_ready", checks } });
  await expect(apiClient.request("/other")).rejects.toMatchObject({ kind: "unavailable", status: 503 });
});
it("rejects malformed readiness payloads instead of reporting readiness", async () => {
  vi.stubGlobal("fetch", async () => new Response(JSON.stringify({ status: "not_ready", checks: { database: "private internal error" } }), { status: 503, headers: { "Content-Type": "application/json" } }));
  await expect(systemApi.readiness()).rejects.toBeInstanceOf(ApiError);
});
