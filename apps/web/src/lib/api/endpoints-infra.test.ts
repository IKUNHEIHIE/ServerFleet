import { afterEach, expect, it, vi } from "vitest";
import { setCsrfProvider } from "./client";
import { monitoringApi } from "./endpoints-infra";

afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); setCsrfProvider(() => null); });
it("lets a manual probe finish beyond the ordinary browser request deadline", async () => {
  vi.useFakeTimers();
  setCsrfProvider(() => "synthetic-csrf");
  let signal: AbortSignal | null | undefined;
  vi.stubGlobal("fetch", (_input: RequestInfo | URL, init?: RequestInit) => {
    signal = init?.signal;
    return new Promise<Response>((resolve, reject) => {
      signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
      setTimeout(() => resolve(new Response(JSON.stringify({ monitorId: "slow", status: "down", state: "down" }), { headers: { "Content-Type": "application/json" } })), 16_000);
    });
  });
  const result = monitoringApi.probe("slow");
  await vi.advanceTimersByTimeAsync(15_001);
  expect(signal?.aborted).toBe(false);
  await vi.advanceTimersByTimeAsync(1_000);
  await expect(result).resolves.toMatchObject({ data: { monitorId: "slow", status: "down" } });
});
