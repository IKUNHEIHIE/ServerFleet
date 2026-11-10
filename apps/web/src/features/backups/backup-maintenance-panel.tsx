import { useRef, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { backupMaintenanceApi as api, type BackupPolicy, type BackupPolicyInput, type CleanupPlan, type OperationTask } from "@/lib/api/endpoints-operations";
import { credentialsApi } from "@/lib/api/endpoints";
import { encryptedBackupsApi } from "@/lib/api/endpoints-system";
import type { CredentialMetadata } from "@/lib/api/dto";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

function PolicyForm({ initial, credentials, onSaved }: { readonly initial: BackupPolicy; readonly credentials: readonly CredentialMetadata[]; readonly onSaved: () => void }) {
  const session = useSession();
  const [value, setValue] = useState<BackupPolicyInput>(() => ({ scheduledBackups: initial.scheduledBackups, intervalHours: initial.intervalHours, autoUpload: initial.autoUpload, scheduleCleanup: initial.scheduleCleanup, localDays: initial.localDays, localKeep: initial.localKeep, remoteDays: initial.remoteDays, remoteKeep: initial.remoteKeep, requireVerifiedRemote: initial.requireVerifiedRemote, destination: initial.destination }));
  const [remoteEnabled, setRemoteEnabled] = useState(Boolean(initial.destination));
  const [destination, setDestination] = useState(initial.destination ?? { kind: "s3" as const, endpoint: "", bucket: "", region: "us-east-1", prefix: "serverfleet", credentialRef: "", credentialRevision: 0 });
  const save = useAction((policy: BackupPolicyInput) => api.configure(initial.revision, policy));
  function number(key: "intervalHours" | "localDays" | "localKeep" | "remoteDays" | "remoteKeep", label: string, max: number) {
    return <Field key={key} label={label}>{ids => <TextInput id={ids.inputId} type="number" min={1} max={max} required value={value[key]} onChange={e => setValue(v => ({ ...v, [key]: Number(e.target.value) }))} />}</Field>;
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    const { destination: unused, ...policy } = value;
    void unused;
    const selected = credentials.find(c => c.id === destination.credentialRef);
    const d = { ...destination, credentialRevision: selected?.revision ?? destination.credentialRevision };
    if (d.kind === "webdav") { delete d.bucket; delete d.region; }
    const result = await save.run({ ...policy, autoUpload: remoteEnabled && policy.autoUpload, ...(remoteEnabled ? { destination: d } : {}) });
    if (result.ok) onSaved();
  }
  return <form onSubmit={submit} className="space-y-4 rounded-lg border p-4">
    <fieldset disabled={save.pending || session.writesLocked} className="space-y-4">
      <legend className="font-medium">备份策略 · 修订 {initial.revision}</legend>
      <div className="flex flex-wrap gap-5">
        <label><input type="checkbox" checked={value.scheduledBackups} onChange={e => setValue(v => ({ ...v, scheduledBackups: e.target.checked }))} /> 定时生成加密备份</label>
        <label><input type="checkbox" checked={remoteEnabled} onChange={e => setRemoteEnabled(e.target.checked)} /> 配置异地存储</label>
        <label><input type="checkbox" checked={value.autoUpload} disabled={!remoteEnabled} onChange={e => setValue(v => ({ ...v, autoUpload: e.target.checked }))} /> 自动上传并读回校验</label>
        <label><input type="checkbox" checked={value.scheduleCleanup} onChange={e => setValue(v => ({ ...v, scheduleCleanup: e.target.checked }))} /> 自动发起清理审批</label>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        {number("intervalHours", "备份间隔（小时）", 168)}{number("localDays", "本地保留天数", 3650)}{number("localKeep", "本地最少份数", 1000)}{number("remoteDays", "异地保留天数", 3650)}{number("remoteKeep", "异地最少份数", 1000)}
      </div>
      <label className="block"><input type="checkbox" checked={value.requireVerifiedRemote} onChange={e => setValue(v => ({ ...v, requireVerifiedRemote: e.target.checked }))} /> 删除本地副本前必须再次验证异地副本</label>
      {remoteEnabled ? <div className="grid gap-3 sm:grid-cols-2">
        <Field label="存储类型">{ids => <Select id={ids.inputId} value={destination.kind} onChange={e => setDestination(d => ({ ...d, kind: e.target.value as "s3" | "webdav" }))}><option value="s3">S3 兼容存储</option><option value="webdav">WebDAV</option></Select>}</Field>
        <Field label="HTTPS 存储端点" hint="填写公网 HTTPS 地址；WebDAV 目录需预先存在。">{ids => <TextInput id={ids.inputId} type="url" required value={destination.endpoint} onChange={e => setDestination(d => ({ ...d, endpoint: e.target.value }))} />}</Field>
        {destination.kind === "s3" ? <>
          <Field label="Bucket">{ids => <TextInput id={ids.inputId} required value={destination.bucket ?? ""} onChange={e => setDestination(d => ({ ...d, bucket: e.target.value }))} />}</Field>
          <Field label="S3 Region">{ids => <TextInput id={ids.inputId} required value={destination.region ?? ""} onChange={e => setDestination(d => ({ ...d, region: e.target.value }))} />}</Field>
        </> : null}
        <Field label="对象前缀">{ids => <TextInput id={ids.inputId} value={destination.prefix} onChange={e => setDestination(d => ({ ...d, prefix: e.target.value }))} />}</Field>
        <Field label="异地存储凭据" hint="先在系统设置添加“备份存储秘密”，允许模块填 backups；此处只选择引用。">{ids => <Select id={ids.inputId} value={destination.credentialRef} required onChange={e => { const c = credentials.find(v => v.id === e.target.value); setDestination(d => ({ ...d, credentialRef: e.target.value, credentialRevision: c?.revision ?? 0 })); }}><option value="">请选择凭据</option>{credentials.map(c => <option key={c.id} value={c.id}>{c.name} · 修订 {c.revision}</option>)}</Select>}</Field>
      </div> : null}
      <p className="text-xs text-muted-foreground">天数与最少份数同时保护副本。清理需另一位管理员审批；对象存储必须支持条件写入、读取校验和基于 ETag 的条件删除。</p>
      <Button type="submit" loading={save.pending}>保存备份策略</Button>
    </fieldset>
    {save.error ? <Notice tone="error" title="策略未保存">{describeApiError(save.error)}。若策略已被修改，请刷新后重新编辑。</Notice> : null}
  </form>;
}

function MaintenanceContent() {
  const session = useSession();
  const page = useResource(`backup-maintenance:${session.viewEpoch}`, async signal => {
    const [maintenance, backups, credentials] = await Promise.all([api.get(signal), encryptedBackupsApi.list(signal), credentialsApi.list(signal)]);
    return { ...maintenance.data, backups: backups.data.items.filter(b => b.kind === "full"), credentials: credentials.data.filter(c => c.kind === "backup_storage_secret" && c.usageScope.includes("backups") && c.status !== "revoked" && c.status !== "expired" && c.status !== "invalid") };
  });
  const [plan, setPlan] = useState<CleanupPlan | null>(null);
  const [task, setTask] = useState<OperationTask | null>(null);
  const [note, setNote] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const keys = useRef(new Map<string, string>());
  const action = useAction(async (operation: string, id: string, revision: number = 0, pinned: boolean = false) => {
    const binding = `${operation}:${id}`;
    const key = keys.current.get(binding) ?? crypto.randomUUID(); keys.current.set(binding, key);
    if (operation === "preview") { const r = await api.preview(); setPlan(r.data); setConfirmed(false); }
    else if (operation === "protect") { await api.protect(id, revision, pinned, note); }
    else { const r = operation === "upload" ? await api.upload(id, key) : operation === "verify" ? await api.verify(id, key) : await api.cleanup(id, key); setTask(r.data); }
    keys.current.delete(binding); page.refreshSilent();
  });
  const data = page.state.status === "ready" ? page.state.data : null;
  const locked = session.writesLocked || action.pending || !data;
  return <div className="space-y-4 px-5 py-4">
    <Button onClick={page.reload} disabled={action.pending}>刷新维护记录</Button>
    {page.state.status === "loading" ? <p role="status">正在读取备份维护配置…</p> : null}
    {page.state.status === "error" ? <Notice tone="error" title="备份维护不可用">{describeApiError(page.state.error)}</Notice> : null}
    {page.state.status === "ready" && page.state.refreshError ? <Notice tone="warning" title="维护记录刷新失败">{describeApiError(page.state.refreshError)}</Notice> : null}
    {action.error ? <Notice tone="error" title="维护操作未确认">{describeApiError(action.error)}。请先核对统一任务；重试同一操作将复用请求编号。</Notice> : null}
    {task ? <Notice tone="info" title="维护任务已登记"><a className="underline" href={`#/tasks/${encodeURIComponent(task.taskId)}`}>{task.taskId}</a> · {task.status}{task.approvalRequired ? " · 请由另一位管理员在任务详情审批。" : " · 执行结果以任务回执为准。"}</Notice> : null}
    {data ? <>
      <PolicyForm key={data.policy.revision} initial={data.policy} credentials={data.credentials} onSaved={() => { setPlan(null); page.reload(); }} />
      <Field label="保留／取消保留原因">{ids => <TextInput id={ids.inputId} value={note} maxLength={512} onChange={e => setNote(e.target.value)} disabled={locked} />}</Field>
      {data.backups.length === 0 ? <p>暂无完整加密备份。</p> : <DataTable caption="备份副本与保留保护" rows={data.backups} rowKey={b => b.id} columns={[
        { key: "id", header: "备份", render: b => <span className="break-all text-xs">{b.id}</span> },
        { key: "status", header: "本地状态", render: b => `${b.localStatus ?? "present"}${b.pinned ? " · 已保留" : ""}` },
        { key: "actions", header: "操作", render: b => <div className="flex gap-2"><Button size="sm" disabled={locked || b.status !== "ready" || b.localStatus === "deleted" || !data.policy.destination || data.remoteObjects.some(r => r.backupId === b.id)} onClick={() => void action.run("upload", b.id)}>上传异地</Button><Button size="sm" disabled={locked || !note.trim()} onClick={() => void action.run("protect", b.id, b.localRevision ?? 0, !b.pinned)}>{b.pinned ? "取消保留" : "保留"}</Button></div> },
      ]} />}
      <DataTable caption="异地密文回执" rows={data.remoteObjects} rowKey={r => r.id} columns={[
        { key: "backup", header: "备份", render: r => <span className="break-all text-xs">{r.backupId}</span> },
        { key: "state", header: "结果", render: r => `${r.state}${r.errorCode ? ` · ${r.errorCode}` : ""}` },
        { key: "object", header: "远端对象", render: r => <span className="break-all text-xs">{r.object.key}</span> },
        { key: "verify", header: "核验", render: r => <Button size="sm" disabled={locked || !data.policy.destination} onClick={() => void action.run("verify", r.id)}>只读核验异地副本</Button> },
      ]} />
      <Button disabled={locked || data.policy.revision === 0} onClick={() => void action.run("preview", "current")}>预览保留清理</Button>
      {plan ? <section className="space-y-3 rounded-lg border p-4" aria-label="清理计划">
        <p className="break-all text-xs">计划 {plan.digest} · 过期 {formatDateTime(plan.expiresAt)}</p>
        <DataTable caption="待审批清理项" rows={plan.candidates} rowKey={(c, i) => `${c.scope}:${c.backupId}:${i}`} columns={[{ key: "scope", header: "范围", render: c => c.scope }, { key: "backup", header: "备份", render: c => c.backupId }]} />
        <details><summary>受保护项（{plan.protected.length}）</summary><ul>{plan.protected.map((c, i) => <li key={i} className="break-all text-xs">{c.scope} · {c.backupId} · {c.reason}</li>)}</ul></details>
        <label className="block"><input type="checkbox" checked={confirmed} onChange={e => setConfirmed(e.target.checked)} /> 已核对候选清单，提交独立审批</label>
        <Button disabled={locked || !confirmed || !plan.candidates.length || Date.parse(plan.expiresAt) <= Date.now()} onClick={() => void action.run("cleanup", plan.digest)}>创建清理审批任务</Button>
      </section> : null}
      <DataTable caption="清理执行回执" rows={data.cleanupReceipts} rowKey={r => r.taskId} columns={[{ key: "task", header: "任务", render: r => <a className="underline" href={`#/tasks/${encodeURIComponent(r.taskId)}`}>{r.taskId}</a> }, { key: "state", header: "状态", render: r => r.state }, { key: "items", header: "逐项结果", render: r => <ul>{r.items.map((i, n) => <li key={n} className="break-all text-xs">{i.candidate.scope} · {i.candidate.backupId} · {i.state} {i.reason}</li>)}</ul> }]} />
    </> : null}
  </div>;
}
export function BackupMaintenancePanel() {
  const session = useSession();
  const [open, setOpen] = useState(false);
  const allowed = session.can("backup.manage") || session.can("system.backup") || session.can("settings.manage");
  return <Panel title="异地备份与保留清理" description="加密副本自动上传、读回校验、保留周期和冻结清理清单。"><div className="px-5 py-4"><Button disabled={!allowed} onClick={() => setOpen(v => !v)} aria-expanded={open}>{open ? "收起备份维护" : "打开备份维护"}</Button></div>{open && allowed ? <MaintenanceContent key={session.viewEpoch} /> : null}</Panel>;
}
