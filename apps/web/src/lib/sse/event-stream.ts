import { ApiError } from "@/lib/api/errors";

export interface SSEEvent {
  readonly id: string | null;
  readonly event: string;
  readonly data: string;
  readonly retry: number | null;
}

export interface SSEParseResult {
  readonly events: readonly SSEEvent[];
  readonly rest: string;
}

/**
 * 有状态增量 SSE 解析器。
 *
 * 字段必须跨 chunk 保留：网络可能在 `data:` 行之前切断，只缓存“半行”会丢失 id/event。
 * 空行分发、`:` 注释、多行 data 以 \n 连接、id 不含 NUL、retry 为整数毫秒。
 */
export class SSEParser {
  private buffer = "";
  private eventId: string | null = null;
  private eventName = "message";
  private data: string[] = [];
  private retryValue: number | null = null;
  private started = false;

  /** 服务端建议的重连时间（毫秒）；仅影响后续重连，不改变事件内容。 */
  get reconnectionTime(): number | null {
    return this.retryValue;
  }

  push(chunk: string): SSEEvent[] {
    if (!this.started) {
      this.started = true;
      if (chunk.charCodeAt(0) === 0xfeff) chunk = chunk.slice(1);
    }
    this.buffer += chunk;
    const events: SSEEvent[] = [];

    while (true) {
      const newline = this.buffer.search(/\r\n|\n|\r/);
      if (newline < 0) break;
      const isCrLf = this.buffer.startsWith("\r\n", newline);
      // A bare CR at the very end may be the first half of CRLF; wait for the next chunk.
      if (!isCrLf && this.buffer[newline] === "\r" && newline === this.buffer.length - 1) break;
      const line = this.buffer.slice(0, newline);
      this.buffer = this.buffer.slice(newline + (isCrLf ? 2 : 1));
      this.consumeLine(line, events);
    }

    return events;
  }

  private consumeLine(line: string, events: SSEEvent[]): void {
    if (line === "") {
      if (this.data.length > 0) {
        events.push({
          id: this.eventId,
          event: this.eventName,
          data: this.data.join("\n"),
          retry: this.retryValue,
        });
      }
      // last-event-id 与 retry 对后续事件/重连持续生效，只重置单个事件的字段。
      this.eventName = "message";
      this.data = [];
      return;
    }
    if (line.startsWith(":")) return;

    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    if (field === "data") this.data.push(value);
    else if (field === "event") this.eventName = value === "" ? "message" : value;
    else if (field === "id") {
      if (!value.includes("\0")) this.eventId = value;
    } else if (field === "retry") {
      if (/^\d+$/.test(value)) this.retryValue = Number(value);
    }
  }
}

/** 便捷函数：解析一个完整的、明确结束的缓冲区。跨 chunk 请使用 SSEParser。 */
export function parseSSEBuffer(buffer: string): SSEParseResult {
  const parser = new SSEParser();
  const events = parser.push(buffer);
  return { events, rest: "" };
}

export type StreamState = "idle" | "connecting" | "open" | "reconnecting" | "closed" | "error";

export interface EventStreamHandlers {
  readonly onEvent: (event: SSEEvent) => void;
  readonly onState?: (state: StreamState) => void;
  readonly onError?: (error: ApiError, terminal: boolean) => void;
}

export interface EventStreamOptions extends EventStreamHandlers {
  readonly signal: AbortSignal;
  readonly lastEventId?: string | null;
  readonly fetchImpl?: typeof fetch;
  readonly baseUrl?: string;
  readonly minRetryMs?: number;
  readonly maxRetryMs?: number;
}

const TERMINAL_STATUSES = new Set([401, 403, 404, 410]);

function retryDelay(attempt: number, min: number, max: number): number {
  const base = Math.min(max, min * 2 ** Math.min(attempt, 6));
  return Math.round(base * (0.7 + Math.random() * 0.6));
}

/**
 * 用 fetch 读取 SSE。相比 EventSource，这里可以读取状态码、主动中止、
 * 控制重连退避，并在 401/403/404 时终止，避免自动循环。
 */
export async function runEventStream(url: string, options: EventStreamOptions): Promise<void> {
  const fetchImpl = options.fetchImpl ?? ((input, init) => globalThis.fetch(input, init));
  const minRetry = options.minRetryMs ?? 1_000;
  const maxRetry = options.maxRetryMs ?? 30_000;
  let lastEventId = options.lastEventId ?? null;
  let attempt = 0;
  let terminal = false;

  const state = (next: StreamState) => {
    if (!options.signal.aborted) options.onState?.(next);
  };

  while (!options.signal.aborted && !terminal) {
    state(attempt === 0 ? "connecting" : "reconnecting");
    let response: Response;
    try {
      const headers = new Headers({ Accept: "text/event-stream" });
      if (lastEventId) headers.set("Last-Event-ID", lastEventId);
      response = await fetchImpl(url, {
        method: "GET",
        headers,
        credentials: "include",
        cache: "no-store",
        signal: options.signal,
      });
    } catch {
      if (options.signal.aborted) return;
      options.onError?.(new ApiError({ kind: "network", status: 0 }), false);
      state("reconnecting");
      await sleep(retryDelay(attempt++, minRetry, maxRetry), options.signal);
      continue;
    }

    if (response.status === 204) {
      state("closed");
      return;
    }

    if (!response.ok || !response.body) {
      const error = new ApiError({
        kind:
          response.status === 401
            ? "auth"
            : response.status === 403
              ? "forbidden"
              : response.status === 404
                ? "not-found"
                : response.status === 503
                  ? "unavailable"
                  : "server",
        status: response.status,
        retryAfterSeconds: null,
      });
      if (TERMINAL_STATUSES.has(response.status)) {
        terminal = true;
        state("error");
        options.onError?.(error, true);
        return;
      }
      options.onError?.(error, false);
      state("reconnecting");
      await sleep(retryDelay(attempt++, minRetry, maxRetry), options.signal);
      continue;
    }

    state("open");
    attempt = 0;
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const parser = new SSEParser();

    try {
      while (!options.signal.aborted) {
        const { done, value } = await reader.read();
        if (done) break;
        for (const event of parser.push(decoder.decode(value, { stream: true }))) {
          if (event.id) lastEventId = event.id;
          options.onEvent(event);
        }
      }
    } catch {
      if (options.signal.aborted) return;
    } finally {
      reader.releaseLock();
    }

    if (options.signal.aborted) return;
    // 服务端正常结束但未声明终态：按断线重连，保留 Last-Event-ID。
    state("reconnecting");
    await sleep(retryDelay(attempt++, minRetry, maxRetry), options.signal);
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise(resolve => {
    if (signal.aborted) {
      resolve();
      return;
    }
    const timer = setTimeout(done, ms);
    function done() {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    }
    signal.addEventListener("abort", done, { once: true });
  });
}
