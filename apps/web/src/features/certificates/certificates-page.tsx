import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
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
  const [detail, setDetail] = useState<InternalCertificateJob | null>(null);
  const [revoking, setRevoking] = useState<InternalCertificateJob | null>(null);
  const [rotating, setRotating] = useState<InternalCertificateJob | null>(null);
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);

  const [serviceGroupId, setServiceGroupId] = useState("");
  const [credentialType, setCredentialType] = useState<"trust_bundle" | "per_node_identity">("trust_bundle");
  const [identityProfile, setIdentityProfile] = useState("");
  const [validityHours, setValidityHours] = useState("");
  const [confirmImpact, setConfirmImpact] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);

  const jobs = useResource(`certs:jobs:${session.viewEpoch}`, signal => certificatesApi.distributions(signal).then(result => result.data));
  const groups = useResource(`certs:groups:${session.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data));
  const jobItems = jobs.state.status === "ready" ? jobs.state.data.items : [];
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
    setIdentityProfile("");
    setValidityHours("");
    setConfirmImpact(false);
    setLocalError(null);
  }, [createOpen]);

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!serviceGroupId || !selectedGroup) return setLocalError("必须选择一个服务组。");
    if (!confirmImpact) return setLocalError("必须确认服务影响。");
    setLocalError(null);
    const key = globalThis.crypto?.randomUUID?.() ?? `cert-${Date.now()}`;
    const result = await createAction.run(
      {
        serviceGroupId,
        credentialType,
        ...(identityProfile.trim() ? { identityProfile: identityProfile.trim() } : {}),
        ...(validityHours ? { validityHours: Number(validityHours) } : {}),
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
        description="内部 TLS 下发按服务组成员快照逐节点执行；公网 ACME/证书生命周期当前没有契约端点。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">TLS</span>}
        actions={<Button type="button" onClick={() => jobs.reload()} loading={jobs.state.status === "loading"}>刷新</Button>}
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

      {tab === "public" ? (
        <Notice tone="warning" title="公网证书契约缺口">
          当前 OpenAPI 只有 `certificates/internal/distributions`，没有 ACME 申请、续期、吊销或公网证书列表端点。
          本页不伪造证书签发成功；公网 HTTPS 验收保持 BLOCKED/UNTESTED。
        </Notice>
      ) : null}

      {tab === "internal" ? (
        <div className="space-y-5">
          <Notice tone="info" title="安全边界">
            信任包与节点身份凭证分开；每个节点使用独立身份/私钥，主控不导出私钥。任务保留提交时成员快照，新增成员不会继承旧下发。
          </Notice>
          <Panel
            title="内部证书下发任务"
            description="GET /api/v1/certificates/internal/distributions；离线节点显示等待，不计为成功。"
            actions={<Button size="sm" variant="primary" disabled={!canManage || session.writesLocked} onClick={() => setCreateOpen(true)}>新建下发</Button>}
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
                          <Button size="sm" onClick={() => setDetail(job)}>详情</Button>
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
        description="提交时冻结服务组成员快照；实际签发/下发是异步任务，不能在此声称 TLS 已建立。"
        onClose={() => { if (!createAction.pending) setCreateOpen(false); }}
        footer={
          <>
            <Button type="button" onClick={() => setCreateOpen(false)} disabled={createAction.pending}>取消</Button>
            <Button type="submit" form="cert-create" variant="primary" loading={createAction.pending}>提交下发</Button>
          </>
        }
      >
        <form id="cert-create" onSubmit={create} className="space-y-4">
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          {createAction.error ? <Notice tone="error" title="提交失败">{describeApiError(createAction.error)}</Notice> : null}
          <Field label="服务组">
            {ids => (
              <Select id={ids.inputId} value={serviceGroupId} onChange={event => setServiceGroupId(event.target.value)} required>
                <option value="">请选择</option>
                {groupItems.map(group => <option key={group.id} value={group.id}>{group.name}（{group.memberNodeIds.length} 台，rev {group.revision}）</option>)}
              </Select>
            )}
          </Field>
          <Field label="凭据类型">
            {ids => (
              <Select id={ids.inputId} value={credentialType} onChange={event => setCredentialType(event.target.value as typeof credentialType)}>
                <option value="trust_bundle">CA 信任包</option>
                <option value="per_node_identity">逐节点身份凭证</option>
              </Select>
            )}
          </Field>
          <Field label="身份模板（可选）" hint="不同节点必须获得不同身份；服务端负责约束。">
            {ids => <TextInput id={ids.inputId} value={identityProfile} onChange={event => setIdentityProfile(event.target.value)} />}
          </Field>
          <Field label="有效期小时（可选）">
            {ids => <TextInput id={ids.inputId} type="number" min={1} max={8760} value={validityHours} onChange={event => setValidityHours(event.target.value)} />}
          </Field>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1 size-4 accent-[var(--primary)]" checked={confirmImpact} onChange={event => setConfirmImpact(event.target.checked)} />
            我确认该操作会影响所选服务组成员，并由服务端逐节点执行。
          </label>
        </form>
      </Modal>

      <Modal
        open={detail !== null}
        title={detail ? `证书任务 ${detail.jobId}` : "证书任务"}
        onClose={() => setDetail(null)}
        wide
        footer={<Button type="button" onClick={() => setDetail(null)}>关闭</Button>}
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
