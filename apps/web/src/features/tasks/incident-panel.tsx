import { useRef, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { incidentsApi as api, type IncidentAction, type IncidentDecision, type OperatorIncident } from "@/lib/api/endpoints-operations";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const STATES: Record<OperatorIncident["state"], string> = { open: "待认领", investigating: "核查中", review_required: "待独立复核", resolved: "已人工结案" };
const OUTCOMES: Record<string, string> = { confirmed_succeeded: "人工确认已生效", confirmed_failed: "人工确认失败", confirmed_no_effect: "人工确认未生效", accepted_unresolved: "接受尚未查明的结果" };
function IncidentDetail({ id, onChanged }: { readonly id: string; readonly onChanged: () => void }) {
  const session = useSession();
  const page = useResource(`incident:${session.viewEpoch}:${id}`, signal => api.get(id, signal).then(r => r.data));
  const [note, setNote] = useState("");
  const [outcome, setOutcome] = useState("confirmed_no_effect");
  const [followup, setFollowup] = useState("");
  const [kind, setKind] = useState<IncidentAction>("note");
  const pendingRequest = useRef<{ binding: string; body: IncidentDecision } | null>(null);
  const action = useAction((body: IncidentDecision) => api.decide(id, body));
  const data = page.state.status === "ready" ? page.state.data : null;
  const own = data?.assigneeId === session.identity?.userId;
  const review = data?.state === "review_required" && data.proposal?.proposedBy !== session.identity?.userId;
  const locked = session.writesLocked || action.pending || !data;
  const choices: readonly { value: IncidentAction; label: string; available: boolean }[] = [
    { value: "note", label: "补充核验记录", available: data?.state !== "resolved" },
    { value: "claim", label: "认领处置", available: data?.state === "open" },
    { value: "release", label: "释放认领", available: data?.state === "investigating" && own },
    { value: "take_over", label: "接管并重新核查", available: data?.state === "investigating" || data?.state === "review_required" },
    { value: "propose", label: "提交结论待复核", available: data?.state === "investigating" && own },
    { value: "approve", label: "复核通过并人工结案", available: review },
    { value: "reject", label: "驳回复核", available: review },
    { value: "reopen", label: "重新打开处置", available: data?.state === "resolved" },
    { value: "link_task", label: "关联新建的后续任务", available: data?.state !== "resolved" },
  ];
  const available = choices.filter(v => v.available);
  const selected = available.some(c => c.value === kind) ? kind : available[0]?.value ?? "note";
  async function submit(event: FormEvent) {
    event.preventDefault(); if (!data || !note.trim()) return;
    const input = { expectedRevision: data.revision, action: selected, note, ...(selected === "propose" ? { outcome } : {}), ...(selected === "link_task" ? { followupTaskId: followup.trim() } : {}) };
    const binding = JSON.stringify(input);
    const body = pendingRequest.current?.binding === binding ? pendingRequest.current.body : { ...input, requestId: crypto.randomUUID() };
    pendingRequest.current = { binding, body };
    const result = await action.run(body);
    if (result.ok) { pendingRequest.current = null; setNote(""); setFollowup(""); page.reload(); onChanged(); }
  }
  return <section className="space-y-3 rounded-lg border p-4" aria-label="人工处置详情">
    <Button onClick={page.reload} disabled={action.pending}>重新读取处置详情</Button>
    {page.state.status === "loading" ? <p role="status">正在读取处置记录…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="无法读取处置">{describeApiError(page.state.error)}</Notice> : null}
    {action.error ? <Notice tone="error" title="处置未确认">{describeApiError(action.error)}。请刷新核对；原请求不会自动生成新的结论。</Notice> : null}
    {data ? <>
      <p className="break-all text-sm">{data.id} · 修订 {data.revision} · {STATES[data.state]} · 负责人 {data.assigneeId ?? "未认领"}</p>
      <Notice tone="warning" title={`原始结果：${data.source.state}`}>人工结案只记录人工判断，不改写原任务或 AI 运行结果，也不会重放操作。请通过资源状态、日志、备份校验回执等证据核查；不要粘贴密码、私钥或令牌。</Notice>
      <p className="break-all text-xs">来源 {data.source.kind} / {data.source.id} · {data.source.action} · {data.source.reason}</p>
      {data.source.taskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(data.source.taskId)}`}>查看原任务与逐节点回执</a> : null}
      {data.source.kind === "ai_run" ? <p className="text-xs">此处仅显示 AI 运行标识和状态；私人聊天仍由会话所属用户查看。</p> : null}
      {data.proposal ? <Notice tone="info" title={`待复核人工结论：${OUTCOMES[data.proposal.outcome] ?? data.proposal.outcome}`}><p>提出人：{data.proposal.proposedBy}</p><p className="whitespace-pre-wrap">{data.proposal.evidence}</p></Notice> : null}
      {data.followupTaskIds.length ? <ul>{data.followupTaskIds.map(task => <li key={task}><a className="underline" href={`#/tasks/${encodeURIComponent(task)}`}>后续任务 {task}</a></li>)}</ul> : null}
      <form onSubmit={submit} className="space-y-3">
        <fieldset disabled={locked} className="space-y-3">
          <legend className="font-medium">记录人工处置</legend>
          <Field label="处置动作">{ids => <Select id={ids.inputId} value={selected} onChange={e => setKind(e.target.value as IncidentAction)}>{available.map(c => <option value={c.value} key={c.value}>{c.label}</option>)}</Select>}</Field>
          {selected === "propose" ? <Field label="人工结论">{ids => <Select id={ids.inputId} value={outcome} onChange={e => setOutcome(e.target.value)}>{Object.entries(OUTCOMES).map(([v, label]) => <option key={v} value={v}>{label}</option>)}</Select>}</Field> : null}
          {selected === "link_task" ? <Field label="新建后续任务 ID" hint="先通过对应功能创建核验／补偿任务，再填入 ID；关联不会赋予审批或执行权限。">{ids => <TextInput id={ids.inputId} required value={followup} onChange={e => setFollowup(e.target.value)} />}</Field> : null}
          <Field label="核验证据与处置说明">{ids => <TextArea id={ids.inputId} required maxLength={4096} value={note} onChange={e => setNote(e.target.value)} />}</Field>
          <Button type="submit" disabled={!note.trim() || (selected === "link_task" && !followup.trim())} loading={action.pending}>提交人工处置</Button>
        </fieldset>
      </form>
      <DataTable caption="不可覆盖的处置历史" rows={data.events} rowKey={e => String(e.sequence)} columns={[
        { key: "action", header: "动作", render: e => e.action }, { key: "actor", header: "操作人", render: e => e.actor }, { key: "at", header: "时间", render: e => formatDateTime(e.at) }, { key: "note", header: "说明", render: e => <span className="whitespace-pre-wrap">{e.note}</span> },
      ]} />
    </> : null}
  </section>;
}
function IncidentContent({ taskId }: { readonly taskId?: string }) {
  const session = useSession();
  const page = useResource(`incidents:${session.viewEpoch}`, signal => api.list(signal).then(r => r.data));
  const sync = useAction(() => api.sync());
  const [selected, setSelected] = useState<string | null>(null);
  const [showResolved, setShowResolved] = useState(false);
  const items = page.state.status === "ready" ? page.state.data.items.filter(i => (!taskId || i.source.taskId === taskId) && (showResolved || i.state !== "resolved")) : [];
  return <div className="space-y-4 px-5 py-4">
    <div className="flex flex-wrap gap-3"><Button onClick={page.reload}>刷新处置列表</Button><Button disabled={session.writesLocked} loading={sync.pending} onClick={async () => { const r = await sync.run(); if (r.ok) page.reload(); }}>收集最新异常</Button><label><input type="checkbox" checked={showResolved} onChange={e => setShowResolved(e.target.checked)} /> 显示已结案</label></div>
    {page.state.status === "loading" ? <p role="status">正在读取异常处置…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="异常处置不可用">{describeApiError(page.state.error)}</Notice> : null}
    {sync.error ? <Notice tone="error" title="异常收集失败">{describeApiError(sync.error)}</Notice> : null}
    {page.state.status === "ready" ? items.length ? <DataTable caption="异常与 UNKNOWN 人工处置" rows={items} rowKey={i => i.id} columns={[
      { key: "source", header: "来源", render: i => <span className="break-all text-xs">{i.source.kind} · {i.source.id}</span> },
      { key: "state", header: "原始结果／处置", render: i => `${i.source.state} / ${STATES[i.state]}` },
      { key: "owner", header: "负责人", render: i => i.assigneeId ?? "未认领" },
      { key: "open", header: "操作", render: i => <Button size="sm" onClick={() => setSelected(i.id)}>查看与处置</Button> },
    ]} /> : <p>当前筛选下没有待处置记录。异常会按分钟收集，也可以手动收集。</p> : null}
    {selected ? <IncidentDetail key={`${session.viewEpoch}:${selected}`} id={selected} onChanged={page.reload} /> : null}
  </div>;
}
export function IncidentPanel({ taskId }: { readonly taskId?: string }) {
  const session = useSession();
  const [open, setOpen] = useState(false);
  const allowed = session.can("settings.manage") || session.can("task.manage") || session.can("user.manage");
  return <Panel title="异常与 UNKNOWN 人工处置" description="认领 → 核验留证 → 提交结论 → 独立管理员复核；原始结果保留。"><div className="px-5 py-4"><Button disabled={!allowed} aria-expanded={open} onClick={() => setOpen(v => !v)}>{open ? "收起人工处置" : "打开人工处置"}</Button></div>{open && allowed ? <IncidentContent key={session.viewEpoch} taskId={taskId} /> : null}</Panel>;
}
