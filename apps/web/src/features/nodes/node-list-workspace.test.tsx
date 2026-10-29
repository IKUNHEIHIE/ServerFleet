import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NodeListWorkspace } from "./node-list-workspace";
import {
  FIXED_NOW,
  buildReadyState,
  emptyState,
  failedState,
  idleOnlineNodeDetail,
  loadingState,
  nodeMetrics,
  onlineNodeDetail,
  readyNodes,
  readyState,
} from "./test/fixtures";

const freshAt = (secondsAgo: number) => new Date(FIXED_NOW - secondsAgo * 1000).toISOString();

/** 只含 aws-tokyo-01 的状态，用于验证同 ID 数据更新。 */
const onlyOnlineNode = (cpuPercent: number) =>
  buildReadyState(
    [onlineNodeDetail],
    new Map([[onlineNodeDetail.nodeId, { cpuPercent, memoryPercent: 46, sampledAt: freshAt(10) }]]),
  );

function listRows() {
  const table = screen.getByRole("table", { name: "节点列表" });
  const body = table.querySelector("tbody");
  if (!body) throw new Error("tbody not found");
  return within(body).getAllByRole("row");
}

function rowNames() {
  return listRows().map(row => row.querySelector("th span")?.textContent ?? "");
}

function detailRegion() {
  return screen.queryByRole("region", { name: "节点详情" });
}

function searchBox() {
  return screen.getByRole("searchbox", { name: "搜索节点" });
}

describe("NodeListWorkspace · 选择与详情", () => {
  it("初始不显示详情区", () => {
    render(<NodeListWorkspace state={readyState} />);

    expect(detailRegion()).toBeNull();
    expect(screen.getByRole("table", { name: "节点列表" })).toBeVisible();
  });

  it("点击行后显示该节点详情，并在列表中标记当前行", () => {
    render(<NodeListWorkspace state={readyState} />);

    fireEvent.click(listRows()[0]);

    const region = detailRegion();
    expect(region).not.toBeNull();
    expect(within(region!).getByText("aws-tokyo-01")).toBeVisible();
    expect(within(region!).getByText("18%")).toBeVisible();
    expect(listRows()[0]).toHaveAttribute("aria-current", "true");
    expect(listRows()[1]).not.toHaveAttribute("aria-current");
  });

  it("键盘 Enter 也能选择节点", () => {
    render(<NodeListWorkspace state={readyState} />);
    const rows = listRows();

    rows[1].focus();
    fireEvent.keyDown(rows[1], { key: "Enter" });

    expect(within(detailRegion()!).getByText("gcp-singapore-01")).toBeVisible();
    expect(within(detailRegion()!).getByText("0%")).toBeVisible();
  });

  it("切换节点时不残留上一个节点的数据", () => {
    render(<NodeListWorkspace state={readyState} />);

    fireEvent.click(listRows()[0]);
    fireEvent.click(listRows()[2]);

    const region = detailRegion()!;
    expect(within(region).getByText("gcp-frankfurt-01")).toBeVisible();
    expect(within(region).queryByText("aws-tokyo-01")).not.toBeInTheDocument();
    expect(within(region).queryByText("18%")).not.toBeInTheDocument();
    expect(within(region).getByText("100%")).toBeVisible();
  });

  it("关闭按钮清除选择并移除详情", () => {
    render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    fireEvent.click(screen.getByRole("button", { name: "关闭详情" }));

    expect(detailRegion()).toBeNull();
    expect(listRows()[0]).not.toHaveAttribute("aria-current");
  });

  it("Escape 关闭详情", () => {
    render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    fireEvent.keyDown(detailRegion()!, { key: "Escape" });

    expect(detailRegion()).toBeNull();
  });
});

describe("NodeListWorkspace · 数据更新与来源变化", () => {
  it("同一 ID 的数据更新后，详情展示本轮新记录而不是旧快照", () => {
    const { rerender } = render(<NodeListWorkspace state={onlyOnlineNode(18)} />);
    fireEvent.click(listRows()[0]);
    expect(within(detailRegion()!).getByText("18%")).toBeVisible();

    rerender(<NodeListWorkspace state={onlyOnlineNode(77)} />);

    expect(within(detailRegion()!).getByText("77%")).toBeVisible();
    expect(within(detailRegion()!).queryByText("18%")).not.toBeInTheDocument();
  });

  it("选中节点从来源移除时立即隐藏详情，且不自动改选其他节点", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);
    expect(detailRegion()).not.toBeNull();

    rerender(<NodeListWorkspace state={buildReadyState([idleOnlineNodeDetail], nodeMetrics)} />);

    expect(detailRegion()).toBeNull();
    expect(screen.getByRole("table", { name: "节点列表" })).toBeVisible();
    expect(rowNames()).toEqual(["gcp-singapore-01"]);
  });

  it("ready → loading 时隐藏旧详情，恢复后按当前记录重新呈现", () => {
    const { rerender } = render(<NodeListWorkspace state={onlyOnlineNode(18)} />);
    fireEvent.click(listRows()[0]);

    rerender(<NodeListWorkspace state={loadingState} />);
    expect(detailRegion()).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");

    rerender(<NodeListWorkspace state={onlyOnlineNode(42)} />);
    expect(within(detailRegion()!).getByText("42%")).toBeVisible();
  });

  it("ready → error 时隐藏详情并保留失败提示", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    rerender(<NodeListWorkspace state={failedState} />);

    expect(detailRegion()).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("节点列表加载失败");
  });

  it("ready → 空列表时隐藏详情并显示“暂无节点”", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    rerender(<NodeListWorkspace state={emptyState} />);

    expect(detailRegion()).toBeNull();
    expect(screen.getByText("暂无节点")).toBeVisible();
  });

  it("本地筛选隐藏选中行后，详情仍按已加载集合展示", () => {
    render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    fireEvent.change(searchBox(), { target: { value: "gcp" } });

    expect(rowNames()).not.toContain("aws-tokyo-01");
    expect(within(detailRegion()!).getByText("aws-tokyo-01")).toBeVisible();
  });
});

describe("NodeListWorkspace · 既有行为回归", () => {
  it("筛选后的表格仍保留离线无值与陈旧可见时间", () => {
    render(<NodeListWorkspace state={readyState} />);

    fireEvent.change(screen.getByRole("combobox", { name: "状态筛选" }), { target: { value: "offline" } });
    const row = listRows()[0];
    expect(within(row).getAllByText("无有效数据（节点不在线或状态未确认）")).toHaveLength(2);
    expect(within(row).queryByText(/%/)).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole("combobox", { name: "状态筛选" }), { target: { value: "all" } });
    fireEvent.change(searchBox(), { target: { value: "legacy-berlin" } });
    fireEvent.click(listRows()[0]);
    expect(within(detailRegion()!).getAllByText("陈旧 · 采样于 2026-10-29 02:41 UTC")).toHaveLength(2);
  });

  it("选择后列表的方向键导航仍然可用", () => {
    render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[0]);

    const rows = listRows();
    rows[0].focus();
    fireEvent.keyDown(rows[0], { key: "ArrowDown" });

    expect(rows[1]).toHaveFocus();
    expect(detailRegion()).not.toBeNull();
  });

  it("选择不会改动调用方传入的节点集合", () => {
    const before = readyNodes.map(node => node.id);
    render(<NodeListWorkspace state={readyState} />);

    fireEvent.click(listRows()[0]);
    fireEvent.click(listRows()[3]);

    expect(readyNodes.map(node => node.id)).toEqual(before);
    expect(readyNodes).toHaveLength(8);
  });

  it("渲染与选择期间不发起网络请求", () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      render(<NodeListWorkspace state={readyState} />);
      fireEvent.click(listRows()[0]);
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
