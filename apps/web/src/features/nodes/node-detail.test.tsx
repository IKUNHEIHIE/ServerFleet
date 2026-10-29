import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NodeDetailPanel } from "./node-detail";
import { readyNodes } from "./test/fixtures";

const byName = (name: string) => {
  const node = readyNodes.find(item => item.name === name);
  if (!node) throw new Error(`fixture node not found: ${name}`);
  return node;
};

const detailRegion = () => screen.getByRole("region", { name: "节点详情" });

describe("NodeDetailPanel · 只读字段", () => {
  it("展示名称、ID、状态、平台、Agent 版本、最近在线与指标", () => {
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} onClose={vi.fn()} />);

    const region = detailRegion();
    expect(within(region).getByText("aws-tokyo-01")).toBeVisible();
    expect(within(region).getByText("node-online-01")).toBeVisible();
    expect(within(region).getByText("在线")).toBeVisible();
    expect(within(region).getByText("Ubuntu 24.04")).toBeVisible();
    expect(within(region).getByText("v0.1.0")).toBeVisible();
    expect(within(region).getByText("2026-10-29 02:59 UTC")).toBeVisible();
    expect(within(region).getByText("18%")).toBeVisible();
    expect(within(region).getByText("46%")).toBeVisible();
    expect(within(region).getByText("CPU 使用率")).toBeVisible();
    expect(within(region).getByText("内存使用率")).toBeVisible();
  });

  it("未知元数据保持未知，不推断为空或正常", () => {
    render(<NodeDetailPanel node={byName("node-unknown-07")} />);

    const region = detailRegion();
    expect(within(region).getAllByText("未知").length).toBeGreaterThanOrEqual(4);
    expect(within(region).getAllByText("node-unknown-07").length).toBeGreaterThanOrEqual(1);
  });

  it("没有关闭回调时不渲染任何按钮，避免出现可点但无效的入口", () => {
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("只提供一个关闭操作，不含编辑/删除/执行入口", () => {
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} onClose={vi.fn()} />);

    const buttons = screen.getAllByRole("button");
    expect(buttons).toHaveLength(1);
    expect(buttons[0]).toHaveAccessibleName("关闭详情");
  });
});

describe("NodeDetailPanel · 指标质量回归", () => {
  it("陈旧样本保留数值并显示可见采样时间", () => {
    render(<NodeDetailPanel node={byName("legacy-berlin-01")} />);

    const region = detailRegion();
    expect(within(region).getByText("55%")).toBeVisible();
    expect(within(region).getAllByText("陈旧 · 采样于 2026-10-29 02:41 UTC")).toHaveLength(2);
  });

  it("离线和暂停节点只给原因，不显示任何百分比", () => {
    render(<NodeDetailPanel node={byName("azure-us-01")} />);
    expect(screen.getAllByText("无有效数据（节点不在线或状态未确认）")).toHaveLength(2);
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();

    render(<NodeDetailPanel node={byName("gcp-osaka-01")} />);
    expect(screen.getAllByText("无有效数据（节点不在线或状态未确认）")).toHaveLength(4);
  });

  it("真实的 0% 与 100% 原样显示", () => {
    render(<NodeDetailPanel node={byName("gcp-singapore-01")} />);
    expect(screen.getByText("0%")).toBeVisible();

    render(<NodeDetailPanel node={byName("gcp-frankfurt-01")} />);
    expect(screen.getByText("100%")).toBeVisible();
  });
});

describe("NodeDetailPanel · 键盘与关闭", () => {
  it("点击关闭按钮触发回调", () => {
    const onClose = vi.fn();
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} onClose={onClose} />);

    fireEvent.click(screen.getByRole("button", { name: "关闭详情" }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("在详情区内按 Escape 关闭，其他按键不影响", () => {
    const onClose = vi.fn();
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} onClose={onClose} />);

    const region = detailRegion();
    fireEvent.keyDown(region, { key: "Enter" });
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(region, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("没有关闭回调时 Escape 不报错也不产生副作用", () => {
    render(<NodeDetailPanel node={byName("aws-tokyo-01")} />);
    const event = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
    expect(() => detailRegion().dispatchEvent(event)).not.toThrow();
    expect(event.defaultPrevented).toBe(false);
  });
});

describe("NodeDetailPanel · 隔离性", () => {
  it("渲染期间不发起网络请求", () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      render(<NodeDetailPanel node={byName("aws-tokyo-01")} onClose={vi.fn()} />);
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
