import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IDENTITY, jsonResponse, type FetchRoutes } from "@/test/http";
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
  { id: "n-1", name: "Alpha", online: true, region: "eu", provider: "aws", ip: "192.0.2.1", tags: ["web"], cpuPercent: 0, memoryPercent: 20 },
  { id: "n-2", name: "Beta", online: false, region: "eu", provider: "hetzner", ip: "192.0.2.2", tags: ["web"], cpuPercent: null, memoryPercent: null },
  { id: "n-3", name: "Gamma", online: true, region: "us", provider: "gcp", ip: "192.0.2.3", tags: [], cpuPercent: 10, memoryPercent: 30 },
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
        items: [...new Set(NODES.flatMap(node => dimension === "tag" ? node.tags : [dimension === "provider" ? node.provider : node.region]))]
          .sort()
          .map(value => ({ value, label: value, nodeCount: NODES.filter(node => dimension === "tag" ? node.tags.includes(value) : (dimension === "provider" ? node.provider : node.region) === value).length })),
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
        items = NODES.filter(node => url.searchParams.get("dimension") === "tag" ? node.tags.includes(value ?? "") : (url.searchParams.get("dimension") === "provider" ? node.provider : node.region) === value);
      }
      if (search) items = items.filter(node => `${node.name} ${node.ip}`.toLowerCase().includes(search));
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

describe("one-click classification", () => {
  it("refreshes the current dimension, clears stale filters and pagination without writes", async () => {
    const routes = groupRoutes();
    let generation = 0;
    let facetReads = 0;
    const manyNodes = Array.from({ length: 51 }, (_, index) => ({
      ...NODES[1], id: `page-${String(index).padStart(3, "0")}`, name: `old search ${index}`,
    }));
    const refreshedNodes = [{ ...NODES[2], id: "fresh-node", name: "Fresh Gamma" }];
    routes["GET /api/v1/groups/classifications"] = (_init, url) => {
      facetReads += 1;
      const dimension = url.searchParams.get("dimension");
      const inventory = generation === 0 ? manyNodes : refreshedNodes;
      const values = [...new Set(inventory.map(node => dimension === "provider" ? node.provider : node.region))].sort();
      return jsonResponse({ dimension, items: values.map(value => ({ value, label: value, nodeCount: inventory.length })) });
    };
    routes["GET /api/v1/groups/node-results"] = (_init, url) => {
      if (url.searchParams.get("mode") === "manual") return jsonResponse({ items: NODES.slice(0, 2), total: 2, nextCursor: null });
      const inventory = generation === 0 ? manyNodes : refreshedNodes;
      const value = url.searchParams.get("value");
      const search = (url.searchParams.get("search") ?? "").toLowerCase();
      const status = url.searchParams.get("status") ?? "all";
      const matched = inventory.filter(node =>
        (url.searchParams.get("dimension") === "provider" ? node.provider : node.region) === value &&
        (!search || `${node.name} ${node.ip}`.toLowerCase().includes(search)) &&
        (status === "all" || (status === "online" ? node.online : !node.online)),
      );
      const cursor = url.searchParams.get("cursor");
      const after = cursor ? JSON.parse(atob(cursor)).after as string : "";
      const remaining = matched.filter(node => node.id > after);
      const items = remaining.slice(0, Number(url.searchParams.get("limit") ?? 50));
      const nextCursor = remaining.length > items.length ? btoa(JSON.stringify({ after: items[items.length - 1].id })) : null;
      return jsonResponse({ items, total: matched.length, nextCursor });
    };
    const { calls } = await renderAuthenticated("/groups", routes);
    await screen.findByText("Alpha");
    fireEvent.click(screen.getByRole("button", { name: "一键分类" }));
    expect(await screen.findByRole("tab", { name: "自动分类（只读）" })).toHaveAttribute("aria-selected", "true");
    fireEvent.click(await screen.findByRole("tab", { name: "云厂商" }));
    fireEvent.click(await screen.findByRole("button", { name: /hetzner/ }));
    fireEvent.change(screen.getByLabelText("搜索名称或 IP"), { target: { value: "old search" } });
    fireEvent.click(screen.getByRole("button", { name: "搜索" }));
    fireEvent.change(screen.getByLabelText("状态"), { target: { value: "offline" } });
    await waitFor(() => expect(screen.getByRole("button", { name: "下一页" })).toBeEnabled());
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      expect(new URL(last?.url ?? "", "http://x").searchParams.get("cursor")).toBeTruthy();
    });
    generation = 1;
    const previousReads = facetReads;
    fireEvent.click(screen.getByRole("button", { name: "一键分类" }));
    expect(await screen.findByRole("button", { name: /gcp/ })).toBeVisible();
    expect(await screen.findByText("Fresh Gamma")).toBeVisible();
    expect(screen.queryByRole("button", { name: /hetzner/ })).not.toBeInTheDocument();
    expect(facetReads).toBeGreaterThan(previousReads);
    expect(screen.getByLabelText("搜索名称或 IP")).toHaveValue("");
    expect(screen.getByLabelText("状态")).toHaveValue("all");
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    await waitFor(() => {
      const last = [...calls].reverse().find(call => call.url.startsWith("/api/v1/groups/node-results"));
      const params = new URL(last?.url ?? "", "http://x").searchParams;
      expect(Object.fromEntries(params)).toEqual({ mode: "automatic", dimension: "provider", value: "gcp", status: "all", limit: "50" });
    });
    expect(screen.queryByRole("button", { name: "管理成员" })).not.toBeInTheDocument();
    expect(calls.filter(call => call.url.startsWith("/api/v1/groups")).every(call => call.method === "GET")).toBe(true);
  });

  it("shows an empty classification without fabricating results", async () => {
    const routes = groupRoutes();
    routes["GET /api/v1/groups/classifications"] = () => jsonResponse({ dimension: "region", items: [] });
    const { calls } = await renderAuthenticated("/groups", routes);
    await screen.findByText("Alpha");
    fireEvent.click(screen.getByRole("button", { name: "一键分类" }));
    expect(await screen.findByText("当前维度没有分类值。")).toBeVisible();
    expect(screen.getByText("请选择一个分类值")).toBeVisible();
    expect(calls.filter(call => call.url.startsWith("/api/v1/groups/node-results"))
      .some(call => new URL(call.url, "http://x").searchParams.get("mode") === "automatic")).toBe(false);
    expect(calls.filter(call => call.url.startsWith("/api/v1/groups")).every(call => call.method === "GET")).toBe(true);
  });

  it("allows a read-only identity to classify and shows real read failures", async () => {
    const routes = groupRoutes();
    routes["GET /api/v1/groups/classifications"] = () => jsonResponse({ error: { code: "forbidden", message: "request is not permitted" } }, 403);
    const { calls } = await renderAuthenticated("/groups", routes, { ...IDENTITY, roles: ["viewer"], permissions: ["node.read"], isBootstrapAdmin: false });
    const classify = await screen.findByRole("button", { name: "一键分类" });
    expect(classify).toBeEnabled();
    expect(screen.getByRole("button", { name: "新建自定义分组" })).toBeDisabled();
    fireEvent.click(classify);
    expect(await screen.findByText("无权限")).toBeVisible();
    expect(screen.queryByRole("button", { name: "重新加载" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /eu.*台/ })).not.toBeInTheDocument();
    expect(calls.filter(call => call.url.startsWith("/api/v1/groups")).every(call => call.method === "GET")).toBe(true);
  });
});
