import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, isAbortError } from "@/lib/api/errors";

export type ResourceState<T> =
  | { readonly status: "idle" }
  | { readonly status: "loading" }
  | { readonly status: "error"; readonly error: ApiError }
  | {
      readonly status: "ready";
      readonly data: T;
      readonly refreshing: boolean;
      /** 静默刷新失败但保留旧数据时的可见警告；403/401 会清空数据而不是保留。 */
      readonly refreshError: ApiError | null;
      readonly updatedAt: number;
    };

export interface UseResourceOptions {
  readonly enabled?: boolean;
  /** 仅对成功的 ready 状态生效；页面隐藏时暂停。 */
  readonly refreshIntervalMs?: number;
  readonly shouldPoll?: (data: unknown) => boolean;
}

export interface UseResourceResult<T> {
  readonly state: ResourceState<T>;
  readonly reload: () => void;
  readonly refreshSilent: () => void;
}

interface InternalState<T> {
  readonly key: string;
  readonly snapshot: ResourceState<T>;
}

/**
 * 只读资源加载钩子。
 *
 * - key 变化（身份、作用域、筛选条件）时，在渲染期同步失效旧数据：
 *   首个提交就是 loading，不会先显示上一作用域的行/计数。
 * - 每次请求带序号并在卸载/重载时 abort；迟到响应即使忽略 AbortSignal 也会被丢弃。
 * - 失败不产生任何替代数据；403/401 清空当前资源，503 等静默刷新失败保留旧数据但显式警告。
 * - 不自动重试，只允许页面显式 reload；轮询仅用于明确声明的作业状态页。
 */
export function useResource<T>(
  key: string,
  loader: (signal: AbortSignal) => Promise<T>,
  options: UseResourceOptions = {},
): UseResourceResult<T> {
  const { enabled = true } = options;
  const loaderRef = useRef(loader);
  loaderRef.current = loader;
  const optionsRef = useRef(options);
  optionsRef.current = options;

  const [internal, setInternal] = useState<InternalState<T>>({
    key,
    snapshot: enabled ? { status: "loading" } : { status: "idle" },
  });
  const [attempt, setAttempt] = useState(0);
  const sequence = useRef(0);
  const controllerRef = useRef<AbortController | null>(null);
  const keyRef = useRef(key);
  keyRef.current = key;

  // 渲染期同步切换世代：旧 key 的 ready 数据不能出现在新作用域的首帧。
  if (internal.key !== key) {
    setInternal({ key, snapshot: enabled ? { status: "loading" } : { status: "idle" } });
  }

  const current: ResourceState<T> =
    internal.key === key ? internal.snapshot : enabled ? { status: "loading" } : { status: "idle" };

  useEffect(() => {
    if (!enabled) {
      setInternal({ key, snapshot: { status: "idle" } });
      return;
    }
    controllerRef.current?.abort();
    const controller = new AbortController();
    controllerRef.current = controller;
    const id = ++sequence.current;

    setInternal(currentState =>
      currentState.key === key && currentState.snapshot.status === "ready"
        ? currentState
        : { key, snapshot: { status: "loading" } },
    );

    let pending: Promise<T>;
    try {
      pending = loaderRef.current(controller.signal);
    } catch (error) {
      setInternal({ key, snapshot: toError(error) });
      return () => controller.abort();
    }

    pending.then(
      data => {
        if (id !== sequence.current || controller.signal.aborted || keyRef.current !== key) return;
        setInternal({
          key,
          snapshot: { status: "ready", data, refreshing: false, refreshError: null, updatedAt: Date.now() },
        });
      },
      error => {
        if (id !== sequence.current || controller.signal.aborted || keyRef.current !== key) return;
        if (isAbortError(error)) return;
        setInternal({ key, snapshot: toError(error) });
      },
    );

    return () => controller.abort();
  }, [key, enabled, attempt]);

  const reload = useCallback(() => {
    controllerRef.current?.abort();
    setInternal({ key: keyRef.current, snapshot: { status: "loading" } });
    setAttempt(value => value + 1);
  }, []);

  const refreshSilent = useCallback(() => {
    setInternal(state => {
      if (state.key !== keyRef.current || state.snapshot.status !== "ready") return state;
      return { key: state.key, snapshot: { ...state.snapshot, refreshing: true } };
    });
    const controller = new AbortController();
    controllerRef.current?.abort();
    controllerRef.current = controller;
    const id = ++sequence.current;
    const targetKey = keyRef.current;
    let pending: Promise<T>;
    try {
      pending = loaderRef.current(controller.signal);
    } catch (error) {
      applySilentError(setInternal, targetKey, error);
      return;
    }
    pending.then(
      data => {
        if (id !== sequence.current || controller.signal.aborted || keyRef.current !== targetKey) return;
        setInternal({
          key: targetKey,
          snapshot: { status: "ready", data, refreshing: false, refreshError: null, updatedAt: Date.now() },
        });
      },
      error => {
        if (id !== sequence.current || controller.signal.aborted || keyRef.current !== targetKey) return;
        if (isAbortError(error)) return;
        applySilentError(setInternal, targetKey, error);
      },
    );
  }, []);

  // 明确声明的作业状态轮询：页面隐藏时暂停，终止态由 shouldPoll 决定。
  const interval = options.refreshIntervalMs;
  useEffect(() => {
    if (!interval || current.status !== "ready") return;
    if (optionsRef.current.shouldPoll && !optionsRef.current.shouldPoll(current.data)) return;
    let visible = typeof document === "undefined" || document.visibilityState !== "hidden";
    const onVisibility = () => {
      visible = document.visibilityState !== "hidden";
    };
    document.addEventListener("visibilitychange", onVisibility);
    const timer = setInterval(() => {
      if (visible && current.status === "ready") refreshSilent();
    }, interval);
    return () => {
      clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [interval, current, refreshSilent]);

  return { state: current, reload, refreshSilent };
}

function toError(error: unknown): ResourceState<never> {
  if (error instanceof ApiError) return { status: "error", error };
  return { status: "error", error: new ApiError({ kind: "unknown", status: 0, cause: error }) };
}

function applySilentError<T>(
  setInternal: (updater: (state: InternalState<T>) => InternalState<T>) => void,
  key: string,
  error: unknown,
): void {
  setInternal(state => {
    if (state.key !== key) return state;
    const apiError = error instanceof ApiError ? error : new ApiError({ kind: "unknown", status: 0, cause: error });
    const mustClear =
      apiError.kind === "forbidden" || apiError.kind === "auth" || apiError.kind === "csrf-missing";
    if (mustClear || state.snapshot.status !== "ready") {
      return { key: state.key, snapshot: { status: "error", error: apiError } };
    }
    return {
      key: state.key,
      snapshot: { ...state.snapshot, refreshing: false, refreshError: apiError },
    };
  });
}

export interface ActionResult<T> {
  readonly ok: boolean;
  readonly data: T | null;
  readonly error: ApiError | null;
}

/** 写操作执行器：防重复提交，返回明确结果，不在成功后伪造任何服务端状态。 */
export function useAction<TArgs extends readonly unknown[], T>(action: (...args: TArgs) => Promise<T>): {
  readonly run: (...args: TArgs) => Promise<ActionResult<T>>;
  readonly pending: boolean;
  readonly error: ApiError | null;
  readonly reset: () => void;
} {
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);
  const pendingRef = useRef(false);
  const actionRef = useRef(action);
  actionRef.current = action;

  const run = useCallback(async (...args: TArgs): Promise<ActionResult<T>> => {
    if (pendingRef.current) return { ok: false, data: null, error: null };
    pendingRef.current = true;
    setPending(true);
    setError(null);
    try {
      const data = await actionRef.current(...args);
      return { ok: true, data, error: null };
    } catch (caught) {
      const apiError = caught instanceof ApiError ? caught : new ApiError({ kind: "unknown", status: 0, cause: caught });
      if (apiError.kind !== "aborted") setError(apiError);
      return { ok: false, data: null, error: apiError };
    } finally {
      pendingRef.current = false;
      setPending(false);
    }
  }, []);

  return { run, pending, error, reset: () => setError(null) };
}
