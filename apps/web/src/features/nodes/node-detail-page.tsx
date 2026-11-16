import { formatDateTime } from "@/lib/format";
import { useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button, LinkButton } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextArea } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { DefinitionList } from "@/components/ui/table";
import { PageHeader, Panel } from "@/components/ui/panel";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi } from "@/lib/api/endpoints";
import type { NodeCapabilities, NodeDetail, NodeDiagnostic, NodeIdentity, TrafficQuota, TrafficUsage } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { NodeQuotaEditor } from "./node-quota-editor";
import { NodeEditor } from "./node-editor";

type ActionKind = "suspend" | "revoke" | "remove";

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
        { term: "地址", value: detail.address || "未设置" },
        { term: "地区", value: detail.region || "未设置" },
        { term: "云厂商", value: detail.provider || "未设置" },
        { term: "国家码", value: detail.countryCode || "未设置" },
        { term: "标签", value: detail.tags?.join("、") || "未设置" },
        { term: "更新时间", value: detail.updatedAt ?? "未提供" },
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

function formatBytes(value?: number): string {
  if (value === undefined || value <= 0) return "未采集";
  const gb = value / 1024 ** 3;
  return gb >= 1024 ? `${(gb / 1024).toFixed(1)} TB` : `${gb.toFixed(1)} GB`;
}

function HardwareBody({ facts, usage }: { readonly facts: NodeCapabilities; readonly usage: TrafficUsage | null }) {
  const monthly = usage?.available && usage.bytesIn !== undefined && usage.bytesOut !== undefined
    ? `${formatBytes(usage.bytesIn + usage.bytesOut)}（下行 ${formatBytes(usage.bytesIn)} / 上行 ${formatBytes(usage.bytesOut)}）`
    : "暂无采样";
  return (
    <DefinitionList
      className="px-5 py-4"
      items={[
        { term: "CPU 型号", value: facts.cpuModel || "未采集" },
        { term: "CPU 核数", value: facts.cpuCores ? `${facts.cpuCores} 核` : "未采集" },
        { term: "内存大小", value: formatBytes(facts.memoryTotalBytes) },
        { term: "存储大小", value: formatBytes(facts.diskTotalBytes) },
        { term: "本月流量", value: monthly },
        { term: "采集时间", value: facts.observedAt ?? "未采集" },
      ]}
    />
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
  const usedRatio =
    quota.usedGb !== undefined && quota.quotaGb > 0
      ? Math.max(0, Math.min(100, Math.round((quota.usedGb / quota.quotaGb) * 100)))
      : null;
  const barColor =
    usedRatio === null
      ? "bg-muted"
      : usedRatio >= 85
        ? "bg-[#DE5555]"
        : usedRatio >= 65
          ? "bg-[#D89B2B]"
          : "bg-[#6658E8]";

  return (
    <div className="space-y-4 px-5 py-4 text-sm">
      {usedRatio !== null ? (
        <div className="rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
          <div className="flex items-center justify-between text-xs font-semibold">
            <span className="text-muted-foreground">流量使用进度</span>
            <span className="tabular-nums text-foreground">
              {quota.usedGb} / {quota.quotaGb} GB ({usedRatio}%)
            </span>
          </div>
          <div className="mt-2 h-2 overflow-hidden rounded-full bg-secondary">
            <div
              className={`h-full rounded-full transition-all duration-700 ${barColor}`}
              style={{ width: `${usedRatio}%` }}
            />
          </div>
        </div>
      ) : null}
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
  const [editorOpen, setEditorOpen] = useState(false);

  const canManage = session.can("node.manage");
  const canSuspend = session.can("node.suspend");
  const canRevoke = session.can("node.revoke");
  const canManageQuota = session.can("quota.manage") || canManage;
  const detail = useResource(`node:detail:${session.viewEpoch}:${nodeId}`, signal =>
    nodesApi.detail(nodeId, signal).then(result => result.data),
    { refreshIntervalMs: 5000 },
  );
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
    { refreshIntervalMs: 5000 },
  );
  const quota = useResource(
    `node:quota:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.trafficQuota(nodeId, signal).then(result => result.data),
  );
  const usage = useResource(
    `node:usage:${session.viewEpoch}:${nodeId}`,
    signal => nodesApi.trafficUsage(nodeId, signal).then(result => result.data),
  );
  // 真实 Controller 在节点未配置配额时返回 404 quota_not_found；这是“尚未配置”而非加载失败，
  // 据此展示诚实空态并允许首次创建（GM-W4-02）。其余错误仍按失败展示。
  const quotaMissing = quota.state.status === "error" && quota.state.error.status === 404;
  const quotaEditable = quota.state.status === "ready" || quotaMissing;
  const suspendAction = useAction((body: { reason: string }) => nodesApi.suspend(nodeId, body));
  const revokeAction = useAction((body: { reason: string }) => nodesApi.revoke(nodeId, body));
  const removeAction = useAction((body: { reason: string }) => nodesApi.remove(nodeId, body));
  const actionPending = suspendAction.pending || revokeAction.pending || removeAction.pending;
  const activeAction = actionKind === "remove" ? removeAction : actionKind === "revoke" ? revokeAction : suspendAction;
  const actionError = activeAction.error;
  const actionAllowed = !session.writesLocked && (actionKind === "suspend" ? canSuspend : canRevoke);

  function openAction(kind: ActionKind) {
    if (session.writesLocked || (kind === "suspend" ? !canSuspend : !canRevoke)) return;
    suspendAction.reset();
    revokeAction.reset();
    removeAction.reset();
    setReason("");
    setConfirmRevoke(false);
    setOutcome(null);
    setActionKind(kind);
  }

  async function submitAction() {
    if (!actionKind || !actionAllowed || actionPending || (actionKind !== "remove" && !reason.trim()) || (actionKind !== "suspend" && !confirmRevoke)) return;
    const result = await activeAction.run({ reason: actionKind === "remove" ? "未填写原因" : reason.trim() });
    if (result.ok) {
      setActionKind(null);
      setReason("");
      setConfirmRevoke(false);
      if (actionKind === "remove") {
        window.location.hash = "#/nodes";
        return;
      }
      setOutcome(actionKind === "revoke" ? "节点身份已吊销（服务端结果）。" : "已提交暂停请求（服务端结果）。");
      detail.reload();
      identity.reload();
      diagnostic.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={detail.state.status === "ready" ? detail.state.data.displayName || nodeId : "节点详情"}
        description="节点元数据、身份、能力与流量配额；连接诊断每 5 秒刷新。未接入或失联不等于关机。"
        actions={
          <>
            <LinkButton href="#/nodes">
              返回节点列表
            </LinkButton>
            <Button type="button" disabled={!canManage || session.writesLocked || detail.state.status !== "ready"} onClick={() => setEditorOpen(true)}>编辑节点</Button>
            <Button type="button" variant="danger" disabled={!canRevoke || session.writesLocked} onClick={() => openAction("remove")}>删除节点</Button>
            <Button
              type="button"
              disabled={!canSuspend || session.writesLocked}
              onClick={() => openAction("suspend")}
            >
              暂停派发
            </Button>
            <Button
              type="button"
              variant="danger"
              disabled={!canRevoke || session.writesLocked}
              onClick={() => openAction("revoke")}
            >
              吊销身份
            </Button>
          </>
        }
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      {!canManage && !canSuspend && !canRevoke ? (
        <Notice tone="warning" title="操作入口已锁定">
          当前身份没有节点写入权限，详情仍可查看。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="写入已锁定">请重新登录后再编辑、删除或管理节点。</Notice>
      ) : null}

      <Panel title="节点元数据">
        <AsyncPanel
          state={detail.state}
          onRetry={detail.reload}
          errorTitle="节点详情加载失败"
          empty={<p className="px-6 py-8 text-sm text-muted-foreground">未找到该节点。</p>}
          isEmpty={() => false}
        >
          {data => <DetailBody detail={data} />}
        </AsyncPanel>
      </Panel>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="节点身份" description="不显示私钥。">
          <AsyncPanel state={identity.state} onRetry={identity.reload} errorTitle="身份信息加载失败">
            {data => <IdentityBody identity={data} />}
          </AsyncPanel>
        </Panel>
        <Panel title="硬件与流量" description="来自节点上次身份握手；未采集不按 0 处理。">
          <AsyncPanel state={capabilities.state} onRetry={capabilities.reload} errorTitle="硬件信息加载失败">
            {data => <HardwareBody facts={data} usage={usage.state.status === "ready" ? usage.state.data : null} />}
          </AsyncPanel>
        </Panel>
        <Panel title="连接诊断">
          <AsyncPanel state={diagnostic.state} onRetry={diagnostic.reload} errorTitle="诊断加载失败">
            {data => <DiagnosticBody diagnostic={data} />}
          </AsyncPanel>
        </Panel>
        <Panel
          title="流量与配额"
          description="估算值与账单分开。"
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

      {editorOpen && detail.state.status === "ready" ? (
        <NodeEditor
          key={`editor:${session.viewEpoch}:${nodeId}`}
          nodeId={nodeId}
          metadata={detail.state.data}
          canManage={canManage}
          writesLocked={session.writesLocked}
          onClose={() => setEditorOpen(false)}
          onRefresh={() => { setEditorOpen(false); detail.reload(); }}
          onSaved={() => {
            setEditorOpen(false);
            setOutcome("节点信息已保存。");
            detail.reload();
          }}
        />
      ) : null}

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
        title={actionKind === "remove" ? "删除节点" : actionKind === "revoke" ? "吊销节点身份" : "暂停节点任务派发"}
        description={
          actionKind === "remove"
            ? "停止此节点接入并从列表移除，保留历史审计与关联记录；重新使用须重新接入。"
            : actionKind === "revoke"
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
            <Button type="button" disabled={actionPending} onClick={() => { setActionKind(null); setConfirmRevoke(false); }}>
              取消
            </Button>
            <Button
              type="button"
              variant="danger"
              onClick={submitAction}
              loading={actionPending}
              disabled={!actionAllowed || (actionKind !== "remove" && !reason.trim()) || (actionKind !== "suspend" && !confirmRevoke)}
            >
              {actionKind === "remove" ? "确认删除" : "确认执行"}
            </Button>
          </>
        }
      >
        {actionError ? <Notice tone="error" title="操作未完成">{describeApiError(actionError)}</Notice> : null}
        <div className="space-y-4">
          {actionKind === "remove" ? null : (
            <Field label="原因" hint="请勿包含秘密或凭据。">
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
          )}
          {actionKind === "revoke" || actionKind === "remove" ? (
            <label className="flex items-start gap-2 text-sm">
              <Checkbox
                className="mt-1 accent-[var(--destructive)]"
                checked={confirmRevoke}
                onChange={event => setConfirmRevoke(event.target.checked)}
                disabled={actionPending}
              />
              {actionKind === "remove" ? "停止此节点接入并从列表移除，保留历史审计；重新使用须重新接入" : "我理解吊销身份不可恢复，且不会自动回滚在途任务。"}
            </label>
          ) : null}
          <p className="text-xs text-muted-foreground">
            {actionKind === "remove" ? "删除会记录到审计中，实际结果以服务端返回为准。" : "操作原因会记录到审计中，实际结果以服务端返回为准。"}
          </p>
        </div>
      </Modal>
    </div>
  );
}
