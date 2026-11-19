import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useResource } from "./use-resource";

it("waits for a slow polling request before scheduling the next poll", async () => {
  vi.useFakeTimers();
  let finishRefresh: (value: string) => void = () => undefined;
  const load = vi.fn((_signal: AbortSignal) => Promise.resolve("initial"))
    .mockImplementationOnce(() => Promise.resolve("initial"))
    .mockImplementationOnce(() => new Promise(resolve => { finishRefresh = resolve; }));
  const { result, unmount } = renderHook(() => useResource("poll", load, { refreshIntervalMs: 1000 }));
  try {
    await act(async () => {});
    await act(() => vi.advanceTimersByTimeAsync(1000));
    const signal = load.mock.calls[1][0];
    expect(result.current.state).toMatchObject({ status: "ready", refreshing: true });
    await act(() => vi.advanceTimersByTimeAsync(5000));
    expect(load).toHaveBeenCalledTimes(2);
    expect(signal.aborted).toBe(false);
    await act(async () => finishRefresh("updated"));
    expect(result.current.state).toMatchObject({ status: "ready", data: "updated", refreshing: false });
    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(load).toHaveBeenCalledTimes(3);
  } finally {
    unmount();
    vi.useRealTimers();
  }
});
