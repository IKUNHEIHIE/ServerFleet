import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, onUnauthorized, setCsrfProvider } from "@/lib/api/client";
import { ApiError } from "@/lib/api/errors";

afterEach(() => setCsrfProvider(() => null));

function client(fetchImpl: typeof fetch, timeoutMs = 1_000) {
  return new ApiClient({ fetchImpl, baseUrl: "http://controller.test/", timeoutMs });
}

describe("ApiClient", () => {
  it("sends query parameters, JSON and same-origin credentials", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ items: [] }), { status: 200, headers: { "Content-Type": "application/json" } }),
    ) as unknown as typeof fetch;
    const api = client(fetchImpl);
    const result = await api.request<{ items: unknown[] }>("/api/v1/nodes", {
      query: { onlineOnly: true, skip: undefined, tags: ["a", "b"] },
    });
    expect(result.data.items).toEqual([]);
    const [url, init] = (fetchImpl as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0];
    expect(url).toBe("http://controller.test/api/v1/nodes?onlineOnly=true&tags=a&tags=b");
    expect(init.credentials).toBe("include");
    expect(init.cache).toBe("no-store");
    expect(new Headers(init.headers).get("X-Requested-With")).toBe("ServerFleetWeb");
  });

  it("classifies fixed error statuses and keeps request metadata", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ code: "PERMISSION_DENIED", message: "no", requestId: "req-1" }), {
        status: 403,
        headers: { "Content-Type": "application/json" },
      }),
    ) as unknown as typeof fetch;
    await expect(client(fetchImpl).request("/api/v1/nodes")).rejects.toMatchObject({
      kind: "forbidden",
      status: 403,
      code: "PERMISSION_DENIED",
      requestId: "req-1",
    });
  });

  it("does not treat an HTML 404 from an unimplemented route as a valid DTO", async () => {
    const fetchImpl = vi.fn(async () => new Response("<html>404</html>", { status: 404 })) as unknown as typeof fetch;
    await expect(client(fetchImpl).request("/api/v1/nodes")).rejects.toBeInstanceOf(ApiError);
    await expect(client(fetchImpl).request("/api/v1/nodes")).rejects.toMatchObject({ kind: "not-found", status: 404 });
  });

  it("rejects a 200 non-JSON body instead of inventing a payload", async () => {
    const fetchImpl = vi.fn(async () => new Response("plain", { status: 200 })) as unknown as typeof fetch;
    await expect(client(fetchImpl).request("/api/v1/nodes")).rejects.toMatchObject({ kind: "invalid", status: 200 });
  });

  it("requires an in-memory CSRF token for cookie-session writes", async () => {
    const fetchImpl = vi.fn() as unknown as typeof fetch;
    await expect(
      client(fetchImpl).request("/api/v1/groups", { method: "POST", body: { name: "g" } }),
    ).rejects.toMatchObject({ kind: "csrf-missing" });
    expect(fetchImpl).not.toHaveBeenCalled();

    setCsrfProvider(() => "csrf-token");
    const okFetch = vi.fn(async () =>
      new Response(null, { status: 204 }),
    ) as unknown as typeof fetch;
    await client(okFetch).request("/api/v1/groups", { method: "POST", body: { name: "g" } });
    const [, init] = (okFetch as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0];
    expect(new Headers(init.headers).get("X-CSRF-Token")).toBe("csrf-token");
  });

  it("does not send CSRF for public login", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ identity: {}, sessionExpiresAt: "2026-01-01T00:00:00Z" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ) as unknown as typeof fetch;
    await client(fetchImpl).request("/api/v1/auth/login", { method: "POST", body: {}, csrf: false });
    const [, init] = (fetchImpl as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls[0];
    expect(new Headers(init.headers).get("X-CSRF-Token")).toBeNull();
  });

  it("notifies the session layer on 401 but not when authEvent is disabled", async () => {
    const listener = vi.fn();
    const unsubscribe = onUnauthorized(listener);
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ code: "UNAUTHENTICATED", message: "no" }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      }),
    ) as unknown as typeof fetch;
    await expect(client(fetchImpl).request("/api/v1/nodes")).rejects.toMatchObject({ kind: "auth" });
    expect(listener).toHaveBeenCalledTimes(1);
    await expect(client(fetchImpl).request("/api/v1/auth/login", { method: "POST", csrf: false, authEvent: false })).rejects.toMatchObject({ kind: "auth" });
    expect(listener).toHaveBeenCalledTimes(1);
    unsubscribe();
  });

  it("maps abort and timeout into distinct fixed errors", async () => {
    const abortable = vi.fn((_input: RequestInfo | URL, init?: RequestInit) =>
      new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), { once: true });
      }),
    ) as unknown as typeof fetch;
    const controller = new AbortController();
    const pending = client(abortable).request("/slow", { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toMatchObject({ kind: "aborted" });

    const timeout = client(abortable, 10).request("/slow");
    await expect(timeout).rejects.toMatchObject({ kind: "network" });
  });

  it("surfaces nested error.code from the real controller fixed-error shape (GM-W4-02)", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ error: { code: "quota_not_found", message: "no traffic quota is configured" } }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      }),
    ) as unknown as typeof fetch;
    await expect(client(fetchImpl).request("/api/v1/nodes/node-alpha/traffic-quota")).rejects.toMatchObject({
      kind: "not-found",
      status: 404,
      code: "quota_not_found",
    });
  });

  it("returns undefined for 204 and preserves Retry-After", async () => {
    const noContent = vi.fn(async () => new Response(null, { status: 204 })) as unknown as typeof fetch;
    await expect(client(noContent).request<void>("/x")).resolves.toEqual({ data: undefined, status: 204 });

    const rateLimited = vi.fn(async () =>
      new Response(JSON.stringify({ code: "RATE_LIMITED", message: "slow" }), {
        status: 429,
        headers: { "Content-Type": "application/json", "Retry-After": "7" },
      }),
    ) as unknown as typeof fetch;
    await expect(client(rateLimited).request("/x")).rejects.toMatchObject({ kind: "rate-limited", retryAfterSeconds: 7 });
  });
});
