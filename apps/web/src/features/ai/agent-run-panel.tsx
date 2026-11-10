import { useRef, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import type { AISession } from "@/lib/api/dto";
import { aiAgentApi, aiApi, type AgentRun, type AgentRunInput, type AgentRunState, type AgentStep } from "@/lib/api/endpoints-ai";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const TERMINAL = new Set<AgentRunState>(["completed", "failed", "canceled", "unknown", "limit_reached"]);
const STATE_LABEL: Record<AgentRunState, string> = {
  queued: "准备下一步", thinking: "模型规划中", waiting_approval: "等待工具审批", submitting: "提交任务中",
  waiting_task: "等待任务结果", canceling: "等待取消确认", completed: "已完成", failed: "已停止",
  canceled: "已取消", unknown: "结果未知，已停止", limit_reached: "达到步数或上下文上限",
};

export function AgentRunPanel({ session }: { readonly session: AISession }) {
  const appSession = useSession();
  const [selected, setSelected] = useState("");
  const [goal, setGoal] = useState("");
  const [maxSteps, setMaxSteps] = useState(8);
  const [timeoutSeconds, setTimeoutSeconds] = useState(900);
  // Keep the exact key+body after an ambiguous HTTP outcome. A retry can only
  // recover this same durable run, never silently create another side effect.
  const pendingInput = useRef<AgentRunInput | null>(null);
  const [uncertain, setUncertain] = useState(false);
  const canUse = appSession.can("ai.use") || appSession.can("ai.manage");
  const locked = !canUse || appSession.writesLocked || session.status !== "active";
  const runs = useResource(`ai:runs:${session.id}:${session.scopeRevision}:${appSession.viewEpoch}`, signal =>
    aiAgentApi.list(session.id, signal).then(result => result.data.items), { refreshIntervalMs: 4000 });
  const create = useAction((input: AgentRunInput) => aiAgentApi.create(session.id, input));

  async function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!pendingInput.current) {
      pendingInput.current = { goal: goal.trim(), clientRunId: globalThis.crypto.randomUUID(), scopeRevision: session.scopeRevision, maxSteps, timeoutSeconds };
    }
    const result = await create.run(pendingInput.current);
    if (result.ok && result.data) {
      setSelected(result.data.data.id); pendingInput.current = null; setUncertain(false); setGoal(""); runs.refreshSilent();
    } else { setUncertain(true); runs.reload(); }
  }

  return (
    <div className="space-y-5">
      <Panel title="发起 Agent 运维" description="描述目标后，Agent 会选择工具、等待审批、读取真实结果，再决定下一步。关闭页面后本次运行仍可在主控内继续。">
        <form onSubmit={start} className="space-y-4 px-5 py-4">
          <Field label="运维目标" hint="可查看已授权节点、运行固定自检或已安装的命令配置。命令需提供 profileId 和策略摘要；缺少配置时 Agent 会说明原因。不要粘贴密钥。">
            {ids => <TextArea id={ids.inputId} aria-describedby={ids.hintId} value={goal} onChange={event => setGoal(event.target.value)} maxLength={8000} required disabled={locked || create.pending || uncertain} placeholder="例如：检查已授权节点的运行状态，对异常节点执行自检，然后汇总结果。" />}
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="最多规划步数">{ids => <Select id={ids.inputId} value={maxSteps} disabled={locked || create.pending || uncertain} onChange={event => setMaxSteps(Number(event.target.value))}>{[4, 8, 12].map(value => <option key={value} value={value}>{value} 步</option>)}</Select>}</Field>
            <Field label="运行期限（含等待审批）">{ids => <Select id={ids.inputId} value={timeoutSeconds} disabled={locked || create.pending || uncertain} onChange={event => setTimeoutSeconds(Number(event.target.value))}>{[300, 900, 1800, 3600].map(value => <option key={value} value={value}>{value / 60} 分钟</option>)}</Select>}</Field>
          </div>
          <p className="text-sm text-muted-foreground">授权节点：{session.allowedNodeIds?.length ?? 0} 台。资源变更会使旧运行失效；执行和结果读取均由后端重新校验权限。</p>
          {create.error ? <Notice tone="error" title="发起请求未确认">{describeApiError(create.error)} 请先查看下方运行列表；再次提交使用同一请求编号和原始目标。</Notice> : null}
          <Button type="submit" variant="primary" loading={create.pending} disabled={locked || (!goal.trim() && !uncertain)}>{uncertain ? "核对并继续同一次请求" : "开始运行"}</Button>
        </form>
      </Panel>
      <Panel title="运行记录" description="仅显示当前资源修订下仍有权查看的运行。未知副作用不会自动重试。" actions={<Button type="button" onClick={runs.reload}>刷新记录</Button>}>
        <AsyncPanel state={runs.state} onRetry={runs.reload} errorTitle="运行记录读取失败" isEmpty={items => items.length === 0} empty={<EmptyState title="暂无运行" description="先在资源授权页签分配节点与动作，再发起运维目标。" />}>
          {items => <div className="flex flex-wrap gap-2 px-5 py-4">{items.map(run => <Button type="button" key={run.id} aria-pressed={selected === run.id} onClick={() => setSelected(run.id)}>{run.goal.slice(0, 32)} · {STATE_LABEL[run.state]}</Button>)}</div>}
        </AsyncPanel>
      </Panel>
      {selected ? <AgentRunDetail key={`${selected}:${appSession.viewEpoch}`} sessionId={session.id} runId={selected} locked={locked} onChanged={runs.refreshSilent} /> : null}
    </div>
  );
}

function AgentRunDetail({ sessionId, runId, locked, onChanged }: { readonly sessionId: string; readonly runId: string; readonly locked: boolean; readonly onChanged: () => void }) {
  const run = useResource(`ai:run:${sessionId}:${runId}`, signal => aiAgentApi.get(sessionId, runId, signal).then(result => result.data), {
    refreshIntervalMs: 2000, shouldPoll: value => !TERMINAL.has((value as AgentRun).state),
  });
  const advance = useAction((revision: number) => aiAgentApi.advance(sessionId, runId, revision));
  const cancel = useAction((revision: number) => aiAgentApi.cancel(sessionId, runId, revision));
  const decide = useAction((approvalId: string, decision: "approve" | "deny") => aiApi.decideApproval(sessionId, approvalId, { decision }));
  const pending = advance.pending || cancel.pending || decide.pending;
  const error = advance.error ?? cancel.error ?? decide.error;
  async function act(action: () => Promise<unknown>) { await action(); run.refreshSilent(); onChanged(); }

  return (
    <Panel title="执行过程" actions={<Button type="button" onClick={run.reload}>刷新详情</Button>}>
      {error ? <Notice tone="error" title="操作未完成">{describeApiError(error)} 状态已重新读取，请核对后再操作。</Notice> : null}
      <AsyncPanel state={run.state} onRetry={run.reload} errorTitle="运行详情读取失败">
        {data => {
          const terminal = TERMINAL.has(data.state);
          const last = data.steps.at(-1);
          const stale = run.state.status === "ready" && run.state.refreshError !== null;
          return <div className="space-y-4 px-5 py-4">
            <p className="whitespace-pre-wrap text-sm">{data.goal}</p>
            <div role="status" aria-live="polite" className="flex flex-wrap gap-3 text-sm">
              <strong>{STATE_LABEL[data.state]}</strong><span>{data.steps.length} / {data.maxSteps} 步</span><span>截至 {formatDateTime(data.expiresAt) ?? "—"}</span>
            </div>
            {!terminal && !data.driverActive ? <Notice tone="warning" title="运行已暂停">主控重启或当前授权上下文结束后，需要点击继续。正在规划或提交时中断的步骤会停止并标记结果未知。</Notice> : null}
            {data.state === "unknown" ? <Notice tone="warning" title="请人工核对关联任务">已有动作可能执行。此运行不会重放步骤；核对实际结果后再发起新的目标。</Notice> : null}
            {!terminal ? <div className="flex flex-wrap gap-2">
              {!data.driverActive ? <Button type="button" variant="primary" loading={advance.pending} disabled={locked || pending || stale} onClick={() => void act(() => advance.run(data.revision))}>继续运行</Button> : null}
              <Button type="button" variant="danger" loading={cancel.pending} disabled={locked || pending || stale || data.state === "canceling"} onClick={() => void act(() => cancel.run(data.revision))}>取消运行</Button>
            </div> : null}
            {data.state === "waiting_approval" && last?.approvalId ? <Notice tone="info" title="等待你批准本次工具调用">
              <p>请核对下方目标节点和参数。修改类和 Root 命令随后还需在关联任务页完成独立管理员审批。</p>
              <div className="mt-3 flex gap-2"><Button type="button" disabled={locked || pending || stale} onClick={() => void act(() => decide.run(last.approvalId!, "approve"))}>批准本次调用</Button><Button type="button" variant="danger" disabled={locked || pending || stale} onClick={() => void act(() => decide.run(last.approvalId!, "deny"))}>拒绝</Button></div>
            </Notice> : null}
            <ol className="space-y-3" aria-label="Agent 执行步骤">{data.steps.map(step => <StepCard key={step.number} step={step} />)}</ol>
            {data.finalText ? <section className="rounded-lg border bg-secondary/40 p-4"><h3 className="font-semibold">最终答复</h3><p className="mt-2 whitespace-pre-wrap text-sm">{data.finalText}</p></section> : null}
            {data.stopReason ? <p className="break-all text-sm text-muted-foreground">停止原因：{data.stopReason}</p> : null}
          </div>;
        }}
      </AsyncPanel>
    </Panel>
  );
}

function StepCard({ step }: { readonly step: AgentStep }) {
  return <li className="min-w-0 rounded-lg border p-4 text-sm">
    <div className="flex flex-wrap justify-between gap-2"><h3 className="font-semibold">第 {step.number} 步 · {step.toolName ?? "模型规划"}</h3><span>{STATE_LABEL[step.state]}</span></div>
    {step.nodeIds.length > 0 ? <p className="mt-2 break-all text-xs">节点：{step.nodeIds.join("、")}</p> : null}
    {step.parameters ? <details className="mt-2" open={step.state === "waiting_approval"}><summary className="cursor-pointer">调用参数</summary><pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-secondary p-3 text-xs">{JSON.stringify(step.parameters, null, 2)}</pre></details> : null}
    {step.taskId ? <p className="mt-2"><a className="text-primary underline" href={`#/tasks/${encodeURIComponent(step.taskId)}`}>查看关联任务与审批</a> · {step.taskState ?? "等待状态"}</p> : null}
    {step.result ? <details className="mt-2"><summary className="cursor-pointer">实际结果（已脱敏）</summary><pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap break-all rounded bg-secondary p-3 text-xs">{step.result}</pre></details> : null}
  </li>;
}
