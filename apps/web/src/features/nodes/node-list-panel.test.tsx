import { createRef } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { NodeListPanel, type NodeListPanelHandle } from "./node-list-panel";
import {
  buildReadyState,
  emptyState,
  failedState,
  idleOnlineNodeDetail,
  loadingState,
  nodeMetrics,
  readyNodes,
  readyState,
} from "./test/fixtures";

function bodyRows() {
  const table = screen.getByRole("table", { name: "节点列表" });
  const body = table.querySelector("tbody");
  if (!body) throw new Error("tbody not found");
  return within(body).getAllByRole("row");
}

function rowNames() {
  return bodyRows().map(row => row.querySelector("th span")?.textContent ?? "");
}

function searchBox() {
  return screen.getByRole("searchbox", { name: "搜索节点" });
}

function statusSelect() {
  return screen.getByRole("combobox", { name: "状态筛选" });
}

function clearButton() {
  return screen.getByRole("button", { name: "清空筛选" });
}

const typeQuery = (value: string) => fireEvent.change(searchBox(), { target: { value } });
const chooseStatus = (value: string) => fireEvent.change(statusSelect(), { target: { value } });

describe("NodeListPanel · 控件与可访问名称", () => {
  it("搜索、筛选、清空都有可访问名称与命名区域", () => {
    render(<NodeListPanel state={readyState} />);

    expect(screen.getByRole("search", { name: "节点搜索与筛选" })).toBeVisible();
    expect(searchBox()).toBeVisible();
    expect(statusSelect()).toBeVisible();
    expect(clearButton()).toBeVisible();
    expect(screen.getByLabelText("搜索节点")).toBe(searchBox());
    expect(screen.getByLabelText("状态筛选")).toBe(statusSelect());
  });

  it("初始展示全部节点并给出总数，清空按钮不可用", () => {
    render(<NodeListPanel state={readyState} />);

    expect(bodyRows()).toHaveLength(readyNodes.length);
    expect(screen.getByText("共 8 个节点")).toBeVisible();
    expect(clearButton()).toBeDisabled();
    expect(screen.getByText(/筛选只作用于当前已加载的节点/)).toBeVisible();
  });
});

describe("NodeListPanel · 搜索", () => {
  it("按名称过滤并保持原有顺序", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("gcp");

    expect(rowNames()).toEqual(["gcp-singapore-01", "gcp-frankfurt-01", "gcp-osaka-01"]);
    expect(screen.getByText("显示 3 / 共 8 个节点")).toBeVisible();
  });

  it("忽略大小写与首尾空白", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("  AWS-TOKYO  ");

    expect(rowNames()).toEqual(["aws-tokyo-01"]);
  });

  it("按节点 ID 搜索", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("node-unknown-07");

    expect(rowNames()).toEqual(["node-unknown-07"]);
  });

  it("纯空白不改变结果，但允许一键清空", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("   ");

    expect(bodyRows()).toHaveLength(readyNodes.length);
    expect(clearButton()).toBeEnabled();
  });
});

describe("NodeListPanel · 状态筛选与组合", () => {
  it("单独按状态筛选", () => {
    render(<NodeListPanel state={readyState} />);
    chooseStatus("online");

    expect(bodyRows()).toHaveLength(4);
    expect(screen.getByText("显示 4 / 共 8 个节点")).toBeVisible();
  });

  it("搜索与状态同时生效", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("gcp");
    chooseStatus("online");

    expect(rowNames()).toEqual(["gcp-singapore-01", "gcp-frankfurt-01"]);
    expect(screen.getByText("显示 2 / 共 8 个节点")).toBeVisible();
  });

  it("清空后恢复全部节点与默认计数", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("gcp");
    chooseStatus("online");

    fireEvent.click(clearButton());

    expect(searchBox()).toHaveValue("");
    expect(statusSelect()).toHaveValue("all");
    expect(bodyRows()).toHaveLength(readyNodes.length);
    expect(screen.getByText("共 8 个节点")).toBeVisible();
    expect(clearButton()).toBeDisabled();
  });
});

describe("NodeListPanel · 空结果与空列表的区分", () => {
  it("筛选无匹配时说明是没有匹配，而不是系统里没有节点", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("zzz-no-such-node");

    expect(screen.getByText("没有匹配结果")).toBeVisible();
    expect(screen.getByText(/当前筛选条件下没有节点/)).toBeVisible();
    expect(screen.queryByText("暂无节点")).not.toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText("0 个节点")).toBeVisible();
    expect(screen.getByText("显示 0 / 共 8 个节点")).toBeVisible();
  });

  it("源列表为空时沿用“暂无节点”，不显示“没有匹配结果”", () => {
    render(<NodeListPanel state={emptyState} />);

    expect(screen.getByText("暂无节点")).toBeVisible();
    expect(screen.queryByText("没有匹配结果")).not.toBeInTheDocument();
    expect(screen.queryByText(/共 \d+ 个节点/)).not.toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("清空后从无匹配恢复到全部节点", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("zzz-no-such-node");
    expect(screen.getByText("没有匹配结果")).toBeVisible();

    fireEvent.click(clearButton());

    expect(screen.queryByText("没有匹配结果")).not.toBeInTheDocument();
    expect(bodyRows()).toHaveLength(readyNodes.length);
  });
});

describe("NodeListPanel · 数据更新", () => {
  it("外部数据变化时保留当前搜索词并重算结果", () => {
    const subset = buildReadyState([idleOnlineNodeDetail], nodeMetrics);
    const { rerender } = render(<NodeListPanel state={subset} />);

    typeQuery("gcp");
    expect(bodyRows()).toHaveLength(1);
    expect(screen.getByText("共 1 个节点")).toBeVisible();

    rerender(<NodeListPanel state={readyState} />);

    expect(searchBox()).toHaveValue("gcp");
    expect(bodyRows()).toHaveLength(3);
    expect(screen.getByText("显示 3 / 共 8 个节点")).toBeVisible();
  });

  it("筛选不会改动调用方传入的节点集合", () => {
    const before = readyNodes.map(node => node.id);
    render(<NodeListPanel state={readyState} />);
    typeQuery("gcp");
    chooseStatus("online");

    expect(readyNodes.map(node => node.id)).toEqual(before);
    expect(readyNodes).toHaveLength(8);
  });
});

describe("NodeListPanel · 加载与失败", () => {
  it("加载中禁用控件，不显示节点或计数", () => {
    render(<NodeListPanel state={loadingState} />);

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(searchBox()).toBeDisabled();
    expect(statusSelect()).toBeDisabled();
    expect(screen.queryByText(/个节点/)).not.toBeInTheDocument();
    expect(screen.queryByText(/显示 \d/)).not.toBeInTheDocument();
  });

  it("失败时保留告警与重试入口，但不显示成功节点或计数", () => {
    const onRetry = vi.fn();
    render(<NodeListPanel state={failedState} onRetry={onRetry} />);

    expect(screen.getByRole("alert")).toHaveTextContent("节点列表加载失败");
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.queryByRole("row")).not.toBeInTheDocument();
    expect(screen.queryByText(/个节点/)).not.toBeInTheDocument();
    expect(searchBox()).toBeDisabled();

    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });
});

describe("NodeListPanel · 已验收指标行为回归", () => {
  it("筛选后仍保留陈旧标记与可见采样时间", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("legacy-berlin");

    const row = bodyRows()[0];
    expect(within(row).getByText("55%")).toBeVisible();
    expect(within(row).getAllByText("陈旧 · 采样于 2026-10-29 02:41 UTC")).toHaveLength(2);
  });

  it("有效 0% 与 100% 仍照常显示", () => {
    render(<NodeListPanel state={readyState} />);

    typeQuery("gcp-singapore");
    expect(within(bodyRows()[0]).getByText("0%")).toBeVisible();

    typeQuery("gcp-frankfurt");
    expect(within(bodyRows()[0]).getByText("100%")).toBeVisible();
  });

  it("筛选到离线节点时仍不显示数字", () => {
    render(<NodeListPanel state={readyState} />);
    chooseStatus("offline");

    const row = bodyRows()[0];
    expect(within(row).getAllByText("无有效数据（节点不在线或状态未确认）")).toHaveLength(2);
    expect(within(row).queryByText(/%/)).not.toBeInTheDocument();
  });
});

describe("NodeListPanel · 键盘交互", () => {
  it("筛选后的行仍支持方向键与 Enter 激活", () => {
    const onNodeActivate = vi.fn();
    render(<NodeListPanel state={readyState} onNodeActivate={onNodeActivate} />);
    typeQuery("gcp");

    const rows = bodyRows();
    expect(rows).toHaveLength(3);

    rows[0].focus();
    fireEvent.keyDown(rows[0], { key: "ArrowDown" });
    expect(rows[1]).toHaveFocus();

    fireEvent.keyDown(rows[1], { key: "Enter" });
    expect(onNodeActivate).toHaveBeenCalledWith(
      readyNodes.find(node => node.id === "node-online-03"),
      "keyboard",
    );
  });

  it("从搜索框输入后可直接用 Tab 顺序进入列表行", () => {
    render(<NodeListPanel state={readyState} />);
    typeQuery("aws");

    const rows = bodyRows();
    rows[0].focus();
    expect(rows[0]).toHaveFocus();
    expect(rows[0]).toHaveAttribute("tabindex", "0");
  });
});

describe("NodeListPanel · 命令式焦点", () => {
  it("focusSearch 聚焦可用的搜索框；加载时控件禁用则返回 false", () => {
    const ref = createRef<NodeListPanelHandle>();
    const { rerender } = render(<NodeListPanel ref={ref} state={readyState} />);

    expect(ref.current?.focusSearch()).toBe(true);
    expect(searchBox()).toHaveFocus();

    rerender(<NodeListPanel ref={ref} state={loadingState} />);
    expect(ref.current?.focusSearch()).toBe(false);
    expect(searchBox()).toBeDisabled();
  });
});

describe("NodeListPanel · 隔离性", () => {
  it("渲染与筛选期间不发起网络请求", () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      render(<NodeListPanel state={readyState} />);
      typeQuery("gcp");
      chooseStatus("online");
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
