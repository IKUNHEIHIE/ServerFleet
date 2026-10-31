import { describe, expect, it } from "vitest";
import { hasPermission } from "./backend-aliases";

describe("hasPermission", () => {
  it("accepts the exact UI key (candidate contract / acceptance stub)", () => {
    expect(hasPermission(["task.approve", "node.manage"], "task.approve")).toBe(true);
    expect(hasPermission(["task.approve", "node.manage"], "node.manage")).toBe(true);
  });

  it("maps UI keys to the real controller permission catalog", () => {
    // 真实 internal/auth 内置 5 键 + server.go ExtraPermissions 键。
    expect(hasPermission(["tasks.approve", "tasks.read"], "task.approve")).toBe(true);
    expect(hasPermission(["tasks.submit"], "task.submit")).toBe(true);
    expect(hasPermission(["nodes.write"], "node.manage")).toBe(true);
    expect(hasPermission(["node.create"], "node.manage")).toBe(true);
    expect(hasPermission(["groups.write"], "group.manage")).toBe(true);
    expect(hasPermission(["servicegroups.write"], "servicegroup.manage")).toBe(true);
    expect(hasPermission(["gateway.write"], "gateway.manage")).toBe(true);
    expect(hasPermission(["gateway.provision"], "gateway.install")).toBe(true);
    expect(hasPermission(["files.transfer"], "file.transfer")).toBe(true);
    expect(hasPermission(["dns.record.delete"], "dns.record.manage")).toBe(true);
    expect(hasPermission(["notifications.channel"], "notify.channel.manage")).toBe(true);
    expect(hasPermission(["audit.read"], "audit.read")).toBe(true);
    // GM-W5-01a：工作流后端权限键（internal/workflows/http.go ActionRead/ActionWrite）。
    expect(hasPermission(["workflows.read"], "workflow.read")).toBe(true);
    expect(hasPermission(["workflows.write"], "workflow.write")).toBe(true);
  });

  it("keeps workflow write out of read and vice versa", () => {
    expect(hasPermission(["workflows.read"], "workflow.write")).toBe(false);
    expect(hasPermission(["workflows.write"], "workflow.read")).toBe(false);
  });

  it("never grants task.manage without at least one real task mutation key", () => {
    expect(hasPermission(["tasks.read"], "task.manage")).toBe(false);
    expect(hasPermission(["tasks.revise"], "task.manage")).toBe(true);
  });

  it("returns false for missing results and unknown keys instead of guessing", () => {
    expect(hasPermission(undefined, "task.approve")).toBe(false);
    expect(hasPermission([], "task.approve")).toBe(false);
    expect(hasPermission(["tasks.approve"], "xray.manage")).toBe(false);
    // AdminOnly 的 host.root 不映射到任何 UI 管理键：界面不得因持有 root 键而放开普通管理入口。
    expect(hasPermission(["host.root"], "node.manage")).toBe(false);
    expect(hasPermission(["host.root"], "gateway.manage")).toBe(false);
  });
});
