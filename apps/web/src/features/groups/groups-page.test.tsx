import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { jsonResponse, type FetchRoutes } from "@/test/http";
import { renderAuthenticated } from "@/test/app";

afterEach(() => {
  vi.unstubAllGlobals();
  window.location.hash = "";
});

interface GroupFixture {
  id: string;
  name: string;
  kind: "manual";
  memberNodeIds: string[];
  revision: number;
  memberCount: number;
  description?: string;
}

const NODES = [
  { id: "n-1", name: "Alpha", online: true, region: "eu", provider: "aws", cpuPercent: 0, memoryPercent: 20 },
  { id: "n-2", name: "Beta", online: false, region: "eu", provider: "hetzner", cpuPercent: 50, memoryPercent: 60 },
  { id: "n-3", name: "Gamma", online: true, region: "us", provider: "gcp", cpuPercent: 10, memoryPercent: 30 },
];

function groupRoutes() {
  let groups: GroupFixture[] = [
    { id: "g-1", name: "Group A", kind: "manual", memberNodeIds: ["n-1", "n-2"], revision: 3, memberCount: 2 },
    { id: "g-2", name: "Group B", kind: "manual", memberNodeIds: ["n-3"], revision: 1, memberCount: 1 },
  ];

  const routes: FetchRoutes = {
    "GET /api/v1/groups": () => jsonResponse({ items: groups }),
    "GET /api/v1/groups/classifications": (_init, url) => {
      const dimension = url.searchParams.get("dimension");
      return jsonResponse({
        dimension,
        items:
          dimension === "provider"
            ? [{ value: "aws", nodeCount: 1 }, { value: "hetzner", nodeCount: 1 }]
            : [{ value: "eu", nodeCount: 2 }, { value: "us", nodeCount: 1 }],
      });
    },
    "GET /api/v1/groups/node-results": (_init, url) => {
      const mode = url.searchParams.get("mode");
      const search = (url.searchParams.get("search") ?? "").toLowerCase();
      const status = url.searchParams.get("status") ?? "all";
      let items: Array<(typeof NODES)[number]> = [];
      if (mode === "manual") {
        const group = groups.find(entry => entry.id === url.searchParams.get("groupId"));
        items = NODES.filter(node => group?.memberNodeIds.includes(node.id));
      } else {
        const value = url.searchParams.get("value");
        items = NODES.filter(node => (url.searchParams.get("dimension") === "provider" ? node.provider : node.region) === value);
      }
      if (search) items = items.filter(node => `${node.name} ${node.id}`.toLowerCase().includes(search));
      if (status !== "all") items = items.filter(node => (status === "online" ? node.online : !node.online));
      return jsonResponse({ items, total: items.length, nextCursor: null });
    },
    "GET /api/v1/nodes": () => jsonResponse({ items: NODES }),
    "POST /api/v1/groups": (_init, _url) => {
      const body = JSON.parse(String(_init?.body));
      const created: GroupFixture = {
        id: "g-new",
        name: body.name,
        kind: "manual",
        memberNodeIds: [],
        revision: 1,
        memberCount: 0,
        description: body.description,
      };
      groups = [...groups, created];
      return jsonResponse(created, 201);
    },
    "PATCH /api/v1/groups/g-1": (_init) => {
      const body = JSON.parse(String(_init?.body));
      const updated = { ...groups[0], name: body.name ?? groups[0].name, description: body.description, revision: groups[0].revision + 1 };
      groups = [updated, groups[1]];
      return jsonResponse(updated);
    },
    "DELETE /api/v1/groups/g-1": () => new Response(null, { status: 204 }),
    "PUT /api/v1/groups/g-1/members": (_init) => {
      const body = JSON.parse(String(_init?.body));
      const updated: GroupFixture = {
        ...groups[0],
        memberNodeIds: body.memberNodeIds,
        memberCount: body.memberNodeIds.length,
        revision: groups[0].revision + 1,
      };
      groups = [updated, groups[1]];
      return jsonResponse(updated);
    },
  };
  return routes;
}

async function openGroups() {
  return renderAuthenticated("/groups", groupRoutes());
}

describe("groups page", () => {
  it("selects a manual group and reads results from the server with search and status", async () => {
    const { calls } = await openGroups();
    expect(await screen.findByRole("heading", { level: 1, name: "分组管理" })).toBeVisible();
    expect(await screen.findByRole("table", { name: "自定义分组成员节点" })).toBeVisible();
    expect(screen.getByText("Alpha")).toBeVisible();
    const first = new URL(calls.find(call => call.url.startsWith("/api/v1/groups/node-results"))?.url ?? "", "http://x");
    expect(first.searchParams.get("mode")).toBe("manual");
    expect(first.searchParams.get("groupId")).toBe("g-1");

    fireEvent.click(screen.getByRole("button", { name: /Group B/ }));
    await waitFor(() => expect(screen.getByText("Gamma")).toBeVisible());
    expect(screen.queryByText("Alpha")).not.toBeInTheDocument();
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("groupId")).toBe("g-2");
    });

    fireEvent.change(screen.getByLabelText("搜索名称或 IP"), { target: { value: "gamma" } });
    fireEvent.click(screen.getByRole("button", { name: "搜索" }));
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("search")).toBe("gamma");
    });

    fireEvent.change(screen.getByLabelText("状态"), { target: { value: "offline" } });
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("status")).toBe("offline");
    });
    expect(await screen.findByText("该分组暂无节点")).toBeVisible();
  });

  it("switches to read-only automatic classification and never shows member editing", async () => {
    const { calls } = await openGroups();
    fireEvent.click(await screen.findByRole("tab", { name: "自动分类（只读）" }));
    expect(await screen.findByRole("button", { name: /eu/ })).toBeVisible();
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("mode")).toBe("automatic");
    });
    expect(screen.queryByRole("button", { name: "管理成员" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "云厂商" }));
    expect(await screen.findByRole("button", { name: /aws/ })).toBeVisible();
  });

  it("saves a complete member set with expectedRevision and closes only after success", async () => {
    const { calls } = await openGroups();
    expect(await screen.findByText("Alpha")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "管理成员" }));
    expect(await screen.findByRole("heading", { level: 2, name: /管理成员 · Group A/ })).toBeVisible();
    expect(await screen.findByLabelText("选择节点 Gamma")).not.toBeChecked();
    expect(screen.getByLabelText("选择节点 Alpha")).toBeChecked();
    fireEvent.click(screen.getByLabelText("选择节点 Gamma"));
    fireEvent.click(screen.getByRole("button", { name: "保存成员（3）" }));

    await waitFor(() => {
      const save = calls.find(call => call.method === "PUT" && call.url === "/api/v1/groups/g-1/members");
      expect(save).toBeDefined();
      expect(JSON.parse(String(save?.init?.body))).toEqual({ memberNodeIds: ["n-1", "n-2", "n-3"], expectedRevision: 3 });
    });
    expect(await screen.findByText(/成员集合已保存/)).toBeVisible();
    expect(screen.queryByRole("heading", { level: 2, name: /管理成员/ })).not.toBeInTheDocument();
  });

  it("does not submit when the member drawer is cancelled", async () => {
    const { calls } = await openGroups();
    expect(await screen.findByText("Alpha")).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: "管理成员" }));
    await screen.findByLabelText("选择节点 Gamma");
    fireEvent.click(screen.getByLabelText("选择节点 Gamma"));
    fireEvent.click(screen.getByRole("button", { name: "取消" }));
    expect(calls.some(call => call.method === "PUT" && call.url.includes("/members"))).toBe(false);
  });

  it("creates a manual group through the server and selects it", async () => {
    const { calls } = await openGroups();
    fireEvent.click(await screen.findByRole("button", { name: "新建自定义分组" }));
    expect(await screen.findByRole("heading", { level: 2, name: "新建自定义分组" })).toBeVisible();
    fireEvent.change(screen.getByLabelText("分组名称"), { target: { value: "Group C" } });
    fireEvent.change(screen.getByLabelText("备注（可选）"), { target: { value: "新建分组" } });
    fireEvent.click(screen.getByRole("button", { name: "创建分组" }));
    await waitFor(() => {
      const create = calls.find(call => call.method === "POST" && call.url === "/api/v1/groups");
      expect(create).toBeDefined();
      expect(JSON.parse(String(create?.init?.body))).toEqual({ name: "Group C", description: "新建分组" });
    });
    expect(await screen.findByText(/分组已创建/)).toBeVisible();
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("groupId")).toBe("g-new");
    });
  });
});
