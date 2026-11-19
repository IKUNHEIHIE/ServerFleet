import { ManagedWebsiteCertificates } from "./managed-website-certificates";
import { useEffect, useState, type FormEvent } from "react";
import { RefreshCw } from "lucide-react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Checkbox, Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable, TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { certificatesApi } from "@/lib/api/endpoints-infra";
import { serviceGroupsApi } from "@/lib/api/endpoints";
import type { InternalCertificateJob, ServiceGroup } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const JOB_STATE_TEXT: Record<InternalCertificateJob["state"], string> = {
  queued: "排队中",
  running: "执行中",
  completed: "已完成",
  partial: "部分完成",
  failed: "失败",
  revoking: "撤销中",
  revoked: "已撤销",
};

const NODE_STATE_TEXT: Record<InternalCertificateJob["results"][number]["state"], string> = {
  queued: "排队",
  keygen: "生成密钥",
  csr_pending: "等待 CSR",
  signed: "已签发",
  distributing: "下发中",
  verified: "已验证",
  failed: "失败",
  waiting_offline: "等待节点上线",
  revoked: "已撤销",
};

export function CertificatesPage() {
  const session = useSession();
  const [tab, setTab] = useState<"internal" | "public">("internal");
  const [createOpen, setCreateOpen] = useState(false);
  const [detailId, setDetailId] = useState<string | null>(null);
  const [revoking, setRevoking] = useState<InternalCertificateJob | null>(null);
  const [rotating, setRotating] = useState<InternalCertificateJob | null>(null);
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);

  const [serviceGroupId, setServiceGroupId] = useState("");
  const [credentialType, setCredentialType] = useState<"trust_bundle" | "per_node_identity">("trust_bundle");
  const [validityHours, setValidityHours] = useState("");
  const [confirmImpact, setConfirmImpact] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);

  const jobs = useResource(`certs:jobs:${session.viewEpoch}`, signal => certificatesApi.distributions(signal).then(result => result.data), { enabled: tab === "internal", refreshIntervalMs: 4000, shouldPoll: data => (data as { items: readonly InternalCertificateJob[] }).items.some(job => job.state === "queued" || job.state === "running" || job.state === "revoking") });
  const groups = useResource(`certs:groups:${session.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data), { enabled: tab === "internal" });
  const internalAvailable = jobs.state.status === "ready" && jobs.state.data.configured !== false;
  const needsIssuer = jobs.state.status === "ready" && jobs.state.data.configured === false;
  const jobItems = jobs.state.status === "ready" ? jobs.state.data.items : [];
  const detail = jobItems.find(job => job.jobId === detailId) ?? null;
  const groupItems = groups.state.status === "ready" ? groups.state.data.items : [];
  const selectedGroup: ServiceGroup | undefined = groupItems.find(group => group.id === serviceGroupId);
  const canManage = session.can("certificate.manage") || session.can("certificates.manage");

  const createAction = useAction((body: Parameters<typeof certificatesApi.create>[0], key: string) => certificatesApi.create(body, key));
  const revokeAction = useAction((jobId: string, body: { reason: string; includeAllIssuedLeafs?: boolean }, key: string) =>
    certificatesApi.revoke(jobId, body, key),
  );
  const rotateAction = useAction((jobId: string, body: { reason: string; targetExpiryDays?: number }, key: string) =>
    certificatesApi.rotate(jobId, body, key),
  );

  useEffect(() => {
    if (!createOpen) return;
    setServiceGroupId("");
    setCredentialType("trust_bundle");
    setValidityHours("");
    setConfirmImpact(false);
    setLocalError(null);
  }, [createOpen]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!internalAvailable || session.writesLocked || !canManage) return;
    if (!selectedGroup?.memberNodeIds.length) return setLocalError("请选择至少有一台节点的服务组。");
    if (!confirmImpact) return setLocalError("必须确认服务影响。");
    setLocalError(null);
    const key = globalThis.crypto?.randomUUID?.() ?? `cert-${Date.now()}`;
    const result = await createAction.run(
      {
        serviceGroupId,
        credentialType,
        ...(credentialType === "per_node_identity" && validityHours ? { validityHours: Number(validityHours) } : {}),
        confirmServiceImpact: true,
        expectedGroupRevision: selectedGroup.revision,
      },
      key,
    );
    if (result.ok) {
      setCreateOpen(false);
      setOutcome("证书下发任务已提交；逐节点结果以服务端为准。");
      jobs.reload();
    }
  }

  async function revoke() {
    if (!revoking) return;
    const key = globalThis.crypto?.randomUUID?.() ?? `cert-revoke-${Date.now()}`;
    const result = await revokeAction.run(revoking.jobId, { reason: reason.trim() }, key);
    if (result.ok) {
      setRevoking(null);
      setReason("");
      setOutcome("撤销请求已提交；吊销不代表对端立即停止信任，需等待传播/信任更新。");
      jobs.reload();
    }
  }

  async function rotate() {
    if (!rotating) return;
    const key = globalThis.crypto?.randomUUID?.() ?? `cert-rotate-${Date.now()}`;
    const result = await rotateAction.run(rotating.jobId, { reason: reason.trim() }, key);
    if (result.ok) {
      setRotating(null);
      setReason("");
      setOutcome("轮换请求已提交；逐节点状态以服务端为准。");
      jobs.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="证书管理"
        description="为服务组下发内部 TLS 凭据，或为网站签发、安装公网证书。"
        actions={tab === "internal" ? <Button type="button" onClick={() => { jobs.reload(); groups.reload(); }} loading={jobs.state.status === "loading"}><RefreshCw className="size-3.5" />刷新</Button> : undefined}
      />

      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      <TabBar
        label="证书分区"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: "internal", label: "内部 TLS 与下发" },
          { id: "public", label: "公网 HTTPS 证书" },
        ]}
      />

      {tab === "public" ? <ManagedWebsiteCertificates key={session.viewEpoch} /> : null}

      {tab === "internal" ? (
        <div className="space-y-5">
          {needsIssuer ? (
            <Notice tone="warning" title="内部 TLS 签发尚未配置">主控需要配置内部证书签发器后才能下发。网站公网证书可在另一页签申请。</Notice>
          ) : null}
          <Panel
            title="内部证书下发任务"
            description="选择服务组与下发内容，提交后查看每台节点的结果。"
            actions={<Button size="sm" variant="primary" disabled={!canManage || session.writesLocked || !internalAvailable} onClick={() => setCreateOpen(true)}>新建下发</Button>}
          >
            <AsyncPanel state={jobs.state} onRetry={jobs.reload} errorTitle="证书任务加载失败" empty={<EmptyState title="暂无内部证书任务" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="内部证书下发任务"
                  rows={jobItems}
                  rowKey={job => job.jobId}
                  columns={[
                    { key: "id", header: "任务", render: job => <span className="font-mono text-xs">{job.jobId}</span> },
                    { key: "group", header: "服务组", render: job => job.serviceGroupId },
                    { key: "type", header: "类型", render: job => (job.credentialType === "trust_bundle" ? "信任包" : "节点身份") },
                    { key: "state", header: "状态", render: job => JOB_STATE_TEXT[job.state] },
                    { key: "members", header: "成员快照", align: "right", render: job => job.memberNodeIdsSnapshot.length },
                    { key: "results", header: "逐节点", render: job => `${job.results.filter(result => result.state === "verified").length} 已验证 / ${job.results.length}` },
                    { key: "created", header: "创建", render: job => formatDateTime(job.createdAt) ?? "—" },
                    {
                      key: "actions",
                      header: "操作",
                      render: job => (
                        <div className="flex flex-wrap gap-1">
                          <Button size="sm" onClick={() => setDetailId(job.jobId)}>详情</Button>
                          <Button size="sm" onClick={() => { setRotating(job); setReason(""); }} disabled={!canManage || session.writesLocked}>轮换</Button>
                          <Button size="sm" variant="danger" onClick={() => { setRevoking(job); setReason(""); }} disabled={!canManage || session.writesLocked}>撤销</Button>
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
        </div>
      ) : null}

      <Modal
        open={createOpen}
        title="新建内部证书下发"
        description="选择要下发的服务组和内容。系统按当前成员列表创建任务。"
        onClose={() => { if (!createAction.pending) setCreateOpen(false); }}
        footer={
          <>
            <Button type="button" onClick={() => setCreateOpen(false)} disabled={createAction.pending}>取消</Button>
            <Button type="submit" form="cert-create" variant="primary" loading={createAction.pending} disabled={!selectedGroup?.memberNodeIds.length || !confirmImpact}>提交下发</Button>
          </>
        }
      >
        <form id="cert-create" onSubmit={create} className="space-y-4">
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          {createAction.error ? <Notice tone="error" title="提交失败">{describeApiError(createAction.error)}</Notice> : null}
          {groups.state.status === "error" ? <Notice tone="error" title="服务组加载失败">{describeApiError(groups.state.error)}</Notice> : null}
          {groups.state.status === "ready" && groupItems.length === 0 ? <Notice tone="info" title="请先创建服务组">在服务组中添加节点，再返回这里提交下发。</Notice> : null}
          <Field label="服务组">
            {ids => (
              <Select id={ids.inputId} value={serviceGroupId} onChange={event => setServiceGroupId(event.target.value)} required>
                <option value="">选择服务组</option>
                {groupItems.map(group => <option key={group.id} value={group.id}>{group.name}（{group.memberNodeIds.length} 台）</option>)}
              </Select>
            )}
          </Field>
          <Field label="下发内容">
            {ids => (
              <Select id={ids.inputId} value={credentialType} onChange={event => setCredentialType(event.target.value as typeof credentialType)}>
                <option value="trust_bundle">信任内部 CA</option>
                <option value="per_node_identity">为每台节点签发身份证书</option>
              </Select>
            )}
          </Field>
          {credentialType === "per_node_identity" ? (
            <details className="rounded-lg border border-border p-3">
              <summary className="cursor-pointer text-sm">调整有效期（默认 30 天）</summary>
              <div className="mt-2">
                <Field label="有效期（小时）">
                  {ids => <TextInput id={ids.inputId} type="number" min={1} max={8760} value={validityHours} onChange={event => setValidityHours(event.target.value)} />}
                </Field>
              </div>
            </details>
          ) : null}
          <label className="flex items-start gap-2 text-sm">
            <Checkbox className="mt-1" checked={confirmImpact} onChange={event => setConfirmImpact(event.target.checked)} />
            我确认该操作会影响所选服务组成员，并由服务端逐节点执行。
          </label>
        </form>
      </Modal>

      <Modal
        open={detail !== null}
        title={detail ? `证书任务 ${detail.jobId}` : "证书任务"}
        onClose={() => setDetailId(null)}
        wide
        footer={<Button type="button" onClick={() => setDetailId(null)}>关闭</Button>}
      >
        {detail ? (
          <div className="space-y-3 text-sm">
            <p>状态：{JOB_STATE_TEXT[detail.state]} · 类型 {detail.credentialType}</p>
            <p>成员快照：{detail.memberNodeIdsSnapshot.join("、") || "空"}</p>
            <DataTable
              caption="证书逐节点结果"
              rows={detail.results}
              rowKey={result => result.nodeId}
              columns={[
                { key: "node", header: "节点", render: result => <span className="font-mono text-xs">{result.nodeId}</span> },
                { key: "state", header: "状态", render: result => NODE_STATE_TEXT[result.state] },
                { key: "serial", header: "序列号", render: result => result.certificateSerial ?? "—" },
                { key: "identity", header: "身份", render: result => result.identity ?? "—" },
                { key: "expires", header: "到期", render: result => formatDateTime(result.expiresAt) ?? "—" },
                { key: "error", header: "错误", render: result => result.errorCode ?? "—" },
              ]}
            />
            <p className="text-xs text-muted-foreground">下发成功不等于 TLS 已建立；还需服务进程启用 TLS 并验证 SAN/信任链。离线节点显示等待，不按成功计数。</p>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={revoking !== null}
        title="撤销内部证书下发"
        description="吊销不会让对端立即停止信任；需要记录撤销传播/信任更新状态。"
        onClose={() => { if (!revokeAction.pending) setRevoking(null); }}
        footer={
          <>
            <Button type="button" onClick={() => setRevoking(null)} disabled={revokeAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={revoke} loading={revokeAction.pending} disabled={!reason.trim()}>确认撤销</Button>
          </>
        }
      >
        {revokeAction.error ? <Notice tone="error" title="撤销失败">{describeApiError(revokeAction.error)}</Notice> : null}
        <Field label="原因">{ids => <TextInput id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} required />}</Field>
      </Modal>

      <Modal
        open={rotating !== null}
        title="轮换内部证书"
        description="轮换是异步任务；逐节点结果以服务端为准。"
        onClose={() => { if (!rotateAction.pending) setRotating(null); }}
        footer={
          <>
            <Button type="button" onClick={() => setRotating(null)} disabled={rotateAction.pending}>取消</Button>
            <Button type="button" variant="primary" onClick={rotate} loading={rotateAction.pending} disabled={!reason.trim()}>确认轮换</Button>
          </>
        }
      >
        {rotateAction.error ? <Notice tone="error" title="轮换失败">{describeApiError(rotateAction.error)}</Notice> : null}
        <Field label="原因">{ids => <TextInput id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} required />}</Field>
      </Modal>
    </div>
  );
}
