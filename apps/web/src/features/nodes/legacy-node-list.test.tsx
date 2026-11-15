import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { LegacyNodeList } from "./legacy-node-list";
import { adaptLegacyNodes } from "./legacy-adapter";
import type { LegacyNodeRow } from "./legacy-types";

function rows(): LegacyNodeRow[] {
  const result = adaptLegacyNodes({
    items: [
      { id: "n-tokyo", name: "Tokyo", online: true, region: "ap-northeast", provider: "aws", cpuPercent: 0, memoryPercent: 100 },
      { id: "n-frank", name: "Frankfurt", online: false, region: "", provider: "hetzner", cpuPercent: 55, memoryPercent: 77 },
      { id: "n-empty", name: "  ", online: true, cpuPercent: null },
    ],
  });
  if (!result.ok) throw new Error("fixture invalid");
  return [...result.rows];
}

describe("LegacyNodeList", () => {
  it("renders server order, counts and accessible detail links without clickable rows", () => {
    render(<LegacyNodeList rows={rows()} />);
    const table = screen.getByRole("table", { name: "兼容旧节点列表" });
    const bodyRows = within(table).getAllByRole("row").slice(1);
    expect(bodyRows).toHaveLength(3);
    expect(within(bodyRows[0]).getByText("Tokyo")).toBeVisible();
    expect(within(bodyRows[1]).getByText("Frankfurt")).toBeVisible();
    expect(within(bodyRows[2]).getByText("名称为空，显示节点 ID")).toBeVisible();
    expect(within(table).getAllByRole("button", { name: /更多操作$/ })).toHaveLength(3);
    expect(within(table).getAllByRole("link")).toHaveLength(3);
    expect(within(bodyRows[0]).getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/n-tokyo");
    expect(within(bodyRows[2]).getByRole("link", { name: "n-empty" })).toHaveAttribute("href", "#/nodes/n-empty");
    for (const row of bodyRows) expect(row).not.toHaveAttribute("tabindex");
    expect(screen.getByRole("status")).toHaveTextContent("共 3 个节点");
  });

  it("opens row actions without changing the name link and hides writes without permission", () => {
    render(<LegacyNodeList rows={[rows()[0]]} />);
    const table = screen.getByRole("table", { name: "兼容旧节点列表" });
    fireEvent.click(within(table).getByRole("button", { name: "Tokyo 更多操作" }));
    expect(screen.getByRole("menu", { name: "Tokyo 节点操作" })).toBeVisible();
    expect(screen.getByText("无可用操作")).toBeVisible();
    expect(within(table).getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/n-tokyo");
  });

  it("offers edit and delete in the row menu only when authorized", () => {
    render(<LegacyNodeList rows={[rows()[0]]} canEdit canDelete />);
    fireEvent.click(screen.getByRole("button", { name: "Tokyo 更多操作" }));
    expect(screen.getByRole("menuitem", { name: "编辑" })).toBeVisible();
    expect(screen.getByRole("menuitem", { name: "删除" })).toBeVisible();
  });

  it("offers edit and delete in the row menu only when authorized", () => {
    render(<LegacyNodeList rows={[rows()[0]]} canEdit canDelete />);
    fireEvent.click(screen.getByRole("button", { name: "Tokyo 更多操作" }));
    expect(screen.getByRole("menuitem", { name: "编辑" })).toBeVisible();
    expect(screen.getByRole("menuitem", { name: "删除" })).toBeVisible();
  });

  it("encodes the node id in the detail link", () => {
    render(<LegacyNodeList rows={[{ ...rows()[0], id: "node/a?#" }]} />);
    expect(screen.getByRole("link", { name: "Tokyo" })).toHaveAttribute("href", "#/nodes/node%2Fa%3F%23");
  });

  it("shows real 0/100 with the missing-sampling-time note, and no percentage for offline", () => {
    render(<LegacyNodeList rows={rows()} />);
    expect(screen.getAllByText("0.00%")).toHaveLength(1);
    expect(screen.getAllByText("100.00%")).toHaveLength(1);
    expect(screen.getAllByText("无采样时间")).toHaveLength(2);
    expect(screen.getAllByText("离线，无当前读数")).toHaveLength(3);
    expect(screen.queryByText("55%")).not.toBeInTheDocument();
  });

  it("searches only loaded name/id and distinguishes empty server set from no match", () => {
    const { rerender } = render(<LegacyNodeList rows={rows()} />);
    const search = screen.getByLabelText("搜索名称或节点 ID");
    fireEvent.change(search, { target: { value: "tok" } });
    expect(screen.getByRole("status")).toHaveTextContent("显示 1 / 共 3 个节点");
    expect(screen.queryByText("Frankfurt")).not.toBeInTheDocument();

    fireEvent.change(search, { target: { value: "zzz" } });
    expect(screen.getByText("没有匹配结果")).toBeVisible();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "清空搜索" }));
    expect(screen.getByRole("status")).toHaveTextContent("共 3 个节点");

    rerender(<LegacyNodeList rows={[]} />);
    expect(screen.getByText("暂无可显示节点")).toBeVisible();
    expect(screen.queryByText("没有匹配结果")).not.toBeInTheDocument();
    expect(screen.getByLabelText("搜索名称或节点 ID")).toBeDisabled();
  });

  it("keeps the query but recomputes counts when the server set changes", () => {
    const { rerender } = render(<LegacyNodeList rows={rows()} />);
    fireEvent.change(screen.getByLabelText("搜索名称或节点 ID"), { target: { value: "n-" } });
    expect(screen.getByRole("status")).toHaveTextContent("共 3 个节点");
    rerender(<LegacyNodeList rows={rows().slice(0, 1)} />);
    expect(screen.getByRole("status")).toHaveTextContent("共 1 个节点");
    expect(screen.getByText("Tokyo")).toBeVisible();
  });
});
