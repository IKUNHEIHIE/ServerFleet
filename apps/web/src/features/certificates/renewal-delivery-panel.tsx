import { RenewalRecoveryPanel } from "./renewal-recovery-panel";
import { useState } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea } from "@/components/ui/form";
import { DataTable } from "@/components/ui/table";
import { apiClient } from "@/lib/api/client";
import type { PublicRenewalPolicy } from "@/lib/api/endpoints-infra";
import type { IncidentTaskReceipt } from "@/lib/api/endpoints-operations";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

interface Publication {
  readonly baseId: string;
  readonly id: string;
  readonly certificateId?: string;
}
interface Cycle {
  readonly id: string;
  readonly policyRevision: number;
  readonly issueTaskId: string;
  readonly installTaskId?: string;
  readonly state: string;
  readonly errorCode?: string;
  readonly recoveredOrderTaskId?: string;
  readonly recoveredInstallTaskId?: string;
  readonly recoveryNote?: string;
  readonly previousErrorCode?: string;
  readonly recoveryIncidentIds?: readonly string[];
  readonly recoveryPublicationIds?: readonly string[];
  readonly publications: readonly Publication[];
  readonly updatedAt: string;
}
interface Delivery {
  readonly policyId: string;
  readonly revision: number;
  readonly enabled: boolean;
  readonly publicationIds: readonly string[];
  readonly cycle?: Cycle;
}
interface View {
  readonly delivery: Delivery;
  readonly history: readonly Cycle[];
  readonly tasks: readonly IncidentTaskReceipt[];
}

const states: Record<string, string> = {
  issuance: "等待签发完成",
  installation: "等待证书安装审批与结果",
  publication: "等待网关审批、发布与 HTTPS 核验",
  completed: "已安装、发布并接续下一周期",
  blocked: "已停止，等待人工核查",
  recovered: "已采用人工修复的成功结果",
};

function DeliveryContent({ policy, onChanged }: { readonly policy: PublicRenewalPolicy; readonly onChanged: () => void }) {
  const session = useSession();
  const path = `/api/v1/certificates/public-renewals/${encodeURIComponent(policy.id)}/delivery`;
  const page = useResource(
    `renewal-delivery:${session.viewEpoch}:${policy.id}:${policy.revision}`,
    signal => apiClient.request<View>(path, { signal }).then(r => r.data),
    { refreshIntervalMs: 10_000 },
  );
  const [draft, setDraft] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const save = useAction((body: { expectedRevision: number; policyRevision: number; enabled: boolean; publicationIds: readonly string[] }) =>
    apiClient.request<Delivery>(path, { method: "POST", body }),
  );
  const view = page.state.status === "ready" ? page.state.data : null;
  const d = view?.delivery;
  const value = draft ?? d?.publicationIds.join("\n") ?? "";
  const ids = value.split(/[\s,，]+/).filter(Boolean);
  const valid = ids.length <= 8 && new Set(ids).size === ids.length && ids.every(id => /^[a-zA-Z0-9_-]{1,128}$/.test(id));
  const locked = session.writesLocked || save.pending || !d;

  async function update(enabled: boolean) {
    if (!d) return;
    const result = await save.run({
      expectedRevision: d.revision,
      policyRevision: policy.revision,
      enabled,
      publicationIds: enabled ? ids : d.publicationIds,
    });
    page.reload();
    onChanged();
    if (result.ok) {
      setDraft(null);
      setMessage(
        enabled
          ? "安装发布编排已启用。后续修改任务仍须独立审批；任务与生效证明全部成功后自动接续下一周期。"
          : "编排已停用。已准入任务可在任务中心单独取消；停用不回滚已生效操作。",
      );
    }
  }

  return (
    <section className="mt-3 space-y-3 rounded-xl border border-border/80 bg-card p-3.5" aria-label="续签安装发布编排">
      {page.state.status === "loading" ? <p role="status" className="text-xs text-muted-foreground">正在读取安装发布进度…</p> : null}
      {page.state.status === "error" ? (
        <Notice title="编排读取失败" tone="error">
          {describeApiError(page.state.error)}
          <Button size="sm" onClick={page.reload}>重新读取编排</Button>
        </Notice>
      ) : null}
      {d ? (
        <>
          <p className="text-xs font-semibold text-foreground">后续编排：{d.enabled ? "已启用" : "未启用"}</p>
          <Field label="需要更新的现有发布计划 ID" hint="每行一个，最多8个；应为已完成、使用本策略证书的 HTTPS 发布。留空仅安装并接续策略。">
            {a => <TextArea id={a.inputId} value={value} rows={3} disabled={locked} onChange={e => setDraft(e.target.value)} className="font-mono text-xs" />}
          </Field>
          {!valid ? <p role="alert" className="text-xs text-destructive">请填写最多8个不重复的有效发布计划 ID。</p> : null}
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="primary" disabled={locked || !valid || !policy.enabled} loading={save.pending} onClick={() => void update(true)}>
              启用或保存安装发布编排
            </Button>
            <Button size="sm" disabled={locked || !d.enabled} onClick={() => void update(false)}>
              停用后续编排
            </Button>
            <Button size="sm" onClick={page.reload}>刷新编排进度</Button>
          </div>
          {d.cycle ? (
            <Notice tone={d.cycle.state === "blocked" ? "warning" : "info"} title={states[d.cycle.state] ?? d.cycle.state}>
              {d.cycle.errorCode ? <p>{d.cycle.errorCode}。请到任务中心查看异常与人工处置，禁止盲目重试原操作。</p> : null}
              <p>更新时间 {formatDateTime(d.cycle.updatedAt)}</p>
              {d.cycle.publications.map(p => (
                <p key={p.id}>
                  发布 <a className="font-semibold text-primary underline" href="#/publishing">{p.id}</a> · 原计划 {p.baseId}
                </p>
              ))}
            </Notice>
          ) : (
            <p className="text-xs text-muted-foreground">等待本周期的续签任务；无需手工复制证书公链。</p>
          )}
          {d.cycle?.state === "blocked" ? (
            <RenewalRecoveryPanel
              policyId={policy.id}
              policyRevision={policy.revision}
              deliveryRevision={d.revision}
              onChanged={() => {
                page.reload();
                onChanged();
              }}
            />
          ) : null}
          {d.cycle?.state === "recovered" ? (
            <p className="text-xs text-muted-foreground">
              采用签发 {d.cycle.recoveredOrderTaskId} · 安装 {d.cycle.recoveredInstallTaskId}。{d.cycle.recoveryNote}
            </p>
          ) : null}
          <DataTable
            caption="编排关联任务与审批"
            rows={view?.tasks ?? []}
            rowKey={t => t.taskId}
            columns={[
              { key: "id", header: "任务", render: t => <a className="font-mono text-xs text-primary underline" href={`#/tasks/${encodeURIComponent(t.taskId)}`}>{t.taskId}</a> },
              { key: "action", header: "步骤", render: t => t.action },
              { key: "state", header: "实际状态", render: t => t.state },
            ]}
          />
          {view?.history.length ? (
            <details className="rounded-lg border border-border/70 bg-[#FAFBFD] p-2.5 text-xs">
              <summary>历史周期（最近50次）</summary>
              <div className="mt-2 space-y-1 text-muted-foreground">
                {view.history.map(c => (
                  <p key={c.id}>
                    {states[c.state] ?? c.state} · {formatDateTime(c.updatedAt)} · {c.id}
                  </p>
                ))}
              </div>
            </details>
          ) : null}
        </>
      ) : null}
      {message ? <Notice tone="info" title="编排已更新">{message}</Notice> : null}
      {save.error ? <Notice tone="error" title="编排未确认">{describeApiError(save.error)}。已刷新当前版本，请核对后操作。</Notice> : null}
    </section>
  );
}

export function RenewalDeliveryPanel({ policy, onChanged }: { readonly policy: PublicRenewalPolicy; readonly onChanged: () => void }) {
  const session = useSession();
  const [open, setOpen] = useState(false);
  return (
    <div>
      <Button size="sm" aria-expanded={open} onClick={() => setOpen(v => !v)}>
        {open ? "收起安装发布编排" : "安装发布编排"}
      </Button>
      {open ? <DeliveryContent key={`${session.viewEpoch}:${policy.id}`} policy={policy} onChanged={onChanged} /> : null}
    </div>
  );
}
