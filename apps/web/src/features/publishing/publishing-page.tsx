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
import { dnsApi, gatewaysApi, publicationsApi } from "@/lib/api/endpoints-infra";
import { serviceGroupsApi } from "@/lib/api/endpoints";
import type { AsyncOperation, DNSAccount, DNSZone, GatewayNode, PublicationPreview, ServiceGroup } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const PUBLICATION_STATE: Record<AsyncOperation["status"], string> = {
  queued: "排队中",
  prechecking: "预检中",
  running: "执行中",
  partially_completed: "部分完成",
  succeeded: "已完成",
  failed: "失败",
  paused: "已暂停",
  manual_intervention: "需人工修复",
};

export function PublishingPage() {
  const session = useSession();
  const [serviceGroupId, setServiceGroupId] = useState("");
  const [name, setName] = useState("");
  const [gatewayIds, setGatewayIds] = useState<ReadonlySet<string>>(new Set());
  const [domain, setDomain] = useState("");
  const [dnsAccountId, setDnsAccountId] = useState("");
  const [zoneId, setZoneId] = useState("");
  const [https, setHttps] = useState(true);
  const [certificateId, setCertificateId] = useState("");
  const [createMonitor, setCreateMonitor] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [preview, setPreview] = useState<PublicationPreview | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [detail, setDetail] = useState<AsyncOperation | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const groups = useResource(`publish:groups:${session.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data));
  const gateways = useResource(`publish:gateways:${session.viewEpoch}`, signal => gatewaysApi.list(signal).then(result => result.data));
  const accounts = useResource(`publish:accounts:${session.viewEpoch}`, signal => dnsApi.accounts(signal).then(result => result.data));
  const zones = useResource(
    `publish:zones:${session.viewEpoch}:${dnsAccountId}`,
    signal => dnsAccountId ? dnsApi.zones(dnsAccountId, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(dnsAccountId) },
  );
  const publications = useResource(`publish:list:${session.viewEpoch}`, signal => publicationsApi.list(signal).then(result => result.data));

  const groupItems: readonly ServiceGroup[] = groups.state.status === "ready" ? groups.state.data.items : [];
  const gatewayItems: readonly GatewayNode[] = gateways.state.status === "ready" ? gateways.state.data.items : [];
  const accountItems: readonly DNSAccount[] = accounts.state.status === "ready" ? accounts.state.data.items : [];
  const zoneItems: readonly DNSZone[] = zones.state.status === "ready" ? zones.state.data.items : [];
  const publicationItems = publications.state.status === "ready" ? publications.state.data : [];
  const selectedGroup = groupItems.find(group => group.id === serviceGroupId) ?? null;
  const canManage = session.can("publication.manage") || session.can("gateway.manage");

  const previewAction = useAction((body: Parameters<typeof publicationsApi.preview>[0]) => publicationsApi.preview(body));
  const createAction = useAction((body: Parameters<typeof publicationsApi.create>[0]) => publicationsApi.create(body));

  function draft() {
    return {
      ...(name.trim() ? { name: name.trim() } : {}),
      serviceGroupId,
      gatewayIds: [...gatewayIds],
      ...(dnsAccountId ? { dnsAccountId } : {}),
      ...(zoneId ? { zoneId } : {}),
      domain: domain.trim(),
      https,
      ...(certificateId.trim() ? { certificateId: certificateId.trim() } : {}),
      createMonitor,
    };
  }

  function validate(): string | null {
    if (!serviceGroupId) return "必须选择服务组；向导不会静默创建新服务组。";
    if (gatewayIds.size === 0) return "至少选择一个调度可用的网关。";
    if (!domain.trim()) return "访问域名不能为空。";
    if (!confirm) return "必须确认变更预览与风险。";
    return null;
  }

  async function runPreview() {
    const error = validate();
    if (error && !error.includes("确认变更")) return setLocalError(error);
    setLocalError(null);
    setPreview(null);
    const result = await previewAction.run(draft());
    if (result.ok && result.data) setPreview(result.data.data);
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const error = validate();
    if (error) return setLocalError(error);
    if (!preview) return setLocalError("请先预览变更；创建前不得伪造预检结果。");
    if (preview.eligible === false) return setLocalError("服务端预检判定不可发布；请先修复列出的检查项。");
    setLocalError(null);
    const result = await createAction.run(draft());
    if (result.ok && result.data) {
      setOutcome(`发布作业 ${result.data.data.id} 已创建；按阶段查看结果，不表示全部完成。`);
      publications.reload();
      setPreview(null);
      setConfirm(false);
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="服务发布"
        description="将现有服务组发布到合格网关、Cloudflare DNS 与证书。多步骤不是原子事务，局部失败不会显示全部完成。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">发布向导</span>}
        actions={<Button type="button" onClick={() => { groups.reload(); gateways.reload(); publications.reload(); }}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_26rem]">
        <Panel title="发布配置" description="GET 服务组/网关/DNS 列表；POST /api/v1/publications/preview 与 /publications。">
          <form onSubmit={create} className="space-y-4 px-5 py-4">
            {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
            {(previewAction.error ?? createAction.error) ? <Notice tone="error" title="操作失败">{describeApiError(previewAction.error ?? createAction.error)}</Notice> : null}
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="发布名称（可选）">{ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} />}</Field>
              <Field label="服务组">
                {ids => (
                  <Select id={ids.inputId} value={serviceGroupId} onChange={event => { setServiceGroupId(event.target.value); setPreview(null); }} required>
                    <option value="">请选择现有服务组</option>
                    {groupItems.map(group => <option key={group.id} value={group.id}>{group.name}（{group.memberNodeIds.length} 台）</option>)}
                  </Select>
                )}
              </Field>
            </div>
            {selectedGroup ? (
              <p className="text-xs text-muted-foreground">
                服务组端口：{selectedGroup.service ? `${selectedGroup.service.protocol ?? "—"}:${selectedGroup.service.port ?? "—"}` : "未配置"} · revision {selectedGroup.revision}
                。PublicationDraft 没有独立端口字段，本页不伪造端口覆盖。
              </p>
            ) : null}
            <fieldset className="rounded-lg border px-4 py-3">
              <legend className="px-1 text-sm font-medium">网关（只列服务端声明可调度的网关）</legend>
              {gatewayItems.length === 0 ? <p className="text-xs text-muted-foreground">没有可用网关或列表未加载。</p> : null}
              {gatewayItems.map(gateway => (
                <label key={gateway.id} className={`mt-1 flex items-start gap-2 text-sm ${gateway.schedulingEligible ? "" : "opacity-60"}`}>
                  <input
                    type="checkbox"
                    className="mt-0.5 size-4 accent-[var(--primary)]"
                    disabled={!gateway.schedulingEligible}
                    checked={gatewayIds.has(gateway.id)}
                    onChange={event => {
                      setGatewayIds(current => {
                        const next = new Set(current);
                        if (event.target.checked) next.add(gateway.id);
                        else next.delete(gateway.id);
                        return next;
                      });
                      setPreview(null);
                    }}
                  />
                  {gateway.name} {gateway.schedulingEligible ? "" : "（不可调度）"}
                </label>
              ))}
            </fieldset>
            <Field label="访问域名">{ids => <TextInput id={ids.inputId} value={domain} onChange={event => { setDomain(event.target.value); setPreview(null); }} required />}</Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="Cloudflare 账号（可选）">
                {ids => (
                  <Select id={ids.inputId} value={dnsAccountId} onChange={event => { setDnsAccountId(event.target.value); setZoneId(""); setPreview(null); }}>
                    <option value="">不关联 DNS</option>
                    {accountItems.map(account => <option key={account.id} value={account.id}>{account.displayName}</option>)}
                  </Select>
                )}
              </Field>
              <Field label="Zone（可选）">
                {ids => (
                  <Select id={ids.inputId} value={zoneId} onChange={event => { setZoneId(event.target.value); setPreview(null); }} disabled={!dnsAccountId}>
                    <option value="">不关联 Zone</option>
                    {zoneItems.map(zone => <option key={zone.id} value={zone.id}>{zone.name}</option>)}
                  </Select>
                )}
              </Field>
            </div>
            <Field label="证书 ID（可选）" hint="来自证书模块；服务端会验证引用有效性。">
              {ids => <TextInput id={ids.inputId} value={certificateId} onChange={event => { setCertificateId(event.target.value); setPreview(null); }} />}
            </Field>
            <div className="flex flex-wrap gap-4">
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={https} onChange={event => { setHttps(event.target.checked); setPreview(null); }} />
                HTTPS
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={createMonitor} onChange={event => { setCreateMonitor(event.target.checked); setPreview(null); }} />
                生成服务监控草案
              </label>
            </div>
            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-1 size-4 accent-[var(--primary)]" checked={confirm} onChange={event => setConfirm(event.target.checked)} />
              我确认已查看变更预览；部分失败不会自动全量回滚，DNS 缓存可能延迟。
            </label>
            <div className="flex flex-wrap gap-2">
              <Button type="button" onClick={runPreview} loading={previewAction.pending} disabled={!canManage || session.writesLocked}>预览变更</Button>
              <Button type="submit" variant="primary" loading={createAction.pending} disabled={!canManage || session.writesLocked || !preview}>创建发布作业</Button>
            </div>
          </form>
        </Panel>

        <Panel title="预检结果" description="服务端返回的变更预览；本页不自行推断 DNS 传播或网关卡健康。">
          {!preview ? (
            <EmptyState title="尚未预览" description="填写配置后点击“预览变更”；创建前必须有预检结果。" />
          ) : (
            <div className="space-y-3 px-5 py-4 text-sm">
              <p>可发布：<strong>{preview.eligible === undefined ? "服务端未返回" : preview.eligible ? "是" : "否"}</strong></p>
              <p>目标节点快照：{preview.targetNodeIdsSnapshot?.join("、") || "未返回"}</p>
              {preview.checks?.length ? (
                <ul className="space-y-1">
                  {preview.checks.map(check => (
                    <li key={check.name} className="text-xs">
                      {check.passed ? "通过" : "未通过"} · {check.name}{check.reason ? `：${check.reason}` : ""}
                    </li>
                  ))}
                </ul>
              ) : <p className="text-xs text-muted-foreground">服务端未返回检查项。</p>}
              {preview.configDiff?.length ? (
                <div>
                  <p className="font-medium">配置变更</p>
                  <ul className="mt-1 list-disc pl-5 text-xs">{preview.configDiff.map(item => <li key={item}>{item}</li>)}</ul>
                </div>
              ) : null}
              {preview.warnings?.length ? <Notice tone="warning" title="风险提示">{preview.warnings.join("；")}</Notice> : null}
            </div>
          )}
        </Panel>
      </div>

      <Panel title="发布记录" description="GET /api/v1/publications；状态与阶段由服务端返回，局部失败不改成全部成功。" actions={<Button size="sm" onClick={() => publications.reload()} loading={publications.state.status === "loading"}>刷新</Button>}>
        <AsyncPanel state={publications.state} onRetry={publications.reload} errorTitle="发布记录加载失败" empty={<EmptyState title="暂无发布记录" />} isEmpty={data => data.length === 0}>
          {() => (
            <DataTable
              caption="服务发布记录"
              rows={publicationItems}
              rowKey={operation => operation.id}
              columns={[
                { key: "id", header: "作业", render: operation => <span className="font-mono text-xs">{operation.id}</span> },
                { key: "status", header: "状态", render: operation => PUBLICATION_STATE[operation.status] },
                { key: "created", header: "创建", render: operation => formatDateTime(operation.createdAt) ?? "—" },
                { key: "stages", header: "阶段", render: operation => `${operation.stages?.length ?? 0} 个` },
                { key: "actions", header: "操作", render: operation => <Button size="sm" onClick={() => setDetail(operation)}>阶段详情</Button> },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Modal
        open={detail !== null}
        title={detail ? `发布作业 ${detail.id}` : "发布作业"}
        onClose={() => setDetail(null)}
        footer={<Button type="button" onClick={() => setDetail(null)}>关闭</Button>}
      >
        {detail ? (
          <div className="space-y-2 text-sm">
            <p>状态：{PUBLICATION_STATE[detail.status]}</p>
            {detail.stages?.length ? (
              <ol className="space-y-1">
                {detail.stages.map((stage, index) => (
                  <li key={`${stage.name}-${index}`} className="text-xs">
                    {stage.status} · {stage.name}{stage.message ? `：${stage.message}` : ""}
                  </li>
                ))}
              </ol>
            ) : <p className="text-muted-foreground">服务端未返回阶段详情。</p>}
            <p className="text-xs text-muted-foreground">发布跨 DNS/网关/证书步骤，不是原子事务；DNS TTL 使即时全网回滚不可保证。</p>
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
