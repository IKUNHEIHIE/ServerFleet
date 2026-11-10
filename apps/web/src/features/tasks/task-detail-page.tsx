import { IncidentPanel } from "./incident-panel";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, TextArea } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { tasksApi } from "@/lib/api/endpoints";
import type { TaskSummary } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { useEventStream } from "@/lib/sse/use-event-stream";
import { parseTaskEvent, type TaskLiveEvent } from "./task-event";
import { isTaskTerminal, TASK_STATE_LABEL } from "./task-state";

type DecisionKind = "approve" | "deny" | "cancel" | "resume";

const DECISION_TITLE: Record<DecisionKind, string> = {
  approve: "批准任务",
  deny: "拒绝任务",
  cancel: "取消任务",
  resume: "恢复任务",
};

export function TaskDetailPage({ taskId }: { readonly taskId: string }) {
  const session = useSession();
  const [decision, setDecision] = useState<DecisionKind | null>(null);
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);
  const [liveEvents, setLiveEvents] = useState<readonly TaskLiveEvent[]>([]);
  const [streamError, setStreamError] = useState<unknown>(null);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const task = useResource(`task:detail:${session.viewEpoch}:${taskId}`, signal =>
    tasksApi.get(taskId, signal).then(result => result.data),
  );
  const executions = useResource(`task:executions:${session.viewEpoch}:${taskId}`, signal =>
    tasksApi.executions(taskId, { limit: 200 }, signal).then(result => result.data),
  );
  const current: TaskSummary | null = task.state.status === "ready" ? task.state.data : null;
  // 真实 GET /api/v1/tasks/{jobId} 返回 batch 形状，不含 planDigest；审批必须使用服务端签发的
  // planDigest，因此从服务端任务列表按 taskId 补齐，绝不本地编造摘要。
  const planDigestLookup = useResource(
    `task:plan:${session.viewEpoch}:${taskId}`,
    signal => tasksApi.list({ limit: 200 }, signal).then(result => result.data.items.find(item => item.taskId === taskId)?.planDigest ?? null),
    { enabled: Boolean(current) && !current?.planDigest },
  );
  const planDigest: string | null = current?.planDigest ?? (planDigestLookup.state.status === "ready" ? planDigestLookup.state.data : null);
  const terminal = current ? isTaskTerminal(current.state) : false;

  const stream = useEventStream({
    url: current && !terminal ? tasksApi.eventsUrl(taskId) : null,
    enabled: Boolean(current) && !terminal,
    onEvent: event => {
      const projected = parseTaskEvent(event);
      setLiveEvents(events => [projected, ...events].slice(0, 200));
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
      refreshTimer.current = setTimeout(() => {
        task.refreshSilent();
        executions.refreshSilent();
      }, 400);
    },
    onTerminalError: error => setStreamError(error),
  });

  useEffect(() => {
    setLiveEvents([]);
    setStreamError(null);
    setOutcome(null);
    setDecision(null);
    return () => {
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
    };
  }, [taskId]);

  const approveAction = useAction((body: { planDigest: string; decision: "approve" | "deny"; reason?: string }) =>
    tasksApi.approve(taskId, body),
  );
  const cancelAction = useAction((body: { reason: string; expectedRevision?: number }) => tasksApi.cancel(taskId, body));
  const resumeAction = useAction((body: { reason: string; expectedRevision?: number }) => tasksApi.resume(taskId, body));

  const canApprove = session.can("task.approve") || session.can("task.manage");
  const canCancel = session.can("task.cancel") || session.can("task.manage");
  const canResume = session.can("task.submit") || session.can("task.manage");
  const writesLocked = session.writesLocked;
  const busy = approveAction.pending || cancelAction.pending || resumeAction.pending;
  const actionError = approveAction.error ?? cancelAction.error ?? resumeAction.error;

  function openDecision(kind: DecisionKind) {
    setOutcome(null);
    setReason("");
    setDecision(kind);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!decision || !current) return;
    if (decision === "approve" || decision === "deny") {
      if (!planDigest) return; // 按钮已禁用：服务端未提供 planDigest 时不提交，不编造。
      const result = await approveAction.run({
        planDigest,
        decision,
        ...(reason.trim() ? { reason: reason.trim() } : {}),
      });
      if (result.ok) {
        setOutcome(decision === "approve" ? "审批已提交（服务端结果）。" : "拒绝已提交（服务端结果）。");
        setDecision(null);
      }
      return;
    }
    if (decision === "cancel") {
      const result = await cancelAction.run({ reason: reason.trim(), expectedRevision: current.revision });
      if (result.ok) {
        setOutcome("取消请求已提交；已复制的数据不会被自动回滚。");
        setDecision(null);
      }
      return;
    }
    const result = await resumeAction.run({ reason: reason.trim(), expectedRevision: current.revision });
    if (result.ok) {
      setOutcome("恢复请求已提交；只恢复未开始批次。");
      setDecision(null);
    }
  }

  const hasProgress = liveEvents.some(event => event.progressPercent !== null);
  const latestProgress = liveEvents.find(event => event.progressPercent !== null)?.progressPercent ?? null;
  const latestState = liveEvents.find(event => event.state !== null)?.state ?? null;

  return (
    <div className="space-y-5">
      <IncidentPanel key={`incidents:${session.viewEpoch}`} taskId={taskId} />
      <PageHeader
        title={current ? `任务 ${current.taskId}` : "任务详情"}
        description="任务状态、目标快照、审批、逐节点执行与 SSE 事件。取消不冒充回滚；未知结果不自动重跑。"
        actions={
          <>
            <a href="#/tasks" className="inline-flex h-9 items-center rounded-md border bg-card px-3 text-sm hover:bg-secondary">返回任务中心</a>
            {current?.state === "awaiting_approval" ? (
              <>
                <Button type="button" disabled={!canApprove || writesLocked || busy || !planDigest} onClick={() => openDecision("approve")}>批准</Button>
                <Button type="button" variant="danger" disabled={!canApprove || writesLocked || busy || !planDigest} onClick={() => openDecision("deny")}>拒绝</Button>
              </>
            ) : null}
            {current?.state === "paused" ? (
              <Button type="button" disabled={!canResume || writesLocked || busy} onClick={() => openDecision("resume")}>恢复</Button>
            ) : null}
            {current && !terminal && current.state !== "awaiting_approval" ? (
              <Button type="button" variant="danger" disabled={!canCancel || writesLocked || busy} onClick={() => openDecision("cancel")}>取消任务</Button>
            ) : null}
          </>
        }
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      {writesLocked ? <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能审批/取消/恢复。</Notice> : null}
      {streamError ? (
        <Notice tone="error" title="事件流已终止">
          {describeApiError(streamError)} 不会自动重连；可手动重连或刷新任务。
        </Notice>
      ) : null}

      <AsyncPanel
        state={task.state}
        onRetry={task.reload}
        errorTitle="任务加载失败"
        empty={<EmptyState title="任务不存在" description="服务端未返回该任务，或当前身份无权查看。" />}
        isEmpty={() => false}
      >
        {data => (
          <div className="space-y-5">
            <Panel title="任务快照" description="GET /api/v1/tasks/{taskId}；状态与摘要以服务端为准。">
              <div className="grid gap-4 px-5 py-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
                <p>状态：<strong>{TASK_STATE_LABEL[data.state]}</strong></p>
                <p>创建：{formatDateTime(data.createdAt) ?? "—"}</p>
                <p>创建者：{data.createdBy ?? "—"}</p>
                <p>到期：{formatDateTime(data.expiresAt) ?? "—"}</p>
                <p className="lg:col-span-2">planDigest：<span className="break-all font-mono text-xs">{data.planDigest ?? planDigest ?? "（此端点未返回 planDigest；服务端任务列表中也没有该任务，审批保持禁用）"}</span></p>
                <p>revision：{data.revision}</p>
                <p>目标快照：{data.targetType === "controller" ? "1 个主控" : `${data.targetNodeIdsSnapshot.length} 台`}</p>
              </div>
              <div className="border-t px-5 py-3 text-xs text-muted-foreground">
                目标集合：{data.targetType === "controller" ? `主控 ${data.targetControllerId ?? "身份未返回"}` : data.targetNodeIdsSnapshot.length > 0 ? data.targetNodeIdsSnapshot.join("、") : "空快照"}
              </div>
            </Panel>

            <Panel
              title="实时事件"
              description="SSE 只投影已知字段；原始 data 不回显，避免命令输出或秘密进入界面。"
              actions={
                <Button size="sm" onClick={stream.reconnect} disabled={terminal}>重连事件流</Button>
              }
            >
              <div className="flex flex-wrap items-center gap-4 border-b px-5 py-3 text-xs text-muted-foreground">
                <span>连接：{stream.state}</span>
                <span>游标：{stream.lastEventId ?? "尚未收到"}</span>
                <span>最新进度：{hasProgress && latestProgress !== null ? `${latestProgress}%` : "未收到进度事件"}</span>
                <span>事件中的状态：{latestState ? TASK_STATE_LABEL[latestState] : "未收到"}</span>
              </div>
              {liveEvents.length === 0 ? (
                <p className="px-5 py-6 text-sm text-muted-foreground">
                  {terminal ? "任务已结束；不再订阅事件流。" : "尚未收到事件。任务执行状态以服务端快照与本表为准。"}
                </p>
              ) : (
                <ul className="max-h-72 divide-y overflow-y-auto">
                  {liveEvents.map((event, index) => (
                    <li key={`${event.id ?? "evt"}-${event.receivedAt}-${index}`} className="px-5 py-2 text-xs">
                      <span className="font-mono">{event.id ?? "无游标"}</span> · {event.event}
                      {event.state ? ` · ${TASK_STATE_LABEL[event.state]}` : ""}
                      {event.progressPercent !== null ? ` · ${event.progressPercent}%` : ""}
                      {event.nodeId ? ` · ${event.nodeId}` : ""}
                      {event.message ? ` · ${event.message}` : ""}
                    </li>
                  ))}
                </ul>
              )}
            </Panel>

            <Panel title="逐节点执行" description="GET /api/v1/tasks/{taskId}/executions；未知/超时不等于成功。">
              <AsyncPanel
                state={executions.state}
                onRetry={executions.reload}
                errorTitle="执行结果加载失败"
                empty={<EmptyState title="暂无执行记录" description="任务尚未派发到节点或服务端未返回执行项。" />}
                isEmpty={page => page.items.length === 0}
              >
                {page => (
                  <DataTable
                    caption="任务执行结果"
                    rows={page.items}
                    rowKey={(execution, index) => execution.executionId ?? execution.nodeId ?? execution.controllerId ?? `execution-${index}`}
                    columns={[
                      { key: "node", header: "执行目标", render: execution => <span className="font-mono text-xs">{execution.controllerId ? `主控 ${execution.controllerId}` : execution.nodeId}</span> },
                      { key: "state", header: "状态", render: execution => execution.state },
                      { key: "exit", header: "退出码", render: execution => (execution.exitCode === null || execution.exitCode === undefined ? "—" : execution.exitCode) },
                      { key: "start", header: "开始", render: execution => formatDateTime(execution.startedAt) ?? "—" },
                      { key: "finish", header: "结束", render: execution => formatDateTime(execution.finishedAt) ?? "—" },
                      { key: "seq", header: "最后序号", align: "right", render: execution => execution.lastSequence ?? "—" },
                      { key: "error", header: "执行说明", render: execution => execution.error ?? "—" },
                      { key: "truncated", header: "输出截断", render: execution => (execution.outputTruncated === undefined ? "—" : execution.outputTruncated ? "是" : "否") },
                    ]}
                  />
                )}
              </AsyncPanel>
            </Panel>
          </div>
        )}
      </AsyncPanel>

      <Modal
        open={decision !== null}
        title={decision ? DECISION_TITLE[decision] : "任务操作"}
        description={
          decision === "cancel"
            ? "取消是尽力请求；已执行/已复制的部分不会被自动回滚。"
            : decision === "resume"
              ? "只恢复未开始的批次；已过期或已撤权的目标不会被自动放行。"
              : "审批绑定当前 planDigest、目标快照与权限世代；重复或改参提交必须被服务端拒绝。"
        }
        onClose={() => {
          if (!busy) setDecision(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setDecision(null)} disabled={busy}>取消</Button>
            <Button type="submit" form="task-decision" variant={decision === "approve" ? "primary" : "danger"} loading={busy} disabled={!reason.trim() && decision !== "approve" && decision !== "deny"}>
              确认
            </Button>
          </>
        }
      >
        <form id="task-decision" onSubmit={submit} className="space-y-4">
          {actionError ? <Notice tone="error" title="操作失败">{describeApiError(actionError)}</Notice> : null}
          {current ? <p className="text-xs text-muted-foreground">planDigest：<span className="break-all font-mono">{current.planDigest ?? planDigest ?? "未返回"}</span></p> : null}
          <Field label="原因（可选）" hint="服务端会写入审计；取消/恢复需要说明。">
            {ids => <TextArea id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} maxLength={500} />}
          </Field>
        </form>
      </Modal>
    </div>
  );
}
