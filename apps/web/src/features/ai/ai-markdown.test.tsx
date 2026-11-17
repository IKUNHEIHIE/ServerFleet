import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AIMarkdown } from "./ai-markdown";

describe("AI Markdown", () => {
  it("renders model headings, bold, lists, tables and literal code", () => {
    const { container } = render(<AIMarkdown>{"#### 检查结果\n\n**运行正常**\n\n- 节点在线\n\n| 项目 | 状态 |\n| --- | --- |\n| API | 正常 |\n\n"+String.fromCharCode(96).repeat(3)+"sh\necho '**literal**'\n"+String.fromCharCode(96).repeat(3)}</AIMarkdown>);
    expect(screen.getByRole("heading", { level: 4, name: "检查结果" })).toBeInTheDocument();
    expect(screen.getByText("运行正常").tagName).toBe("STRONG");
    expect(screen.getByRole("table")).toBeInTheDocument();
    expect(container.querySelector("pre code")).toHaveTextContent("**literal**");
  });

  it("does not execute model HTML or fetch remote images", () => {
    const { container } = render(<AIMarkdown>{"<script>alert(1)</script>\n\n![x](https://example.org/tracker)\n\n[link](javascript:alert%281%29)"}</AIMarkdown>);
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("img")).toBeNull();
    expect(screen.getByText("link")).not.toHaveAttribute("href", expect.stringContaining("javascript:"));
  });
});
