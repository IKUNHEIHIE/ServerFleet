import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "@/lib/api/errors";
import { runEventStream, type SSEEvent, type StreamState } from "./event-stream";

export interface UseEventStreamOptions {
  readonly url: string | null;
  readonly enabled?: boolean;
  readonly onEvent: (event: SSEEvent) => void;
  /** 页面可据此显示断线/终止状态；401 由全局会话层处理。 */
  readonly onTerminalError?: (error: ApiError) => void;
}

export interface EventStreamStatus {
  readonly state: StreamState;
  readonly lastEventId: string | null;
  readonly error: ApiError | null;
  /** 手动重连会清除终止标记并从头/游标继续。 */
  readonly reconnect: () => void;
}

export function useEventStream({ url, enabled = true, onEvent, onTerminalError }: UseEventStreamOptions): EventStreamStatus {
  const [state, setState] = useState<StreamState>("idle");
  const [lastEventId, setLastEventId] = useState<string | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [visible, setVisible] = useState(() => (typeof document === "undefined" ? true : document.visibilityState !== "hidden"));
  const handler = useRef(onEvent);
  handler.current = onEvent;
  const terminalHandler = useRef(onTerminalError);
  terminalHandler.current = onTerminalError;

  useEffect(() => {
    if (typeof document === "undefined") return;
    const update = () => setVisible(document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);

  useEffect(() => {
    if (!url || !enabled || !visible) {
      setState("idle");
      return;
    }
    const controller = new AbortController();
    setError(null);
    void runEventStream(url, {
      signal: controller.signal,
      lastEventId,
      onEvent: event => {
        if (event.id) setLastEventId(event.id);
        handler.current(event);
      },
      onState: next => setState(next),
      onError: (next, terminal) => {
        if (terminal) {
          setError(next);
          terminalHandler.current?.(next);
        }
      },
    });
    return () => controller.abort();
    // lastEventId 不作为依赖：由流内部维护，避免每次事件都重连。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, enabled, visible, attempt]);

  const reconnect = useCallback(() => {
    setError(null);
    setAttempt(value => value + 1);
  }, []);

  return { state, lastEventId, error, reconnect };
}
