import { formatDateTime } from "@/lib/format";
import { useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { DefinitionList } from "@/components/ui/table";
import { PageHeader, Panel } from "@/components/ui/panel";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi } from "@/lib/api/endpoints";
import type { NodeCapabilities, NodeDetail, NodeDiagnostic, NodeIdentity, TrafficQuota } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { NodeQuotaEditor } from "./node-quota-editor";

type ActionKind = "suspend" | "revoke";

const CONNECTION_TEXT: Record<NodeDiagnostic["connectionState"], string> = {
  online: "在线",
  unreachable: "失联（不能据此推断关机）",
  identity_rejected: "身份被拒绝",
  unknown: "未知",
};

function DetailBody({ detail }: { readonly detail: NodeDetail }) {
  return (
    <DefinitionList
      items={[
        { term: "显示名称", value: detail.displayName || detail.nodeId },
        { term: "节点 ID", value: <span className="font-mono text-xs">{detail.nodeId}</span> },
        { term: "状态", value: detail.status },
        { term: "版本 revision", value: detail.revision },
        { term: "平台", value: detail.platform ?? "未知" },
        { term: "Agent 版本", value: detail.agentVersion ?? "未知" },
        { term: "最近在线", value: detail.lastSeenAt ?? "未知" },
      ]}
    />
  );
}

function IdentityBody({ identity }: { readonly identity: NodeIdentity }) {
  return (
    <DefinitionList
      items={[
        { term: "身份状态", value: identity.revoked ? "已吊销" : "未吊销" },
        { term: "证书 SHA-256", value: identity.certificateFingerprintSha256 ?? "未提供" },
        { term: "签发者 SHA-256", value: identity.issuerFingerprintSha256 ?? "未提供" },
        { term: "序列号", value: identity.serial ?? "未提供" },
        { term: "到期", value: identity.notAfter ?? "未提供" },
        { term: "最近验证", value: identity.lastVerifiedAt ?? "未提供" },
      ]}
    />
  );
}

function CapabilitiesBody({ capabilities }: { readonly capabilities: NodeCapabilities }) {
  const protocol = capabilities.protocolMajor === undefined || capabilities.protocolMinor === undefined
    ? "未返回（当前 Controller 不记录协议版本）"
    : `${capabilities.protocolMajor}.${capabilities.protocolMinor}`;
  return (
    <div className="space-y-3 px-5 py-4 text-sm">
      <p>
        协议版本：{protocol}
        {capabilities.observedAt ? ` · 观察于 ${capabilities.observedAt}` : ""}
      </p>
      {capabilities.message ? <p className="text-xs text-muted-foreground">{capabilities.message}</p> : null}
      {capabilities.capabilities.length === 0 ? (
        <p className="text-muted-foreground">服务端未返回任何能力项。</p>
      ) : (
        <ul className="flex flex-wrap gap-2">
          {capabilities.capabilities.map(capability => (
            <li key={capability} className="rounded-md border px-2 py-1 font-mono text-xs">
              {capability}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function DiagnosticBody({ diagnostic }: { readonly diagnostic: NodeDiagnostic }) {
  return (
    <div className="space-y-4 px-5 py-4 text-sm">
      <DefinitionList
        className="px-0 py-0"
        items={[
          { term: "连接状态", value: CONNECTION_TEXT[diagnostic.connectionState] },
          { term: "最近心跳", value: diagnostic.lastHeartbeatAt ?? "未提供" },
          { term: "身份已验证", value: diagnostic.identityVerified === undefined ? "未知" : diagnostic.identityVerified ? "是" : "否" },
          { term: "Agent 版本", value: diagnostic.agentVersion ?? "未提供" },
          { term: "身份有效期", value: diagnostic.agent?.certificateExpiresAt ?? "未提供" },
          { term: "最近身份握手", value: diagnostic.agent?.recorded ? diagnostic.agent.observedAt ?? "未提供" : "尚未记录" },
        ]}
      />
      {diagnostic.agent?.history && diagnostic.agent.history.length > 0 ? <div><p className="font-medium">最近连接记录（最多 100 条）</p><ul className="mt-2 space-y-1 text-xs">{diagnostic.agent.history.map((event,index) => <li key={`${event.at}-${index}`}>{event.at} · {event.code === "authenticated" ? "身份握手已验证" : "连接断开"}</li>)}</ul></div> : null}
      <div>
        <p className="font-medium">待执行任务</p>
        {diagnostic.pendingTasksKnown === false ? <p className="mt-1 text-muted-foreground">待执行任务状态未记录，请到任务管理查看。</p> : !diagnostic.pendingTasks || diagnostic.pendingTasks.length === 0 ? (
          <p className="mt-1 text-muted-foreground">没有待执行任务。</p>
        ) : (
          <ul className="mt-2 space-y-1">
            {diagnostic.pendingTasks.map(task => (
              <li key={task.id} className="flex flex-wrap gap-2 text-xs">
                <span className="font-mono">{task.id}</span>
                <span>{task.expired ? "已过期" : "未过期"}</span>
                {task.requiresReapproval ? <span className="text-amber-800">危险任务需重新审批</span> : null}
              </li>
            ))}
          </ul>
        )}
      </div>
      {diagnostic.diagnosticNotes && diagnostic.diagnosticNotes.length > 0 ? (
        <div>
          <p className="font-medium">诊断说明</p>
          <ul className="mt-1 list-disc space-y-1 pl-5 text-muted-foreground">
            {diagnostic.diagnosticNotes.map(note => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </div>
      ) : null}
      <p className="text-xs text-muted-foreground">
        失去心跳只表示“失联”，不代表节点关机；重连必须重新验证身份，已吊销凭据不可恢复。
      </p>
    </div>
  );
}

function QuotaBody({ quota }: { readonly quota: TrafficQuota }) {
  const sourceText = quota.source === "agent_estimate"
    ? "Agent 估算（非云账单）"
    : quota.source === "provider_billing" ? "供应商账单" : quota.source === "manual_correction" ? "人工修正" : "未返回";
  return (
    <div className="space-y-3 px-5 py-4 text-sm">
      <DefinitionList
        className="px-0 py-0"
        items={[
          { term: "配额 (GB)", value: quota.quotaGb },
          { term: "已用 (GB)", value: quota.usedGb === undefined ? "缺失（离线或未采集）" : quota.usedGb },
          {
            term: "周期重置日",
            value: quota.cycleResetDay === undefined
              ? "未返回"
              : `每月 ${quota.cycleResetDay} 日 · ${quota.timezone ?? "未返回"}`,
          },
          { term: "方向", value: quota.direction },
          { term: "来源", value: sourceText },
          { term: "数据完整", value: quota.dataComplete === undefined ? "未知" : quota.dataComplete ? "是" : "否（存在缺口）" },
          { term: "采样覆盖", value: quota.coverage ? `${formatDateTime(quota.coverage.observedFrom)} 至 ${formatDateTime(quota.coverage.observedUntil)} · ${quota.coverage.samples} 次采样 · ${quota.coverage.gaps} 次缺口/重置` : "等待 Agent 计数采样" },
          { term: "阈值", value: quota.thresholdsPct?.join(" / ") ?? "未设置" },
          { term: "预测", value: quota.predictionGb === undefined ? "样本不足，无法估算" : `${quota.predictionGb} GB（置信度 ${quota.predictionConfidence ?? "未知"}）` },
          { term: "到期类型", value: quota.expiryType ?? "未设置" },
          { term: "到期时间", value: quota.expiresAt ?? "未设置" },
          { term: "提醒天数", value: quota.remindDays?.join(" / ") ?? "未设置" },
        ]}
      />
      <p className="text-xs text-muted-foreground">
        估算值不等同云厂商账单；缺失流量不按 0 GB 处理。默认仅提醒，不自动断网或购买流量。
      </p>
    </div>
  );
}

export function NodeDetailPage({ nodeId }: { readonly nodeId: string }) {
  const session = useSession();
  const [actionKind, setActionKind] = useState<ActionKind | null>(null);
  const [reason, setReason] = useState("");
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [quotaOpen, setQuotaOpen] = useState(false);

  const canManage = session.can("node.manage") || session.can("node.suspend") || session.can("node.revoke");
  const canManageQuota = session.can("quota.manage") || session.can("node.manage");
  const locked = !canManage || session.writesLocked;
  const detail = useResource(`node:detail:${session.viewEpoch}:${nodeId}`, signal =>
    nodesApi.detail(nodeId, signal).then(result => result.data),
  );
  // 真实 Controller 只暴露 identity/capabilities/diagnostics 子资源，没有
  // `GET /api/v1/nodes/{id}` 详情路由（GM-W4-02）：子资源不再门控在详情 ready 之后，
  // 独立加载、独立展示错误；详情 404 时元数据面板回退为节点列表中的真实摘要，不编造字段。
  const detailMissing = detail.state.status === "error" && detail.state.error.status === 404;
  const identity = useResource(
    `node:identity:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.identity(nodeId, signal).then(result => result.data),
  );
  const capabilities = useResource(
    `node:capabilities:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.capabilities(nodeId, signal).then(result => result.data),
  );
  const diagnostic = useResource(
    `node:diagnostics:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.diagnostics(nodeId, signal).then(result => result.data),
  );
  const quota = useResource(
    `node:quota:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.trafficQuota(nodeId, signal).then(result => result.data),
  );
  // 真实 Controller 在节点未配置配额时返回 404 quota_not_found；这是“尚未配置”而非加载失败，
  // 据此展示诚实空态并允许首次创建（GM-W4-02）。其余错误仍按失败展示。
  const quotaMissing = quota.state.status === "error" && quota.state.error.status === 404;
  const quotaEditable = quota.state.status === "ready" || quotaMissing;
  // 真实后端拒绝节点生命周期请求携带 expectedRevision（节点清单无持久化 revision，
  // 返回 `revision_not_supported`），因此只提交 reason，不伪造乐观锁（GM-W4-02）。
  const suspendAction = useAction((body: { reason: string }) => nodesApi.suspend(nodeId, body));
  const revokeAction = useAction((body: { reason: string }) => nodesApi.revoke(nodeId, body));
  const actionPending = suspendAction.pending || revokeAction.pending;
  const actionError = suspendAction.error ?? revokeAction.error;

  async function submitAction() {
    if (!actionKind) return;
    const body = { reason: reason.trim() };
    const result = actionKind === "revoke" ? await revokeAction.run(body) : await suspendAction.run(body);
    if (result.ok) {
      setOutcome(actionKind === "revoke" ? "节点身份已吊销（服务端结果）。" : "已提交暂停请求（服务端结果）。");
      setActionKind(null);
      setReason("");
      setConfirmRevoke(false);
      detail.reload();
      identity.reload();
      diagnostic.reload();
    }
  }

  // 详情端点未提供时的诚实回退：只展示节点列表返回的真实摘要字段。
  const summary = useResource(
    `node:summary:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.list({ signal }).then(result => result.data.items.find(item => item.id === nodeId) ?? null),
    { enabled: detailMissing },
  );

  return (
    <div className="space-y-5">
      <PageHeader
        title={detail.state.status === "ready" ? detail.state.data.displayName || nodeId : "节点详情"}
        description="节点元数据、身份、能力、诊断与流量配额。所有字段来自服务端；失联不推断关机。"
        actions={
          <>
            <a
              href="#/nodes"
              className="inline-flex h-9 items-center rounded-md border border-input bg-card px-3 text-sm hover:bg-secondary"
            >
              返回节点列表
            </a>
            <Button
              type="button"
              disabled={locked}
              onClick={() => {
                setOutcome(null);
                setActionKind("suspend");
              }}
            >
              暂停派发
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={locked}
              onClick={() => {
                setOutcome(null);
                setActionKind("revoke");
              }}
            >
              吊销身份
            </Button>
          </>
        }
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      {!canManage ? (
        <Notice tone="warning" title="操作入口已锁定">
          当前身份没有返回节点管理权限（`node.manage`/`node.suspend`/`node.revoke`）。详情仍为只读。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能执行暂停/吊销。</Notice>
      ) : null}

      <Panel title="节点元数据" description="GET /api/v1/nodes/{nodeId}">
        {detailMissing ? (
          <>
            <Notice tone="info" title="详情端点未提供">
              当前 Controller 未实现 GET /api/v1/nodes/&#123;nodeId&#125;；以下字段来自节点列表返回的真实摘要，
              不展示候选契约中的 revision/platform/agentVersion。
            </Notice>
            <AsyncPanel
              state={summary.state}
              onRetry={summary.reload}
              errorTitle="节点摘要加载失败"
              empty={<p className="px-6 py-8 text-sm text-muted-foreground">节点列表中没有该节点（可能未授权或已删除）。</p>}
              isEmpty={data => data === null}
            >
              {data =>
              data === null ? null : (
                <DefinitionList
                  items={[
                    { term: "显示名称", value: data.name.trim() || data.id },
                    { term: "节点 ID", value: <span className="font-mono text-xs">{data.id}</span> },
                    { term: "在线状态", value: data.online ? "在线" : "离线" },
                    { term: "区域", value: data.region ?? "未返回" },
                    { term: "供应商", value: data.provider ?? "未返回" },
                    { term: "IP", value: data.ip ?? "未返回" },
                    { term: "标签", value: data.tags?.join("、") || "未设置" },
                  ]}
                />
              )
            }
            </AsyncPanel>
          </>
        ) : (
          <AsyncPanel
            state={detail.state}
            onRetry={detail.reload}
            errorTitle="节点详情加载失败"
            empty={<p className="px-6 py-8 text-sm text-muted-foreground">未找到该节点。</p>}
            isEmpty={() => false}
          >
            {data => <DetailBody detail={data} />}
          </AsyncPanel>
        )}
      </Panel>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="节点身份" description="GET /api/v1/nodes/{nodeId}/identity；不返回私钥。">
          <AsyncPanel state={identity.state} onRetry={identity.reload} errorTitle="身份信息加载失败">
            {data => <IdentityBody identity={data} />}
          </AsyncPanel>
        </Panel>
        <Panel title="协商能力" description="GET /api/v1/nodes/{nodeId}/capabilities">
          <AsyncPanel state={capabilities.state} onRetry={capabilities.reload} errorTitle="能力信息加载失败">
            {data => <CapabilitiesBody capabilities={data} />}
          </AsyncPanel>
        </Panel>
        <Panel title="连接诊断" description="GET /api/v1/nodes/{nodeId}/diagnostics">
          <AsyncPanel state={diagnostic.state} onRetry={diagnostic.reload} errorTitle="诊断加载失败">
            {data => <DiagnosticBody diagnostic={data} />}
          </AsyncPanel>
        </Panel>
        <Panel
          title="流量与配额"
          description="GET /api/v1/nodes/{nodeId}/traffic-quota；估算值与账单分开。"
          actions={
            <Button
              type="button"
              size="sm"
              disabled={!canManageQuota || session.writesLocked || !quotaEditable}
              onClick={() => setQuotaOpen(true)}
            >
              编辑配额
            </Button>
          }
        >
          {quotaMissing ? (
            <p className="px-5 py-8 text-sm text-muted-foreground">
              服务端尚未配置该节点的流量配额（404 quota_not_found）；点击“编辑配额”创建，提交前不预填任何服务端数值。
            </p>
          ) : (
            <AsyncPanel state={quota.state} onRetry={quota.reload} errorTitle="流量配额加载失败">
              {data => <QuotaBody quota={data} />}
            </AsyncPanel>
          )}
        </Panel>
      </div>

      <NodeQuotaEditor
        nodeId={nodeId}
        quota={quota.state.status === "ready" ? quota.state.data : null}
        open={quotaOpen}
        canManage={canManageQuota}
        writesLocked={session.writesLocked}
        onClose={() => setQuotaOpen(false)}
        onSaved={() => {
          setQuotaOpen(false);
          setOutcome("流量配额已保存（服务端结果）。");
          quota.reload();
        }}
      />

      <Modal
        open={actionKind !== null}
        title={actionKind === "revoke" ? "吊销节点身份" : "暂停节点任务派发"}
        description={
          actionKind === "revoke"
            ? "吊销后该节点凭据不可恢复；在途任务与待审批计划需要人工处置。该操作不可用简单回滚。"
            : "暂停只停止新的任务派发，不删除节点，也不代表节点已离线。"
        }
        onClose={() => {
          if (actionPending) return;
          setActionKind(null);
          setConfirmRevoke(false);
        }}
        footer={
          <>
            <Button type="button" disabled={actionPending} onClick={() => setActionKind(null)}>
              取消
            </Button>
            <Button
              type="button"
              variant="danger"
              onClick={submitAction}
              loading={actionPending}
              disabled={!reason.trim() || (actionKind === "revoke" && !confirmRevoke)}
            >
              确认执行
            </Button>
          </>
        }
      >
        {actionError ? <Notice tone="error" title="操作未完成">{describeApiError(actionError)}</Notice> : null}
        <div className="space-y-4">
          <Field label="原因" hint="服务端会记录到审计；请勿包含秘密。">
            {ids => (
              <TextArea
                id={ids.inputId}
                value={reason}
                onChange={event => setReason(event.target.value)}
                disabled={actionPending}
                required
              />
            )}
          </Field>
          {actionKind === "revoke" ? (
            <label className="flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-1 size-4 accent-[var(--destructive)]"
                checked={confirmRevoke}
                onChange={event => setConfirmRevoke(event.target.checked)}
                disabled={actionPending}
              />
              我理解吊销身份不可恢复，且不会自动回滚在途任务。
            </label>
          ) : null}
          <p className="text-xs text-muted-foreground">
            当前 Controller 的节点生命周期操作不接受 expectedRevision（节点清单无持久化版本号）；
            暂停/吊销以服务端审计与权限校验为准。
          </p>
        </div>
      </Modal>
    </div>
  );
}
