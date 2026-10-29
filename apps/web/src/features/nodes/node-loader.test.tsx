import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { StrictMode, useLayoutEffect, useRef, type ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { LOAD_FAILURE_MESSAGE, NodeLoader, type NodeSource } from "./node-loader";
import type { NodeListItemView, NodeListState } from "./types";
import { buildReadyState, idleOnlineNodeDetail, nodeMetrics, readyNodes } from "./test/fixtures";

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
};

type Deferred = ReturnType<typeof deferred<readonly NodeListItemView[]>>;

interface Harness {
  readonly source: NodeSource;
  readonly calls: Array<{ signal: AbortSignal; deferred: Deferred }>;
  readonly load: ReturnType<typeof vi.fn>;
}

/** 可控来源：每次 load 生成一个独立 Promise，测试自行决定何时、以何值 settle。 */
function makeSource(key: string): Harness {
  const calls: Array<{ signal: AbortSignal; deferred: Deferred }> = [];
  const load = vi.fn(({ signal }: { signal: AbortSignal }) => {
    const pending = deferred<readonly NodeListItemView[]>();
    calls.push({ signal, deferred: pending });
    return pending.promise;
  });
  return { source: { key, load }, calls, load };
}

const nodesOf = (state: NodeListState) => (state.status === "ready" ? state.nodes : []);
const nodesB = nodesOf(buildReadyState([idleOnlineNodeDetail], nodeMetrics));

const settle = async (run: () => void) => {
  await act(async () => {
    run();
  });
};

const bodyRows = () => {
  const table = screen.getByRole("table", { name: "节点列表" });
  const body = table.querySelector("tbody");
  if (!body) throw new Error("tbody not found");
  return within(body).getAllByRole("row");
};

/** 在布局阶段记录每次提交后的 DOM 文本：可证明“首个提交”就没有旧数据（早于被动 effect）。 */
function CommitProbe({ commits, children }: { readonly commits: string[]; readonly children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    commits.push(ref.current?.textContent ?? "");
  });
  return <div ref={ref}>{children}</div>;
}

describe("NodeLoader · 加载与成功", () => {
  it("首次渲染就是 loading，不显示表格或计数", () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByText(/个节点/)).toBeNull();
    expect(a.load).toHaveBeenCalledTimes(1);
  });

  it("成功后用现有列表展示节点与计数", async () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(screen.getByRole("table", { name: "节点列表" })).toBeVisible();
    expect(screen.getByText("共 8 个节点")).toBeVisible();
    expect(bodyRows()).toHaveLength(8);
  });

  it("同一来源重复渲染不会重复请求（无轮询/无自动重试）", async () => {
    const a = makeSource("scope-a");
    const { rerender } = render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    rerender(<NodeLoader source={{ key: "scope-a", load: a.source.load }} />);
    rerender(<NodeLoader source={{ key: "scope-a", load: a.source.load }} />);

    expect(a.load).toHaveBeenCalledTimes(1);
  });
});

describe("NodeLoader · 安全失败与重试", () => {
  it("失败只显示固定安全提示，不泄露异常原文", async () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() =>
      a.calls[0].deferred.reject(new Error("ENOENT /srv/secret/config token=abc123")),
    );

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent(LOAD_FAILURE_MESSAGE);
    expect(document.body.textContent).not.toContain("secret");
    expect(document.body.textContent).not.toContain("ENOENT");
    expect(document.body.textContent).not.toContain("abc123");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("重试回到 loading，且不保留成功节点、计数或详情", async () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.reject(new Error("boom")));

    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByText(/个节点/)).toBeNull();
    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(a.load).toHaveBeenCalledTimes(2);

    await settle(() => a.calls[1].deferred.resolve(readyNodes));
    expect(screen.getByRole("table", { name: "节点列表" })).toBeVisible();
  });

  it("重试不会继承上一次失败的重试按钮状态", async () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.reject(new Error("boom")));
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    await settle(() => a.calls[1].deferred.reject(new Error("boom again")));

    expect(screen.getByRole("button", { name: "重新加载" })).toBeVisible();
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
  });
});

describe("NodeLoader · 来源/作用域切换", () => {
  it("切换后的首个提交就隐藏旧节点、计数与详情", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const commits: string[] = [];
    const { rerender } = render(
      <CommitProbe commits={commits}>
        <NodeLoader source={a.source} />
      </CommitProbe>,
    );
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    // 打开详情，随后切换来源。
    fireEvent.click(bodyRows()[0]);
    expect(screen.getByRole("region", { name: "节点详情" })).toBeVisible();
    expect(
      within(screen.getByRole("region", { name: "节点详情" })).getByText("aws-tokyo-01"),
    ).toBeVisible();

    commits.length = 0;
    rerender(
      <CommitProbe commits={commits}>
        <NodeLoader source={b.source} />
      </CommitProbe>,
    );

    // 布局阶段（早于被动 effect）的第一次提交：已经是 loading，没有任何旧内容。
    expect(commits[0]).toContain("正在加载节点");
    expect(commits[0]).not.toContain("aws-tokyo-01");
    expect(commits[0]).not.toContain("共 8 个节点");
    expect(screen.queryByRole("region", { name: "节点详情" })).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("切换来源后不继承上一个来源的筛选条件", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.resolve(readyNodes));
    fireEvent.change(screen.getByRole("searchbox", { name: "搜索节点" }), { target: { value: "gcp" } });
    expect(bodyRows()).toHaveLength(3);

    rerender(<NodeLoader source={b.source} />);
    await settle(() => b.calls[0].deferred.resolve(nodesB));

    expect(screen.getByRole("searchbox", { name: "搜索节点" })).toHaveValue("");
    expect(bodyRows()).toHaveLength(nodesB.length);
  });
});

describe("NodeLoader · 竞态：旧响应不得覆盖新结果", () => {
  it("先发后到：旧来源迟到的成功被丢弃", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={b.source} />);

    await settle(() => b.calls[0].deferred.resolve(nodesB));
    expect(bodyRows()).toHaveLength(nodesB.length);

    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(bodyRows()).toHaveLength(nodesB.length);
    expect(screen.queryByText("aws-tokyo-01")).toBeNull();
    expect(a.calls[0].signal.aborted).toBe(true);
    expect(b.calls[0].signal.aborted).toBe(false);
  });

  it("旧来源迟到的失败不覆盖新结果", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={b.source} />);
    await settle(() => b.calls[0].deferred.resolve(nodesB));

    await settle(() => a.calls[0].deferred.reject(new Error("late failure")));

    expect(screen.queryByRole("alert")).toBeNull();
    expect(bodyRows()).toHaveLength(nodesB.length);
  });

  it("来源忽略 AbortSignal 时，迟到的成功同样被序号丢弃", async () => {
    const calls: Deferred[] = [];
    const ignoringSource: NodeSource = {
      key: "scope-stubborn",
      load: () => {
        const pending = deferred<readonly NodeListItemView[]>();
        calls.push(pending);
        return pending.promise; // 故意不监听 signal
      },
    };
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={ignoringSource} />);
    rerender(<NodeLoader source={b.source} />);
    await settle(() => b.calls[0].deferred.resolve(nodesB));

    await settle(() => calls[0].resolve(readyNodes));

    expect(bodyRows()).toHaveLength(nodesB.length);
    expect(screen.queryByText("aws-tokyo-01")).toBeNull();
  });

  it("过期请求仍 pending：切到 B 后旧 A 迟到成功不覆盖 B 的 loading", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={b.source} />);
    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");

    // B 仍 pending，A 这时才成功：不得把 A 的节点显示成本轮结果。
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("B 失败后重试仍 pending 时，旧 A 迟到成功不得结束 loading", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={b.source} />);
    await settle(() => b.calls[0].deferred.reject(new Error("b failed")));

    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));
    expect(b.load).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");

    // 旧 A 从未 settle，这时才成功：重试请求仍是 pending，不能被它覆盖。
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("B 失败后重试仍 pending 时，旧 A 迟到失败不得变成错误态", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={b.source} />);
    await settle(() => b.calls[0].deferred.reject(new Error("b failed")));
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));

    await settle(() => a.calls[0].deferred.reject(new Error("a failed late")));

    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
  });

  it("同一个 Promise 已 reject 后再 resolve 不构成迟到成功（说明性断言）", async () => {
    // 这是原用例的缺陷：Promise 一旦 reject，再调用 resolve 不产生新的回调，
    // 因此不能用来证明重试竞态。真正的迟到成功由上面“仍 pending 的旧 A”用例覆盖。
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.reject(new Error("boom")));
    fireEvent.click(screen.getByRole("button", { name: "重新加载" }));

    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).toBeNull();
  });
});

describe("NodeLoader · 来源失效与恢复（W05-R1）", () => {
  it("A 成功 → source=null → 恢复 A：首个提交已是 loading，不显示上一世代数据", async () => {
    const a = makeSource("scope-a");
    const commits: string[] = [];
    const view = (next: NodeSource | null) => (
      <CommitProbe commits={commits}>
        <NodeLoader source={next} />
      </CommitProbe>
    );
    const { rerender } = render(view(a.source));
    await settle(() => a.calls[0].deferred.resolve(readyNodes));
    expect(bodyRows()).toHaveLength(8);

    rerender(view(null));
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
    expect(screen.queryByRole("table")).toBeNull();

    commits.length = 0;
    rerender(view(a.source));

    expect(a.load).toHaveBeenCalledTimes(2);
    expect(commits[0]).toContain("正在加载节点");
    expect(commits[0]).not.toContain("aws-tokyo-01");
    expect(commits[0]).not.toContain("共 8 个节点");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("失效前发出、恢复后才成功的旧世代请求不能覆盖新世代 loading", async () => {
    const a = makeSource("scope-a");
    const { rerender } = render(<NodeLoader source={a.source} />);
    rerender(<NodeLoader source={null} />);
    rerender(<NodeLoader source={a.source} />);
    expect(a.load).toHaveBeenCalledTimes(2);

    // 旧世代（第一次 load）迟到成功
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    expect(screen.getByRole("status")).toHaveTextContent("正在加载节点");
    expect(screen.queryByRole("table")).toBeNull();

    // 新世代成功后才显示
    await settle(() => a.calls[1].deferred.resolve(nodesB));
    expect(bodyRows()).toHaveLength(nodesB.length);
  });

  it("精确 key 不同（首尾空白）不被 trim 合并，且该 key 被拒绝为无效来源", async () => {
    const a = makeSource("scope-a");
    const padded = makeSource(" scope-a ");
    const commits: string[] = [];
    const view = (next: NodeSource) => (
      <CommitProbe commits={commits}>
        <NodeLoader source={next} />
      </CommitProbe>
    );
    const { rerender } = render(view(a.source));
    await settle(() => a.calls[0].deferred.resolve(readyNodes));
    expect(bodyRows()).toHaveLength(8);

    commits.length = 0;
    rerender(view(padded.source));

    // 首个提交就不能继续展示旧作用域的节点/计数
    expect(commits[0]).not.toContain("aws-tokyo-01");
    expect(commits[0]).not.toContain("共 8 个节点");
    // 带首尾空白的标识按无效来源安全失败，不会静默启动新请求
    expect(padded.load).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("StrictMode 下仍能完成首次加载，不会卡在 loading", async () => {
    const a = makeSource("scope-a");
    render(
      <StrictMode>
        <NodeLoader source={a.source} />
      </StrictMode>,
    );

    // StrictMode 会重复挂载 effect：第一个请求被 cleanup abort，保留最后一个。
    await settle(() => a.calls[a.calls.length - 1].deferred.resolve(readyNodes));

    expect(bodyRows()).toHaveLength(8);
  });

  it("不同精确 key 各自独立加载", async () => {
    const a = makeSource("scope-a");
    const b = makeSource("scope-b");
    const { rerender } = render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    rerender(<NodeLoader source={b.source} />);

    expect(b.load).toHaveBeenCalledTimes(1);
    await settle(() => b.calls[0].deferred.resolve(nodesB));
    expect(bodyRows()).toHaveLength(nodesB.length);
  });
});

describe("NodeLoader · 卸载与无效来源", () => {
  it("卸载时 abort 本容器的请求，迟到响应不再更新状态", async () => {
    const a = makeSource("scope-a");
    const { unmount } = render(<NodeLoader source={a.source} />);
    expect(a.calls[0].signal.aborted).toBe(false);

    unmount();

    expect(a.calls[0].signal.aborted).toBe(true);
    await act(async () => {
      a.calls[0].deferred.resolve(readyNodes);
    });
  });

  it("缺少来源时按安全失败处理，不发起请求也不提供假重试", () => {
    render(<NodeLoader source={null} />);

    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
    expect(screen.queryByRole("button", { name: "重新加载" })).toBeNull();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("空白来源标识视为无效来源", () => {
    const load = vi.fn(() => Promise.resolve(readyNodes));
    render(<NodeLoader source={{ key: "   ", load }} />);

    expect(load).not.toHaveBeenCalled();
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
  });

  it("来源同步抛错或返回非数组都转为安全失败", async () => {
    const throwing: NodeSource = {
      key: "scope-throw",
      load: () => {
        throw new Error("sync /path token=zzz");
      },
    };
    const { unmount } = render(<NodeLoader source={throwing} />);
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
    expect(document.body.textContent).not.toContain("token");
    unmount();

    const wrongShape = makeSource("scope-shape");
    render(<NodeLoader source={wrongShape.source} />);
    await settle(() => wrongShape.calls[0].deferred.resolve("not-an-array" as unknown as readonly NodeListItemView[]));
    expect(screen.getByRole("alert")).toHaveTextContent(LOAD_FAILURE_MESSAGE);
  });
});

describe("NodeLoader · 既有行为回归", () => {
  it("成功渲染后筛选、指标质量与键盘焦点仍然正常", async () => {
    const a = makeSource("scope-a");
    render(<NodeLoader source={a.source} />);
    await settle(() => a.calls[0].deferred.resolve(readyNodes));

    fireEvent.change(screen.getByRole("searchbox", { name: "搜索节点" }), { target: { value: "legacy-berlin" } });
    const rows = bodyRows();
    expect(rows).toHaveLength(1);
    expect(within(rows[0]).getAllByText("陈旧 · 采样于 2026-10-29 02:41 UTC")).toHaveLength(2);

    fireEvent.change(screen.getByRole("searchbox", { name: "搜索节点" }), { target: { value: "gcp-singapore" } });
    expect(within(bodyRows()[0]).getByText("0%")).toBeVisible();

    rows[0].focus();
    fireEvent.keyDown(bodyRows()[0], { key: "Enter" });
    expect(screen.getByRole("region", { name: "节点详情" })).toHaveFocus();
  });

  it("整个加载流程不发起网络请求", async () => {
    const fetchSpy = vi.fn(() => Promise.reject(new Error("blocked in tests")));
    const original = globalThis.fetch;
    vi.stubGlobal("fetch", fetchSpy);
    try {
      const a = makeSource("scope-a");
      render(<NodeLoader source={a.source} />);
      await settle(() => a.calls[0].deferred.resolve(readyNodes));
      fireEvent.change(screen.getByRole("searchbox", { name: "搜索节点" }), { target: { value: "gcp" } });
      expect(fetchSpy).not.toHaveBeenCalled();
    } finally {
      vi.stubGlobal("fetch", original);
    }
  });
});
