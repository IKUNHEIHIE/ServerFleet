import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { navigate } from "@/app/router";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

const GROUP = {
  id: "sg-1",
  revision: 2,
  name: "Web",
  description: "frontend",
  memberNodeIds: ["n-1", "n-2"],
  service: { port: 443, protocol: "https", healthCheck: "/health" },
  loadBalancing: { enabled: true, strategy: "round_robin" },
  gatewayIds: ["gw-1"],
  healthyNodeIds: ["n-1"],
  dispatchEligibleNodeIds: ["n-1"],
};

const NODES = [
  { id: "n-1", name: "Alpha", online: true, region: "eu", provider: "aws" },
  { id: "n-2", name: "Beta", online: false, region: "eu", provider: "hetzner" },
];

function routes(): FetchRoutes {
  return {
    "GET /api/v1/service-groups": () => jsonResponse({ items: [GROUP] }),
    "GET /api/v1/service-groups/sg-1": () => jsonResponse(GROUP),
    "GET /api/v1/nodes": () => jsonResponse({ items: NODES }),
    "GET /api/v1/service-groups/sg-1/dispatch-preview": () =>
      jsonResponse({ serviceGroupId: "sg-1", gatewayEligible: true, eligibleNodeIds: ["n-1"], excludedNodes: [{ nodeId: "n-2", reason: "offline" }] }),
    "PATCH /api/v1/service-groups/sg-1/members/n-1/scheduling": (_init) => jsonResponse({ nodeId: "n-1", paused: true, dispatchEligible: false }),
    "PUT /api/v1/service-groups/sg-1/members": (_init) => jsonResponse(GROUP),
    "POST /api/v1/service-groups": (_init) => jsonResponse({ ...GROUP, id: "sg-new", name: "API", memberNodeIds: [] }, 201),
    "DELETE /api/v1/service-groups/sg-1": () => new Response(null, { status: 204 }),
  };
}

describe("service groups", () => {
  it("lists groups, previews dispatch and pauses a member through the server", async () => {
    const { calls } = await renderAuthenticated("/service-groups", routes());
    expect(await screen.findByRole("heading", { level: 1, name: "服务组管理" })).toBeVisible();
    const link = await screen.findByRole("link", { name: "Web" });
    expect(link).toHaveAttribute("href", "#/service-groups/sg-1");
    navigate("/service-groups/sg-1");

    expect(await screen.findByRole("heading", { level: 1, name: "Web" })).toBeVisible();
    expect(await screen.findByText("Alpha")).toBeVisible();
    expect(screen.getByText("Beta")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "调度预演" }));
    expect(await screen.findByText("网关可调度：是")).toBeVisible();
    expect(screen.getByText("n-2：offline")).toBeVisible();

    const pauseButtons = screen.getAllByRole("button", { name: "暂停调度" });
    fireEvent.click(pauseButtons[0]);
    expect(await screen.findByRole("heading", { level: 2, name: "暂停成员调度" })).toBeVisible();
    fireEvent.change(screen.getByLabelText("原因"), { target: { value: "维护" } });
    fireEvent.click(screen.getByRole("button", { name: "确认" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PATCH" && call.url === "/api/v1/service-groups/sg-1/members/n-1/scheduling");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ paused: true, reason: "维护" });
    });
    expect(await screen.findByText("成员已暂停调度（服务端结果）。")).toBeVisible();
  });

  it("saves a complete member set with revision and deletes only after confirmation", async () => {
    const { calls } = await renderAuthenticated("/service-groups/sg-1", routes());
    expect(await screen.findByText("Alpha")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "管理成员" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByLabelText("选择节点 Alpha")).toBeChecked();
    expect(within(dialog).getByLabelText("选择节点 Beta")).toBeChecked();
    fireEvent.click(within(dialog).getByRole("button", { name: "保存成员（2）" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "PUT" && call.url === "/api/v1/service-groups/sg-1/members");
      expect(call).toBeDefined();
      expect(JSON.parse(String(call?.init?.body))).toEqual({ memberNodeIds: ["n-1", "n-2"], expectedRevision: 2 });
    });
    expect(await screen.findByText("服务组成员已保存（服务端结果）。")).toBeVisible();

    await waitFor(() => expect(screen.getByRole("button", { name: "删除" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "删除" }));
    expect(await screen.findByRole("heading", { level: 2, name: "删除服务组" })).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "确认删除" }));
    await waitFor(() => {
      expect(calls.some(call => call.method === "DELETE" && call.url === "/api/v1/service-groups/sg-1")).toBe(true);
    });
    await waitFor(() => expect(window.location.hash).toBe("#/service-groups"));
  });

  it("creates a service group from the list page with validated fields", async () => {
    const { calls } = await renderAuthenticated("/service-groups", routes());
    fireEvent.click(await screen.findByRole("button", { name: "新建服务组" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("服务组名称"), { target: { value: "API" } });
    fireEvent.change(within(dialog).getByLabelText("服务端口"), { target: { value: "8080" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "创建服务组" }));
    await waitFor(() => {
      const call = calls.find(call => call.method === "POST" && call.url === "/api/v1/service-groups");
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.init?.body));
      expect(body.name).toBe("API");
      expect(body.service).toEqual({ port: 8080, protocol: "http" });
      expect(body.loadBalancing).toEqual({ enabled: false, strategy: "round_robin" });
    });
    await waitFor(() => expect(window.location.hash).toBe("#/service-groups/sg-new"));
  });
});
