import { describe, expect, it } from "vitest";
import { buildPath, matchPath, readLocation } from "@/app/router";
import { NAV_GROUPS, OVERVIEW, ROUTE_META } from "@/app/navigation";
import { ROUTES } from "@/app/routes";

describe("hash router", () => {
  it("matches static and parameterised paths without prefix leakage", () => {
    expect(matchPath("/nodes", "/nodes")).toEqual({ params: {} });
    expect(matchPath("/nodes/:nodeId", "/nodes/n-1")).toEqual({ params: { nodeId: "n-1" } });
    expect(matchPath("/nodes/:nodeId", "/nodes")).toBeNull();
    expect(matchPath("/nodes/:nodeId", "/nodes/a/b")).toBeNull();
    expect(matchPath("/nodes/:nodeId", "/nodes/a%20b")).toEqual({ params: { nodeId: "a b" } });
    expect(matchPath("/nodes/:nodeId", "/nodes/%E0%A4%A")).toBeNull();
  });

  it("builds query strings without empty values", () => {
    expect(buildPath("/nodes", { onlineOnly: true, empty: "", missing: undefined })).toBe("/nodes?onlineOnly=true");
  });

  it("defaults an empty hash to the overview", () => {
    window.location.hash = "";
    expect(readLocation()).toEqual({ path: "/overview", search: "", hash: "" });
    window.location.hash = "#/nodes?x=1";
    expect(readLocation()).toEqual({ path: "/nodes", search: "?x=1", hash: "" });
    window.location.hash = "";
  });
});

describe("navigation registry", () => {
  it("registers every navigation entry exactly once", () => {
    const paths = [OVERVIEW, ...NAV_GROUPS.flatMap(group => group.items)].map(item => item.path);
    expect(new Set(paths).size).toBe(paths.length);
    for (const path of paths) {
      expect(ROUTES.some(route => route.path === path), `missing route for ${path}`).toBe(true);
      expect(ROUTE_META[path], `missing title for ${path}`).toBeTruthy();
    }
  });

  it("keeps the complete v0.9 functional map", () => {
    const labels = NAV_GROUPS.flatMap(group => group.items.map(item => item.label));
    expect(labels).toEqual(
      expect.arrayContaining([
        "节点列表",
        "添加节点",
        "分组管理",
        "文件互传",
        "批量任务",
        "监控告警",
        "AI 运维",
        "网关管理",
        "服务发布",
        "服务组管理",
        "自动化工作流",
        "插件中心",
        "DNS 管理",
        "证书管理",
        "更新中心",
        "备份与恢复",
        "用户与权限",
        "审计日志",
        "系统设置",
      ]),
    );
  });

  it("has metadata for every route", () => {
    for (const route of ROUTES) {
      expect(ROUTE_META[route.path], `missing meta for ${route.path}`).toBeTruthy();
    }
  });
});
