import { useState, type FormEvent } from "react";
import { EncryptedBackupsPanel } from "./encrypted-backups-panel";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextInput } from "@/components/ui/form";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { recoveryApi } from "@/lib/api/endpoints-system";
import type { AsyncOperation, RecoveryPlan } from "@/lib/api/dto";
import { useAction } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const OPERATION_STATUS: Record<AsyncOperation["status"], string> = {
  queued: "排队",
  prechecking: "预检中",
  running: "执行中",
  partially_completed: "部分完成",
  succeeded: "成功",
  failed: "失败",
  paused: "已暂停",
  manual_intervention: "需要人工介入",
};

function newKey(prefix: string) {
  return globalThis.crypto?.randomUUID?.() ?? `${prefix}-${Date.now()}`;
}

function ChecksTable({ checks, caption }: { readonly checks: RecoveryPlan["checks"]; readonly caption: string }) {
  if (!checks || checks.length === 0) {
    return <p className="text-xs text-muted-foreground">服务端未返回检查项；不能据此认定备份可用或隔离已生效。</p>;
  }
  return (
    <DataTable
      caption={caption}
      rows={checks}
      rowKey={(item, index) => `${item.name}-${index}`}
      columns={[
        { key: "name", header: "检查项", render: item => item.name },
        { key: "passed", header: "结果", render: item => (item.passed ? <span className="text-emerald-700">通过</span> : <span className="text-destructive">未通过</span>) },
        { key: "reason", header: "原因", render: item => item.reason ?? "—" },
      ]}
    />
  );
}

export function BackupsPage() {
  const session = useSession();
  const [backupId, setBackupId] = useState("");
  const [plan, setPlan] = useState<RecoveryPlan | null>(null);
  const [targetControllerId, setTargetControllerId] = useState("");
  const [fenced, setFenced] = useState(false);
  const [expectedVersion, setExpectedVersion] = useState("");
  const [expectedNodeIds, setExpectedNodeIds] = useState("");
  const [keyReference, setKeyReference] = useState("");
  const [preview, setPreview] = useState<RecoveryPlan | null>(null);
  const [operation, setOperation] = useState<AsyncOperation | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const canManage = session.can("backup.manage") || session.can("system.backup") || session.can("settings.manage");
  const writesLocked = session.writesLocked;

  const verifyAction = useAction((id: string) => recoveryApi.verifyBackup(id, newKey("backup-verify")));
  const previewAction = useAction((body: RecoveryPlan) => recoveryApi.preview(body, newKey("dr-preview")));
  const createAction = useAction((body: RecoveryPlan) => recoveryApi.createJob(body, newKey("dr-job")));
  const refreshAction = useAction((jobId: string) => recoveryApi.job(jobId));

  function planBody(): RecoveryPlan | null {
    if (!plan) return null;
    const nodeIds = expectedNodeIds.split(",").map(value => value.trim()).filter(Boolean);
    return {
      backupId: plan.backupId,
      targetControllerId: targetControllerId.trim(),
      oldControllerFenced: fenced,
      ...(expectedVersion.trim() ? { expectedVersion: expectedVersion.trim() } : {}),
      ...(nodeIds.length ? { expectedNodeIds: nodeIds } : {}),
      ...(keyReference.trim() ? { recoveryKeyReference: keyReference.trim() } : {}),
      ...(plan.checks ? { checks: plan.checks } : {}),
    };
  }

  const body = planBody();
  const previewChecks = preview?.checks ?? [];
  const previewBlocked = !preview || !fenced || !targetControllerId.trim() || (previewChecks.length > 0 && previewChecks.some(item => !item.passed));

  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!backupId.trim()) return setLocalError("必须填写备份 ID。");
    setLocalError(null);
    const result = await verifyAction.run(backupId.trim());
    if (result.ok && result.data) {
      const payload = result.data.data;
      setPlan(payload);
      setPreview(null);
      setOperation(null);
      setBackupId(payload.backupId);
      setTargetControllerId(payload.targetControllerId ?? "");
      setFenced(payload.oldControllerFenced === true);
      setExpectedVersion(payload.expectedVersion ?? "");
      setExpectedNodeIds((payload.expectedNodeIds ?? []).join(","));
      setKeyReference(payload.recoveryKeyReference ?? "");
      setOutcome("校验请求已受理；结果以服务端返回的计划与检查项为准。");
    }
  }

  async function runPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!body) return setLocalError("必须先校验备份并获得恢复计划。");
    if (!targetControllerId.trim()) return setLocalError("必须填写目标主控 ID。");
    setLocalError(null);
    const result = await previewAction.run(body);
    if (result.ok && result.data) {
      setPreview(result.data.data);
      setOutcome("预演完成；预演不执行恢复，也不改动任何主控。");
    }
  }

  async function runCreate() {
    if (!body) return setLocalError("必须先校验备份并获得恢复计划。");
    if (!fenced) return setLocalError("必须声明旧主控已隔离（fenced）。");
    if (previewBlocked) return setLocalError("预演未通过或尚未预演；不能创建恢复作业。");
    setLocalError(null);
    const result = await createAction.run(body);
    if (result.ok && result.data) {
      setOperation(result.data.data);
      setOutcome("恢复作业已创建（202）；accepted 不代表恢复成功。");
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="备份与恢复"
        description="备份校验 → 灾难恢复预演 → 恢复作业。恢复密钥只传引用，不传明文。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">DR</span>}
      />
      <EncryptedBackupsPanel key={session.viewEpoch} />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}

      <Notice tone="info" title="离线恢复">上方管理加密备份；下方恢复预演仅展示服务端校验。实际恢复密钥和 Root 私钥不进入网页。</Notice>

      <Panel title="校验备份" description="POST /api/v1/backups/{backupId}/verify → 返回恢复计划（202 表示已受理）。">
        <form onSubmit={verify} className="space-y-3 px-5 py-4">
          {verifyAction.error ? <Notice tone="error" title="校验失败">{describeApiError(verifyAction.error)}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
            <Field label="备份 ID" hint="不接受通配或猜测值；无效 ID 应由服务端拒绝。">
              {ids => <TextInput id={ids.inputId} value={backupId} onChange={event => setBackupId(event.target.value)} disabled={!canManage || writesLocked} required />}
            </Field>
            <div className="flex items-end">
              <Button type="submit" variant="primary" loading={verifyAction.pending} disabled={!canManage || writesLocked || !backupId.trim()}>校验备份</Button>
            </div>
          </div>
          {plan ? (
            <div className="space-y-3 rounded-lg border px-4 py-3 text-sm">
              <p>备份：<span className="font-mono text-xs">{plan.backupId}</span> · 目标主控 <span className="font-mono text-xs">{plan.targetControllerId}</span> · 期望版本 {plan.expectedVersion ?? "未返回"}</p>
              <p>期望节点：{plan.expectedNodeIds?.join("、") || "未返回"} · 密钥引用：<span className="font-mono text-xs">{plan.recoveryKeyReference ?? "未返回"}</span></p>
              <ChecksTable checks={plan.checks} caption="备份校验检查项" />
            </div>
          ) : <p className="text-xs text-muted-foreground">尚未校验任何备份。</p>}
        </form>
      </Panel>

      <Panel title="灾难恢复预演" description="POST /api/v1/disaster-recovery/preview；预演不执行恢复，只返回服务端检查结论。">
        <form onSubmit={runPreview} className="space-y-3 px-5 py-4">
          {previewAction.error ? <Notice tone="error" title="预演失败">{describeApiError(previewAction.error)}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="目标主控 ID">
              {ids => <TextInput id={ids.inputId} value={targetControllerId} onChange={event => setTargetControllerId(event.target.value)} disabled={!plan || !canManage || writesLocked} required />}
            </Field>
            <Field label="期望版本（可选）">
              {ids => <TextInput id={ids.inputId} value={expectedVersion} onChange={event => setExpectedVersion(event.target.value)} disabled={!plan || !canManage || writesLocked} />}
            </Field>
            <Field label="期望节点 ID（逗号分隔，可选）" hint="用于校验恢复后节点身份是否一致；由服务端比对。">
              {ids => <TextInput id={ids.inputId} value={expectedNodeIds} onChange={event => setExpectedNodeIds(event.target.value)} disabled={!plan || !canManage || writesLocked} />}
            </Field>
            <Field label="恢复密钥引用（可选）" hint="只传引用标识；严禁在此填写密钥明文，页面也不会保存。">
              {ids => <TextInput id={ids.inputId} value={keyReference} onChange={event => setKeyReference(event.target.value)} disabled={!plan || !canManage || writesLocked} autoComplete="off" />}
            </Field>
          </div>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-1 size-4 accent-[var(--primary)]"
              checked={fenced}
              disabled={!plan || !canManage || writesLocked}
              onChange={event => setFenced(event.target.checked)}
            />
            旧主控已隔离（fenced）。这是**必要声明**，不是充分的技术隔离证据；服务端仍需独立校验。
          </label>
          <Button type="submit" loading={previewAction.pending} disabled={!plan || !canManage || writesLocked || !targetControllerId.trim()}>执行预演</Button>
          {preview ? (
            <div className="space-y-2 rounded-lg border px-4 py-3">
              <p className="text-sm">预演返回：备份 <span className="font-mono text-xs">{preview.backupId}</span> · 目标 {preview.targetControllerId} · fenced {preview.oldControllerFenced ? "是" : "否"}</p>
              <ChecksTable checks={preview.checks} caption="恢复预演检查项" />
            </div>
          ) : null}
        </form>
      </Panel>

      <Panel title="恢复作业" description="POST /api/v1/disaster-recovery/jobs（202）→ GET /api/v1/disaster-recovery/jobs/{jobId}。">
        <div className="space-y-3 px-5 py-4">
          {createAction.error ? <Notice tone="error" title="创建恢复作业失败">{describeApiError(createAction.error)}</Notice> : null}
          {!preview ? <Notice tone="info" title="需要先预演">没有预演结论时不能创建恢复作业；本页不跳过检查。</Notice> : null}
          {preview && previewChecks.some(item => !item.passed) ? (
            <Notice tone="error" title="预演未通过">存在未通过检查项；服务端应拒绝恢复，本页同样禁止提交。</Notice>
          ) : null}
          <Button type="button" variant="primary" loading={createAction.pending} disabled={previewBlocked || !canManage || writesLocked} onClick={runCreate}>
            创建恢复作业
          </Button>
          {operation ? (
            <div className="space-y-2 rounded-lg border px-4 py-3 text-sm">
              <p>作业 <span className="font-mono text-xs">{operation.id}</span> · 状态 {OPERATION_STATUS[operation.status] ?? operation.status} · 创建 {formatDateTime(operation.createdAt) ?? "—"}</p>
              {operation.auditId ? <p className="text-xs text-muted-foreground">审计 {operation.auditId}</p> : null}
              {operation.stages && operation.stages.length ? (
                <DataTable
                  caption="恢复作业阶段"
                  rows={operation.stages}
                  rowKey={(item, index) => `${item.name}-${index}`}
                  columns={[
                    { key: "name", header: "阶段", render: item => item.name },
                    { key: "status", header: "状态", render: item => item.status },
                    { key: "message", header: "信息", render: item => item.message ?? "—" },
                  ]}
                />
              ) : <p className="text-xs text-muted-foreground">服务端未返回阶段信息。</p>}
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  loading={refreshAction.pending}
                  onClick={async () => {
                    const result = await refreshAction.run(operation.id);
                    if (result.ok && result.data) setOperation(result.data.data);
                  }}
                >
                  刷新作业
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                契约没有恢复作业列表端点，刷新页面后无法从服务端找回历史作业；本页不本地存储作业记录。
              </p>
            </div>
          ) : null}
        </div>
      </Panel>

      <Panel title="演练说明" description="主控灾难恢复演练要求可验证的备份、隔离声明与逐阶段结果。">
        <div className="space-y-2 px-5 py-4 text-sm">
          <p>恢复成功不等于业务恢复：还需要确认控制面健康、数据库可读写、已连接 Agent 的基本功能与证书/凭据有效性。</p>
          <p className="text-xs text-muted-foreground">相关入口：<a className="text-primary underline" href="#/updates">更新中心（升级前备份要求）</a> · <a className="text-primary underline" href="#/audit">审计日志</a> · <a className="text-primary underline" href="#/nodes">节点状态</a></p>
        </div>
      </Panel>
    </div>
  );
}
