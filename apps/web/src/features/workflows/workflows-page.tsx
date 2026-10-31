import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { workflowsApi } from "@/lib/api/endpoints-workflows";
import type { WorkflowRunView, WorkflowStep, WorkflowView } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const TRIGGER_OPTIONS: readonly { readonly value: string; readonly label: string }[] = [
  { value: "manual", label: "手动触发（manual）" },
  { value: "monitor.alert", label: "监控告警（monitor.alert）" },
  { value: "task.completed", label: "任务完成（task.completed）" },
  { value: "schedule", label: "定时（schedule）" },
];

const ACTION_OPTIONS: readonly string[] = ["tasks.command", "host.patch", "host.root"];

/** 服务端只接受字符串键值参数（internal/workflows http.go definitionFromBody）；每行 key=value。 */
function parseParameterLines(raw: string): Record<string, string> | string {
  const parameters: Record<string, string> = {};
  for (const line of raw.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed) continue;
    const separator = trimmed.indexOf("=");
    if (separator <= 0) return "参数必须每行写成 key=value（键不能为空）。";
    const key = trimmed.slice(0, separator).trim();
    const value = trimmed.slice(separator + 1).trim();
    if (!key || /[\s*?[\]]/.test(key)) return "参数键不能为空，且不能包含空格或 *?[]。";
    parameters[key] = value;
  }
  return parameters;
}

function stepSummary(step: WorkflowStep): string {
  const parameters = step.parameters && Object.keys(step.parameters).length > 0 ? JSON.stringify(step.parameters) : "{}";
  return `${step.actionId} → ${step.nodeIds.join("、")} · 参数 ${parameters}${step.humanApproval ? " · 需人工审批" : ""}`;
}

function runTaskIds(run: WorkflowRunView): string[] {
  return (run.steps ?? []).map(step => step.taskId).filter((taskId): taskId is string => Boolean(taskId));
}

/** 详情弹窗：GET /api/v1/workflows/{id} → {workflow, runs}；任务 ID 来自服务端运行快照。 */
function WorkflowDetail({ workflowId, onClose }: { readonly workflowId: string; readonly onClose: () => void }) {
  const [state, setState] = useState<{ loading: boolean; error: string | null; workflow: WorkflowView | null; runs: readonly WorkflowRunView[] }>({
    loading: true, error: null, workflow: null, runs: [],
  });

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true, error: null, workflow: null, runs: [] });
    workflowsApi
      .get(workflowId)
      .then(result => {
        if (cancelled) return;
        setState({ loading: false, error: null, workflow: result.data.workflow, runs: result.data.runs });
      })
      .catch(error => {
        if (cancelled) return;
        setState({ loading: false, error: describeApiError(error), workflow: null, runs: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [workflowId]);

  return (
    <Modal
      open
      title={state.workflow ? `工作流 ${state.workflow.id}` : "工作流详情"}
      description="工作流与运行记录来自服务端；任务执行与审批在任务中心进行。"
      onClose={onClose}
      wide
      footer={<Button type="button" onClick={onClose}>关闭</Button>}
    >
      {state.loading ? <p className="text-sm text-muted-foreground">正在加载…</p> : null}
      {state.error ? <Notice tone="error" title="详情加载失败">{state.error}</Notice> : null}
      {state.workflow ? (
        <div className="space-y-3 text-sm">
          <p>标识：<span className="font-mono text-xs">{state.workflow.id}</span> · revision {state.workflow.revision} · {state.workflow.enabled ? "启用" : "已停用"}</p>
          <p>触发器：{state.workflow.trigger}{state.workflow.match && Object.keys(state.workflow.match).length > 0 ? ` · 匹配 ${JSON.stringify(state.workflow.match)}` : ""}</p>
          <ol className="space-y-1">
            {state.workflow.steps.map((step, index) => (
              <li key={`${step.actionId}-${index}`} className="text-xs">{index + 1}. {stepSummary(step)}</li>
            ))}
          </ol>
          <div className="border-t pt-2">
            <p className="font-medium">运行记录</p>
            {state.runs.length === 0 ? (
              <p className="mt-1 text-xs text-muted-foreground">暂无运行记录。</p>
            ) : (
              <ul className="mt-1 space-y-1">
                {state.runs.map(run => {
                  const taskIds = runTaskIds(run);
                  return (
                    <li key={run.runId} className="text-xs">
                      <span className="font-mono">{run.runId}</span> · 事件 {run.eventId} · 状态 {run.state}
                      {formatDateTime(run.createdAt) ? ` · ${formatDateTime(run.createdAt)}` : ""}
                      {taskIds.length > 0 ? ` · 任务 ID：${taskIds.join("、")}` : " · 尚未生成任务 ID"}
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      ) : null}
    </Modal>
  );
}

export function WorkflowsPage() {
  const session = useSession();
  const [triggerKind, setTriggerKind] = useState("manual");
  const [matchKey, setMatchKey] = useState("");
  const [matchValue, setMatchValue] = useState("");
  const [nodeIds, setNodeIds] = useState("");
  const [actionId, setActionId] = useState<string>(ACTION_OPTIONS[0]);
  const [parameters, setParameters] = useState("");
  const [humanApproval, setHumanApproval] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [detailId, setDetailId] = useState<string | null>(null);

  const list = useResource(`workflows:${session.viewEpoch}`, signal => workflowsApi.list(signal).then(result => result.data));
  const canWrite = session.can("workflow.write");

  const createAction = useAction((body: Parameters<typeof workflowsApi.create>[0]) => workflowsApi.create(body));
  const toggleAction = useAction((workflow: WorkflowView) =>
    workflowsApi.update(workflow.id, { expectedRevision: workflow.revision, enabled: !workflow.enabled }),
  );
  const triggerAction = useAction((workflow: WorkflowView) => {
    const eventId = `wf-run-${Date.now().toString(36)}`;
    return workflowsApi.trigger(workflow.id, { eventId, kind: workflow.trigger });
  });

  async function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nodeIdsParsed = nodeIds.split(/[,，\s]+/).map(id => id.trim()).filter(Boolean);
    if (nodeIdsParsed.length === 0) return setLocalError("至少填写一个目标节点 ID。");
    if (new Set(nodeIdsParsed).size !== nodeIdsParsed.length) return setLocalError("目标节点 ID 不能重复（服务端会拒绝）。");
    const parsedParameters = parseParameterLines(parameters);
    if (typeof parsedParameters === "string") return setLocalError(parsedParameters);
    if (Boolean(matchKey) !== Boolean(matchValue)) return setLocalError("匹配条件的键和值必须同时填写或同时留空。");
    setLocalError(null);
    const result = await createAction.run({
      trigger: triggerKind,
      ...(matchKey && matchValue ? { match: { [matchKey.trim()]: matchValue.trim() } } : {}),
      steps: [{ actionId, nodeIds: nodeIdsParsed, parameters: parsedParameters, humanApproval }],
    });
    if (result.ok && result.data) {
      setOutcome(`工作流 ${result.data.data.id} 已创建（revision ${result.data.data.revision}，${result.data.data.enabled ? "启用" : "停用"}）。`);
      list.reload();
    }
  }

  async function toggle(workflow: WorkflowView) {
    const result = await toggleAction.run(workflow);
    if (result.ok && result.data) {
      setOutcome(`工作流 ${workflow.id} 已${result.data.data.enabled ? "启用" : "停用"}（revision ${result.data.data.revision}）。`);
      list.reload();
    }
  }

  async function trigger(workflow: WorkflowView) {
    const result = await triggerAction.run(workflow);
    if (result.ok && result.data) {
      const payload = result.data.data;
      if (!payload.matched || !payload.run) {
        setOutcome(`服务端返回 matched=false：事件与工作流触发器（${workflow.trigger}）不匹配，未创建运行。`);
        return;
      }
      const taskIds = runTaskIds(payload.run);
      setOutcome(
        `运行 ${payload.run.runId} 已触发（状态 ${payload.run.state}）${taskIds.length > 0 ? ` · 任务 ID：${taskIds.join("、")}` : " · 服务端尚未返回任务 ID"}。`,
      );
      list.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="自动化工作流"
        description="规则创建、启停与手动触发对接真实 Controller（GET/POST/PATCH /api/v1/workflows*）。步骤执行与人工审批始终经统一任务系统；本页不执行任何命令。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">自动化</span>}
        actions={<Button type="button" onClick={list.reload} loading={list.state.status === "loading"}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作结果" actions={<Button size="sm" onClick={() => setOutcome(null)}>关闭</Button>}>{outcome}</Notice> : null}
      {toggleAction.error ? <Notice tone="error" title="启停失败">{describeApiError(toggleAction.error)}</Notice> : null}
      {triggerAction.error ? <Notice tone="error" title="触发失败">{describeApiError(triggerAction.error)}</Notice> : null}
      {!canWrite ? (
        <Notice tone="warning" title="写操作已禁用">
          当前身份没有工作流写权限（后端键 `workflows.write`）；只能查看规则与运行记录。
        </Notice>
      ) : null}

      <Panel
        title="工作流列表"
        description="服务端返回的规则快照；启停切换带 expectedRevision 乐观锁，冲突时以服务端为准。"
      >
        <AsyncPanel
          state={list.state}
          onRetry={list.reload}
          errorTitle="工作流列表加载失败"
          empty={<EmptyState title="暂无工作流" description="使用下方表单创建第一条规则；创建会写审计。" />}
          isEmpty={data => data.items.length === 0}
        >
          {data => (
            <DataTable
              caption="自动化工作流"
              rows={data.items}
              rowKey={workflow => workflow.id}
              empty={<EmptyState title="暂无工作流" description="使用下方表单创建第一条规则；创建会写审计。" />}
              columns={[
                { key: "id", header: "标识", render: workflow => <span className="font-mono text-xs">{workflow.id}</span> },
                { key: "enabled", header: "状态", render: workflow => (workflow.enabled ? "启用" : "已停用") },
                { key: "trigger", header: "触发器", render: workflow => `${workflow.trigger}${workflow.match && Object.keys(workflow.match).length > 0 ? ` · ${JSON.stringify(workflow.match)}` : ""}` },
                { key: "revision", header: "版本", align: "right", render: workflow => `r${workflow.revision}` },
                {
                  key: "steps",
                  header: "步骤",
                  render: workflow => (
                    <ul className="space-y-0.5 text-xs">
                      {workflow.steps.map((step, index) => <li key={`${step.actionId}-${index}`}>{stepSummary(step)}</li>)}
                    </ul>
                  ),
                },
                {
                  key: "actions",
                  header: "操作",
                  render: workflow => (
                    <div className="flex flex-wrap gap-1">
                      <Button
                        size="sm"
                        disabled={!canWrite || session.writesLocked || toggleAction.pending}
                        onClick={() => void toggle(workflow)}
                        loading={toggleAction.pending}
                      >
                        {workflow.enabled ? "停用" : "启用"}
                      </Button>
                      <Button
                        size="sm"
                        variant="primary"
                        disabled={!canWrite || session.writesLocked || triggerAction.pending || !workflow.enabled}
                        onClick={() => void trigger(workflow)}
                        loading={triggerAction.pending}
                      >
                        触发运行
                      </Button>
                      <Button size="sm" onClick={() => setDetailId(workflow.id)}>详情</Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Panel
        title="创建工作流"
        description="POST /api/v1/workflows → 201。动作只接受服务端注册的 tasks.command / host.patch / host.root；参数是字符串键值对象。"
      >
        <form onSubmit={submitCreate} className="space-y-4 px-5 py-4">
          {createAction.error ? <Notice tone="error" title="创建失败">{describeApiError(createAction.error)}</Notice> : null}
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="触发器标识" hint="手动触发的工作流可用列表中的“触发运行”。">
              {ids => (
                <Select id={ids.inputId} value={triggerKind} onChange={event => setTriggerKind(event.target.value)}>
                  {TRIGGER_OPTIONS.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
                </Select>
              )}
            </Field>
            <Field label="动作（步骤 1）" hint="服务端固定允许的动作集合。">
              {ids => (
                <Select id={ids.inputId} value={actionId} onChange={event => setActionId(event.target.value)}>
                  {ACTION_OPTIONS.map(option => <option key={option} value={option}>{option}</option>)}
                </Select>
              )}
            </Field>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="匹配条件键（可选）" hint="触发事件需携带同名键值；留空表示不限制。">
              {ids => <TextInput id={ids.inputId} value={matchKey} onChange={event => setMatchKey(event.target.value)} />}
            </Field>
            <Field label="匹配条件值（可选）">
              {ids => <TextInput id={ids.inputId} value={matchValue} onChange={event => setMatchValue(event.target.value)} />}
            </Field>
          </div>
          <Field label="目标节点 ID" hint="逗号分隔；需要对这些节点具备所选动作的资源授权。">
            {ids => <TextInput id={ids.inputId} value={nodeIds} onChange={event => setNodeIds(event.target.value)} placeholder="node-alpha, node-beta" />}
          </Field>
          <Field label="参数（每行 key=value，可选）">
            {ids => <TextArea id={ids.inputId} rows={3} value={parameters} onChange={event => setParameters(event.target.value)} placeholder={"command=uptime"} />}
          </Field>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={humanApproval} onChange={event => setHumanApproval(event.target.checked)} />
            需要人工审批（任务提交后进入审批流，由任务中心执行）
          </label>
          <Button type="submit" variant="primary" loading={createAction.pending} disabled={!canWrite || session.writesLocked}>创建工作流</Button>
        </form>
      </Panel>

      {detailId ? <WorkflowDetail workflowId={detailId} onClose={() => setDetailId(null)} /> : null}
    </div>
  );
}
