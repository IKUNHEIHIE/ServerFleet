import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NodeList } from "./node-list";
import {
  emptyState,
  failedState,
  loadingState,
  offlineNodeDetail,
  readyNodes,
  readyState,
} from "./test/fixtures";

/** 表体行（排除表头行），避免 getAllByRole("row") 把 <thead> 也算进来。 */
function bodyRows() {
  const table = screen.getByRole("table", { name: "节点列表" });
  const body = table.querySelector("tbody");
  if (!body) throw new Error("tbody not found");
  return within(body).getAllByRole("row");
}

describe("NodeList · 加载状态", () => {
  it("只显示加载提示，不显示空列表或表格", () => {
    render(<NodeList state={loadingState} />);

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.getByText("加载中")).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText("暂无节点")).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "节点列表" })).toHaveAttribute("aria-busy", "true");
  });
});

describe("NodeList · 空列表", () => {
  it("说明空列表原因，且不渲染表格或加载提示", () => {
    render(<NodeList state={emptyState} />);

    expect(screen.getByText("暂无节点")).toBeVisible();
    expect(screen.getByText("0 个节点")).toBeVisible();
    expect(screen.getByText(/已授权节点接入后会显示在这里/)).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });
});

describe("NodeList · 失败状态", () => {
  it("用 alert 报错，且不出现任何成功迹象", () => {
    render(<NodeList state={failedState} onRetry={vi.fn()} />);

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("节点列表加载失败");
    expect(alert).toHaveTextContent("上游返回 503，列表未加载");
    expect(screen.getByText("无法加载")).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByText("在线")).not.toBeInTheDocument();
    expect(screen.queryByText("加载中")).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "节点列表" })).toHaveAttribute("aria-busy", "false");
  });

  it("重试按钮只在提供回调时出现并触发一次", () => {
    const onRetry = vi.fn();
    const { rerender } = render(<NodeList state={failedState} />);
    expect(screen.queryByRole("button", { name: "重新加载" })).not.toBeInTheDocument();

    rerender(<NodeList state={failedState} onRetry={onRetry} />);
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("没有错误详情时给出中性提示，不伪造结果", () => {
    render(<NodeList state={{ status: "error" }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("未能从 Controller 读取节点列表");
  });
});

describe("NodeList · 就绪状态", () => {
  it("表格有可访问名称、7 列表头和逐行状态标签", () => {
    render(<NodeList state={readyState} />);

    const table = screen.getByRole("table", { name: "节点列表" });
    expect(within(table).getAllByRole("columnheader")).toHaveLength(7);
    expect(bodyRows()).toHaveLength(readyNodes.length);
    expect(screen.getByText("2 个在线 · 4 个未在线")).toBeVisible();
    expect(within(table).getAllByText("在线")).toHaveLength(2);
    expect(within(table).getByText("离线")).toBeVisible();
    expect(within(table).getByText("已暂停")).toBeVisible();
    expect(within(table).getByText("已吊销")).toBeVisible();
  });

  it("行标题与单元格共用一个列表标题作为可访问名称", () => {
    render(<NodeList state={readyState} title="生产节点" description="只读视图" />);

    expect(screen.getByRole("table", { name: "生产节点" })).toBeVisible();
    expect(screen.getByRole("heading", { level: 2, name: "生产节点" })).toBeVisible();
    expect(screen.getByText("只读视图")).toBeVisible();
  });

  it("标题为空时回退到默认名称，避免无名区域", () => {
    render(<NodeList state={readyState} title="   " />);
    expect(screen.getByRole("region", { name: "节点列表" })).toBeVisible();
  });
});

describe("NodeList · 离线指标缺失", () => {
  it("离线节点即使上游带 cpu=0 也显示无有效数据，不显示 0%", () => {
    render(<NodeList state={readyState} />);

    const table = screen.getByRole("table", { name: "节点列表" });
    const offlineRow = within(table).getByRole("row", { name: new RegExp(offlineNodeDetail.displayName) });

    expect(within(offlineRow).getAllByText("无有效数据")).toHaveLength(2);
    expect(within(offlineRow).queryByText(/0%/)).not.toBeInTheDocument();
    expect(within(offlineRow).queryByText(/%/)).not.toBeInTheDocument();
  });

  it("已暂停节点的读数同样按无有效数据处理", () => {
    render(<NodeList state={readyState} />);
    const table = screen.getByRole("table", { name: "节点列表" });
    const row = within(table).getByRole("row", { name: /gcp-frankfurt-01/ });

    expect(within(row).getAllByText("无有效数据")).toHaveLength(2);
  });

  it("真实 0% 读数仍照常显示，证明缺失与零值没有混同", () => {
    render(<NodeList state={readyState} />);
    const table = screen.getByRole("table", { name: "节点列表" });
    const idleRow = within(table).getByRole("row", { name: /gcp-singapore-01/ });

    expect(within(idleRow).getByText("0%")).toBeVisible();
    expect(within(idleRow).queryByText("无有效数据")).not.toBeInTheDocument();
  });

  it("在线节点显示四舍五入后的百分比", () => {
    render(<NodeList state={readyState} />);
    const table = screen.getByRole("table", { name: "节点列表" });
    const row = within(table).getByRole("row", { name: /aws-tokyo-01/ });

    expect(within(row).getByText("18%")).toBeVisible();
    expect(within(row).getByText("46%")).toBeVisible();
  });

  it("缺失的元数据也标注为未知，而不是留空", () => {
    render(<NodeList state={readyState} />);
    const table = screen.getByRole("table", { name: "节点列表" });
    const unknownRow = within(table).getByRole("row", { name: /node-unknown-07/ });

    expect(within(unknownRow).getAllByText("未知").length).toBeGreaterThanOrEqual(3);
    expect(within(unknownRow).getAllByText("无有效数据")).toHaveLength(2);
  });
});

describe("NodeList · 键盘交互", () => {
  it("roving tabindex 只让一行进入 Tab 序列", () => {
    render(<NodeList state={readyState} onNodeActivate={vi.fn()} />);
    const rows = bodyRows();

    expect(rows[0]).toHaveAttribute("tabindex", "0");
    for (const row of rows.slice(1)) expect(row).toHaveAttribute("tabindex", "-1");
  });

  it("方向键在行间移动焦点并同步 tabindex", () => {
    render(<NodeList state={readyState} onNodeActivate={vi.fn()} />);
    const rows = bodyRows();

    rows[0].focus();
    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    expect(rows[1]).toHaveFocus();
    expect(rows[1]).toHaveAttribute("tabindex", "0");
    expect(rows[0]).toHaveAttribute("tabindex", "-1");

    fireEvent.keyDown(rows[1], { key: "ArrowUp" });
    expect(rows[0]).toHaveFocus();

    fireEvent.keyDown(rows[0], { key: "ArrowUp" });
    expect(rows[0]).toHaveFocus();

    fireEvent.keyDown(rows[0], { key: "End" });
    expect(rows[rows.length - 1]).toHaveFocus();

    fireEvent.keyDown(rows[rows.length - 1], { key: "ArrowDown" });
    expect(rows[rows.length - 1]).toHaveFocus();

    fireEvent.keyDown(rows[rows.length - 1], { key: "Home" });
    expect(rows[0]).toHaveFocus();
  });

  it("Enter 与 Space 激活当前行，且不误触其他行", () => {
    const onNodeActivate = vi.fn();
    render(<NodeList state={readyState} onNodeActivate={onNodeActivate} />);
    const rows = bodyRows();

    rows[0].focus();
    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    fireEvent.keyDown(rows[1], { key: "Enter" });
    expect(onNodeActivate).toHaveBeenCalledTimes(1);
    expect(onNodeActivate).toHaveBeenCalledWith(readyNodes[1]);

    fireEvent.keyDown(rows[1], { key: " " });
    expect(onNodeActivate).toHaveBeenCalledTimes(2);
    expect(onNodeActivate).toHaveBeenLastCalledWith(readyNodes[1]);
  });

  it("未提供激活回调时，Enter 不产生副作用也不被吞掉默认行为", () => {
    render(<NodeList state={readyState} />);
    const rows = bodyRows();

    const event = new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true });
    rows[0].dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it("鼠标点击等价于激活该行", () => {
    const onNodeActivate = vi.fn();
    render(<NodeList state={readyState} onNodeActivate={onNodeActivate} />);
    const rows = bodyRows();

    fireEvent.click(rows[2]);
    expect(onNodeActivate).toHaveBeenCalledWith(readyNodes[2]);
  });
});

describe("NodeList · 隔离性", () => {
  it("渲染期间不发起网络请求", () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      render(<NodeList state={readyState} onNodeActivate={vi.fn()} />);
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
