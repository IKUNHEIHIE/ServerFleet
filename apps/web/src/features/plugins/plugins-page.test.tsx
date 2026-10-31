import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const REPOSITORY = { mode: "official", ref: "main", path: "Plugin", resolvedCommit: "c0ffee", officialVerified: true };

const CATALOG = {
  items: [
    {
      id: "xray-manager",
      name: "Xray 节点管理器",
      version: "0.1.0",
      entryType: "declarative",
      manifestHash: "mh-1",
      artifactSha256: "sha-abc",
      sourceCommit: "c0ffee",
      permissions: ["plugins.node.manage"],
      minControllerVersion: "0.9.0",
    },
    {
      id: "xray-manager-unpublished",
      name: "Xray 节点管理器（尚未发布）",
      version: "0.2.0",
      entryType: "declarative",
      manifestHash: "mh-2",
      permissions: [],
    },
  ],
};

const OPERATION = {
  id: "op-1",
  pluginId: "xray-manager",
  action: "install",
  status: "queued",
  targetVersion: "0.1.0",
};

function pluginRoutes(installed: unknown[]): FetchRoutes {
  return {
    "GET /api/v1/plugins/repository": () => jsonResponse(REPOSITORY),
    "GET /api/v1/plugins/catalog": () => jsonResponse(CATALOG),
    "GET /api/v1/plugins": () => jsonResponse({ items: installed }),
    "POST /api/v1/plugins/xray-manager/install": () => jsonResponse(OPERATION, 202),
    "PUT /api/v1/plugins/xray-manager/activation": () => jsonResponse({ ...OPERATION, action: "deactivate", status: "draining" }),
    "GET /api/v1/plugin-operations/op-1": () => jsonResponse({ ...OPERATION, status: "succeeded" }),
  };
}

const INSTALLED_ENABLED = {
  id: "xray-manager",
  version: "0.1.0",
  status: "enabled",
  enabled: true,
  sourceCommit: "c0ffee",
  installedAt: "2026-10-30T01:00:00Z",
};

describe("plugin center", () => {
  it("refuses entries without an immutable artifact hash and requires explicit permission acceptance", async () => {
    const { calls } = await renderAuthenticated("/plugins", pluginRoutes([]));
    expect(await screen.findByRole("heading", { level: 1, name: "插件中心" })).toBeVisible();
    expect(await screen.findByText("缺少 SHA-256")).toBeVisible();

    const rows = await screen.findAllByRole("row");
    const unpublished = rows.find(row => row.textContent?.includes("尚未发布"));
    expect(unpublished?.textContent).toContain("缺少 SHA-256");

    const published = rows.find(row => row.textContent?.includes("0.1.0"));
    fireEvent.click(within(published as HTMLElement).getByRole("button", { name: "安装" }));

    const confirm = await screen.findByRole("button", { name: "确认安装" });
    expect(screen.getByText("sha-abc")).toBeVisible();
    fireEvent.click(confirm);
    expect(calls.some(call => call.url === "/api/v1/plugins/xray-manager/install")).toBe(false);

    fireEvent.click(screen.getByLabelText("plugins.node.manage"));
    fireEvent.click(screen.getByLabelText("安装后启用"));
    fireEvent.click(confirm);
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/plugins/xray-manager/install");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({
        version: "0.1.0",
        sourceCommit: "c0ffee",
        expectedSha256: "sha-abc",
        acceptedPermissions: ["plugins.node.manage"],
        enableAfterInstall: true,
      });
    });
    expect(await screen.findByText(/安装操作已提交/)).toBeVisible();
    expect(screen.getByRole("heading", { name: "插件操作 op-1" })).toBeVisible();
  });

  it("submits deactivation through the server and removes the plugin entry once the server confirms", async () => {
    let enabled = true;
    const routes = pluginRoutes([]);
    const { calls } = await renderAuthenticated("/plugins", {
      ...routes,
      "GET /api/v1/plugins": () => jsonResponse({ items: [{ ...INSTALLED_ENABLED, enabled, status: enabled ? "enabled" : "installed_disabled" }] }),
      "PUT /api/v1/plugins/xray-manager/activation": () => {
        enabled = false;
        return jsonResponse({ ...OPERATION, action: "deactivate", status: "draining" });
      },
    });
    expect(await screen.findByRole("link", { name: "Xray 节点管理器" })).toBeVisible();
    expect(await screen.findByText("已启用")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "停用" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PUT" && call.url === "/api/v1/plugins/xray-manager/activation");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ enabled: false });
    });
    expect(await screen.findByText(/停用操作已提交/)).toBeVisible();
    await waitFor(() => expect(screen.queryByRole("link", { name: "Xray 节点管理器" })).not.toBeInTheDocument());
    expect(screen.getByText("已安装·停用")).toBeVisible();
  });

  it("hides the plugin navigation entry when the plugin is installed but disabled", async () => {
    await renderAuthenticated("/plugins", pluginRoutes([{ ...INSTALLED_ENABLED, enabled: false, status: "installed_disabled" }]));
    expect(await screen.findByRole("heading", { level: 1, name: "插件中心" })).toBeVisible();
    expect(await screen.findByText("已安装·停用")).toBeVisible();
    expect(screen.queryByRole("link", { name: "Xray 节点管理器" })).not.toBeInTheDocument();
  });
});

function xrayRoutes(status: "enabled" | "installed_disabled"): FetchRoutes {
  return {
    "GET /api/v1/plugins": () => jsonResponse({ items: [{ ...INSTALLED_ENABLED, status, enabled: status === "enabled" }] }),
    "GET /api/v1/plugins/xray-manager": () => jsonResponse({ ...INSTALLED_ENABLED, status, enabled: status === "enabled" }),
    "GET /api/v1/plugins/xray-manager/nodes": () =>
      jsonResponse({ items: [{ nodeId: "n-1", name: "Tokyo", online: true, managed: true, xrayDetected: true }] }),
    "GET /api/v1/plugins/xray-manager/templates": () =>
      jsonResponse({ items: [{ templateId: "vless-reality-vision", name: "VLESS Reality", security: "reality" }] }),
    "GET /api/v1/plugins/xray-manager/users": () => jsonResponse({ items: [] }),
    "GET /api/v1/plugins/xray-manager/traffic": () => jsonResponse({ items: [], sourceType: "proxy_only" }),
    "GET /api/v1/plugins/xray-manager/traffic/settings": () => jsonResponse({ samplingIntervalSeconds: 60 }),
  };
}

describe("xray declarative plugin page", () => {
  it("blocks the console when the plugin is not enabled", async () => {
    await renderAuthenticated("/plugins/xray", xrayRoutes("installed_disabled"));
    expect(await screen.findByRole("heading", { level: 1, name: "Xray 节点管理器" })).toBeVisible();
    expect(screen.getByText("插件未启用")).toBeVisible();
    expect(screen.queryByRole("tab", { name: "节点与实例" })).not.toBeInTheDocument();
  });

  it("renders manifest tabs and real node probe data when enabled", async () => {
    await renderAuthenticated("/plugins/xray", xrayRoutes("enabled"));
    expect(await screen.findByRole("tab", { name: "节点与实例" })).toBeVisible();
    for (const label of ["安装", "配置", "用户", "流量", "历史"]) {
      expect(screen.getByRole("tab", { name: label })).toBeVisible();
    }
    expect(await screen.findByText("Tokyo")).toBeVisible();
  });
});

describe("workflows console (GM-W5-01a)", () => {
  it("reads the real workflows contract; without workflows.read it fails honestly instead of showing a blocked placeholder", async () => {
    // 无任何路由桩：GET /api/v1/workflows 404 → 页面显示加载失败，不再有静态 BLOCKED 页。
    await renderAuthenticated("/workflows", {});
    expect(await screen.findByRole("heading", { level: 1, name: "自动化工作流" })).toBeVisible();
    expect(screen.getByText("工作流列表加载失败")).toBeVisible();
    expect(screen.queryByText("BLOCKED：无工作流接口")).not.toBeInTheDocument();
  });
});
