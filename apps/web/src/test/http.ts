import { vi } from "vitest";

export function jsonResponse(body: unknown, status = 200, extra: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...extra },
  });
}

export function textResponse(body: string, status = 200, contentType = "text/plain"): Response {
  return new Response(body, { status, headers: { "Content-Type": contentType } });
}

export function sseResponse(chunks: readonly string[], keepOpen = false): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      if (!keepOpen) controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { "Content-Type": "text/event-stream" } });
}

export type FetchHandler = (init: RequestInit | undefined, url: URL) => Response | Promise<Response>;
export type FetchRoutes = Record<string, FetchHandler>;

export function installFetch(routes: FetchRoutes) {
  const calls: { method: string; url: string; init: RequestInit | undefined }[] = [];
  const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const raw = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const parsed = new URL(raw, "http://localhost");
    const method = (init?.method ?? "GET").toUpperCase();
    calls.push({ method, url: parsed.pathname + parsed.search, init });
    const handler = routes[`${method} ${parsed.pathname}`];
    if (!handler) return jsonResponse({ code: "NOT_FOUND", message: "no fixture" }, 404);
    return handler(init, parsed);
  });
  vi.stubGlobal("fetch", mock);
  return { mock, calls };
}

export function authenticatedSession(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    bootstrapAvailable: false,
    initialized: true,
    ...overrides,
  };
}

export const IDENTITY = {
  userId: "u-admin",
  displayName: "管理员",
  roles: ["admin"],
  permissions: [
    "node.read",
    "node.manage",
    "task.read",
    "task.submit",
    "audit.read",
    "user.manage",
    "group.manage",
    "servicegroup.manage",
  ],
  isBootstrapAdmin: true,
};

export function coreRoutes(): FetchRoutes {
  return {
    "GET /api/v1/auth/bootstrap-status": () => jsonResponse(authenticatedSession()),
    "GET /api/v1/auth/me": () =>
      jsonResponse({ code: "UNAUTHENTICATED", message: "no session" }, 401),
    "POST /api/v1/auth/login": () =>
      jsonResponse({ identity: IDENTITY, sessionExpiresAt: "2026-12-31T23:59:59Z", csrfToken: "csrf-test" }),
    "GET /healthz": () => jsonResponse({ status: "scaffold", stage: "M0-P1" }),
    "GET /api/v1/nodes": () => jsonResponse({ items: [] }),
    "GET /api/v1/tasks": () => jsonResponse({ items: [], page: { nextCursor: null, limit: 5, hasMore: false } }),
    "GET /api/v1/audit/events": () =>
      jsonResponse({ items: [], page: { nextCursor: null, limit: 5, hasMore: false } }),
  };
}
