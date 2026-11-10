import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { publicRenewalsApi, type PublicRenewalPolicy } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

import { RenewalDeliveryPanel } from "./renewal-delivery-panel";

const validID = (value: string) => /^[a-zA-Z0-9_-]{1,128}$/.test(value);
const stateLabels: Record<string, string> = {
  scheduled: "等待续签窗口", disabled: "已停用", configuration_changed: "签发配置已变化，调度暂停",
  awaiting_approval: "等待人工审批", queued: "等待执行", running: "签发中", cancelling: "取消中",
  completed: "签发任务完成", issuance_completed_installation_required: "新证书已签发，等待安装和接续",
  failed: "失败，等待人工核对", partial_failure: "部分失败，等待人工核对", unknown: "结果未知，禁止自动重跑",
  expired: "审批或执行窗口已过期", canceled: "任务已取消",
};

export function PublicRenewalPanel() {
  const session = useSession();
  const allowed = session.can("certificates.public.issue") || session.can("settings.manage");
  const policies = useResource(`public-renewals:${session.viewEpoch}`, signal => publicRenewalsApi.list(signal).then(response => response.data), { enabled: allowed });
  const save = useAction((body: Parameters<typeof publicRenewalsApi.save>[0]) => publicRenewalsApi.save(body));
  const [selected, setSelected] = useState<PublicRenewalPolicy | null>(null);
  const [order, setOrder] = useState("");
  const [install, setInstall] = useState("");
  const [days, setDays] = useState("30");
  const [mode, setMode] = useState<"configure" | "adopt">("configure");
  const [policyID, setPolicyID] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const locked = !allowed || session.writesLocked || save.pending;

  function edit(policy: PublicRenewalPolicy) {
    setSelected(policy); setMode(policy.taskId ? "adopt" : "configure");
    setOrder(policy.taskId ?? policy.orderTaskId); setInstall(policy.taskId ? "" : policy.installTaskId);
    setDays(String(policy.renewBeforeDays)); setPolicyID(null); setValidation(null); setMessage(null);
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setMessage(null);
    if (locked) return;
    if (!validID(order) || !validID(install) || !/^\d{1,2}$/.test(days) || Number(days) < 1 || Number(days) > 60) {
      setValidation("请填写已签发任务和对应的成功安装任务；提前天数为 1—60 天。"); return;
    }
    setValidation(null);
    const id = selected?.id ?? policyID ?? globalThis.crypto.randomUUID(); setPolicyID(id);
    const result = await save.run({ id, orderTaskId: order, installTaskId: install, expectedRevision: String(selected?.revision ?? 0), renewBeforeDays: days, enabled: "true", operation: mode });
    policies.reload();
    if (result.ok && result.data) {
      setSelected(null); setPolicyID(null); setOrder(""); setInstall(""); setDays("30"); setMode("configure");
      setMessage("续签策略已保存。到期窗口内仅创建需要人工审批的签发任务；新链另行安装，确认安装后接续下一周期。");
    }
  }
  async function disable(policy: PublicRenewalPolicy) {
    if (locked) return;
    setMessage(null); setValidation(null);
    const result = await save.run({ id: policy.id, orderTaskId: policy.orderTaskId, installTaskId: policy.installTaskId, expectedRevision: String(policy.revision), renewBeforeDays: String(policy.renewBeforeDays), enabled: "false", operation: "configure" });
    policies.reload();
    if (result.ok) { setSelected(null); setOrder(""); setInstall(""); setPolicyID(null); setMode("configure"); setMessage("策略已停用，后续签发请求会复核停用状态。已发生的供应商操作仍需查看原任务和清理记录。"); }
  }
  return <Panel title="公开证书续签" description="从已成功安装的证书登记续签窗口，复用该节点的现有网站密钥。每次签发仍需人工审批。">
    <div className="space-y-4 px-5 py-4">
      {!allowed ? <Notice tone="warning" title="需要管理员及节点授权">当前身份不能管理续签。</Notice> : null}
      <Notice tone="info" title="签发、安装与网站生效分别确认">可为每条策略启用安装发布编排：签发后自动创建安装及网关审批任务，核验真实结果后接续下一周期。也可保留手动安装与接续；失败、取消和未知结果不会自动重跑。</Notice>
      <Button type="button" onClick={policies.reload} disabled={!allowed || save.pending}>刷新续签策略</Button>
      {policies.state.status === "loading" ? <p role="status">正在读取续签策略…</p> : null}
      {policies.state.status === "error" ? <Notice tone="error" title="续签策略不可用">{describeApiError(policies.state.error)}</Notice> : null}
      {policies.state.status === "ready" && policies.state.data.items.length === 0 ? <p className="text-sm text-muted-foreground">尚未登记续签策略。</p> : null}
      {policies.state.status === "ready" ? policies.state.data.items.map(item => <div key={item.policy.id} className="space-y-2 rounded-lg border p-3 text-sm">
        <p>{item.policy.domain} · 节点 {item.policy.nodeId} · {stateLabels[item.state] ?? item.state}</p>
        <p>当前证书到期 {formatDateTime(item.policy.notAfter) ?? "—"} · 续签窗口 {formatDateTime(item.dueAt) ?? "—"}</p>
        <p className="break-all font-mono text-xs">策略 {item.policy.id} · 版本 {item.policy.revision}{item.policy.taskId ? ` · 续签任务 ${item.policy.taskId}` : ""}</p>
        <div className="flex flex-wrap gap-2">
          <Button type="button" disabled={locked} onClick={() => edit(item.policy)}>{item.policy.taskId ? "接续已安装的新证书" : "调整或启用策略"}</Button>
          <Button type="button" disabled={locked || !item.policy.enabled} onClick={() => { void disable(item.policy); }}>停用</Button>
          {item.policy.taskId ? <a href="#/tasks" className="underline">查看任务并审批</a> : null}
        </div>
        <RenewalDeliveryPanel policy={item.policy} onChanged={policies.reload} />
      </div>) : null}
      <form onSubmit={submit} className="space-y-3 border-t pt-4">
        <p className="text-sm font-medium">{selected ? `调整策略 ${selected.id}` : "登记续签策略"}</p>
        {selected ? <Field label="操作">{ids => <Select id={ids.inputId} value={mode} disabled={locked} onChange={event => setMode(event.target.value as "configure" | "adopt")}><option value="configure">调整或启用原策略</option><option value="adopt">接续已安装的新证书</option></Select>}</Field> : null}
        <Field label="成功签发任务 ID">{ids => <TextInput id={ids.inputId} value={order} disabled={locked} onChange={event => setOrder(event.target.value)} required autoComplete="off" />}</Field>
        <Field label="对应的成功安装任务 ID">{ids => <TextInput id={ids.inputId} value={install} disabled={locked} onChange={event => setInstall(event.target.value)} required autoComplete="off" />}</Field>
        <Field label="到期前多少天创建审批任务">{ids => <TextInput id={ids.inputId} type="number" min={1} max={60} value={days} disabled={locked} onChange={event => setDays(event.target.value)} required />}</Field>
        <div className="flex gap-2"><Button type="submit" disabled={locked} loading={save.pending}>保存续签策略</Button>{selected ? <Button type="button" disabled={save.pending} onClick={() => { setSelected(null); setOrder(""); setInstall(""); setPolicyID(null); setMode("configure"); }}>取消编辑</Button> : null}</div>
      </form>
      {validation ? <Notice tone="error" title="输入无效">{validation}</Notice> : null}
      {message ? <Notice tone="info" title="策略已更新">{message}</Notice> : null}
      {save.error ? <Notice tone="error" title="请核对当前策略与原任务">{describeApiError(save.error)} 已刷新策略；提交结果不明确时先核对现有条目，避免更换策略编号重复登记。</Notice> : null}
    </div>
  </Panel>;
}
