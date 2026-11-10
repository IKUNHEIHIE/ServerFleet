import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { nodeCSRApi, type NodeCSRReceipt, type NodeCSRTask } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

export function NodeCSRPanel() {
  const session = useSession();
  const allowed = session.can("certificates.csr.create") || session.can("settings.manage") || session.can("certificates.manage");
  const [nodeId, setNodeId] = useState("");
  const [domain, setDomain] = useState("");
  const [keyRef, setKeyRef] = useState("");
  const [lookupId, setLookupId] = useState("");
  const [operationKey, setOperationKey] = useState<string | null>(null);
  const [task, setTask] = useState<NodeCSRTask | null>(null);
  const [receipt, setReceipt] = useState<NodeCSRReceipt | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const create = useAction((body: Parameters<typeof nodeCSRApi.create>[0]) => nodeCSRApi.create(body));
  const read = useAction((id: string) => nodeCSRApi.get(id));
  const locked = !allowed || session.writesLocked || create.pending;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(nodeId) || !/^[a-zA-Z0-9_-]{1,96}$/.test(keyRef) || !domain || domain !== domain.toLowerCase()) {
      setValidation("请填写节点 ID、小写域名和仅含字母、数字、短横线或下划线的独立密钥引用。"); return;
    }
    setValidation(null);
    const key = operationKey ?? globalThis.crypto.randomUUID();
    setOperationKey(key);
    const result = await create.run({ nodeId, domain, keyRef, idempotencyKey: key });
    if (result.ok && result.data) { setTask(result.data.data); setLookupId(result.data.data.taskId); setOperationKey(null); setReceipt(null); }
    else if (result.error && [400, 401, 403, 422, 429].includes(result.error.status)) setOperationKey(null);
  }
  async function refresh(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(lookupId)) { setValidation("请填写已登记的任务 ID。"); return; }
    setValidation(null); setReceipt(null);
    const result = await read.run(lookupId);
    if (result.ok && result.data) setReceipt(result.data.data);
  }
  return <Panel title="节点本地网站密钥与 CSR" description="生成私钥前登记统一任务并完成人工审批。Controller 只接收公开 CSR，网站私钥留在节点的独立私有目录中。">
    <div className="space-y-4 px-5 py-4">
      {!allowed ? <Notice tone="warning" title="需要管理员权限">还需拥有目标节点的当前资源授权。</Notice> : null}
      <form onSubmit={submit} className="space-y-3">
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="目标节点 ID">{ids => <TextInput id={ids.inputId} value={nodeId} onChange={event => setNodeId(event.target.value)} disabled={locked || operationKey !== null} autoComplete="off" required />}</Field>
          <Field label="网站域名">{ids => <TextInput id={ids.inputId} value={domain} onChange={event => setDomain(event.target.value)} disabled={locked || operationKey !== null} autoComplete="off" placeholder="www.example.com" required />}</Field>
          <Field label="新密钥引用" hint="独立名称；已存在或部分生成的密钥不会被覆盖。">{ids => <TextInput id={ids.inputId} value={keyRef} onChange={event => setKeyRef(event.target.value)} disabled={locked || operationKey !== null} maxLength={96} autoComplete="off" required />}</Field>
        </div>
        <Button type="submit" disabled={locked} loading={create.pending}>登记 CSR 生成任务</Button>
      </form>
      {operationKey ? <Notice tone="warning" title="提交结果待核对">再次提交会复用原请求编号。请先查看任务页；未知结果不会自动重放或更换密钥。</Notice> : null}
      {task ? <Notice tone="info" title="任务已登记">{task.taskId} · {task.status}。{task.approvalRequired ? "请在任务页由另一位有权审批者批准。" : "终态以服务端为准。"}<a href="#/tasks" className="ml-2 underline">查看任务</a></Notice> : null}
      <form onSubmit={refresh} className="flex flex-wrap items-end gap-3 border-t pt-4">
        <Field label="读取已完成的 CSR 任务">{ids => <TextInput id={ids.inputId} value={lookupId} onChange={event => setLookupId(event.target.value)} disabled={!allowed || read.pending} autoComplete="off" required />}</Field>
        <Button type="submit" disabled={!allowed || read.pending} loading={read.pending}>读取公开 CSR 回执</Button>
      </form>
      {receipt ? <div className="space-y-2 rounded-lg border p-3 text-sm">
        <p>节点 {receipt.result.nodeId} · 域名 {receipt.result.domain} · 密钥引用 {receipt.result.keyRef}</p>
        <p>观察时间 {formatDateTime(receipt.observedAt) ?? "—"}</p>
        <p className="break-all font-mono text-xs">CSR SHA-256 {receipt.result.sha256}</p>
        <details><summary className="cursor-pointer">查看公开 CSR DER（Base64）</summary><pre className="mt-2 whitespace-pre-wrap break-all text-xs">{receipt.result.csrDerBase64}</pre></details>
      </div> : null}
      {validation ? <Notice tone="error" title="输入无效">{validation}</Notice> : null}
      {create.error ? <Notice tone="error" title="CSR 任务未登记">{describeApiError(create.error)}</Notice> : null}
      {read.error ? <Notice tone="error" title="CSR 回执未就绪">{describeApiError(read.error)}</Notice> : null}
      <p className="text-xs text-muted-foreground">节点需由管理员启用独立的网站密钥目录。生成 CSR 不代表证书已签发、已安装或站点已上线。</p>
    </div>
  </Panel>;
}
