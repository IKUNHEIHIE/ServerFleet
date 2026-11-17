import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgentThoughts } from "./agent-run-panel";

describe("model thoughts and plans", () => {
  it("shows API reasoning, summary and plan as separate content", () => {
    render(<AgentThoughts step={{
      number: 1, state: "thinking", nodeIds: [], startedAt: "2026-11-18T01:00:00Z",
      reasoningContent: "### 分析过程\n\n**详细模型输出**",
      reasoningSummary: "简短概要", plan: "检查节点",
    }} />);
    expect(screen.getByText("思考过程 · 模型输出")).toBeInTheDocument();
    expect(screen.getByText("思考摘要")).toBeInTheDocument();
    expect(screen.getByText("执行计划")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "分析过程" })).toBeInTheDocument();
    expect(screen.getByText("详细模型输出").tagName).toBe("STRONG");
    expect(screen.getByText("简短概要")).toBeInTheDocument();
    expect(screen.getByText("检查节点")).toBeInTheDocument();
  });

  it("does not relabel a short plan as model reasoning", () => {
    render(<AgentThoughts step={{ number: 1, state: "completed", nodeIds: [], startedAt: "2026-11-18T01:00:00Z", plan: "我来检查该节点的运行情况。" }} />);
    expect(screen.getByText("执行计划")).toBeInTheDocument();
    expect(screen.queryByText("思考过程 · 模型输出")).not.toBeInTheDocument();
  });
});
