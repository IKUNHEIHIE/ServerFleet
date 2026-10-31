import { ApiError, type ApiErrorKind, type FieldIssue } from "./errors";

export type QueryValue = string | number | boolean | null | undefined;

export interface RequestOptions {
  readonly method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  readonly query?: Readonly<Record<string, QueryValue | readonly QueryValue[]>>;
  readonly body?: unknown;
  /** 公开认证操作（登录/初始化）不使用已有会话的 CSRF 令牌。 */
  readonly csrf?: boolean;
  readonly idempotencyKey?: string;
  readonly ifMatch?: string;
  readonly signal?: AbortSignal;
  readonly timeoutMs?: number;
  readonly accept?: string;
  /** 登录失败是凭据错误，不应触发全局“会话失效”处理。 */
  readonly authEvent?: boolean;
}

export interface ApiResponse<T> {
  readonly data: T;
  readonly status: number;
}

type UnauthorizedListener = () => void;
type CsrfProvider = () => string | null;

const DEFAULT_TIMEOUT_MS = 15_000;
const MUTATING = new Set(["POST", "PUT", "PATCH", "DELETE"]);
const unauthorizedListeners = new Set<UnauthorizedListener>();
let csrfProvider: CsrfProvider = () => null;

/** 会话层在登录/恢复后绑定当前内存中的 CSRF 令牌；不得写入 localStorage。 */
export function setCsrfProvider(provider: CsrfProvider): void {
  csrfProvider = provider;
}

export function onUnauthorized(listener: UnauthorizedListener): () => void {
  unauthorizedListeners.add(listener);
  return () => unauthorizedListeners.delete(listener);
}

function notifyUnauthorized(): void {
  for (const listener of unauthorizedListeners) listener();
}

function classify(status: number): ApiErrorKind {
  if (status === 401) return "auth";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status === 409) return "conflict";
  if (status === 412 || status === 428) return "precondition";
  if (status === 422 || status === 400) return "validation";
  if (status === 429) return "rate-limited";
  if (status === 502 || status === 504) return "upstream";
  if (status === 503) return "unavailable";
  if (status >= 500) return "server";
  return "unknown";
}

function buildUrl(baseUrl: string, path: string, query: RequestOptions["query"]): string {
  const url = `${baseUrl}${path}`;
  if (!query) return url;
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null) continue;
    if (Array.isArray(value)) {
      for (const item of value) {
        if (item !== undefined && item !== null) params.append(key, String(item));
      }
    } else {
      params.append(key, String(value));
    }
  }
  const result = params.toString();
  return result ? `${url}?${result}` : url;
}

interface ErrorPayload {
  code?: unknown;
  message?: unknown;
  title?: unknown;
  detail?: unknown;
  requestId?: unknown;
  fieldErrors?: unknown;
  /** 真实 Controller 部分域（quota/publishing/certificates/monitoring）把错误码嵌套在 `error.code`。 */
  error?: unknown;
}

function boundedString(value: unknown, max = 200): string | null {
  return typeof value === "string" && value.length > 0 && value.length <= max ? value : null;
}

function parseFieldErrors(value: unknown): FieldIssue[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap(item => {
    if (typeof item !== "object" || item === null) return [];
    const pointer = boundedString((item as Record<string, unknown>).pointer, 300);
    const message = boundedString((item as Record<string, unknown>).message, 500);
    return pointer && message ? [{ pointer, message }] : [];
  });
}

function errorInitFromPayload(status: number, payload: ErrorPayload | null, retryAfter: number | null): {
  kind: ApiErrorKind;
  status: number;
  code: string | null;
  requestId: string | null;
  fieldErrors: FieldIssue[];
  retryAfterSeconds: number | null;
} {
  // GM-W4-02：真实 Controller 的 writeFixed 返回 `{error:{code,message}}`（嵌套），
  // 而问题 JSON 返回顶层 `{code,title}`；两种已知形状都接受，其余形状不推断。
  const nested = typeof payload?.error === "object" && payload.error !== null
    ? (payload.error as Record<string, unknown>)
    : null;
  const code = boundedString(payload?.code, 100)
    ?? boundedString(nested?.code, 100)
    ?? boundedString(payload?.title, 100);
  const requestId = boundedString(payload?.requestId, 200);
  return {
    kind: classify(status),
    status,
    code,
    requestId,
    fieldErrors: parseFieldErrors(payload?.fieldErrors),
    retryAfterSeconds: retryAfter,
  };
}

function parseRetryAfter(value: string | null): number | null {
  if (!value) return null;
  const seconds = Number(value);
  if (Number.isFinite(seconds) && seconds >= 0 && seconds <= 86_400) return seconds;
  const when = Date.parse(value);
  if (Number.isNaN(when)) return null;
  const delta = Math.ceil((when - Date.now()) / 1000);
  return delta >= 0 && delta <= 86_400 ? delta : null;
}

async function readJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text.trim() === "") return null;
  try {
    return JSON.parse(text);
  } catch {
    return null;
  }
}

function mergeSignals(signal: AbortSignal | undefined, timeoutMs: number): { signal: AbortSignal; timeout: boolean; cleanup: () => void } {
  const controller = new AbortController();
  let timedOut = false;
  const onAbort = () => controller.abort(signal?.reason);
  if (signal) {
    if (signal.aborted) controller.abort(signal.reason);
    else signal.addEventListener("abort", onAbort, { once: true });
  }
  const timer = setTimeout(() => {
    timedOut = true;
    controller.abort(new DOMException("Timed out", "TimeoutError"));
  }, timeoutMs);
  return {
    signal: controller.signal,
    get timeout() {
      return timedOut;
    },
    cleanup: () => {
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
    },
  };
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly fetchImpl: typeof fetch;
  private readonly defaultTimeoutMs: number;

  constructor(options: { baseUrl?: string; fetchImpl?: typeof fetch; timeoutMs?: number } = {}) {
    const rawBase = options.baseUrl ?? (import.meta.env.VITE_API_BASE as string | undefined) ?? "";
    this.baseUrl = rawBase.replace(/\/+$/, "");
    this.fetchImpl = options.fetchImpl ?? ((input, init) => globalThis.fetch(input, init));
    this.defaultTimeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  }

  async request<T>(path: string, options: RequestOptions = {}): Promise<ApiResponse<T>> {
    const method = options.method ?? "GET";
    const headers = new Headers();
    headers.set("Accept", options.accept ?? "application/json");
    headers.set("X-Requested-With", "ServerFleetWeb");
    if (options.body !== undefined) headers.set("Content-Type", "application/json");
    if (options.ifMatch) headers.set("If-Match", options.ifMatch);
    if (options.idempotencyKey) headers.set("Idempotency-Key", options.idempotencyKey);

    const needsCsrf = MUTATING.has(method) && options.csrf !== false;
    if (needsCsrf) {
      const token = csrfProvider();
      if (!token) {
        throw new ApiError({ kind: "csrf-missing", status: 0 });
      }
      headers.set("X-CSRF-Token", token);
    }

    const timeout = mergeSignals(options.signal, options.timeoutMs ?? this.defaultTimeoutMs);
    let response: Response;
    try {
      response = await this.fetchImpl(buildUrl(this.baseUrl, path, options.query), {
        method,
        headers,
        credentials: "include",
        cache: "no-store",
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: timeout.signal,
      });
    } catch (cause) {
      if (timeout.signal.aborted) {
        throw new ApiError({
          kind: timeout.timeout ? "network" : "aborted",
          status: 0,
          cause,
        });
      }
      throw new ApiError({ kind: "network", status: 0, cause });
    } finally {
      timeout.cleanup();
    }

    const retryAfter = parseRetryAfter(response.headers.get("Retry-After"));
    const contentType = response.headers.get("Content-Type") ?? "";
    const isJson = /^application\/(?:[a-zA-Z0-9.+-]*\+)?json\b/.test(contentType);

    if (!response.ok) {
      let payload: ErrorPayload | null = null;
      if (isJson) {
        const parsed = await readJson(response);
        if (typeof parsed === "object" && parsed !== null) payload = parsed as ErrorPayload;
      }
      if (response.status === 401 && options.authEvent !== false) notifyUnauthorized();
      throw new ApiError(errorInitFromPayload(response.status, payload, retryAfter));
    }

    if (response.status === 204 || response.status === 205) {
      return { data: undefined as T, status: response.status };
    }

    if (!isJson) {
      throw new ApiError({ kind: "invalid", status: response.status });
    }

    const parsed = await readJson(response);
    return { data: parsed as T, status: response.status };
  }
}

export const apiClient = new ApiClient();
