import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { encryptedBackupsApi, type BackupTaskAdmission } from "@/lib/api/endpoints-system";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const BACKUP_STATES: Readonly<Record<string, string>> = { preparing: "生成中", ready: "密文已生成", unknown: "结果未知，需人工核对" };
export function EncryptedBackupsPanel() {
  const session = useSession();
  const allowed = session.can("backup.manage") || session.can("system.backup") || session.can("settings.manage");
  const page = useResource(`encrypted-backups:${session.viewEpoch}`, async signal => (await encryptedBackupsApi.list(signal)).data, { enabled: allowed });
  const [challengeId, setChallengeId] = useState("");
  const [proof, setProof] = useState("");
  const [task, setTask] = useState<BackupTaskAdmission | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  // One key per explicit user operation. An unknown network outcome never
  // creates a new task automatically; the same operation key remains available.
  const [createKey, setCreateKey] = useState<string | null>(null);
  const [challengeKey, setChallengeKey] = useState<string | null>(null);
  const create = useAction((key: string) => encryptedBackupsApi.create(key));
  const challenge = useAction((key: string) => encryptedBackupsApi.challenge(key));
  const confirm = useAction((body: { challengeId: string; proof: string }) => encryptedBackupsApi.confirm(body.challengeId, body.proof));
  const data = page.state.status === "ready" ? page.state.data : null;
  const locked = !allowed || session.writesLocked || page.state.status !== "ready" || create.pending || challenge.pending || confirm.pending;
  async function submit(kind: "full" | "key_challenge") {
    setValidation(null);
    const existing = kind === "full" ? createKey : challengeKey;
    const key = existing ?? globalThis.crypto.randomUUID();
    if (kind === "full") setCreateKey(key); else setChallengeKey(key);
    const result = await (kind === "full" ? create.run(key) : challenge.run(key));
    if (result.ok && result.data) { setTask(result.data.data); if (kind === "full") setCreateKey(null); else setChallengeKey(null); page.refreshSilent(); }
  }
  async function confirmKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!/^[a-zA-Z0-9_.-]{1,128}$/.test(challengeId) || !/^[a-f0-9]{64}$/.test(proof)) { setValidation("请填写确认文件 ID 与离线命令返回的 64 位公开摘要。"); return; }
    setValidation(null);
    const result = await confirm.run({ challengeId, proof });
    if (result.ok) { setProof(""); page.reload(); }
  }
  return <Panel title="加密备份" description="恢复私钥保存在离线设备。在线主控只保存公开接收方，并通过统一任务生成加密备份。">
    <div className="space-y-4 px-5 py-4">
      {!allowed ? <Notice tone="warning" title="需要管理员权限">当前身份无法管理加密备份。</Notice> : null}
      {page.state.status === "loading" ? <p role="status">正在读取备份记录…</p> : null}
      {page.state.status === "error" ? <Notice tone="error" title="加密备份不可用">{describeApiError(page.state.error)}<Button size="sm" onClick={page.reload}>重新读取</Button></Notice> : null}
      {page.state.status === "ready" && page.state.refreshError ? <Notice tone="warning" title="刷新失败">{describeApiError(page.state.refreshError)}</Notice> : null}
      {data ? <>
        {!data.items.some(item => item.kind === "full" && ["uploaded", "verified"].includes(item.remoteStatus)) ? <Notice tone="warning" title="尚无已确认的异地备份">本机副本仅是缓存。可在“异地备份与保留清理”中配置自动上传，或下载并安全转移到另一处设备；下载和密文校验仍不能证明实际恢复成功。</Notice> : null}
        {!data.automaticBackupsConfigured ? <Notice tone="warning" title="每日自动备份尚未配置">当前可手动备份。管理员可在下方备份策略中启用定时备份；系统使用保存策略的当前授权身份。</Notice> : null}
        <p className="text-sm">公开接收方指纹：<span className="break-all font-mono text-xs">{data.recipientFingerprint}</span></p>
        <p className="text-sm">离线恢复密钥：{data.recoveryKeyConfirmed ? "已通过解密确认" : "尚未确认"}</p>
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => void submit("key_challenge")} loading={challenge.pending} disabled={locked}>生成离线密钥确认文件</Button>
          <Button onClick={() => void submit("full")} loading={create.pending} disabled={locked || !data.recoveryKeyConfirmed}>创建加密备份</Button>
          <Button variant="secondary" onClick={page.reload} disabled={page.state.status === "ready" && page.state.refreshing}>刷新记录</Button>
        </div>
        {createKey || challengeKey ? <Notice tone="warning" title="请求结果待核对">网络结果未知时不会自动换请求编号。再次提交将复用相同编号，请先查看统一任务和备份记录。</Notice> : null}
        {task ? <Notice tone="info" title="统一任务已登记">任务 {task.taskId} · 状态 {task.status} · 主控 {task.controllerId}。{task.approvalRequired ? "请在任务页完成人工审批。" : "生成结果以备份记录为准。"}<a className="ml-2 underline" href="#/tasks">查看任务</a></Notice> : null}
        {data.items.length === 0 ? <p className="text-sm text-muted-foreground">尚无加密备份或离线密钥确认文件。</p> : <DataTable caption="加密备份记录" rows={data.items} rowKey={item => item.id} columns={[
          { key: "id", header: "备份", render: item => <span className="break-all font-mono text-xs">{item.id}</span> },
          { key: "kind", header: "类型", render: item => item.kind === "full" ? "完整备份" : "密钥确认文件" },
          { key: "status", header: "状态", render: item => <span>{BACKUP_STATES[item.status] ?? item.status}{item.errorCode ? ` · ${item.errorCode}` : ""}</span> },
          { key: "createdAt", header: "创建时间", render: item => formatDateTime(item.createdAt) ?? "—" },
          { key: "bytes", header: "密文字节", render: item => item.bytes.toLocaleString() },
          { key: "sha256", header: "密文摘要", render: item => <span className="break-all font-mono text-xs">{item.sha256 ?? "未生成"}</span> },
          { key: "download", header: "操作", render: item => item.status === "ready" && (!item.localStatus || item.localStatus === "present") ? <a className="underline" href={`/api/v1/backups/${encodeURIComponent(item.id)}/download`} download>下载密文</a> : "不可下载" },
        ]} />}
      </> : null}
      <form onSubmit={confirmKey} className="space-y-3 rounded-lg border p-4">
        <p className="text-sm">下载密钥确认文件，在离线设备执行恢复密钥证明命令，仅将返回的公开摘要填入此处。</p>
        <Field label="密钥确认文件 ID">{ids => <TextInput id={ids.inputId} value={challengeId} onChange={event => setChallengeId(event.target.value)} disabled={locked} autoComplete="off" required />}</Field>
        <Field label="离线证明公开摘要">{ids => <TextInput id={ids.inputId} value={proof} onChange={event => setProof(event.target.value)} disabled={locked} autoComplete="off" maxLength={64} required />}</Field>
        <Button type="submit" loading={confirm.pending} disabled={locked || !challengeId || !proof}>确认恢复密钥</Button>
      </form>
      {validation ? <Notice tone="error" title="输入无效">{validation}</Notice> : null}
      {[create.error, challenge.error, confirm.error].map((error, index) => error ? <Notice key={index} tone="error" title="备份操作未完成">{describeApiError(error)}</Notice> : null)}
      <p className="text-xs text-muted-foreground">密文生成和摘要校验不代表恢复成功。实际恢复需离线解密、新签发主控权威、旧主控技术隔离及恢复后的完整验证。</p>
    </div>
  </Panel>;
}
