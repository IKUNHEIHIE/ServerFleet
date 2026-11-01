import { fireEvent, render, screen } from "@testing-library/react";
import { expect } from "vitest";
import { App } from "@/App";
import { coreRoutes, IDENTITY, installFetch, jsonResponse, type FetchRoutes } from "@/test/http";

/** 走真实登录表单进入指定 hash 页面；默认 /me 首调 401（匿名），后续返回身份。 */
export async function renderAuthenticated(
  hash: string,
  routes: FetchRoutes = {},
  identity: typeof IDENTITY = IDENTITY,
) {
  let meCalls = 0;
  const merged: FetchRoutes = {
    ...coreRoutes(),
    "POST /api/v1/auth/login": () =>
      jsonResponse({ identity, sessionExpiresAt: "2026-12-31T23:59:59Z", csrfToken: "csrf-test" }),
    "GET /api/v1/auth/me": () => {
      meCalls += 1;
      return meCalls === 1
        ? jsonResponse({ code: "UNAUTHENTICATED", message: "no session" }, 401)
        : jsonResponse(identity);
    },
    ...routes,
  };
  window.location.hash = hash;
  const installed = installFetch(merged);
  render(<App />);
  fireEvent.change(await screen.findByLabelText("用户名"), { target: { value: "admin" } });
  fireEvent.change(screen.getByLabelText("密码"), { target: { value: "secret" } });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));
  await screen.findByRole("navigation", { name: "主导航" });
  expect(window.location.hash).toBe(`#${hash}`);
  return installed;
}
