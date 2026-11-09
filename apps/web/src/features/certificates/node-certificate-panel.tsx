import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { nodeCertificateApi, type NodeCSRTask, type NodeCertificateReceipt } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

export function NodeCertificatePanel() {
  const session = useSession();
  const allowed = session.can("certificates.chain.install") || session.can("settings.manage") || session.can("certificates.manage");
  const [csrTaskId, setCSRTaskId] = useState("");
  const [certificate, setCertificate] = useState("");
  const [key, setKey] = useState<string | null>(null);
  const [task, setTask] = useState<NodeCSRTask | null>(null);
  const [lookupId, setLookupId] = useState("");
  const [receipt, setReceipt] = useState<NodeCertificateReceipt | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const install = useAction((body: Parameters<typeof nodeCertificateApi.install>[0]) => nodeCertificateApi.install(body));
  const read = useAction((id: string) => nodeCertificateApi.get(id));
  const locked = !allowed || session.writesLocked || install.pending;
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const pem = `${certificate.trim().replace(/\r\n/g, "\n")}\n`;
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(csrTaskId) || pem.length > 8192 || !pem.startsWith("-----BEGIN CERTIFICATE-----\n") || !pem.endsWith("-----END CERTIFICATE-----\n") || /PRIVATE|REQUEST/.test(pem) || /[^\x20-\x7e\n]/.test(pem)) {
      setValidation("请填写已完成的 CSR 任务 ID，并只粘贴不超过 8 KiB 的公开证书链 PEM；不要输入任何私钥。"); return;
    }
    setValidation(null);
    const operation = key ?? globalThis.crypto.randomUUID();
    setKey(operation);
    const result = await install.run({ csrTaskId, certificatePemBase64: globalThis.btoa(pem), idempotencyKey: operation });
    if (result.ok && result.data) { setTask(result.data.data); setLookupId(result.data.data.taskId); setKey(null); setCertificate(""); setReceipt(null); }
    else if (result.error && [400, 401, 403, 422, 429].includes(result.error.status)) setKey(null);
  }
  async function refresh(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!/^[a-zA-Z0-9_-]{1,128}$/.test(lookupId)) { setValidation("请填写证书安装任务 ID。"); return; }
    setValidation(null); setReceipt(null);
    const result = await read.run(lookupId);
    if (result.ok && result.data) setReceipt(result.data.data);
  }
  return <Panel title="安装公开网站证书链" description="使用已完成的节点 CSR 关联网站私钥。节点校验信任链、域名、有效期与私钥匹配；私钥不会上传到 Controller。">
    <div className="space-y-4 px-5 py-4">
      {!allowed ? <Notice tone="warning" title="需要管理员及节点授权">当前身份不能安装网站证书。</Notice> : null}
      <form onSubmit={submit} className="space-y-3">
        <Field label="已完成的 CSR 任务 ID">{ids => <TextInput id={ids.inputId} value={csrTaskId} onChange={event => setCSRTaskId(event.target.value)} disabled={locked || key !== null} required autoComplete="off" />}</Field>
        <Field label="公开证书链 PEM" hint="仅包含 CERTIFICATE 块，叶证书在前；不接受恢复密钥、网站私钥或 CA 私钥。">{ids => <TextArea id={ids.inputId} value={certificate} onChange={event => setCertificate(event.target.value)} disabled={locked || key !== null} maxLength={8192} rows={7} spellCheck={false} required />}</Field>
        <Button type="submit" loading={install.pending} disabled={locked}>登记证书安装任务</Button>
      </form>
      {key ? <Notice tone="warning" title="提交结果待核对">再次提交复用当前请求编号。请先查看任务状态；不会自动覆盖节点文件或重放未知结果。</Notice> : null}
      {task ? <Notice tone="info" title="安装任务已登记">{task.taskId} · {task.status}。请先完成人工审批，再核对节点实际回执。<a href="#/tasks" className="ml-2 underline">查看任务</a></Notice> : null}
      <form onSubmit={refresh} className="flex flex-wrap items-end gap-3 border-t pt-4">
        <Field label="证书安装任务 ID">{ids => <TextInput id={ids.inputId} value={lookupId} onChange={event => setLookupId(event.target.value)} disabled={!allowed || read.pending} required autoComplete="off" />}</Field>
        <Button type="submit" disabled={!allowed || read.pending} loading={read.pending}>读取节点安装回执</Button>
      </form>
      {receipt ? <div className="space-y-2 rounded-lg border p-3 text-sm">
        <p className="break-all font-mono text-xs">发布证书 ID：{receipt.certificateId}</p>
        <p>节点 {receipt.result.nodeId} · 域名 {receipt.result.domain} · 密钥引用 {receipt.result.keyRef}</p>
        <p>观察时间 {formatDateTime(receipt.observedAt) ?? "—"} · 证书到期 {formatDateTime(receipt.result.notAfter) ?? "—"}</p>
        <p className="break-all font-mono text-xs">叶证书 SHA-256 {receipt.result.leafSha256}</p>
        <p className="break-all font-mono text-xs">实际链字节 SHA-256 {receipt.result.chainSha256}</p>
      </div> : null}
      {validation ? <Notice tone="error" title="输入无效">{validation}</Notice> : null}
      {install.error ? <Notice tone="error" title="安装任务未登记">{describeApiError(install.error)}</Notice> : null}
      {read.error ? <Notice tone="error" title="安装回执未就绪">{describeApiError(read.error)}</Notice> : null}
      <p className="text-xs text-muted-foreground">证书落盘不代表 Nginx 已加载或 HTTPS 已健康；实际发布还需独立授权的配置检查、重载与请求字节验证。</p>
    </div>
  </Panel>;
}
