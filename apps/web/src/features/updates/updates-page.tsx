import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { updatesApi } from "@/lib/api/endpoints-system";
import type { ControllerUpdateJob, ControllerUpdatePlan, ControllerUpdateState, UpdateCheckResult } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const STATE_TEXT: Record<ControllerUpdateState, string> = {
  queued: "排队",
  preflight: "预检",
  verifying: "校验签名/哈希",
  backing_up: "备份中",
  installing: "安装中",
  restarting: "重启中",
  health_check: "健康检查",
  completed: "已完成",
  failed: "失败",
  rolling_back: "回滚中",
  rolled_back: "已回滚",
  manual_recovery_required: "需要人工恢复",
};

const TERMINAL_STATES: readonly ControllerUpdateState[] = ["completed", "failed", "rolled_back", "manual_recovery_required"];

function newKey(prefix: string) {
  return globalThis.crypto?.randomUUID?.() ?? `${prefix}-${Date.now()}`;
}

/** 主控更新的唯一实现；系统设置的「系统更新」页签复用同一组件，避免两套更新配置。 */
export function ControllerUpdatePanel() {
  const session = useSession();
  const [channel, setChannel] = useState<"stable" | "beta">("stable");
  const [check, setCheck] = useState<UpdateCheckResult | null>(null);
  const [targetVersion, setTargetVersion] = useState("");
  const [dryRun, setDryRun] = useState(true);
  const [plan, setPlan] = useState<ControllerUpdatePlan | null>(null);
  const [confirmation, setConfirmation] = useState("");
  const [riskAccepted, setRiskAccepted] = useState(false);
  const [detail, setDetail] = useState<ControllerUpdateJob | null>(null);
  const [rollbackTarget, setRollbackTarget] = useState<ControllerUpdateJob | null>(null);
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const jobs = useResource(`updates:jobs:${session.viewEpoch}`, signal => updatesApi.jobs(signal).then(result => result.data));
  const jobItems = jobs.state.status === "ready" ? jobs.state.data.items : [];
  const canManage = session.can("system.update") || session.can("settings.manage") || session.can("update.manage");
  const writesLocked = session.writesLocked;

  const checkAction = useAction((body: { channel: "stable" | "beta" }) => updatesApi.check(body, newKey("update-check")));
  const planAction = useAction((body: { targetVersion: string; dryRun?: boolean }) => updatesApi.plan(body, newKey("update-plan")));
  const applyAction = useAction((body: { planId: string; confirmation: string; backupMode: "required" }) => updatesApi.apply(body, newKey("update-apply")));
  const rollbackAction = useAction((jobId: string, body: { reason: string }) => updatesApi.rollback(jobId, body, newKey("update-rollback")));
  const detailAction = useAction((jobId: string) => updatesApi.job(jobId));

  const planBlocked = plan ? !plan.eligible || (plan.preflightChecks ?? []).some(item => !item.passed) : true;
  const backupBlocked = Boolean(plan?.backupRequired) && !jobItems.some(job => job.backupId);
  const confirmationMatches = Boolean(plan) && confirmation.trim() === plan?.targetVersion;
  const applyDisabled = !canManage || writesLocked || planBlocked || backupBlocked || !confirmationMatches || !riskAccepted || applyAction.pending;

  async function runCheck(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setLocalError(null);
    const result = await checkAction.run({ channel });
    if (result.ok && result.data) {
      const payload = result.data.data;
      setCheck(payload);
      setPlan(null);
      setConfirmation("");
      setRiskAccepted(false);
      if (payload.latestVersion) setTargetVersion(payload.latestVersion);
      setOutcome(payload.updateAvailable ? "检查完成：发现可用版本；检查不会安装或重启。" : "检查完成：当前通道没有可用更新。");
    }
  }

  async function runPlan(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!targetVersion.trim()) return setLocalError("必须填写目标版本。");
    setLocalError(null);
    const result = await planAction.run({ targetVersion: targetVersion.trim(), dryRun });
    if (result.ok && result.data) {
      setPlan(result.data.data);
      setConfirmation("");
      setRiskAccepted(false);
      setOutcome(dryRun ? "已生成预演计划；dryRun 不会安装。" : "已生成计划；仍需人工确认后才会创建升级作业。");
    }
  }

  async function runApply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!plan) return setLocalError("必须先生成升级计划。");
    if (!confirmationMatches) return setLocalError("确认文本必须与目标版本完全一致。");
    if (!riskAccepted) return setLocalError("必须勾选风险确认。");
    setLocalError(null);
    const result = await applyAction.run({ planId: plan.planId, confirmation: confirmation.trim(), backupMode: "required" });
    if (result.ok && result.data) {
      setDetail(result.data.data);
      setOutcome("升级作业已创建；202 只代表已受理，完成状态以作业详情为准。");
      jobs.reload();
    }
  }

  async function runRollback() {
    if (!rollbackTarget) return;
    if (!reason.trim()) return setLocalError("必须填写回滚原因。");
    setLocalError(null);
    const result = await rollbackAction.run(rollbackTarget.jobId, { reason: reason.trim() });
    if (result.ok && result.data) {
      setDetail(result.data.data);
      setRollbackTarget(null);
      setReason("");
      setOutcome("回滚请求已提交；失败时可能需要人工恢复。");
      jobs.reload();
    }
  }

  return (
    <div className="space-y-5">
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
      {!canManage ? <Notice tone="warning" title="更新权限缺失">当前身份没有返回更新管理权限；服务端仍会独立校验。</Notice> : null}

      <Panel title="检查更新" description="POST /api/v1/system/updates/check；检查只读取版本清单，不安装、不自动重启、不静默升级。">
        <form onSubmit={runCheck} className="space-y-3 px-5 py-4">
          {checkAction.error ? <Notice tone="error" title="检查失败">{describeApiError(checkAction.error)}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="发布通道">
              {ids => (
                <Select id={ids.inputId} value={channel} onChange={event => setChannel(event.target.value as "stable" | "beta")} disabled={!canManage || writesLocked}>
                  <option value="stable">stable</option>
                  <option value="beta">beta</option>
                </Select>
              )}
            </Field>
            <div className="sm:col-span-2 flex items-end">
              <Button type="submit" variant="primary" loading={checkAction.pending} disabled={!canManage || writesLocked}>检查更新</Button>
            </div>
          </div>
          {check ? (
            <div className="rounded-lg border px-4 py-3 text-sm">
              <p>当前版本：<span className="font-mono">{check.currentVersion}</span> · 通道 {check.channel} · 检查时间 {formatDateTime(check.checkedAt) ?? "—"}</p>
              <p>
                最新版本：<span className="font-mono">{check.latestVersion ?? "未返回"}</span>
                {" · "}
                {check.updateAvailable ? <span className="text-amber-700">有可用更新</span> : <span className="text-emerald-700">已是最新</span>}
                {check.publishedAt ? ` · 发布于 ${formatDateTime(check.publishedAt) ?? "—"}` : ""}
              </p>
              <p>
                签名验证：{check.signatureVerified === undefined ? "未返回" : check.signatureVerified ? "通过" : <span className="text-destructive">未通过（禁止安装）</span>}
                {" · "}
                平台兼容：{check.platformCompatible === undefined ? "未返回" : check.platformCompatible ? "兼容" : <span className="text-destructive">不兼容</span>}
              </p>
              {check.releaseNotes ? <p className="mt-2 whitespace-pre-line text-xs text-muted-foreground">{check.releaseNotes}</p> : null}
              {check.updateAvailable && check.signatureVerified === false ? (
                <Notice tone="error" title="签名未验证">签名验证失败时不能创建升级作业；本页不会绕过该校验。</Notice>
              ) : null}
            </div>
          ) : <p className="text-xs text-muted-foreground">尚未检查；本页不缓存也不猜测版本号。</p>}
        </form>
      </Panel>

      <Panel title="升级计划与一键更新" description="POST /api/v1/system/updates/plan → /apply；计划不合格、预检失败或需要备份而没有备份时不能创建作业。">
        <form onSubmit={runPlan} className="space-y-3 border-b px-5 py-4">
          {planAction.error ? <Notice tone="error" title="生成计划失败">{describeApiError(planAction.error)}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="目标版本" hint="来自检查结果或服务端版本清单；不接受通配或“latest”。">
              {ids => <TextInput id={ids.inputId} value={targetVersion} onChange={event => setTargetVersion(event.target.value)} disabled={!canManage || writesLocked} required />}
            </Field>
            <Field label="预演（dryRun）">
              {ids => (
                <Select id={ids.inputId} value={dryRun ? "true" : "false"} onChange={event => setDryRun(event.target.value === "true")} disabled={!canManage || writesLocked}>
                  <option value="true">是（只生成计划）</option>
                  <option value="false">否（生成正式计划）</option>
                </Select>
              )}
            </Field>
            <div className="flex items-end">
              <Button type="submit" loading={planAction.pending} disabled={!canManage || writesLocked}>生成计划</Button>
            </div>
          </div>
        </form>

        {plan ? (
          <div className="space-y-3 px-5 py-4">
            <p className="text-sm">
              计划 <span className="font-mono text-xs">{plan.planId}</span> → <span className="font-mono">{plan.targetVersion}</span>
              {" · "}
              {plan.eligible ? <span className="text-emerald-700">合格</span> : <span className="text-destructive">不合格（禁止安装）</span>}
              {" · 迁移可逆："}
              {plan.migrationReversible === undefined ? "未返回" : plan.migrationReversible ? "是" : <span className="text-destructive">否</span>}
              {" · 需要备份："}{plan.backupRequired ? "是" : "否"}
            </p>
            {plan.estimatedImpact ? <p className="text-xs text-muted-foreground">预计影响：{plan.estimatedImpact}</p> : null}
            {plan.preflightChecks && plan.preflightChecks.length ? (
              <DataTable
                caption="升级预检"
                rows={plan.preflightChecks}
                rowKey={(item, index) => `${item.name}-${index}`}
                columns={[
                  { key: "name", header: "检查项", render: item => item.name },
                  { key: "passed", header: "结果", render: item => (item.passed ? <span className="text-emerald-700">通过</span> : <span className="text-destructive">未通过</span>) },
                  { key: "reason", header: "原因", render: item => item.reason ?? "—" },
                ]}
              />
            ) : <p className="text-xs text-muted-foreground">服务端未返回预检项；不能据此认定空间、任务或迁移兼容性已确认。</p>}

            <form onSubmit={runApply} className="space-y-3 rounded-lg border px-4 py-3">
              {applyAction.error ? <Notice tone="error" title="创建升级作业失败">{describeApiError(applyAction.error)}</Notice> : null}
              {planBlocked ? <Notice tone="warning" title="计划不可执行">计划不合格或存在未通过的预检项；服务端应拒绝安装。</Notice> : null}
              {backupBlocked ? (
                <Notice tone="warning" title="缺少可验证备份">
                  计划要求备份（backupMode 固定 `required`），但更新作业历史中没有服务端返回的 backupId。备份记录列表/创建端点缺失（见 SHARED-CHANGES §4），本页不伪造备份成功。
                </Notice>
              ) : null}
              <Field label="确认文本" hint={`必须逐字输入目标版本 ${plan.targetVersion}；不得由模型或脚本自批准。`}>
                {ids => <TextInput id={ids.inputId} value={confirmation} onChange={event => setConfirmation(event.target.value)} disabled={!canManage || writesLocked} autoComplete="off" />}
              </Field>
              <label className="flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-1 size-4 accent-[var(--primary)]"
                  checked={riskAccepted}
                  disabled={!canManage || writesLocked}
                  onChange={event => setRiskAccepted(event.target.checked)}
                />
                我确认升级会重启主控、期间管理面不可用，且失败时可能需要回滚或人工恢复。
              </label>
              <Button type="submit" variant="primary" loading={applyAction.pending} disabled={applyDisabled}>一键更新</Button>
            </form>
          </div>
        ) : <p className="px-5 py-4 text-xs text-muted-foreground">尚未生成计划。</p>}
      </Panel>

      <Panel
        title="更新作业与历史"
        description="GET /api/v1/system/updates/jobs；与系统设置共用同一份状态与历史。"
        actions={<Button size="sm" onClick={() => jobs.reload()} loading={jobs.state.status === "loading"}>刷新</Button>}
      >
        <AsyncPanel state={jobs.state} onRetry={jobs.reload} errorTitle="更新作业加载失败" empty={<EmptyState title="暂无更新作业" description="检查更新不会创建作业。" />} isEmpty={data => data.items.length === 0}>
          {() => (
            <DataTable
              caption="主控更新作业"
              rows={jobItems}
              rowKey={job => job.jobId}
              columns={[
                { key: "job", header: "作业", render: job => <span className="font-mono text-xs">{job.jobId}</span> },
                { key: "version", header: "版本", render: job => `${job.fromVersion} → ${job.toVersion}` },
                { key: "state", header: "状态", render: job => STATE_TEXT[job.state] ?? job.state },
                { key: "phase", header: "阶段", render: job => job.phase ?? "—" },
                { key: "backup", header: "备份", render: job => job.backupId ?? "—" },
                { key: "error", header: "错误", render: job => job.errorCode ?? "—" },
                { key: "by", header: "发起人", render: job => job.requestedBy ?? "—" },
                { key: "updated", header: "更新", render: job => formatDateTime(job.updatedAt ?? job.createdAt) ?? "—" },
                {
                  key: "actions",
                  header: "操作",
                  render: job => (
                    <div className="flex flex-wrap gap-1">
                      <Button
                        size="sm"
                        loading={detailAction.pending}
                        onClick={async () => {
                          const result = await detailAction.run(job.jobId);
                          if (result.ok && result.data) setDetail(result.data.data);
                        }}
                      >
                        详情
                      </Button>
                      <Button
                        size="sm"
                        variant="danger"
                        disabled={!canManage || writesLocked || !job.rollbackEligible}
                        onClick={() => { setRollbackTarget(job); setReason(""); setLocalError(null); }}
                      >
                        回滚
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
        <p className="border-t px-5 py-2 text-xs text-muted-foreground">
          只有 {TERMINAL_STATES.map(state => STATE_TEXT[state]).join("、")} 是终态；“需要人工恢复”表示服务端无法自动回滚，必须按备份与恢复流程处理。
        </p>
      </Panel>

      <Panel title="Agent 更新" description="被控 Agent 升级在当前候选契约中没有专用端点。">
        <div className="space-y-2 px-5 py-4 text-sm">
          <Notice tone="warning" title="契约缺口：Agent 升级">
            候选契约只有主控 `/api/v1/system/updates/*`，没有 Agent 版本清单、分批升级或失败熔断端点；`/api/v1/tasks` 也缺少分批/金丝雀参数（SHARED-CHANGES §5）。
            本页不伪造“选择节点分批升级 Agent”，只能通过批量任务页提交普通任务，且其分批语义尚未冻结。
          </Notice>
          <p><a className="text-primary underline" href="#/tasks">前往批量任务</a> · <a className="text-primary underline" href="#/nodes">查看节点版本与在线状态</a></p>
        </div>
      </Panel>

      <Modal
        open={detail !== null}
        title={detail ? `更新作业 ${detail.jobId}` : "更新作业"}
        onClose={() => setDetail(null)}
        wide
        footer={<Button type="button" onClick={() => setDetail(null)}>关闭</Button>}
      >
        {detail ? (
          <div className="space-y-3 text-sm">
            <p>状态：{STATE_TEXT[detail.state] ?? detail.state}{detail.phase ? ` · 阶段 ${detail.phase}` : ""}</p>
            <p>版本：<span className="font-mono">{detail.fromVersion} → {detail.toVersion}</span></p>
            <p>备份：{detail.backupId ?? "服务端未返回"} · 可回滚：{detail.rollbackEligible ? "是" : "否"}</p>
            {detail.errorCode ? <Notice tone="error" title="作业错误">{detail.errorCode}</Notice> : null}
            {detail.healthChecks && detail.healthChecks.length ? (
              <DataTable
                caption="升级后健康检查"
                rows={detail.healthChecks}
                rowKey={(item, index) => `${item.name}-${index}`}
                columns={[
                  { key: "name", header: "检查", render: item => item.name },
                  { key: "passed", header: "结果", render: item => (item.passed ? <span className="text-emerald-700">通过</span> : <span className="text-destructive">未通过</span>) },
                ]}
              />
            ) : <p className="text-xs text-muted-foreground">服务端未返回升级后健康检查；不能据此认定控制面、数据库或 Agent 连接正常。</p>}
            <p className="text-xs text-muted-foreground">发起人 {detail.requestedBy ?? "—"} · 创建 {formatDateTime(detail.createdAt) ?? "—"} · 更新 {formatDateTime(detail.updatedAt) ?? "—"}</p>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={rollbackTarget !== null}
        title="回滚主控更新"
        description="回滚是异步作业；长事务或不可逆迁移可能需要人工恢复。"
        onClose={() => { if (!rollbackAction.pending) setRollbackTarget(null); }}
        footer={
          <>
            <Button type="button" onClick={() => setRollbackTarget(null)} disabled={rollbackAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={runRollback} loading={rollbackAction.pending} disabled={!reason.trim() || !canManage || writesLocked}>确认回滚</Button>
          </>
        }
      >
        {rollbackAction.error ? <Notice tone="error" title="回滚失败">{describeApiError(rollbackAction.error)}</Notice> : null}
        <p className="text-sm">作业 <span className="font-mono text-xs">{rollbackTarget?.jobId}</span>（{rollbackTarget ? `${rollbackTarget.fromVersion} → ${rollbackTarget.toVersion}` : ""}）</p>
        <Field label="原因">{ids => <TextInput id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} required />}</Field>
      </Modal>
    </div>
  );
}

export function UpdatesPage() {
  return (
    <div className="space-y-5">
      <PageHeader
        title="更新中心"
        description="主控更新：检查 → 计划/预检 → 人工确认 → 异步作业 → 健康检查/回滚。第一版禁止默认静默安装。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">Controller</span>}
      />
      <ControllerUpdatePanel />
    </div>
  );
}
