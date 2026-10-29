import { describe, expect, it, vi, afterEach } from "vitest";
import { SSEParser, parseSSEBuffer, runEventStream } from "@/lib/sse/event-stream";

afterEach(() => vi.useRealTimers());

describe("SSEParser", () => {
  it("keeps fields and the partial line across chunks", () => {
    const parser = new SSEParser();
    expect(parser.push('id: 1\nevent: task\ndata: {"a":')).toEqual([]);
    expect(parser.push('1}\n\n')).toEqual([{ id: "1", event: "task", data: '{"a":1}', retry: null }]);
  });

  it("keeps the last event id for subsequent events", () => {
    const parser = new SSEParser();
    const events = parser.push("id: 9\ndata: a\n\ndata: b\n\n");
    expect(events.map(event => event.id)).toEqual(["9", "9"]);
  });

  it("joins multi-line data, ignores comments, supports CRLF and retry", () => {
    const parsed = parseSSEBuffer(": comment\r\nretry: 3000\r\ndata: a\r\ndata: b\r\n\r\n");
    expect(parsed.events).toEqual([{ id: null, event: "message", data: "a\nb", retry: 3000 }]);
  });

  it("waits for a possible CRLF split across chunks", () => {
    const parser = new SSEParser();
    expect(parser.push("data: x\r")).toEqual([]);
    expect(parser.push("\n\r\n")).toEqual([{ id: null, event: "message", data: "x", retry: null }]);
  });

  it("dispatches a trailing event only after the blank line", () => {
    expect(parseSSEBuffer("data: x").events).toEqual([]);
    expect(parseSSEBuffer("data: x\n\n").events).toHaveLength(1);
  });

  it("ignores ids containing NUL and non-numeric retry", () => {
    const parsed = parseSSEBuffer("id: a\0b\nretry: x\ndata: v\n\n");
    expect(parsed.events[0]).toMatchObject({ id: null, retry: null, data: "v" });
  });
});

function streamResponse(chunks: string[], status = 200): Response {
  const encoder = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
  return new Response(body, { status, headers: { "Content-Type": "text/event-stream" } });
}

describe("runEventStream", () => {
  it("reads events, reconnects with Last-Event-ID and stops on 403", async () => {
    const fetchImpl = vi
      .fn()
      .mockResolvedValueOnce(streamResponse(["id: 41\ndata: one\n\n", "id: 42\ndata: two\n\n"]))
      .mockResolvedValueOnce(new Response("forbidden", { status: 403 })) as unknown as typeof fetch;
    const events: string[] = [];
    const errors: boolean[] = [];
    const states: string[] = [];
    await runEventStream("/stream", {
      fetchImpl,
      signal: new AbortController().signal,
      minRetryMs: 1,
      maxRetryMs: 2,
      onEvent: event => events.push(event.data),
      onError: (_error, terminal) => errors.push(terminal),
      onState: state => states.push(state),
    });
    expect(events).toEqual(["one", "two"]);
    expect(errors).toEqual([true]);
    const calls = (fetchImpl as unknown as { mock: { calls: [string, RequestInit][] } }).mock.calls;
    expect(calls).toHaveLength(2);
    expect(new Headers(calls[1][1].headers).get("Last-Event-ID")).toBe("42");
    expect(states).toContain("open");
    expect(states.at(-1)).toBe("error");
  });

  it("treats 401 as terminal without a retry loop", async () => {
    const fetchImpl = vi.fn(async () => new Response("no", { status: 401 })) as unknown as typeof fetch;
    const onError = vi.fn();
    await runEventStream("/stream", { fetchImpl, signal: new AbortController().signal, onEvent: () => {}, onError });
    expect(onError).toHaveBeenCalledWith(expect.objectContaining({ kind: "auth" }), true);
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("treats 204 as a clean end", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 })) as unknown as typeof fetch;
    const states: string[] = [];
    await runEventStream("/stream", {
      fetchImpl,
      signal: new AbortController().signal,
      onEvent: () => {},
      onState: state => states.push(state),
    });
    expect(states.at(-1)).toBe("closed");
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it("stops when the signal aborts", async () => {
    const fetchImpl = vi.fn(async () => streamResponse(["data: x\n\n"])) as unknown as typeof fetch;
    const controller = new AbortController();
    const promise = runEventStream("/stream", {
      fetchImpl,
      signal: controller.signal,
      minRetryMs: 50,
      onEvent: () => controller.abort(),
    });
    await promise;
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });
});
