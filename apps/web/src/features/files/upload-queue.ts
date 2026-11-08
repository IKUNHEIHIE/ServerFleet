/**
 * 上传会话 → 统一任务（dispatching）的只读映射。
 *
 * 服务端事实（internal/files/http.go）：
 *   - POST …/complete 在校验通过且已绑定统一任务时返回 **202**，
 *     body 为 {uploadId, status:"dispatching", taskId, files[]}；
 *   - 只有真正 complete 的会话才返回 status:"completed"；
 *   - GET …/uploads/{uploadId} 读回真实状态（awaiting_data/dispatching/completed/…）。
 *
 * 证据模型（本项目曾在此处出错，语义必须显式区分）：
 *   - **持久存储只保存“待核验的身份元数据”**（uploadId/name/taskId/expectedSize）。
 *     它绝不保存 status：缓存里的 completed 可能在重新登录、撤权或多端并发之后
 *     早已不是事实，不能冒充当前已验证结果。
 *   - **只有当前身份/当前节点下 GET 成功读回**才产生 verified 证据；
 *     401/403/404/5xx/网络失败一律降级为“未核验 / 无权限 / 会话不存在”，
 *     并且必须把先前缓存或旧一次读回得到的 completed 一并作废。
 *   - 过期响应不回退：每条记录带单调 sequence，旧序号的读回结果直接丢弃；
 *     节点或身份 viewEpoch 变化后，旧作用域的在途请求整体作废。
 *
 * 因此：202/dispatching 只代表“已受理进队列”，本模块禁止把它当成远端完成。
 */

export type UploadStatus = "awaiting_data" | "dispatching" | "completed" | "failed" | "cancelled" | "unknown";

/**
 * 当前记录的证据来源。只有 verified 才是“当前服务端事实”，其余都必须显示未核验。
 */
export type UploadEvidence = "unverified" | "verified" | "forbidden" | "not_found" | "unavailable";

const TERMINAL: ReadonlySet<UploadStatus> = new Set<UploadStatus>(["completed", "failed", "cancelled"]);

export function normalizeUploadStatus(raw: unknown): UploadStatus {
  switch (raw) {
    case "awaiting_data":
    case "dispatching":
    case "completed":
    case "failed":
    case "cancelled":
      return raw;
    default:
      // 未识别的状态不猜测语义，按 unknown 展示并保持“不可声称完成”。
      return "unknown";
  }
}

export function isTerminalStatus(status: UploadStatus): boolean {
  return TERMINAL.has(status);
}

/** 只有“当前已验证”且服务端明确 completed，才允许显示“已完成”。 */
export function claimsRemoteCompletion(item: { readonly status: UploadStatus; readonly evidence: UploadEvidence }): boolean {
  return item.evidence === "verified" && item.status === "completed";
}

export interface UploadStatusText {
  readonly label: string;
  readonly detail: string;
  readonly tone: "pending" | "success" | "danger" | "muted";
}

export function describeUploadStatus(item: {
  readonly status: UploadStatus;
  readonly evidence: UploadEvidence;
  readonly reason?: string;
}): UploadStatusText {
  const reason = item.reason ? `（${item.reason}）` : "";
  switch (item.evidence) {
    case "unverified":
      return {
        label: "待核验",
        detail: "本地只有待核验的记录，尚未取得当前服务端状态；不声称远端完成。",
        tone: "muted",
      };
    case "forbidden":
      return {
        label: "无权限读取（未核验）",
        detail: `当前身份无权读取该上传会话${reason}：先前状态已作废，不声称完成；请确认权限或重新登录后读回。`,
        tone: "danger",
      };
    case "not_found":
      return {
        label: "会话不存在（UNKNOWN）",
        detail: `服务端没有该上传会话${reason}：无法确认是否落盘，不声称完成。`,
        tone: "danger",
      };
    case "unavailable":
      return {
        label: "读取失败（未核验）",
        detail: `服务端或网络不可用${reason}：无法确认状态，不声称完成；可稍后重试读回。`,
        tone: "danger",
      };
    default:
      break;
  }
  switch (item.status) {
    case "awaiting_data":
      return { label: "等待字节流", detail: "会话已创建，尚未提交完整字节。", tone: "muted" };
    case "dispatching":
      return {
        label: "已受理（排队）",
        detail: "字节与服务端校验已通过，已进入统一任务队列；推送、审批与终态由任务链负责，远端尚未落盘。",
        tone: "pending",
      };
    case "completed":
      return { label: "已完成", detail: "服务端读回 completed，远端落盘完成。", tone: "success" };
    case "failed":
      return { label: "失败", detail: "服务端读回 failed；按错误码排查后重新上传。", tone: "danger" };
    case "cancelled":
      return { label: "已取消", detail: "任务已取消，未完整落盘。", tone: "danger" };
    default:
      return { label: "状态未知", detail: "服务端返回了未识别状态；不声称完成，请刷新读回。", tone: "muted" };
  }
}

/** 任务状态（/api/v1/tasks/{id}）只用于辅助展示，不改变上传状态判定。 */
export function taskStateLabel(state: unknown): string {
  switch (state) {
    case "queued":
      return "排队";
    case "running":
      return "执行中";
    case "awaiting_approval":
      return "待审批";
    case "completed":
      return "任务已完成";
    case "failed":
      return "任务失败";
    case "cancelled":
      return "任务已取消";
    default:
      return typeof state === "string" && state !== "" ? state : "未知";
  }
}

export type UploadFailureKind = "forbidden" | "conflict" | "toolarge" | "unavailable" | "digest" | "unknown";

export interface UploadFailure {
  readonly kind: UploadFailureKind;
  readonly text: string;
  /** 冲突/偏移不一致时可以读回真实 bytesReceived 再续传。 */
  readonly recoverable: boolean;
}

/**
 * 把真实 HTTP 状态与错误码翻译成可操作提示。保持“未发生的事不声称”。
 * 409 既可能是 offset 不一致（可读回后续传），也可能是同名冲突/预算拒绝。
 */
export function describeUploadFailure(status: number, code?: string | null): UploadFailure {
  const normalized = (code ?? "").toLowerCase();
  if (status === 403) {
    return { kind: "forbidden", text: "无权限（403）：当前身份缺少该节点文件写入或插件授权。", recoverable: false };
  }
  if (status === 401) {
    return { kind: "forbidden", text: "会话失效（401）：请重新登录后重试。", recoverable: false };
  }
  if (status === 409) {
    if (normalized.includes("offset")) {
      return { kind: "conflict", text: "偏移不一致（409）：读回服务端已收字节后从该偏移续传。", recoverable: true };
    }
    if (normalized.includes("budget")) {
      return { kind: "conflict", text: "超出预算（409）：先调整预算或选择更小的目标。", recoverable: false };
    }
    if (normalized.includes("conflict") || normalized.includes("exist")) {
      return { kind: "conflict", text: "目标冲突（409）：按 skip/overwrite/rename 策略处理后重试。", recoverable: true };
    }
    return { kind: "conflict", text: `冲突（409 ${code ?? ""}）：刷新读回真实状态后再决定。`, recoverable: true };
  }
  if (status === 413 || normalized.includes("too_large") || normalized.includes("size")) {
    return { kind: "toolarge", text: "大小被拒（413/大小不符）：核对声明 sizeBytes 与实际字节。", recoverable: true };
  }
  if (status === 503) {
    return { kind: "unavailable", text: "通道不可用（503）：字节已落暂存但未送达，不能显示完成。", recoverable: true };
  }
  if (normalized.includes("sha") || normalized.includes("digest") || normalized.includes("mismatch")) {
    return { kind: "digest", text: "摘要不一致：服务端校验未通过，重新上传并核对 SHA-256。", recoverable: true };
  }
  return { kind: "unknown", text: `未识别结果（${status} ${code ?? ""}）：按未完成处理并读回状态。`, recoverable: true };
}

export interface PendingUpload {
  readonly uploadId: string;
  readonly nodeId: string;
  readonly name: string;
  readonly taskId?: string;
  readonly expectedSize?: number | null;
  /** 最近一次**当前作用域**成功读回的状态；未验证时恒为 unknown。 */
  readonly status: UploadStatus;
  readonly evidence: UploadEvidence;
  /** evidence 的机器可读原因（http_403 / http_500 / network / aborted）。 */
  readonly reason?: string;
  readonly bytesReceived?: number;
  readonly observedAt?: string;
  /** 单调请求序号：旧序号的结果不回退新证据。 */
  readonly sequence: number;
  /** 记录所属的会话作用域（身份 viewEpoch）。节点由 nodeId 表达。 */
  readonly epoch: string;
}

export interface UploadObservation {
  readonly kind: "verified" | "forbidden" | "not_found" | "unavailable";
  /** 仅 verified 使用；其余证据一律把状态降级为 unknown。 */
  readonly status?: UploadStatus;
  readonly taskId?: string;
  readonly bytesReceived?: number;
  readonly expectedSize?: number | null;
  readonly reason?: string;
  readonly sequence: number;
}

const MAX_PENDING = 20;
const storageKey = (nodeId: string, ownerId?: string) => ownerId === undefined
  ? `sf.files.pendingUploads.${nodeId}` // Legacy fixture/API compatibility only.
  : `sf.files.pendingUploads.v2.${encodeURIComponent(ownerId)}.${encodeURIComponent(nodeId)}`;

function storage(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
}

/**
 * 读回本地记录：**只恢复身份元数据**，状态一律为“未核验”。
 * 缓存里的 status/bytes/observedAt 被有意丢弃——它们不能冒充当前事实。
 */
export function loadPendingUploads(nodeId: string, epoch: string, ownerId?: string): readonly PendingUpload[] {
  const store = storage();
  if (!store || nodeId === "") return [];
  try {
    const raw = store.getItem(storageKey(nodeId, ownerId));
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    const items: PendingUpload[] = [];
    for (const entry of parsed.slice(0, MAX_PENDING)) {
      if (typeof entry !== "object" || entry === null) continue;
      const record = entry as Record<string, unknown>;
      if (typeof record.uploadId !== "string" || !record.uploadId) continue;
      if (typeof record.name !== "string") continue;
      items.push({
        uploadId: record.uploadId,
        nodeId,
        name: record.name,
        ...(typeof record.taskId === "string" && record.taskId ? { taskId: record.taskId } : {}),
        ...(typeof record.expectedSize === "number" || record.expectedSize === null
          ? { expectedSize: record.expectedSize as number | null }
          : {}),
        status: "unknown",
        evidence: "unverified",
        sequence: 0,
        epoch,
      });
    }
    return items;
  } catch {
    return [];
  }
}

/** 只持久化身份元数据；status/evidence 绝不落盘。 */
export function savePendingUploads(nodeId: string, items: readonly PendingUpload[], ownerId?: string): void {
  const store = storage();
  if (!store || nodeId === "") return;
  try {
    if (items.length === 0) {
      store.removeItem(storageKey(nodeId, ownerId));
      return;
    }
    const records = items.slice(0, MAX_PENDING).map(item => ({
      uploadId: item.uploadId,
      name: item.name,
      ...(item.taskId ? { taskId: item.taskId } : {}),
      ...(item.expectedSize === undefined ? {} : { expectedSize: item.expectedSize }),
    }));
    store.setItem(storageKey(nodeId, ownerId), JSON.stringify(records));
  } catch {
    // 存储不可用时只影响刷新读回，不影响本次会话内的真实状态展示。
  }
}

/** 新建一条上传记录：202 受理是**服务端证据**（dispatching），不是完成。 */
export function createPendingUpload(
  input: {
    readonly uploadId: string;
    readonly nodeId: string;
    readonly name: string;
    readonly taskId?: string;
    readonly bytesReceived?: number;
    readonly expectedSize?: number | null;
    readonly epoch: string;
    readonly sequence: number;
  },
  observedAt: string = new Date().toISOString(),
): PendingUpload {
  return {
    uploadId: input.uploadId,
    nodeId: input.nodeId,
    name: input.name,
    ...(input.taskId ? { taskId: input.taskId } : {}),
    ...(input.bytesReceived === undefined ? {} : { bytesReceived: input.bytesReceived }),
    ...(input.expectedSize === undefined ? {} : { expectedSize: input.expectedSize }),
    status: "dispatching",
    evidence: "verified",
    observedAt,
    sequence: input.sequence,
    epoch: input.epoch,
  };
}

/**
 * 以一次读回结果更新记录。
 *
 * 规则（顺序即优先级）：
 *   1. 过期结果不回退：observation.sequence 落后于现有 sequence 时原样返回；
 *   2. 失败/无权限/不存在：状态降级为 unknown，并保留失败原因——缓存或旧读回的
 *      completed 一律作废，不能“终态不倒退回”式地吞掉当前失败；
 *   3. 成功读回：状态与字节取服务端事实，evidence=verified；
 *   4. 任何路径都不会把记录升级成 completed，除非服务端明确返回 completed。
 */
export function mergeUploadState(previous: PendingUpload, observation: UploadObservation): PendingUpload {
  if (observation.sequence !== 0 && observation.sequence < previous.sequence) {
    return previous;
  }
  const observedAt = new Date().toISOString();
  if (observation.kind !== "verified") {
    return {
      ...previous,
      status: "unknown",
      evidence: observation.kind,
      ...(observation.reason ? { reason: observation.reason } : {}),
      ...(observation.taskId ? { taskId: observation.taskId } : {}),
      sequence: observation.sequence,
      observedAt,
    };
  }
  const status = normalizeUploadStatus(observation.status);
  return {
    ...previous,
    status,
    evidence: "verified",
    ...(observation.reason ? { reason: observation.reason } : { reason: undefined }),
    ...(observation.taskId ?? previous.taskId ? { taskId: observation.taskId ?? previous.taskId } : {}),
    ...(observation.bytesReceived === undefined ? {} : { bytesReceived: observation.bytesReceived }),
    ...(observation.expectedSize === undefined ? {} : { expectedSize: observation.expectedSize }),
    sequence: observation.sequence,
    observedAt,
  };
}

/**
 * 把一批读回结果合并进**最新**的记录列表：刷新期间新加入的上传不会被覆盖丢失。
 * 结果里没有的 uploadId 保持原样（仍是未核验）。
 */
export function mergeObservations(
  items: readonly PendingUpload[],
  observations: ReadonlyMap<string, UploadObservation>,
): readonly PendingUpload[] {
  return items.map(item => {
    const observation = observations.get(item.uploadId);
    return observation ? mergeUploadState(item, observation) : item;
  });
}

/** 未到终态、或尚未取得当前证据的记录，都属于“待确认”。 */
export function activePendingUploads(items: readonly PendingUpload[]): readonly PendingUpload[] {
  return items.filter(item => item.evidence !== "verified" || !isTerminalStatus(item.status));
}

/** 记录是否仍属于当前节点+身份作用域。 */
export function isCurrentScope(
  item: { readonly nodeId: string; readonly epoch: string },
  nodeId: string | null,
  epoch: string,
): boolean {
  return nodeId !== null && item.nodeId === nodeId && item.epoch === epoch;
}

/** 把读回失败归类为证据种类，绝不当作“状态未变”。 */
export function classifyReadFailure(status: number | null | undefined): UploadObservation["kind"] {
  if (status === 401 || status === 403) return "forbidden";
  if (status === 404) return "not_found";
  return "unavailable";
}
