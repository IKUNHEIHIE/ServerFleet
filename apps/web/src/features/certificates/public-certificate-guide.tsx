import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { tasksApi } from "@/lib/api/endpoints";
import { nodeCertificateApi, nodeCSRApi, publicCertificateApi, type PublicCertificateProfile, type NodeCSRTask } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

type Stage = "csr" | "issue" | "install";
const stageLabels: Record<Stage, string> = { csr: "生成申请", issue: "签发", install: "安装" };
const terminal = new Set(["completed", "failed", "partially_failed", "canceled", "expired", "unknown"]);

interface Flow {
  nodeId: string;
  domain: string;
  zoneId: string;
  zoneName: string;
  requestKey: string;
  tasks: Partial<Record<Stage, NodeCSRTask>>;
}

function validDomain(value: string): boolean {
  const labels = value.replace(/^\*\./, "").split(".");
  return value.length <= 253 && labels.length > 1 && labels.every(label => /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label));
}

export function PublicCertificateGuide() {
  const { viewEpoch, writesLocked } = useSession();
  const profile = useResource(`certificate-guide:${viewEpoch}`, signal => publicCertificateApi.profile(signal).then(result => result.data));
  return (
    <Panel title="申请并安装网站证书" description="选节点、填域名，然后按页面提示完成签发和安装。"
      actions={<Button size="sm" onClick={profile.refreshSilent} disabled={profile.state.status !== "ready"}>刷新节点和 DNS</Button>}>
      <AsyncPanel state={profile.state} onRetry={profile.reload} errorTitle="公网签发配置不可用">
        {data => <CertificateFlow profile={data} locked={writesLocked} />}
      </AsyncPanel>
    </Panel>
  );
}

function CertificateFlow({ profile, locked }: { profile: PublicCertificateProfile; locked: boolean }) {
  const session = useSession();
  const canApprove = session.can("task.approve") || session.can("task.manage");
  const [node, setNode] = useState("");
  const [domain, setDomain] = useState("");
  const [flow, setFlow] = useState<Flow | null>(null);
  const nodes = profile.nodes ?? [];
  const nodeId = nodes.some(item => item.id === node) ? node : nodes.length === 1 ? nodes[0].id : "";
  const hostname = domain.trim().toLowerCase();
  const name = hostname.replace(/^\*\./, "");
  const matchingZones = (profile.zones ?? []).filter(zone => name === zone.name || name.endsWith("." + zone.name))
    .sort((a, b) => b.name.length - a.name.length);
  const zone = matchingZones[0]?.name === matchingZones[1]?.name ? undefined : matchingZones[0];

  const stage: Stage = flow?.tasks.install ? "install" : flow?.tasks.issue ? "issue" : "csr";
  const activeTask = flow?.tasks[stage];
  const taskId = activeTask?.taskId ?? "";
  const task = useResource(`certificate-task:${taskId}`, signal => tasksApi.get(taskId, signal).then(result => result.data), {
    enabled: Boolean(taskId),
    refreshIntervalMs: 3500,
    shouldPoll: data => !terminal.has((data as { state: string }).state),
  });
  const job = task.state.status === "ready" ? task.state.data : null;

  const receipt = useResource(`certificate-receipt:${stage}:${taskId}`, async signal => {
    switch (stage) {
      case "csr": return { stage, value: (await nodeCSRApi.get(taskId, signal)).data } as const;
      case "issue": return { stage, value: (await publicCertificateApi.get(taskId, signal)).data } as const;
      case "install": return { stage, value: (await nodeCertificateApi.get(taskId, signal)).data } as const;
    }
  }, { enabled: Boolean(taskId) && job?.state === "completed" });
  const proof = receipt.state.status === "ready" ? receipt.state.data : null;

  const submit = useAction(async (next: Stage, draft: Flow) => {
    const idempotencyKey = `${draft.requestKey}-${next}`;
    switch (next) {
      case "csr":
        return (await nodeCSRApi.create({ nodeId: draft.nodeId, domain: draft.domain, keyRef: `tls-${draft.requestKey}`, idempotencyKey })).data;
      case "issue":
        if (proof?.stage !== "csr" || !draft.tasks.csr || proof.value.result.domain !== draft.domain || proof.value.result.nodeId !== draft.nodeId) {
          throw new Error("The verified CSR does not match this request.");
        }
        return (await publicCertificateApi.create({ csrTaskId: draft.tasks.csr.taskId, zoneId: draft.zoneId, idempotencyKey })).data;
      case "install":
        if (proof?.stage !== "issue" || !draft.tasks.csr || proof.value.csrTaskId !== draft.tasks.csr.taskId ||
          proof.value.nodeId !== draft.nodeId || proof.value.domain !== draft.domain || !proof.value.certificatePemBase64) {
          throw new Error("The issued chain does not match this request.");
        }
        return (await nodeCertificateApi.install({
          csrTaskId: draft.tasks.csr.taskId, certificatePemBase64: proof.value.certificatePemBase64, idempotencyKey,
        })).data;
    }
  });

  async function advance(next: Stage, draft: Flow) {
    const result = await submit.run(next, draft);
    if (result.ok && result.data) {
      setFlow({ ...draft, tasks: { ...draft.tasks, [next]: result.data } });
    }
  }

  function start(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (locked || submit.pending || !nodeId || !zone || !validDomain(hostname)) return;
    const draft = { nodeId, domain: hostname, zoneId: zone.id, zoneName: zone.name, requestKey: crypto.randomUUID(), tasks: {} };
    setFlow(draft);
    void advance("csr", draft);
  }

  const approval = useAction((task: NodeCSRTask) => tasksApi.approve(task.taskId, { planDigest: task.planHash, decision: "approve" }));

  async function approveCurrent(current: NodeCSRTask) {
    const result = await approval.run(current);
    if (result.ok || result.error?.status === 409) task.refreshSilent();
  }

  const shownError = submit.error ?? approval.error ?? (task.state.status === "error" ? task.state.error : task.state.status === "ready" ? task.state.refreshError : null);
  const readyToSign = proof?.stage === "csr" && flow && proof.value.result.domain === flow.domain && proof.value.result.nodeId === flow.nodeId;
  const readyToInstall = proof?.stage === "issue" && flow && proof.value.csrTaskId === flow.tasks.csr?.taskId &&
    proof.value.nodeId === flow.nodeId && proof.value.domain === flow.domain && Boolean(proof.value.certificatePemBase64) &&
    (proof.value.orderStatus === "issued" || proof.value.orderStatus === "issued_cleanup_pending");
  const result = proof?.stage === "install" && flow && proof.value.csrTaskId === flow.tasks.csr?.taskId &&
    proof.value.result.nodeId === flow.nodeId && proof.value.result.domain === flow.domain ? proof.value : null;

  return (
    <div className="space-y-4 p-5">
      {shownError ? <Notice tone="error" title="当前步骤需要检查">{describeApiError(shownError)}</Notice> : null}
      {!flow ? (
        <form onSubmit={start} className="space-y-4">
          {!profile.choicesAvailable ? <Notice tone="warning" title="申请选项暂时不可用">请刷新节点和 DNS。已有证书任务可在下方的手动操作中查看。</Notice> : null}
          {profile.choicesAvailable && nodes.length === 0 ? <Notice tone="warning" title="暂无可安装证书的在线节点">请让目标 Agent 启用网站证书目录后重新连接。</Notice> : null}
          {profile.choicesAvailable && !profile.zones?.length ? <Notice tone="warning" title="暂无可用于签发的 DNS 区域">请在 DNS 管理中同步域名，并核对主控的签发账号。</Notice> : null}
          <div className="grid gap-4 md:grid-cols-2">
            <Field label="安装节点">
              {ids => <Select id={ids.inputId} value={nodeId} onChange={event => setNode(event.target.value)} required disabled={locked || !nodes.length}>
                <option value="">选择在线节点</option>
                {nodes.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
              </Select>}
            </Field>
            <Field label="网站域名" hint={zone ? `DNS 区域 ${zone.name}` : hostname && validDomain(hostname) ? "没有可唯一匹配的 DNS 区域，请检查域名和签发账号。" : undefined}>
              {ids => <TextInput id={ids.inputId} aria-describedby={ids.hintId} value={domain} onChange={event => setDomain(event.target.value)} placeholder="www.example.com" autoComplete="off" required disabled={locked} />}
            </Field>
          </div>
          <Button variant="primary" type="submit" disabled={locked || !nodeId || !validDomain(hostname) || !zone || !profile.configured}>开始申请</Button>
        </form>
      ) : (
        <>
          <p className="text-sm font-medium">{flow.domain} <span className="ml-2 text-muted-foreground">{nodes.find(item => item.id === flow.nodeId)?.name ?? flow.nodeId}</span></p>
          <ol className="grid gap-2 sm:grid-cols-3" aria-label="证书申请进度">
            {(["csr", "issue", "install"] as const).map((step, index) => (
              <li key={step} aria-current={stage === step ? "step" : undefined} className={stage === step ? "rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-sm font-medium" : "rounded-lg border border-border px-3 py-2 text-sm"}>
                {index + 1}. {stageLabels[step]}
                {flow.tasks[step] ? <a className="ml-2 text-primary underline" href={`#/tasks/${encodeURIComponent(flow.tasks[step]!.taskId)}`} target="_blank" rel="noreferrer">查看任务</a> : null}
              </li>
            ))}
          </ol>
          <div role="status">
            {!taskId ? <p className="text-sm">{submit.pending ? "正在提交申请…" : "申请提交结果待核对。"}</p> : null}
            {job?.state === "awaiting_approval" ? <p className="text-sm">等待管理员审批。可在这里审核，或打开当前步骤的任务查看详情。</p> : null}
            {job && !terminal.has(job.state) && job.state !== "awaiting_approval" ? <p className="text-sm">任务处理中，结果会自动刷新。</p> : null}
            {job && terminal.has(job.state) && job.state !== "completed" ? (
              <Notice tone="warning" title={job.state === "unknown" ? "任务结果未知" : "当前步骤未完成"}>请查看任务回执核对原因。</Notice>
            ) : null}
            {job?.state === "completed" && receipt.state.status === "loading" ? <p className="text-sm">正在读取回执…</p> : null}
          </div>
          {receipt.state.status === "error" ? <Notice tone="error" title="无法读取回执">
            {describeApiError(receipt.state.error)} <Button size="sm" onClick={receipt.reload}>重新读取</Button>
          </Notice> : null}
          {readyToSign ? <p className="text-sm text-muted-foreground">节点已生成申请。签发时会在 {flow.zoneName} 完成域名验证。</p> : null}
          {readyToInstall ? <p className="text-sm text-muted-foreground">公网证书已签发。下一步把证书链安装到选定节点。</p> : null}
          {proof?.stage === "issue" && proof.value.pendingCleanupCount > 0 ? <Notice tone="warning" title="DNS 验证记录待清理">
            可在下方手动操作区查看清理队列。
          </Notice> : null}
          {result ? <Notice tone="success" title="节点已安装证书">
            到期 {formatDateTime(result.result.notAfter)}。网站开始提供 HTTPS 还需在网站或网关中选择这张证书。
          </Notice> : null}
          <div className="flex flex-wrap gap-2">
            {task.state.status === "error" ? <Button size="sm" onClick={task.reload}>重新读取任务</Button> : null}
            {canApprove && activeTask?.planHash && job?.state === "awaiting_approval" ? (
              <Button variant="primary" loading={approval.pending} disabled={locked} onClick={() => void approveCurrent(activeTask)}>
                批准{stageLabels[stage]}
              </Button>
            ) : null}
            {!taskId && !submit.pending ? <Button variant="primary" disabled={locked} onClick={() => void advance("csr", flow)}>核对或重试提交</Button> : null}
            {readyToSign ? <Button variant="primary" loading={submit.pending} disabled={locked} onClick={() => void advance("issue", flow)}>提交签发</Button> : null}
            {readyToInstall ? <Button variant="primary" loading={submit.pending} disabled={locked} onClick={() => void advance("install", flow)}>提交安装</Button> : null}
            {job && terminal.has(job.state) ? <Button disabled={submit.pending} onClick={() => { setFlow(null); setDomain(""); }}>申请另一张</Button> : null}
          </div>
        </>
      )}
    </div>
  );
}
