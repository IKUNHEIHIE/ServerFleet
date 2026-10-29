import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NodeListWorkspace } from "./node-list-workspace";
import {
  FIXED_NOW,
  allNodeDetails,
  buildReadyState,
  emptyState,
  failedState,
  idleOnlineNodeDetail,
  loadingState,
  nodeMetrics,
  readyState,
} from "./test/fixtures";

function listRows() {
  const table = screen.getByRole("table", { name: "节点列表" });
  const body = table.querySelector("tbody");
  if (!body) throw new Error("tbody not found");
  return within(body).getAllByRole("row");
}

const detailRegion = () => screen.getByRole("region", { name: "节点详情" });
const listRegion = () => screen.getByRole("region", { name: "节点列表" });
const searchBox = () => screen.getByRole("searchbox", { name: "搜索节点" });

/** 键盘打开第 index 行的详情，返回详情区域。 */
function openByKeyboard(index: number) {
  const row = listRows()[index];
  row.focus();
  fireEvent.keyDown(row, { key: "Enter" });
  return detailRegion();
}

describe("焦点 · 键盘选择后到达详情", () => {
  it("Enter 选择后焦点进入详情区域", () => {
    render(<NodeListWorkspace state={readyState} />);

    openByKeyboard(0);

    expect(detailRegion()).toHaveFocus();
    expect(within(detailRegion()).getByText("aws-tokyo-01")).toBeVisible();
  });

  it("Space 选择同样把焦点交给详情", () => {
    render(<NodeListWorkspace state={readyState} />);
    const row = listRows()[1];
    row.focus();
    fireEvent.keyDown(row, { key: " " });

    expect(detailRegion()).toHaveFocus();
    expect(within(detailRegion()).getByText("gcp-singapore-01")).toBeVisible();
  });

  it("鼠标点击不搬动焦点，详情只是出现", () => {
    render(<NodeListWorkspace state={readyState} />);

    fireEvent.click(listRows()[0]);

    expect(detailRegion()).not.toHaveFocus();
  });

  it("详情区域只有一个可聚焦控件（关闭按钮），Tab 未被拦截", () => {
    render(<NodeListWorkspace state={readyState} />);
    const detail = openByKeyboard(0);

    expect(within(detail).getAllByRole("button")).toHaveLength(1);
    const closeButton = within(detail).getByRole("button", { name: "关闭详情" });

    const event = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
    closeButton.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });
});

describe("焦点 · 关闭与返回", () => {
  it("Escape 关闭后焦点回到仍存在的来源行", () => {
    render(<NodeListWorkspace state={readyState} />);
    const detail = openByKeyboard(2);

    fireEvent.keyDown(detail, { key: "Escape" });

    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(listRows()[2]).toHaveFocus();
  });

  it("关闭按钮把焦点送回来源行", () => {
    render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(1);

    fireEvent.click(screen.getByRole("button", { name: "关闭详情" }));

    expect(listRows()[1]).toHaveFocus();
  });

  it("来源行被本地筛选隐藏时，关闭后焦点回到搜索框", () => {
    render(<NodeListWorkspace state={readyState} />);
    const detail = openByKeyboard(0);

    fireEvent.change(searchBox(), { target: { value: "gcp" } });
    expect(listRows().map(row => row.querySelector("th span")?.textContent)).not.toContain("aws-tokyo-01");

    fireEvent.keyDown(detail, { key: "Escape" });

    expect(searchBox()).toHaveFocus();
  });
});

describe("焦点 · 详情被动消失", () => {
  it("选中节点从来源移除时，焦点落到可用搜索入口且不显示旧详情", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    rerender(<NodeListWorkspace state={buildReadyState([idleOnlineNodeDetail], nodeMetrics)} />);

    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(searchBox()).toHaveFocus();
  });

  it("ready → loading 时焦点落到列表区域（搜索框此时被禁用）", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    rerender(<NodeListWorkspace state={loadingState} />);

    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(searchBox()).toBeDisabled();
    expect(listRegion()).toHaveFocus();
  });

  it("ready → error 时同样落到列表区域，不引用旧详情", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    rerender(<NodeListWorkspace state={failedState} />);

    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(screen.getByRole("alert")).toHaveTextContent("节点列表加载失败");
    expect(listRegion()).toHaveFocus();
  });

  it("ready → 空列表时焦点回到搜索框", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    rerender(<NodeListWorkspace state={emptyState} />);

    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(searchBox()).toHaveFocus();
  });

  it("详情在同一 ID 的数据更新中保持焦点，不被重新抢走", () => {
    const build = (cpu: number) =>
      buildReadyState(
        [idleOnlineNodeDetail],
        new Map([
          [
            idleOnlineNodeDetail.nodeId,
            { cpuPercent: cpu, memoryPercent: 50, sampledAt: new Date(FIXED_NOW - 30_000).toISOString() },
          ],
        ]),
      );
    const { rerender } = render(<NodeListWorkspace state={build(10)} />);
    openByKeyboard(0);
    expect(detailRegion()).toHaveFocus();

    rerender(<NodeListWorkspace state={build(64)} />);

    expect(detailRegion()).toHaveFocus();
    expect(within(detailRegion()).getByText("64%")).toBeVisible();
  });
});

describe("焦点 · 重复选择同一节点（W04-R1）", () => {
  it("鼠标选中后用 Enter 激活同一节点，立即把焦点移入详情（不依赖重渲染）", () => {
    render(<NodeListWorkspace state={readyState} />);
    const row = listRows()[0];

    row.focus();
    fireEvent.click(row);
    expect(detailRegion()).not.toHaveFocus();

    // selectedId 未变化，React 不会提交更新；焦点必须当场转移。
    fireEvent.keyDown(row, { key: "Enter" });

    expect(detailRegion()).toHaveFocus();
    expect(within(detailRegion()).getByText("aws-tokyo-01")).toBeVisible();
  });

  it("键盘再次激活同一节点同样立即聚焦详情", () => {
    render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    const row = listRows()[0];
    row.focus();
    expect(row).toHaveFocus();

    fireEvent.keyDown(row, { key: " " });

    expect(detailRegion()).toHaveFocus();
  });

  it("重复选择后转向搜索框，后台刷新不会执行残留意图抢走焦点", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    const row = listRows()[0];

    row.focus();
    fireEvent.click(row);
    fireEvent.keyDown(row, { key: "Enter" });

    searchBox().focus();
    expect(searchBox()).toHaveFocus();

    rerender(<NodeListWorkspace state={buildReadyState(allNodeDetails, nodeMetrics)} />);

    expect(searchBox()).toHaveFocus();
    expect(detailRegion()).not.toHaveFocus();
  });

  it("关闭后数据刷新不会把焦点重新移入详情", () => {
    const { rerender } = render(<NodeListWorkspace state={readyState} />);
    openByKeyboard(0);

    fireEvent.keyDown(detailRegion(), { key: "Escape" });
    searchBox().focus();

    rerender(<NodeListWorkspace state={buildReadyState(allNodeDetails, nodeMetrics)} />);

    expect(searchBox()).toHaveFocus();
    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
  });
});

describe("焦点 · 非焦点路径不受影响", () => {
  it("鼠标选择后关闭，焦点回到来源行而不是搜索框", () => {
    render(<NodeListWorkspace state={readyState} />);
    fireEvent.click(listRows()[3]);

    fireEvent.click(screen.getByRole("button", { name: "关闭详情" }));

    expect(listRows()[3]).toHaveFocus();
    expect(searchBox()).not.toHaveFocus();
  });

  it("渲染与焦点操作期间不发起网络请求", () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      render(<NodeListWorkspace state={readyState} />);
      const detail = openByKeyboard(0);
      fireEvent.keyDown(detail, { key: "Escape" });
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
