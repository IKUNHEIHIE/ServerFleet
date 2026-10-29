import { useEffect, useMemo, useState, type FormEvent } from "react";
import { navigate } from "@/app/router";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { groupsApi, nodesApi, serviceGroupsApi, tasksApi } from "@/lib/api/endpoints";
import type { BatchTaskPreview, NodeGroup, ServiceGroup } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { TASK_STATE_LABEL, TERMINAL_STATES } from "./task-state";

type TargetType = "node_ids" | "node_group" | "service_group";

function CreateTaskModal({ open, onClose }: { readonly open: boolean; readonly onClose: () => void }) {
  const session = useSession();
  const [name, setName] = useState("");
  const [targetType, setTargetType] = useState<TargetType>("node_ids");
  const [selectedNodes, setSelectedNodes] = useState<ReadonlySet<string>>(new Set());
  const [targetGroupId, setTargetGroupId] = useState("");
  const [command, setCommand] = useState("");
  const [dryRun, setDryRun] = useState(true);
  const [dispatchMode, setDispatchMode] = useState<"sequential" | "batch" | "parallel">("sequential");
  const [preview, setPreview] = useState<BatchTaskPreview | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const nodes = useResource(`tasks:create:nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
    { enabled: open },
  );
  const groups = useResource(`tasks:create:groups:${session.viewEpoch}`, signal =>
    groupsApi.list(signal).then(result => result.data),
    { enabled: open && targetType === "node_group" },
  );
  const serviceGroups = useResource(`tasks:create:servicegroups:${session.viewEpoch}`, signal =>
    serviceGroupsApi.list(signal).then(result => result.data),
    { enabled: open && targetType === "service_group" },
  );
  const previewAction = useAction((body: Parameters<typeof tasksApi.preview>[0]) => tasksApi.preview(body));
  const createAction = useAction((body: Parameters<typeof tasksApi.create>[0]) => tasksApi.create(body));

  useEffect(() => {
    if (!open) return;
    setName("");
    setTargetType("node_ids");
    setSelectedNodes(new Set());
    setTargetGroupId("");
    setCommand("");
    setDryRun(true);
    setDispatchMode("sequential");
    setPreview(null);
    setLocalError(null);
  }, [open]);

  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const canSubmit = session.can("task.submit") || session.can("task.manage");

  function buildBody(): Parameters<typeof tasksApi.create>[0] | null {
    if (!name.trim()) {
      setLocalError("任务名称不能为空。");
      return null;
    }
    if (!command.trim()) {
      setLocalError("命令/脚本不能为空。");
      return null;
    }
    if (targetType === "node_ids" && selectedNodes.size === 0) {
      setLocalError("至少选择一个目标节点。");
      return null;
    }
    if (targetType !== "node_ids" && !targetGroupId) {
      setLocalError("必须选择目标分组或服务组。");
      return null;
    }
    return {
      name: name.trim(),
      target:
        targetType === "node_ids"
          ? { type: "node_ids" as const, nodeIds: [...selectedNodes] }
          : { type: targetType, groupId: targetGroupId },
      command,
      dryRun,
      dispatchMode,
    };
  }

  async function runPreview() {
    setLocalError(null);
    const body = buildBody();
    if (!body) return;
    setPreview(null);
    const result = await previewAction.run(body);
    if (result.ok && result.data) setPreview(result.data.data);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError(null);
    const body = buildBody();
    if (!body) return;
    const result = await createAction.run(body);
    if (result.ok && result.data) {
      const job = result.data.data;
      const id = job.jobId;
      if (!id) {
        setLocalError("服务端未返回任务 ID；不能伪造任务详情。");
        return;
      }
      onClose();
      navigate(`/tasks/${encodeURIComponent(id)}`);
    }
  }

  const pending = previewAction.pending || createAction.pending;

  return (
    <Modal
      open={open}
      title="创建任务"
      description="提交时由服务端冻结目标快照并重新鉴权；预演只做检查，不等于执行成功。"
      onClose={() => {
        if (!pending) onClose();
      }}
      wide
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={pending}>取消</Button>
          <Button type="button" onClick={runPreview} loading={previewAction.pending} disabled={pending || !canSubmit}>
            预演目标
          </Button>
          <Button type="submit" form="task-create" variant="primary" loading={createAction.pending} disabled={pending || !canSubmit}>
            提交任务
          </Button>
        </>
      }
    >
      <form id="task-create" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {(previewAction.error ?? createAction.error) ? (
          <Notice tone="error" title="操作失败">{describeApiError(previewAction.error ?? createAction.error)}</Notice>
        ) : null}
        {!canSubmit ? <Notice tone="warning" title="缺少任务提交权限">提交与预演保持禁用。</Notice> : null}
        <Field label="任务名称">
          {ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required />}
        </Field>
        <Field label="目标类型">
          {ids => (
            <Select
              id={ids.inputId}
              value={targetType}
              onChange={event => {
                setTargetType(event.target.value as TargetType);
                setTargetGroupId("");
                setPreview(null);
              }}
            >
              <option value="node_ids">指定节点</option>
              <option value="node_group">普通分组</option>
              <option value="service_group">服务组</option>
            </Select>
          )}
        </Field>

        {targetType === "node_ids" ? (
          <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="节点列表加载失败">
            {() => (
              <fieldset className="max-h-56 overflow-y-auto rounded-lg border px-4 py-3">
                <legend className="px-1 text-sm font-medium">目标节点（{selectedNodes.size} 已选）</legend>
                {nodeItems.map(node => (
                  <label key={node.id} className="mt-1 flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="size-4 accent-[var(--primary)]"
                      checked={selectedNodes.has(node.id)}
                      onChange={event => {
                        setSelectedNodes(current => {
                          const next = new Set(current);
                          if (event.target.checked) next.add(node.id);
                          else next.delete(node.id);
                          return next;
                        });
                        setPreview(null);
                      }}
                    />
                    {node.name.trim() || node.id} {node.online ? "" : "（离线）"}
                  </label>
                ))}
              </fieldset>
            )}
          </AsyncPanel>
        ) : targetType === "node_group" ? (
          <AsyncPanel state={groups.state} onRetry={groups.reload} errorTitle="分组加载失败" empty={<EmptyState title="没有普通分组" />} isEmpty={data => data.items.length === 0}>
            {data => (
              <Field label="普通分组">
                {ids => (
                  <Select id={ids.inputId} value={targetGroupId} onChange={event => { setTargetGroupId(event.target.value); setPreview(null); }}>
                    <option value="">请选择</option>
                    {data.items.map((group: NodeGroup) => <option key={group.id} value={group.id}>{group.name}</option>)}
                  </Select>
                )}
              </Field>
            )}
          </AsyncPanel>
        ) : (
          <AsyncPanel state={serviceGroups.state} onRetry={serviceGroups.reload} errorTitle="服务组加载失败" empty={<EmptyState title="没有服务组" />} isEmpty={data => data.items.length === 0}>
            {data => (
              <Field label="服务组">
                {ids => (
                  <Select id={ids.inputId} value={targetGroupId} onChange={event => { setTargetGroupId(event.target.value); setPreview(null); }}>
                    <option value="">请选择</option>
                    {data.items.map((group: ServiceGroup) => <option key={group.id} value={group.id}>{group.name}</option>)}
                  </Select>
                )}
              </Field>
            )}
          </AsyncPanel>
        )}

        <Field label="命令或脚本" hint="高风险/root 操作仍需服务端审批；客户端不能以 dryRun 代替真实授权。">
          {ids => <TextArea id={ids.inputId} value={command} onChange={event => setCommand(event.target.value)} required className="font-mono" />}
        </Field>
        <Notice tone="info" title="契约限制">
          当前创建 DTO 只提供 `dispatchMode` 与布尔 `dryRun`，没有 batchSize/canarySize/预演级别字段。页面不伪造这些参数；
          服务端安全计划接口（/tasks/safety-plan）若实现分批/金丝雀，需要额外契约协调。
        </Notice>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="执行策略">
            {ids => (
              <Select id={ids.inputId} value={dispatchMode} onChange={event => setDispatchMode(event.target.value as typeof dispatchMode)}>
                <option value="sequential">顺序/分批</option>
                <option value="batch">分批</option>
                <option value="parallel">并行（受服务端并发上限）</option>
              </Select>
            )}
          </Field>
          <label className="mt-6 flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={dryRun} onChange={event => setDryRun(event.target.checked)} />
            预演（dryRun）
          </label>
        </div>

        {preview ? (
          <div className="rounded-lg border bg-secondary/30 px-4 py-3 text-sm">
            <p className="font-medium">服务端预演结果（不执行）</p>
            <p className="mt-1">目标快照：{preview.targetNodeIdsSnapshot.length} 台</p>
            <p>可执行：{preview.eligibleNodeIds.length} 台</p>
            <p>被拒绝/不可用：{preview.deniedNodeIds.length} 台 {preview.deniedNodeIds.length ? `（${preview.deniedNodeIds.join("、")}）` : ""}</p>
            <p>需要审批：{preview.requiresApproval ? "是" : "否"}</p>
          </div>
        ) : null}
      </form>
    </Modal>
  );
}

export function TasksPage() {
  const session = useSession();
  const [filter, setFilter] = useState<"all" | "active" | "awaiting" | "finished">("all");
  const [createOpen, setCreateOpen] = useState(false);
  const tasks = useResource(`tasks:list:${session.viewEpoch}`, signal =>
    tasksApi.list({ limit: 100 }, signal).then(result => result.data),
  );
  const canSubmit = session.can("task.submit") || session.can("task.manage");
  const items = tasks.state.status === "ready" ? tasks.state.data.items : [];
  const visible = useMemo(() => {
    if (filter === "all") return items;
    if (filter === "awaiting") return items.filter(task => task.state === "awaiting_approval");
    if (filter === "finished") return items.filter(task => TERMINAL_STATES.includes(task.state));
    return items.filter(task => !TERMINAL_STATES.includes(task.state));
  }, [items, filter]);

  return (
    <div className="space-y-5">
      <PageHeader
        title="批量任务"
        description="统一任务中心：冻结目标、分批/金丝雀、审批、逐节点结果与 SSE 进度。取消不等于回滚，未知结果不自动重跑。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">统一任务</span>}
        actions={
          <>
            <Button type="button" onClick={tasks.reload} loading={tasks.state.status === "loading"}>刷新</Button>
            <Button type="button" variant="primary" disabled={!canSubmit || session.writesLocked} onClick={() => setCreateOpen(true)}>
              创建任务
            </Button>
          </>
        }
      />
      {!canSubmit ? <Notice tone="warning" title="提交入口已锁定">当前身份未返回任务提交权限（`task.submit`/`task.manage`）。</Notice> : null}
      <Panel
        title="任务历史"
        description="GET /api/v1/tasks；状态筛选只作用于已加载页。"
        actions={
          <label className="text-xs text-muted-foreground">
            状态
            <select
              value={filter}
              onChange={event => setFilter(event.target.value as typeof filter)}
              className="ml-2 h-8 rounded-md border border-input bg-background px-2 text-xs"
            >
              <option value="all">全部</option>
              <option value="active">进行中</option>
              <option value="awaiting">等待审批</option>
              <option value="finished">已结束</option>
            </select>
          </label>
        }
      >
        <AsyncPanel
          state={tasks.state}
          onRetry={tasks.reload}
          errorTitle="任务列表加载失败"
          empty={<EmptyState title="暂无任务" description="提交任务后会显示在这里。" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <DataTable
              caption="任务历史"
              rows={visible}
              rowKey={task => task.taskId}
              empty={<EmptyState title="没有匹配的任务" description="当前筛选没有命中已加载任务。" />}
              columns={[
                {
                  key: "id",
                  header: "任务",
                  render: task => (
                    <a href={`#/tasks/${encodeURIComponent(task.taskId)}`} className="font-mono text-xs text-primary underline-offset-2 hover:underline">
                      {task.taskId}
                    </a>
                  ),
                },
                { key: "state", header: "状态", render: task => TASK_STATE_LABEL[task.state] },
                { key: "targets", header: "目标快照", align: "right", render: task => `${task.targetNodeIdsSnapshot.length} 台` },
                { key: "created", header: "创建时间", render: task => formatDateTime(task.createdAt) ?? "—" },
                { key: "expires", header: "到期", render: task => formatDateTime(task.expiresAt) ?? "—" },
                { key: "by", header: "创建者", render: task => task.createdBy ?? "—" },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>
      <CreateTaskModal open={createOpen} onClose={() => setCreateOpen(false)} />
    </div>
  );
}
