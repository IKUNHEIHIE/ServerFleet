import { useRef, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { incidentsApi as api, type IncidentAction, type IncidentChecks, type IncidentDecision, type IncidentTaskReceipt, type OperatorIncident } from "@/lib/api/endpoints-operations";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const STATES: Record<OperatorIncident["state"], string> = { open: "待认领", investigating: "核查中", review_required: "待独立复核", resolved: "已人工结案" };
const OUTCOMES: Record<string, string> = { confirmed_succeeded: "人工确认已生效", confirmed_failed: "人工确认失败", confirmed_no_effect: "人工确认未生效", accepted_unresolved: "接受尚未查明的结果" };
const CATEGORIES: Record<string, string> = { actual_state: "实际资源状态", execution_log: "执行日志与痕迹", impact_check: "影响与补偿核对", containment: "风险控制措施" };
const TASKS: Record<string, string> = { awaiting_approval: "待独立审批", queued: "排队中", pending: "待执行", dispatched: "已派发", running: "执行中", cancelling: "取消中", completed: "已完成", succeeded: "成功", failed: "失败", unknown: "结果未知", partial_failure: "部分失败", canceled: "已取消", expired: "已过期", timed_out: "超时", unavailable: "当前无权读取或已不可用" };
const EMPTY_CHECKS: IncidentChecks = { actualStateChecked: false, executionLogsChecked: false, impactChecked: false, unknownAccepted: false, residualRisk: "" };
function blockerText(code: string): string {
  if (code.startsWith("evidence_required_")) return `缺少24小时内的「${CATEGORIES[code.slice("evidence_required_".length)] ?? "核验证据"}」`;
  if (code.startsWith("followup_pending_or_unavailable:")) return `后续任务尚未结束或不可读取：${code.split(":")[1]}`;
  return ({ fresh_reconciliation_required: "需要生成15分钟内的核验快照", facts_changed_reconcile_again: "任务或证据已变化，请重新核验" } as Record<string, string>)[code] ?? code;
}
function ReceiptTable({ rows, caption }: { readonly rows: readonly IncidentTaskReceipt[]; readonly caption: string }) {
  return <DataTable caption={caption} rows={rows} rowKey={r => r.taskId} columns={[
    { key: "task", header: "任务", render: r => <a className="break-all underline" href={`#/tasks/${encodeURIComponent(r.taskId)}`}>{r.taskId}</a> },
    { key: "state", header: "当前结果", render: r => TASKS[r.state] ?? r.state },
    { key: "counts", header: "逐目标汇总", render: r => Object.entries(r.results).map(([state, count]) => `${TASKS[state] ?? state}: ${count}`).join(" · ") || "—" },
    { key: "time", header: "更新时间", render: r => formatDateTime(r.updatedAt) ?? "—" },
  ]} />;
}
function localDateTime() { const now = new Date(); return new Date(now.getTime() - now.getTimezoneOffset() * 60_000).toISOString().slice(0, 16); }
function EvidenceLedger({ id, generation }: { readonly id: string; readonly generation: number }) {
  const session = useSession();
  const [after, setAfter] = useState(0);
  const page = useResource(`incident-evidence:${session.viewEpoch}:${id}:${after}:${generation}`, signal => api.evidence(id, after, signal).then(r => r.data));
  const data = page.state.status === "ready" ? page.state.data : null;
  return <section aria-label="证据与快照留档" className="space-y-2"><h3 className="font-medium">证据与快照留档</h3>
    {page.state.status === "loading" ? <p role="status">正在读取证据…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="证据读取失败">{describeApiError(page.state.error)}<Button onClick={page.reload}>重新读取证据</Button></Notice> : null}
    {data ? <><DataTable caption="不可覆盖的核验证据" rows={data.items} rowKey={e => e.id} columns={[
      { key: "kind", header: "类型", render: e => e.kind === "snapshot" ? "服务端事实快照" : CATEGORIES[e.category ?? ""] ?? "人工证据" },
      { key: "reference", header: "核验内容与引用", render: e => <div className="whitespace-pre-wrap break-all text-xs"><p>{e.note}</p><p>{e.reference}</p>{e.snapshot ? <p>快照摘要 {e.snapshot.digest} · 后续任务 {e.snapshot.followups.map(t => `${t.taskId}: ${TASKS[t.state] ?? t.state}`).join("、") || "无"}</p> : null}</div> },
      { key: "actor", header: "记录人／时间", render: e => `${e.actor} · ${formatDateTime(e.observedAt)}` },
    ]} /><div className="flex gap-2">{after ? <Button onClick={() => setAfter(0)}>返回最早证据</Button> : null}{data.nextAfter ? <Button onClick={() => setAfter(data.nextAfter)}>下一页证据</Button> : null}</div></> : null}
  </section>;
}
function IncidentDetail({ id, onChanged }: { readonly id: string; readonly onChanged: () => void }) {
  const session = useSession();
  const page = useResource(`incident:${session.viewEpoch}:${id}`, signal => api.get(id, signal).then(r => r.data), { refreshIntervalMs: 10_000 });
  const [generation, setGeneration] = useState(0);
  const [note, setNote] = useState(""); const [outcome, setOutcome] = useState("confirmed_no_effect");
  const [followup, setFollowup] = useState(""); const [category, setCategory] = useState("actual_state"); const [reference, setReference] = useState("");
  const [observedAt, setObservedAt] = useState(localDateTime); const [kind, setKind] = useState<IncidentAction>("note");
  const [checkInput, setCheckInput] = useState({ binding: "", checks: EMPTY_CHECKS }); const [reviewedBinding, setReviewedBinding] = useState("");
  const [validation, setValidation] = useState<string | null>(null);
  const [verificationTask, setVerificationTask] = useState<{ taskId: string; linked: boolean } | null>(null);
  const pendingRequest = useRef<{ binding: string; body: IncidentDecision } | null>(null);
  const pendingVerify = useRef<{ binding: string; requestId: string } | null>(null);
  const action = useAction((body: IncidentDecision) => api.decide(id, body));
  const verify = useAction((body: { expectedRevision: number; requestId: string; note: string }) => api.verifyBackup(id, body));
  const data = page.state.status === "ready" ? page.state.data : null; const assessment = data?.assessment;
  const own = data?.assigneeId === session.identity?.userId;
  const review = data?.state === "review_required" && Boolean(data.proposal?.observationDigest) && data.proposal?.proposedBy !== session.identity?.userId;
  const locked = session.writesLocked || action.pending || verify.pending || !data;
  const choices: readonly { value: IncidentAction; label: string; available: boolean }[] = [
    { value: "note", label: "补充处置说明", available: data?.state !== "resolved" },
    { value: "claim", label: "认领处置", available: data?.state === "open" },
    { value: "record_evidence", label: "登记结构化核验证据", available: own && (data?.state === "investigating" || data?.state === "review_required") },
    { value: "link_task", label: "关联新建的后续任务", available: data?.state === "investigating" && own },
    { value: "reconcile", label: "生成最新核验快照", available: own && (data?.state === "investigating" || data?.state === "review_required") },
    { value: "propose", label: "提交结论待复核", available: data?.state === "investigating" && own },
    { value: "approve", label: "复核通过并人工结案", available: review },
    { value: "reject", label: "驳回复核", available: data?.state === "review_required" && data.proposal?.proposedBy !== session.identity?.userId },
    { value: "release", label: "释放认领", available: data?.state === "investigating" && own },
    { value: "take_over", label: "接管并重新核查", available: data?.state === "investigating" || data?.state === "review_required" },
    { value: "reopen", label: "重新打开处置", available: data?.state === "resolved" },
  ];
  const available = choices.filter(v => v.available); const selected = available.some(c => c.value === kind) ? kind : available[0]?.value ?? "note";
  const reviewBinding = `${data?.revision}:${data?.proposal?.observationDigest ?? ""}`;
  const checkBinding = `${data?.cycle}:${data?.observationDigest ?? ""}`;
  const checks = checkInput.binding === checkBinding ? checkInput.checks : EMPTY_CHECKS;
  function changeChecks(patch: Partial<IncidentChecks>) { setCheckInput({ binding: checkBinding, checks: { ...checks, ...patch } }); }
  const blockers = assessment?.blockers ?? ["fresh_reconciliation_required"];
  const someFollowupUnclear = assessment?.current.followups.some(t => !t.terminal || t.state === "unknown" || ["unknown", "pending", "dispatched", "unknown_result_code"].some(k => (t.results[k] ?? 0) > 0)) ?? false;
  const someFollowupUnsuccessful = assessment?.current.followups.some(t => t.state !== "completed" || ["failed", "timed_out", "unknown", "pending", "dispatched", "unknown_result_code"].some(k => (t.results[k] ?? 0) > 0)) ?? false;
  const outcomeBlocked = outcome === "confirmed_failed" ? someFollowupUnclear : someFollowupUnsuccessful;
  const proposalBlocked = blockers.length > 0 || !checks.actualStateChecked || !checks.executionLogsChecked || !checks.impactChecked || (outcome === "accepted_unresolved" ? !checks.unknownAccepted || !checks.residualRisk?.trim() || !assessment?.current.evidenceCategories.includes("containment") : outcomeBlocked);
  function refresh() { page.reload(); setGeneration(n => n + 1); onChanged(); }
  async function submit(event: FormEvent) {
    event.preventDefault(); if (!data || !note.trim()) return; setValidation(null);
    if (selected === "record_evidence" && Number.isNaN(Date.parse(observedAt))) { setValidation("请填写有效的实际核验时间。"); return; }
    const input: Omit<IncidentDecision, "requestId"> = { expectedRevision: data.revision, action: selected, note,
      ...(selected === "propose" ? { outcome, observationDigest: data.observationDigest, checks: { ...checks, unknownAccepted: outcome === "accepted_unresolved" && checks.unknownAccepted, residualRisk: outcome === "accepted_unresolved" ? checks.residualRisk : "" } } : {}),
      ...(selected === "approve" ? { observationDigest: data.proposal?.observationDigest, reviewAcknowledged: reviewedBinding === reviewBinding } : {}),
      ...(selected === "link_task" ? { followupTaskId: followup.trim() } : {}),
      ...(selected === "record_evidence" ? { evidenceCategory: category, evidenceReference: reference.trim(), observedAt: new Date(observedAt).toISOString() } : {}),
    };
    const binding = JSON.stringify(input); const body = pendingRequest.current?.binding === binding ? pendingRequest.current.body : { ...input, requestId: crypto.randomUUID() }; pendingRequest.current = { binding, body };
    const result = await action.run(body);
    if (result.ok) { pendingRequest.current = null; setNote(""); setFollowup(""); setCheckInput({ binding: "", checks: EMPTY_CHECKS }); refresh(); }
  }
  async function verifyBackup() {
    if (!data || !note.trim()) return; const input = { expectedRevision: data.revision, note }; const binding = JSON.stringify(input);
    const requestId = pendingVerify.current?.binding === binding ? pendingVerify.current.requestId : crypto.randomUUID(); pendingVerify.current = { binding, requestId };
    const result = await verify.run({ ...input, requestId }); if (result.ok && result.data) { setVerificationTask(result.data.data); pendingVerify.current = null; refresh(); }
  }
  return <section className="space-y-4 rounded-lg border p-4" aria-label="人工处置详情">
    <Button onClick={refresh} disabled={action.pending || verify.pending}>重新读取处置与证据</Button>
    <p className="text-sm">认领 → 登记三类证据 → 核对后续任务 → 生成快照 → 提交结论 → 独立管理员复核</p>
    {page.state.status === "loading" ? <p role="status">正在读取处置记录…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="无法读取处置">{describeApiError(page.state.error)}</Notice> : null}
    {page.state.status === "ready" && page.state.refreshError ? <Notice tone="warning" title="处置刷新失败">{describeApiError(page.state.refreshError)}，请重新读取后操作。</Notice> : null}
    {validation ? <Notice tone="error" title="无法提交">{validation}</Notice> : null}
    {[action.error, verify.error].map((error, index) => error ? <Notice key={index} tone="error" title="处置未确认">{describeApiError(error)}。先刷新核对；未确认的相同请求保留编号。</Notice> : null)}
    {verificationTask ? <Notice tone={verificationTask.linked ? "info" : "warning"} title="只读核验任务已登记"><a className="underline" href={`#/tasks/${encodeURIComponent(verificationTask.taskId)}`}>{verificationTask.taskId}</a>{verificationTask.linked ? " · 已关联，等待任务结果后重新生成快照。" : " · 关联发生冲突，请将此任务 ID 手动关联到处置记录。"}</Notice> : null}
    {data ? <>
      <p className="break-all text-sm">{data.id} · 修订 {data.revision} · {STATES[data.state]} · 负责人 {data.assigneeId ?? "未认领"}</p>
      <Notice tone="warning" title={`原始结果：${data.source.state}`}>人工结案记录人工认定，保留原任务／AI 运行结果，不重放操作。请记录核对结论和日志引用，不要粘贴秘密。</Notice>
      {assessment?.sourceChanged ? <Notice tone="warning" title="来源已有新结果">原始记录仍保留；当前结果为 {assessment.current.source.state}。请基于最新事实重新核查。</Notice> : null}
      {assessment?.needsRecheck ? <Notice tone="warning" title="结案依据需要重新核查">来源、证据或后续任务已变化，旧结论不可直接复用；后台会重新打开处置，也可主动重新核查。</Notice> : null}
      <p className="break-all text-xs">来源 {data.source.kind} / {data.source.id} · {data.source.action} · {data.source.reason}</p>
      {data.source.taskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(data.source.taskId)}`}>查看原任务与逐节点回执</a> : <p className="text-xs">AI 只显示运行标识和状态，私人聊天由会话所属用户查看。</p>}
      {assessment?.current.sourceTask ? <ReceiptTable caption="原任务当前事实" rows={[assessment.current.sourceTask]} /> : null}
      <ReceiptTable caption="后续任务当前结果" rows={assessment?.current.followups ?? []} />
      {data.state !== "resolved" && blockers.length ? <Notice tone="warning" title="结案前待完成事项"><ul>{blockers.map(code => <li key={code}>{blockerText(code)}</li>)}</ul></Notice> : null}
      {data.observationDigest ? <p className="break-all text-xs">核验快照 {data.observationDigest} · {formatDateTime(assessment?.observedAt)} · {assessment?.observationFresh ? "仍在15分钟复核期限内" : "已过复核期限，历史证据继续保留"}</p> : null}
      {data.proposal ? <Notice tone="info" title={`${data.state === "resolved" ? "已归档人工结论" : "待复核人工结论"}：${OUTCOMES[data.proposal.outcome] ?? data.proposal.outcome}`}><p>提出人：{data.proposal.proposedBy}</p><p className="whitespace-pre-wrap">{data.proposal.evidence}</p><p>已确认：实际状态 {data.proposal.checks?.actualStateChecked ? "是" : "否"} · 执行痕迹 {data.proposal.checks?.executionLogsChecked ? "是" : "否"} · 影响核对 {data.proposal.checks?.impactChecked ? "是" : "否"}</p>{data.proposal.checks?.residualRisk ? <p className="whitespace-pre-wrap">明确接受的剩余风险：{data.proposal.checks.residualRisk}</p> : null}</Notice> : null}
      <form onSubmit={submit} className="space-y-3"><fieldset disabled={locked} className="space-y-3"><legend className="font-medium">记录人工处置</legend>
        <Field label="处置动作">{ids => <Select id={ids.inputId} value={selected} onChange={e => setKind(e.target.value as IncidentAction)}>{available.map(c => <option value={c.value} key={c.value}>{c.label}</option>)}</Select>}</Field>
        {selected === "record_evidence" ? <div className="grid gap-3 sm:grid-cols-2"><Field label="证据类别">{ids => <Select id={ids.inputId} value={category} onChange={e => setCategory(e.target.value)}>{Object.entries(CATEGORIES).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select>}</Field><Field label="证据引用" hint="例如任务回执、日志文件与行号、监控记录；不会自动访问该地址。">{ids => <TextInput id={ids.inputId} value={reference} onChange={e => setReference(e.target.value)} maxLength={512} required />}</Field><Field label="实际核验时间">{ids => <TextInput id={ids.inputId} type="datetime-local" required value={observedAt} onChange={e => setObservedAt(e.target.value)} />}</Field></div> : null}
        {selected === "reconcile" ? <Notice tone="info" title="收集最新事实">读取原任务和已关联任务的当前结果，冻结证据摘要；不运行命令或重试原操作。已有提案将撤回，需重新提交复核。</Notice> : null}
        {selected === "propose" ? <div className="space-y-3"><Field label="人工结论">{ids => <Select id={ids.inputId} value={outcome} onChange={e => setOutcome(e.target.value)}>{Object.entries(OUTCOMES).map(([v, label]) => <option key={v} value={v}>{label}</option>)}</Select>}</Field>{([ ["actualStateChecked", "已核对实际资源状态"], ["executionLogsChecked", "已检查执行日志与痕迹"], ["impactChecked", "已核对影响范围与补偿结果"] ] as const).map(([key, label]) => <label className="block" key={key}><input type="checkbox" checked={checks[key]} onChange={e => changeChecks({ [key]: e.target.checked })} /> {label}</label>)}{outcome === "accepted_unresolved" ? <><label className="block"><input type="checkbox" checked={checks.unknownAccepted} onChange={e => changeChecks({ unknownAccepted: e.target.checked })} /> 已明确接受未查明的风险，并登记风险控制措施</label><Field label="剩余风险与接受理由">{ids => <TextArea id={ids.inputId} value={checks.residualRisk} maxLength={2048} required onChange={e => changeChecks({ residualRisk: e.target.value })} />}</Field></> : outcomeBlocked ? <Notice tone="warning" title="后续任务存在未成功结果">先完成处置或选择接受尚未查明的结果，并补齐控制措施及明确风险。</Notice> : null}</div> : null}
        {selected === "link_task" ? <Field label="新建后续任务 ID" hint="须晚于本处置创建且属于原资源范围；关联不赋予审批或执行权限。">{ids => <TextInput id={ids.inputId} required value={followup} onChange={e => setFollowup(e.target.value)} />}</Field> : null}
        {selected === "approve" ? <label className="block"><input type="checkbox" checked={reviewedBinding === reviewBinding} onChange={e => setReviewedBinding(e.target.checked ? reviewBinding : "")} /> 已独立核对本次提案的证据、快照与后续任务结果</label> : null}
        <Field label="核验证据与处置说明">{ids => <TextArea id={ids.inputId} required maxLength={4096} value={note} onChange={e => setNote(e.target.value)} />}</Field>
        <Button type="submit" disabled={!note.trim() || (selected === "link_task" && !followup.trim()) || (selected === "record_evidence" && (!reference.trim() || !observedAt)) || (selected === "propose" && proposalBlocked) || (selected === "approve" && (blockers.length > 0 || reviewedBinding !== reviewBinding))} loading={action.pending}>提交人工处置</Button>
        {data.source.action === "backups.upload" && data.state === "investigating" && own ? <Button type="button" disabled={!note.trim()} loading={verify.pending} onClick={() => void verifyBackup()}>创建并关联备份只读核验任务</Button> : null}
      </fieldset></form>
      <EvidenceLedger key={id} id={id} generation={generation} />
      <DataTable caption="不可覆盖的处置历史" rows={data.events} rowKey={e => String(e.sequence)} columns={[
        { key: "action", header: "动作", render: e => e.action }, { key: "actor", header: "操作人", render: e => e.actor }, { key: "at", header: "时间", render: e => formatDateTime(e.at) }, { key: "note", header: "说明", render: e => <span className="whitespace-pre-wrap">{e.note}</span> },
      ]} />
    </> : null}
  </section>;
}
function IncidentContent({ taskId }: { readonly taskId?: string }) {
  const session = useSession(); const [selected, setSelected] = useState<string | null>(null); const [state, setState] = useState("active"); const [sourceKind, setSourceKind] = useState(""); const [cursors, setCursors] = useState<readonly string[]>([""]);
  const cursor = cursors[cursors.length - 1];
  const page = useResource(`incidents:${session.viewEpoch}:${taskId ?? "all"}:${state}:${sourceKind}:${cursor}`, signal => api.list(signal, { state, sourceKind: sourceKind || undefined, taskId, cursor: cursor || undefined, limit: 50 }).then(r => r.data));
  const sync = useAction(() => api.sync()); const items = page.state.status === "ready" ? page.state.data.items : []; const nextCursor = page.state.status === "ready" ? page.state.data.nextCursor : undefined;
  return <div className="space-y-4 px-5 py-4">
    <div className="flex flex-wrap gap-3"><Button onClick={page.reload}>刷新处置列表</Button><Button disabled={session.writesLocked} loading={sync.pending} onClick={async () => { const r = await sync.run(); if (r.ok) page.reload(); }}>收集最新异常</Button><Field label="处置状态">{ids => <Select id={ids.inputId} value={state} onChange={e => { setState(e.target.value); setCursors([""]); setSelected(null); }}><option value="active">全部待处置</option><option value="all">包含已结案</option>{Object.entries(STATES).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</Select>}</Field><Field label="异常来源">{ids => <Select id={ids.inputId} value={sourceKind} onChange={e => { setSourceKind(e.target.value); setCursors([""]); setSelected(null); }}><option value="">所有来源</option><option value="task">统一任务</option><option value="ai_run">AI 运行</option></Select>}</Field></div>
    {page.state.status === "loading" ? <p role="status">正在读取异常处置…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="异常处置不可用">{describeApiError(page.state.error)}</Notice> : null}
    {sync.error ? <Notice tone="error" title="异常收集失败">{describeApiError(sync.error)}</Notice> : null}
    {page.state.status === "ready" ? items.length ? <DataTable caption="异常与 UNKNOWN 人工处置" rows={items} rowKey={i => i.id} columns={[
      { key: "source", header: "来源", render: i => <span className="break-all text-xs">{i.source.kind} · {i.source.id}</span> }, { key: "state", header: "原始结果／处置", render: i => `${i.source.state} / ${STATES[i.state]}${i.assessment?.needsRecheck ? " · 待重新核查" : ""}` }, { key: "owner", header: "负责人", render: i => i.assigneeId ?? "未认领" }, { key: "open", header: "操作", render: i => <Button size="sm" onClick={() => setSelected(i.id)}>查看与处置</Button> },
    ]} /> : <p>当前页没有可见记录。异常按分钟收集，也可手动收集；如有下一页，可继续查看。</p> : null}
    <div className="flex gap-2">{cursors.length > 1 ? <Button onClick={() => setCursors(c => c.slice(0, -1))}>上一页处置</Button> : null}{nextCursor ? <Button onClick={() => setCursors(c => [...c, nextCursor])}>下一页处置</Button> : null}</div>
    {selected ? <IncidentDetail key={`${session.viewEpoch}:${selected}`} id={selected} onChanged={page.reload} /> : null}
  </div>;
}
export function IncidentPanel({ taskId }: { readonly taskId?: string }) {
  const session = useSession(); const [open, setOpen] = useState(false); const allowed = session.can("settings.manage") || session.can("task.manage") || session.can("user.manage");
  return <Panel title="异常与 UNKNOWN 人工处置" description="证据留档、事实核验、后续任务对账与独立复核；原始结果保留。"><div className="px-5 py-4"><Button disabled={!allowed} aria-expanded={open} onClick={() => setOpen(v => !v)}>{open ? "收起人工处置" : "打开人工处置"}</Button></div>{open && allowed ? <IncidentContent key={session.viewEpoch} taskId={taskId} /> : null}</Panel>;
}
