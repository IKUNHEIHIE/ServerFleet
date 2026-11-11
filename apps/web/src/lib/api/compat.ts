/**
 * 真实 Go Controller 与候选契约的双兼容解析层（GM-W3-01）。
 *
 * 背景：`main` 已合入真实 Controller（`internal/{auth,nodes,servicegroups,tasks,files,audit,dns,
 * notifications,gateway}/http.go`），其响应结构与候选 OpenAPI 契约存在已登记差异（分页元数据平铺、
 * 字段别名、精简条目）。本模块把两种已知形状归一化为 `dto.ts` 的 DTO。
 *
 * 严格性不变式：
 * - 只接受两种**已知**形状；未知结构、缺失必需字段、类型不符一律抛出 `ApiError(kind:"invalid")`，
 *   绝不编造默认值、绝不把错误响应解析成成功数据。
 * - 未在两种已知形状中出现的字段保持“缺失”，由界面按缺口展示；本层不补造数据。
 */
import { ApiError } from "./errors";
import type { InstalledPlugin } from "./dto";

function invalid(detail: string): ApiError {
  return new ApiError({ kind: "invalid", status: 200, code: "RESPONSE_SHAPE", cause: detail });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asString(value: unknown): string | null {
  return typeof value === "string" && value.length > 0 ? value : null;
}

function asCursor(value: unknown): string | null {
  // 真实 tasks/audit 以数字偏移字符串作为游标；候选契约同样是字符串。
  return typeof value === "string" && value.length > 0 ? value : null;
}

/** 列表查询的归一化结果：`page.limit` 为服务端未声明时是 null，不得编造。 */
export interface NormalizedListPage<T> {
  readonly items: readonly T[];
  readonly page: { readonly nextCursor: string | null; readonly limit: number | null; readonly hasMore: boolean };
}

/**
 * 归一化列表响应：
 * - 候选契约：`{items, page: {nextCursor, limit, hasMore}}`；
 * - 真实 Controller（tasks/audit/files）：`{items, nextCursor}`（files 恒为 null）。
 * `hasMore` 只由服务端声明的元数据推导；两种形状都没有游标时为 false（无更多数据）。
 */
export function normalizeListPage<T>(
  payload: unknown,
  mapItem: (item: unknown, index: number) => T,
  label: string,
): NormalizedListPage<T> {
  if (!isRecord(payload)) throw invalid(`${label}: response is not an object`);
  if (!Array.isArray(payload.items)) throw invalid(`${label}: items is not an array`);
  const items = payload.items.map((item, index) => {
    try {
      return mapItem(item, index);
    } catch (cause) {
      throw cause instanceof ApiError ? cause : invalid(`${label}: item ${index} failed validation`);
    }
  });
  if (payload.page !== undefined) {
    const page = payload.page;
    if (!isRecord(page)) throw invalid(`${label}: page meta is not an object`);
    if (page.nextCursor !== undefined && page.nextCursor !== null && typeof page.nextCursor !== "string") {
      throw invalid(`${label}: page.nextCursor is not a string`);
    }
    const limit = typeof page.limit === "number" && Number.isFinite(page.limit) && page.limit >= 0 ? page.limit : null;
    if (typeof page.hasMore !== "boolean") throw invalid(`${label}: page.hasMore is not a boolean`);
    const nextCursor = asCursor(page.nextCursor);
    return { items, page: { nextCursor, limit, hasMore: page.hasMore } };
  }
  const nextCursor = payload.nextCursor === undefined ? null : asCursor(payload.nextCursor);
  if (payload.nextCursor !== undefined && payload.nextCursor !== null && nextCursor === null) {
    throw invalid(`${label}: nextCursor is not a string`);
  }
  return { items, page: { nextCursor, limit: null, hasMore: nextCursor !== null } };
}

/**
 * 归一化单个任务对象：真实 `GET /api/v1/tasks/{jobId}` 返回 batch 形状（`jobId`），
 * 候选契约与真实 `POST …/{jobId}/approve` 返回 summary 形状（`taskId`）。
 * 返回原对象并由 DTO 的可选别名承载；`taskId`/`jobId` 至少存在其一，否则判定为非法响应。
 */
export function requireTaskIdentifier(payload: unknown, label: string): string {
  if (!isRecord(payload)) throw invalid(`${label}: response is not an object`);
  const id = asString(payload.taskId) ?? asString(payload.jobId);
  if (id === null) throw invalid(`${label}: missing taskId/jobId`);
  return id;
}

/**
 * 归一化逐节点执行条目：真实 `GET /api/v1/tasks/{jobId}/executions` 只提供
 * `nodeId`/`state`/`error`；候选契约另有 executionId/startedAt/exitCode 等。缺失字段保持缺失。
 */
export function normalizeTaskExecutions(
  payload: unknown,
  label: string,
): NormalizedListPage<{
  readonly nodeId?: string;
 readonly controllerId?: string;
  readonly state: string;
  readonly error?: string;
  readonly executionId?: string;
  readonly exitCode?: number | null;
  readonly startedAt?: string;
  readonly finishedAt?: string;
  readonly outputTruncated?: boolean;
  readonly lastSequence?: number;
}> {
  return normalizeListPage(payload, item => {
    if (!isRecord(item)) throw invalid(`${label}: execution is not an object`);
    const nodeId = asString(item.nodeId);
    const controllerId = asString(item.controllerId);
    const state = asString(item.state);
    if ((nodeId === null) === (controllerId === null) || state === null || nodeId === "" || controllerId === "") throw invalid(`${label}: execution requires exactly one nodeId/controllerId and state`);
    const normalized: {
      nodeId?: string;
      controllerId?: string;
      state: string;
      error?: string;
      executionId?: string;
      exitCode?: number | null;
      startedAt?: string;
      finishedAt?: string;
      outputTruncated?: boolean;
      lastSequence?: number;
    } = { ...(nodeId !== null ? { nodeId } : { controllerId: controllerId! }), state };
    const error = asString(item.error);
    if (error !== null) normalized.error = error;
    const executionId = asString(item.executionId);
    if (executionId !== null) normalized.executionId = executionId;
    if (item.exitCode !== undefined) {
      if (item.exitCode !== null && typeof item.exitCode !== "number") throw invalid(`${label}: exitCode is not a number`);
      normalized.exitCode = item.exitCode;
    }
    const startedAt = asString(item.startedAt);
    if (startedAt !== null) normalized.startedAt = startedAt;
    const finishedAt = asString(item.finishedAt);
    if (finishedAt !== null) normalized.finishedAt = finishedAt;
    if (typeof item.outputTruncated === "boolean") normalized.outputTruncated = item.outputTruncated;
    if (item.lastSequence !== undefined) {
      if (typeof item.lastSequence !== "number" || !Number.isInteger(item.lastSequence) || item.lastSequence < 0) {
        throw invalid(`${label}: lastSequence is not a valid sequence`);
      }
      normalized.lastSequence = item.lastSequence;
    }
    return normalized;
  }, label);
}

/** DNS 供应商：真实实现返回 `{code, name}`；候选契约返回 `{code, displayName, supported, capabilities}`。 */
export function normalizeDnsProviders(
  payload: unknown,
  label: string,
): { readonly items: readonly { readonly code: string; readonly displayName: string; readonly supported?: boolean; readonly capabilities?: { readonly supportsZoneSync?: boolean; readonly supportsProxy?: boolean; readonly supportedRecordTypes?: readonly string[] } }[] } {
  return normalizeListPage(payload, item => {
    if (!isRecord(item)) throw invalid(`${label}: provider is not an object`);
    const code = asString(item.code);
    const displayName = asString(item.displayName) ?? asString(item.name);
    if (code === null || displayName === null) throw invalid(`${label}: provider missing code/displayName`);
    const normalized: {
      code: string;
      displayName: string;
      supported?: boolean;
      capabilities?: { supportsZoneSync?: boolean; supportsProxy?: boolean; supportedRecordTypes?: readonly string[] };
    } = { code, displayName };
    if (typeof item.supported === "boolean") normalized.supported = item.supported;
    if (item.capabilities !== undefined) {
      if (!isRecord(item.capabilities)) throw invalid(`${label}: capabilities is not an object`);
      const capabilities: { supportsZoneSync?: boolean; supportsProxy?: boolean; supportedRecordTypes?: readonly string[] } = {};
      if (typeof item.capabilities.supportsZoneSync === "boolean") capabilities.supportsZoneSync = item.capabilities.supportsZoneSync;
      if (typeof item.capabilities.supportsProxy === "boolean") capabilities.supportsProxy = item.capabilities.supportsProxy;
      if (Array.isArray(item.capabilities.supportedRecordTypes)) {
        capabilities.supportedRecordTypes = item.capabilities.supportedRecordTypes.filter(
          (entry): entry is string => typeof entry === "string",
        );
      }
      normalized.capabilities = capabilities;
    }
    return normalized;
  }, label);
}

/**
 * Zone 同步：候选契约返回 `{items: [...]}`；真实实现返回单个 zone 对象。
 * 单对象按“一个元素的服务端结果”归一化为单项列表，不编造额外条目。
 */
export function normalizeZoneSync<T>(payload: unknown, isZone: (value: unknown) => boolean, label: string): { readonly items: readonly T[] } {
  if (isRecord(payload) && Array.isArray(payload.items)) {
    if (!payload.items.every(isZone)) throw invalid(`${label}: invalid zone entry`);
    return { items: payload.items as readonly T[] };
  }
  if (isZone(payload)) return { items: [payload as T] };
  throw invalid(`${label}: response is neither a zone list nor a zone`);
}

/**
 * 跨节点传输条目：候选契约字段为 `path`/`state`/`bytes`；真实实现为
 * `sourcePath`/`status`/`bytesCopied`。两边都缺失的字段保持缺失；两边同名字段冲突时视为非法响应。
 */
export function normalizeTransferItem(item: unknown, label: string): {
  readonly path?: string;
  readonly state?: string;
  readonly bytes?: number;
  readonly error?: string;
} {
  if (!isRecord(item)) throw invalid(`${label}: transfer item is not an object`);
  for (const [candidate, actual] of [["path", "sourcePath"], ["state", "status"], ["bytes", "bytesCopied"]] as const) {
    if (item[candidate] !== undefined && item[actual] !== undefined && item[candidate] !== item[actual]) {
      throw invalid(`${label}: conflicting transfer ${candidate} aliases`);
    }
  }
  const path = asString(item.path) ?? asString(item.sourcePath);
  const state = asString(item.state) ?? asString(item.status);
  const rawBytes = item.bytes ?? item.bytesCopied;
  const bytes = typeof rawBytes === "number" && Number.isFinite(rawBytes) && rawBytes >= 0 ? rawBytes : undefined;
  if ((item.path !== undefined || item.sourcePath !== undefined) && path === null) {
    throw invalid(`${label}: transfer item path is invalid`);
  }
  if ((item.state !== undefined || item.status !== undefined) && state === null) {
    throw invalid(`${label}: transfer item state is invalid`);
  }
  const error = asString(item.error) ?? asString(item.message);
  const normalized: { path?: string; state?: string; bytes?: number; error?: string } = {};
  if (path !== null) normalized.path = path;
  if (state !== null) normalized.state = state;
  if (bytes !== undefined) normalized.bytes = bytes;
  if (error !== null) normalized.error = error;
  return normalized;
}

/**
 * 已安装插件：候选契约返回 `status` 枚举；真实 `internal/plugins` Lifecycle 返回
 * `phase`（`installed_disabled/disabled/enabled/starting/stopping/runtime_unknown/stop_unknown`，
 * `uninstalled` 在列表与单个读取中均不可见）且没有 `status`/`installedAt` 字段。
 * 本函数只接受两种已知形状：`status` 缺失时按真实 phase 映射到语义一致的状态，
 * 未知形状抛 `ApiError(kind:"invalid")`，绝不编造默认状态。
 */
export function normalizeInstalledPlugin(payload: unknown, label: string): InstalledPlugin {
  if (!isRecord(payload)) throw invalid(`${label}: response is not an object`);
  const id = asString(payload.id);
  const version = typeof payload.version === "string" ? payload.version : null;
  if (id === null || version === null) throw invalid(`${label}: missing id/version`);
  if (typeof payload.enabled !== "boolean") throw invalid(`${label}: enabled is not a boolean`);
  const PHASE_STATUS: Readonly<Record<string, InstalledPlugin["status"]>> = {
    installed_disabled: "installed_disabled",
    disabled: "installed_disabled",
    enabled: "enabled",
    starting: "starting",
    stopping: "stopping",
    runtime_unknown: "runtime_unknown",
    stop_unknown: "stop_unknown",
  };
  const KNOWN_STATUS = new Set(Object.values(PHASE_STATUS));
  const status: InstalledPlugin["status"] | null =
    typeof payload.status === "string" && KNOWN_STATUS.has(payload.status as InstalledPlugin["status"])
      ? payload.status as InstalledPlugin["status"]
      : typeof payload.phase === "string" ? PHASE_STATUS[payload.phase] ?? null : null;
  if (status === null) throw invalid(`${label}: neither a known status nor a known phase`);
  const source = asString(payload.source) ?? asString(payload.sourceCommit);
  const digest = typeof payload.digest === "string" && payload.digest.length > 0 ? payload.digest : null;
  const runtimeId = typeof payload.runtimeId === "string" && payload.runtimeId.length > 0 ? payload.runtimeId : null;
  const revision = typeof payload.revision === "number" && Number.isInteger(payload.revision) && payload.revision >= 0
    ? payload.revision
    : null;
  return {
    id,
    version,
    status,
    enabled: payload.enabled,
    ...(source !== null ? { source } : {}),
    ...(digest !== null ? { digest } : {}),
    ...(revision !== null ? { revision } : {}),
    ...(runtimeId !== null ? { runtimeId } : {}),
  };
}

/** 已安装插件列表：真实与候选契约都返回 `{items: [...]}`；逐条走 normalizeInstalledPlugin 校验。 */
export function normalizePluginList(payload: unknown, label: string): { readonly items: readonly InstalledPlugin[] } {
  if (!isRecord(payload) || !Array.isArray(payload.items)) throw invalid(`${label}: items is not an array`);
  return { items: payload.items.map((item, index) => normalizeInstalledPlugin(item, `${label}[${index}]`)) };
}
