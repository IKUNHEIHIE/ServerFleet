import type { TaskState } from "@/lib/api/dto";
import type { SSEEvent } from "@/lib/sse/event-stream";

const STATES: readonly TaskState[] = [
  "awaiting_approval",
  "queued",
  "waiting_node",
  "running",
  "paused",
  "completed",
  "partially_failed",
  "failed",
  "canceling",
  "canceled",
  "unknown",
  "expired",
];

export interface TaskLiveEvent {
  readonly id: string | null;
  readonly event: string;
  readonly taskId: string | null;
  readonly state: TaskState | null;
  readonly nodeId: string | null;
  readonly message: string | null;
  readonly progressPercent: number | null;
  readonly sequence: number | null;
  readonly receivedAt: number;
}

function bounded(value: unknown, max = 300): string | null {
  return typeof value === "string" && value.length > 0 && value.length <= max ? value : null;
}

/**
 * 只投影已知、有限的字段。SSE 原始 data 可能包含命令输出或其他敏感内容，
 * 不把整个 JSON 回显到界面；未知事件只显示名称与游标。
 */
export function parseTaskEvent(event: SSEEvent, now = Date.now()): TaskLiveEvent {
  let data: Record<string, unknown> | null = null;
  try {
    const parsed: unknown = JSON.parse(event.data);
    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) data = parsed as Record<string, unknown>;
  } catch {
    data = null;
  }
  const state = typeof data?.state === "string" && (STATES as readonly string[]).includes(data.state) ? (data.state as TaskState) : null;
  const progress = typeof data?.progressPercent === "number" && Number.isFinite(data.progressPercent) && data.progressPercent >= 0 && data.progressPercent <= 100
    ? data.progressPercent
    : null;
  const sequence = typeof data?.sequence === "number" && Number.isInteger(data.sequence) && data.sequence >= 0 ? data.sequence : null;
  return {
    id: event.id,
    event: event.event,
    taskId: bounded(data?.taskId, 200),
    state,
    nodeId: bounded(data?.nodeId, 200),
    message: bounded(data?.message, 300),
    progressPercent: progress,
    sequence,
    receivedAt: now,
  };
}
