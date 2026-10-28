import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "@/App";
import { Badge } from "@/components/ui/badge";
import { offlineNode } from "@/test/fixtures";

describe("development shell", () => {
  it("exposes a clear empty state without an execution control", () => {
    render(<App />);
    expect(screen.getByRole("heading", { level: 1, name: "Server Fleet" })).toBeVisible();
    expect(screen.getByText(/节点接入、任务执行与插件功能尚未开放/)).toBeVisible();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
  it("allows native attributes and style overrides on the base component", () => {
    render(<Badge aria-label="stage" variant="outline" className="text-lg">候选</Badge>);
    expect(screen.getByLabelText("stage")).toHaveClass("text-lg");
    expect(screen.getByLabelText("stage")).not.toHaveClass("text-xs");
  });
  it("blocks unmocked API calls and preserves missing telemetry", async () => {
    await expect(fetch("https://controller.example.invalid/api/v1/nodes")).rejects.toThrow("Network disabled");
    expect(offlineNode.cpu).toBeNull();
  });
});
