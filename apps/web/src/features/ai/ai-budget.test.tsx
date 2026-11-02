import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderAuthenticated } from "@/test/app";
import { IDENTITY, jsonResponse, type FetchRoutes } from "@/test/http";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const ADMIN_IDENTITY = {
  ...IDENTITY,
  userId: "u-admin",
  roles: ["administrator"],
  permissions: ["ai.use", "ai.manage"],
  isBootstrapAdmin: true,
};

const MODEL_SETTINGS = {
  id: "prof-1",
  ownerId: "u-admin",
  revision: 1,
  updatedAt: "2026-11-02T00:00:00Z",
  enabled: true,
  provider: "openai-responses",
  model: "local-test-model",
  credentialRef: "cred-1",
  credentialRevision: 1,
};

const BUDGET = {
  ownerId: "u-admin",
  profileId: "prof-1",
  configured: true,
  active: true,
  outputReservationTokens: 8192,
  policy: {
    revision: 3,
    windowId: "november-2026",
    startsAt: "2026-11-01T00:00:00Z",
    endsAt: "2026-12-01T00:00:00Z",
    requestLimit: 100,
    inputByteLimit: 1048576,
    outputTokenLimit: 128000,
  },
  usage: { requests: 4, inputBytes: 2048, outputTokens: 512 },
  remaining: { requests: 96, inputBytes: 1046528, outputTokens: 127488 },
};

const UNCONFIGURED_BUDGET = {
  ownerId: "u-admin",
  profileId: "prof-1",
  configured: false,
  active: false,
  outputReservationTokens: 8192,
  policy: null,
  usage: { requests: 0, inputBytes: 0, outputTokens: 0 },
  remaining: { requests: 0, inputBytes: 0, outputTokens: 0 },
};

function baseRoutes(): FetchRoutes {
  return {
    "GET /api/v1/ai/security-policy": () =>
      jsonResponse({ defaultDeny: true, serverSideEnforced: true }),
    "GET /api/v1/ai/model-settings": () => jsonResponse(MODEL_SETTINGS),
    "GET /api/v1/credentials": () => jsonResponse([]),
    "GET /api/v1/ai/model-settings/prof-1/budget": () => jsonResponse(BUDGET),
  };
}

describe("AI budget panel", () => {
  it("renders the server window, usage and remaining limits and saves with the current revision", async () => {
    const { calls } = await renderAuthenticated(
      "/ai",
      {
        ...baseRoutes(),
        "PUT /api/v1/ai/model-settings/prof-1/budget": () =>
          jsonResponse({ ...BUDGET, policy: { ...BUDGET.policy, revision: 4 } }),
      },
      ADMIN_IDENTITY,
    );

    expect(await screen.findByRole("heading", { level: 1, name: "AI 运维" })).toBeVisible();
    expect(await screen.findByText("窗口 ID：november-2026")).toBeVisible();
    expect(screen.getByText(/已用请求：4 \/ 剩余 96/)).toBeVisible();
    const outputLine = await screen.findByText(/已用输出 token/);
    const outputNumbers = (outputLine.textContent ?? "").replace(/,/g, "");
    expect(outputNumbers).toContain("512");
    expect(outputNumbers).toContain("127488");
    expect(screen.getByText(/当前窗口内/)).toBeVisible();

    fireEvent.change(screen.getByLabelText("请求次数限额"), { target: { value: "50" } });
    fireEvent.click(screen.getByRole("button", { name: "保存预算窗口（CAS）" }));

    await waitFor(() => {
      const call = calls.find(
        entry => entry.method === "PUT" && entry.url === "/api/v1/ai/model-settings/prof-1/budget",
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body).toMatchObject({
        expectedRevision: 3,
        windowId: "november-2026",
        requestLimit: 50,
        inputByteLimit: 1048576,
        outputTokenLimit: 128000,
      });
      // 时间以本地控件回填后往返为服务端可接受的 ISO 字符串，且结束晚于开始。
      expect(Number.isNaN(Date.parse(body.startsAt))).toBe(false);
      expect(Date.parse(body.endsAt)).toBeGreaterThan(Date.parse(body.startsAt));
    });
  });

  it("renders no budget numbers when the current identity is denied", async () => {
    await renderAuthenticated(
      "/ai",
      {
        ...baseRoutes(),
        "GET /api/v1/ai/model-settings/prof-1/budget": () =>
          jsonResponse({ code: "NOT_AUTHORIZED", message: "denied" }, 403),
      },
      ADMIN_IDENTITY,
    );

    expect(await screen.findByText(/HTTP 403/)).toBeVisible();
    expect(screen.queryByText(/窗口 ID：/)).not.toBeInTheDocument();
    expect(screen.queryByText(/已用请求：/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "保存预算窗口（CAS）" })).not.toBeInTheDocument();
  });

  it("keeps the current view and reloads the server revision after a 409 conflict", async () => {
    let budgetReads = 0;
    const { calls } = await renderAuthenticated(
      "/ai",
      {
        ...baseRoutes(),
        "GET /api/v1/ai/model-settings/prof-1/budget": () => {
          budgetReads += 1;
          return jsonResponse(BUDGET);
        },
        "PUT /api/v1/ai/model-settings/prof-1/budget": () =>
          jsonResponse({ code: "BUDGET_REVISION_CONFLICT", message: "stale revision" }, 409),
      },
      ADMIN_IDENTITY,
    );

    await screen.findByText("窗口 ID：november-2026");
    fireEvent.click(screen.getByRole("button", { name: "保存预算窗口（CAS）" }));

    expect(await screen.findByText(/修订冲突（409 BUDGET_REVISION_CONFLICT）/)).toBeVisible();
    expect(screen.getByText(/预算已被其他操作修改（409 BUDGET_REVISION_CONFLICT）/)).toBeVisible();
    await waitFor(() => expect(budgetReads).toBeGreaterThanOrEqual(2));
    const puts = calls.filter(
      entry => entry.method === "PUT" && entry.url === "/api/v1/ai/model-settings/prof-1/budget",
    );
    // 冲突后不自动重试旧修订：只保留用户那一次提交。
    expect(puts).toHaveLength(1);
  });

  it("is read-only for a non-administrator identity", async () => {
    await renderAuthenticated(
      "/ai",
      baseRoutes(),
      { ...IDENTITY, roles: ["operator"], permissions: [], isBootstrapAdmin: false },
    );

    expect(await screen.findByText("窗口 ID：november-2026")).toBeVisible();
    expect(
      await screen.findByText("当前身份不是管理员：只能查看预算，配置由服务端 403 拒绝。"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "保存预算窗口（CAS）" })).toBeDisabled();
    expect(screen.getByLabelText("请求次数限额")).toBeDisabled();
  });

  it("states that generation is denied until a missing policy is configured, starting at revision 0", async () => {
    const { calls } = await renderAuthenticated(
      "/ai",
      {
        ...baseRoutes(),
        "GET /api/v1/ai/model-settings/prof-1/budget": () => jsonResponse(UNCONFIGURED_BUDGET),
        "PUT /api/v1/ai/model-settings/prof-1/budget": () =>
          jsonResponse({ ...BUDGET, policy: { ...BUDGET.policy, revision: 1 } }),
      },
      ADMIN_IDENTITY,
    );

    expect(await screen.findByText(/未配置窗口预算：服务端拒绝生成/)).toBeVisible();
    expect(screen.getByText(/预算配置：未配置/)).toBeVisible();
    fireEvent.change(screen.getByLabelText("窗口 ID"), { target: { value: "first-window" } });
    fireEvent.click(screen.getByRole("button", { name: "保存预算窗口（CAS）" }));

    await waitFor(() => {
      const call = calls.find(
        entry => entry.method === "PUT" && entry.url === "/api/v1/ai/model-settings/prof-1/budget",
      );
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toMatchObject({
        expectedRevision: 0,
        windowId: "first-window",
      });
    });
  });
});