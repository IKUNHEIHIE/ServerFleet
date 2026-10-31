import type { TaskState } from "@/lib/api/dto";

export const TASK_STATE_LABEL: Record<TaskState, string> = {
  awaiting_approval: "等待审批",
  queued: "排队中",
  waiting_node: "等待节点",
  running: "执行中",
  paused: "已暂停",
  completed: "已完成",
  partially_failed: "部分失败",
  failed: "失败",
  canceling: "取消中",
  canceled: "已取消",
  unknown: "未知",
  expired: "已过期",
};

export function isTaskTerminal(state: TaskState): boolean {
  return state === "completed" || state === "partially_failed" || state === "failed" || state === "canceled" || state === "expired";
}

export const TERMINAL_STATES: readonly TaskState[] = [
  "completed",
  "partially_failed",
  "failed",
  "canceled",
  "expired",
];
