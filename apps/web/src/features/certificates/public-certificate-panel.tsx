import { useState, type FormEvent } from "react";
import { FileCheck2 } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { nodeCSRApi, publicCertificateApi, type NodeCSRTask, type PublicCertificateOrder } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const validID = (id: string) => /^[a-zA-Z0-9_-]{1,128}$/.test(id);

export function PublicCertificatePanel() {
  const session = useSession();
  const allowed = session.can("certificates.public.issue") || session.can("settings.manage");
  const profile = useResource(`public-certificate-profile:${session.viewEpoch}`, signal => publicCertificateApi.profile(signal).then(response => response.data), { enabled: allowed });
  const [csr, setCSR] = useState("");
  const [zone, setZone] = useState("");
  const [key, setKey] = useState<string | null>(null);
  const [task, setTask] = useState<NodeCSRTask | null>(null);
  const [lookup, setLookup] = useState("");
  const [order, setOrder] = useState<PublicCertificateOrder | null>(null);
  const [validation, setValidation] = useState<string | null>(null);
  const [nodeId, setNodeId] = useState("");
  const [domain, setDomain] = useState("");
  const [keyRef, setKeyRef] = useState("");
  const [csrTask, setCSRTask] = useState<NodeCSRTask | null>(null);
  const create = useAction((body: Parameters<typeof publicCertificateApi.create>[0]) => publicCertificateApi.create(body));
  const read = useAction((id: string) => publicCertificateApi.get(id));
  const createCSR = useAction((body: Parameters<typeof nodeCSRApi.create>[0]) => nodeCSRApi.create(body));
  const locked = !allowed || session.writesLocked || profile.state.status !== "ready" || create.pending;

  async function requestCSR(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validID(nodeId) || !domain.trim() || domain !== domain.trim().toLowerCase() || !validID(keyRef)) {
      setValidation("请填写节点 ID、小写域名和仅含字母数字/短横线/下划线的密钥引用。");
      return;
    }
    setValidation(null);
    const result = await createCSR.run({ nodeId, domain: domain.trim(), keyRef, idempotencyKey: globalThis.crypto.randomUUID() });
    if (result.ok && result.data) {
      setCSRTask(result.data.data);
      setCSR(result.data.data.taskId);
    }
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validID(csr) || !validID(zone)) {
      setValidation("请填写已完成的节点 CSR 任务 ID 与已同步的 DNS Zone ID。");
      return;
    }
    setValidation(null);
    const operation = key ?? globalThis.crypto.randomUUID();
    setKey(operation);
    const result = await create.run({ csrTaskId: csr, zoneId: zone, idempotencyKey: operation });
    if (result.ok && result.data) {
      setTask(result.data.data);
      setLookup(result.data.data.taskId);
      setKey(null);
      setOrder(null);
    } else if (result.error && [400, 401, 403, 422, 429].includes(result.error.status)) {
      setKey(null);
    }
  }

  async function refresh(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!validID(lookup)) {
      setValidation("请填写签发任务 ID。");
      return;
    }
    setOrder(null);
    setValidation(null);
    const result = await read.run(lookup);
    if (result.ok && result.data) setOrder(result.data.data);
  }

  const chain = order?.certificatePemBase64 ? globalThis.atob(order.certificatePemBase64) : "";

  return (
    <Panel
      title="签发公开网站证书"
      description="先在节点生成网站密钥与 CSR，并同步 DNS Zone。签发需要人工审批；网站私钥始终保留在节点。"
      actions={
        <Badge tone="info" dot>
          <FileCheck2 aria-hidden="true" className="size-3" />
          Step 02 · ACME Issue
        </Badge>
      }
    >
      <div className="space-y-4 px-5 py-4">
        {!allowed ? <Notice tone="warning" title="需要管理员及节点授权">当前身份不能签发证书。</Notice> : null}
        {profile.state.status === "error" ? (
          <Notice tone="warning" title="签发配置不可用">
            {describeApiError(profile.state.error)} 请由管理员核对受信任的签发配置与凭据版本。
          </Notice>
        ) : null}
        {profile.state.status === "ready" && profile.state.data.configured ? (
          <p className="text-xs text-muted-foreground">签发配置已就绪 · DNS 凭据第 {profile.state.data.dnsCredentialRevision} 版 · 网站私钥不进入 Controller。</p>
        ) : null}
        <details className="rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
          <summary className="cursor-pointer text-sm font-semibold">先在节点生成 CSR（可选）</summary>
          <form onSubmit={requestCSR} className="mt-3 space-y-3">
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="节点 ID">{ids => <TextInput id={ids.inputId} value={nodeId} onChange={event => setNodeId(event.target.value)} disabled={!allowed || createCSR.pending} autoComplete="off" />}</Field>
              <Field label="网站域名">{ids => <TextInput id={ids.inputId} value={domain} onChange={event => setDomain(event.target.value)} placeholder="www.example.com" disabled={!allowed || createCSR.pending} autoComplete="off" />}</Field>
              <Field label="密钥引用" hint="独立名称，不覆盖已存在密钥。">{ids => <TextInput id={ids.inputId} value={keyRef} onChange={event => setKeyRef(event.target.value)} disabled={!allowed || createCSR.pending} autoComplete="off" />}</Field>
            </div>
            {csrTask ? (
              <Notice tone="info" title="CSR 任务已登记">
                {csrTask.taskId} · {csrTask.status}。完成后该任务 ID 已自动带入下方签发。
                <a href="#/tasks" className="ml-2 font-semibold text-primary underline">查看任务并审批</a>
              </Notice>
            ) : null}
            {createCSR.error ? <Notice tone="error" title="CSR 任务未登记">{describeApiError(createCSR.error)}</Notice> : null}
            <Button type="submit" size="sm" loading={createCSR.pending} disabled={!allowed || session.writesLocked || !nodeId.trim() || !domain.trim() || !keyRef.trim()}>
              登记 CSR 生成任务
            </Button>
          </form>
        </details>
        <form onSubmit={submit} className="space-y-3.5">
          <div className="grid gap-3.5 sm:grid-cols-2">
            <Field label="已完成的节点 CSR 任务 ID">
              {ids => <TextInput id={ids.inputId} value={csr} onChange={event => setCSR(event.target.value)} disabled={locked || key !== null} required autoComplete="off" />}
            </Field>
            <Field label="DNS Zone ID">
              {ids => <TextInput id={ids.inputId} value={zone} onChange={event => setZone(event.target.value)} disabled={locked || key !== null} required autoComplete="off" />}
            </Field>
          </div>
          <Button type="submit" variant="primary" loading={create.pending} disabled={locked}>
            登记签发审批任务
          </Button>
        </form>
        {key ? <Notice tone="warning" title="提交结果待核对">再次提交复用当前请求编号。先核对任务，避免重复签发或重复创建 DNS 记录。</Notice> : null}
        {task ? (
          <Notice tone="info" title="签发任务已登记">
            {task.taskId} · {task.status}。
            <a href="#/tasks" className="ml-2 font-semibold text-primary underline">查看任务并审批</a>
          </Notice>
        ) : null}
        <form onSubmit={refresh} className="flex flex-wrap items-end gap-3 rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
          <div className="min-w-[14rem] flex-1">
            <Field label="签发任务 ID">
              {ids => <TextInput id={ids.inputId} value={lookup} onChange={event => setLookup(event.target.value)} disabled={!allowed || read.pending} required autoComplete="off" />}
            </Field>
          </div>
          <Button type="submit" loading={read.pending} disabled={!allowed || read.pending}>
            读取签发结果
          </Button>
        </form>
        {order ? (
          <div className="neurox-subcard space-y-3 text-sm">
            <p className="font-semibold text-foreground">
              签发状态 {order.orderStatus} · 任务 {order.taskStatus} · 节点 {order.nodeId} · 域名 {order.domain}
            </p>
            <p className="text-xs text-muted-foreground">
              待核对清理 {order.pendingCleanupCount} 项 · {order.errorCode || "无已记录错误"}
            </p>
            <p className="text-xs text-muted-foreground">
              观察时间 {formatDateTime(order.observedAt) ?? "—"} · 到期 {formatDateTime(order.notAfter) ?? "—"}
            </p>
            {chain ? (
              <Field label="已观察的公开证书链" hint="将公开证书链提交到证书安装面板；签发成功不代表已安装或网站健康。">
                {ids => <TextArea id={ids.inputId} value={chain} readOnly rows={6} spellCheck={false} className="font-mono text-xs" />}
              </Field>
            ) : null}
            <p className="break-all font-mono text-xs text-muted-foreground">公开证书链 SHA-256 {order.chainSha256 || "尚未观察"}</p>
          </div>
        ) : null}
        {validation ? <Notice tone="error" title="输入无效">{validation}</Notice> : null}
        {create.error ? <Notice tone="error" title="签发任务未登记">{describeApiError(create.error)}</Notice> : null}
        {read.error ? <Notice tone="error" title="结果未就绪">{describeApiError(read.error)}</Notice> : null}
      </div>
    </Panel>
  );
}
