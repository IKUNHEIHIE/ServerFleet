/**
 * 上传会话 → 统一任务（dispatching）的只读映射。
 *
 * 服务端事实（internal/files/http.go）：
 *   - POST …/complete 在校验通过且已绑定统一任务时返回 **202**，
 *     body 为 {uploadId, status:"dispatching", taskId, files[]}；
 *   - 只有真正 complete 的会话才返回 status:"completed"；
 *   - GET …/uploads/{uploadId} 读回真实状态（awaiting_data/dispatching/completed/…）。
 *
 * 因此：202/dispatching 只代表“已受理进队列”，本模块禁止把它当成远端完成。
 */

export type UploadStatus = "awaiting_data" | "dispatching" | "completed" | "failed" | "cancelled" | "unknown";

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

/** 只有服务端明确 completed 才允许显示“已完成”。 */
export function claimsRemoteCompletion(status: UploadStatus): boolean {
  return status === "completed";
}

export interface UploadStatusText {
  readonly label: string;
  readonly detail: string;
  readonly tone: "pending" | "success" | "danger" | "muted";
}

export function describeUploadStatus(status: UploadStatus): UploadStatusText {
  switch (status) {
    case "awaiting_data":
      return { label: "等待字节流", detail: "会话已创建，尚未提交完整字节。", tone: "muted" };
    case "dispatching":
      return {
        label: "已受理（排队）",
        detail: "字节与服务端校验已通过，已进入统一任务队列；推送、审批与终态由任务链负责，远端尚未落盘。",
        tone: "pending",
      };
    case "completed":
      return { label: "已完成", detail: "服务端返回 completed，远端落盘完成。", tone: "success" };
    case "failed":
      return { label: "失败", detail: "服务端返回 failed；按错误码排查后重新上传。", tone: "danger" };
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
  readonly status: UploadStatus;
  readonly taskId?: string;
  readonly bytesReceived?: number;
  readonly expectedSize?: number | null;
  readonly updatedAt: string;
}

const MAX_PENDING = 20;
const storageKey = (nodeId: string) => `sf.files.pendingUploads.${nodeId}`;

function storage(): Storage | null {
  try {
    return globalThis.sessionStorage ?? null;
  } catch {
    return null;
  }
}

export function loadPendingUploads(nodeId: string): readonly PendingUpload[] {
  const store = storage();
  if (!store || nodeId === "") return [];
  const raw = store.getItem(storageKey(nodeId));
  if (!raw) return [];
  try {
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
        status: normalizeUploadStatus(record.status),
        ...(typeof record.taskId === "string" && record.taskId ? { taskId: record.taskId } : {}),
        ...(typeof record.bytesReceived === "number" ? { bytesReceived: record.bytesReceived } : {}),
        ...(typeof record.expectedSize === "number" || record.expectedSize === null
          ? { expectedSize: record.expectedSize as number | null }
          : {}),
        updatedAt: typeof record.updatedAt === "string" ? record.updatedAt : "",
      });
    }
    return items;
  } catch {
    return [];
  }
}

export function savePendingUploads(nodeId: string, items: readonly PendingUpload[]): void {
  const store = storage();
  if (!store || nodeId === "") return;
  try {
    if (items.length === 0) {
      store.removeItem(storageKey(nodeId));
      return;
    }
    store.setItem(storageKey(nodeId), JSON.stringify(items.slice(0, MAX_PENDING)));
  } catch {
    // 存储不可用时只影响刷新读回，不影响本次会话内的真实状态展示。
  }
}

/**
 * 以服务端读回为准合并一条记录。
 * 规则：终态不回退（除非服务端明确 cancelled/failed），且任何读回都不会升级成 completed。
 */
export function mergeUploadState(previous: PendingUpload, state: {
  readonly status: UploadStatus;
  readonly taskId?: string;
  readonly bytesReceived?: number;
  readonly expectedSize?: number | null;
}): PendingUpload {
  const next: UploadStatus = isTerminalStatus(previous.status) && !isTerminalStatus(state.status)
    ? previous.status
    : state.status;
  return {
    ...previous,
    status: next,
    ...(state.taskId ?? previous.taskId ? { taskId: state.taskId ?? previous.taskId } : {}),
    ...(state.bytesReceived !== undefined ? { bytesReceived: state.bytesReceived } : {}),
    ...(state.expectedSize !== undefined ? { expectedSize: state.expectedSize } : {}),
    updatedAt: new Date().toISOString(),
  };
}

export function activePendingUploads(items: readonly PendingUpload[]): readonly PendingUpload[] {
  return items.filter(item => !isTerminalStatus(item.status));
}
