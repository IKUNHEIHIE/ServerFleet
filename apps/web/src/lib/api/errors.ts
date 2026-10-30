export type ApiErrorKind =
  | "aborted"
  | "network"
  | "auth"
  | "forbidden"
  | "not-found"
  | "conflict"
  | "precondition"
  | "validation"
  | "rate-limited"
  | "unavailable"
  | "upstream"
  | "server"
  | "invalid"
  | "csrf-missing"
  | "unknown";

export interface FieldIssue {
  readonly pointer: string;
  readonly message: string;
}

export interface ApiErrorInit {
  readonly kind: ApiErrorKind;
  readonly status: number;
  readonly code?: string | null;
  readonly requestId?: string | null;
  readonly fieldErrors?: readonly FieldIssue[];
  readonly retryAfterSeconds?: number | null;
  readonly cause?: unknown;
}

/**
 * Controller 响应的本地错误模型。刻意不保留服务端原始正文：
 * 未实现路由会返回 HTML/纯文本，错误链也可能带内部路径；界面只使用固定文案与错误码。
 */
export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  readonly status: number;
  readonly code: string | null;
  readonly requestId: string | null;
  readonly fieldErrors: readonly FieldIssue[];
  readonly retryAfterSeconds: number | null;

  constructor(init: ApiErrorInit) {
    super(init.code ?? init.kind);
    this.name = "ApiError";
    this.kind = init.kind;
    this.status = init.status;
    this.code = init.code ?? null;
    this.requestId = init.requestId ?? null;
    this.fieldErrors = init.fieldErrors ?? [];
    this.retryAfterSeconds = init.retryAfterSeconds ?? null;
    if (init.cause !== undefined) this.cause = init.cause;
  }

  get isAborted(): boolean {
    return this.kind === "aborted";
  }

  get retryable(): boolean {
    return this.kind === "network" || this.kind === "unavailable" || this.kind === "upstream" || this.kind === "server";
  }
}

export function isApiError(value: unknown): value is ApiError {
  return value instanceof ApiError;
}

export function isAbortError(value: unknown): boolean {
  if (isApiError(value)) return value.kind === "aborted";
  return value instanceof DOMException && value.name === "AbortError";
}

const KIND_TEXT: Record<ApiErrorKind, string> = {
  aborted: "请求已取消。",
  network: "无法连接 Controller。请确认服务地址与网络后重试。",
  auth: "登录会话已失效，请重新登录。",
  forbidden: "当前身份没有执行此操作或读取此资源的权限。",
  "not-found": "资源不存在，或接口尚未在该 Controller 中提供。",
  conflict: "资源已被其他操作修改，请刷新后重试。",
  precondition: "提交所依据的版本已过期，请刷新最新状态后重试。",
  validation: "提交内容未通过服务端校验。",
  "rate-limited": "请求过于频繁，请稍后再试。",
  unavailable: "Controller 暂时不可用。当前会话已保留，可稍后重试。",
  upstream: "上游服务未能完成请求。",
  server: "Controller 处理请求时发生错误。",
  invalid: "Controller 返回了无法识别的响应，接口可能尚未实现。",
  "csrf-missing": "缺少安全写入令牌。刷新页面后需重新登录才能执行写操作。",
  unknown: "请求未能完成。",
};

/** 固定文案 + 便于支持定位的错误码/请求 ID；不回显原始响应体。 */
export function describeApiError(error: unknown): string {
  if (!isApiError(error)) return "请求未能完成。";
  const base = KIND_TEXT[error.kind];
  const meta: string[] = [];
  if (error.code) meta.push(`错误码 ${error.code}`);
  if (error.requestId) meta.push(`请求 ${error.requestId}`);
  return meta.length > 0 ? `${base}（${meta.join("，")}）` : base;
}

export const CHECK_UPDATE_MESSAGE = "检查网络与服务状态后重试；不会自动重复提交有副作用的操作。";
