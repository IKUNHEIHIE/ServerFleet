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
import { nodesApi, trafficApi } from "@/lib/api/endpoints";
import { monitoringApi, notificationsApi } from "@/lib/api/endpoints-infra";
import type { AvailabilityMonitor, MonitorState, NotificationDelivery, NotificationChannel, NodeSummary, TrafficRisk } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { adaptMetric } from "@/features/nodes/legacy-adapter";
import { LegacyMetricCell } from "@/features/nodes/metric-display";

type Tab = "resources" | "service" | "alerts";

const MONITOR_STATE_TEXT: Record<MonitorState, string> = {
  healthy: "正常",
  degraded: "降级",
  unhealthy: "异常",
  pending: "待确认",
  unknown: "未知",
  paused: "已暂停",
};

const DELIVERY_STATUS_TEXT: Record<NotificationDelivery["status"], string> = {
  queued: "排队",
  submitted: "已提交",
  provider_accepted: "平台已接受",
  rejected: "被拒绝",
  rate_limited: "被限流",
  failed: "失败",
  delivery_unknown: "投递未知",
};

function MonitorEditor({
  open,
  monitor,
  nodes,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly monitor: AvailabilityMonitor | null;
  readonly nodes: readonly NodeSummary[];
  readonly onClose: () => void;
  readonly onSaved: (monitor: AvailabilityMonitor) => void;
}) {
  const session = useSession();
  const [name, setName] = useState("");
  const [type, setType] = useState<AvailabilityMonitor["type"]>("http");
  const [target, setTarget] = useState("");
  const [sourceNodeId, setSourceNodeId] = useState("");
  const [intervalSeconds, setIntervalSeconds] = useState("60");
  const [timeoutSeconds, setTimeoutSeconds] = useState("5");
  const [failureCount, setFailureCount] = useState("3");
  const [recoveryCount, setRecoveryCount] = useState("2");
  const [channels, setChannels] = useState<ReadonlySet<"smtp" | "qqbot">>(new Set());
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: AvailabilityMonitor) =>
    monitor?.id ? monitoringApi.update(monitor.id, body) : monitoringApi.create(body),
  );

  useEffect(() => {
    if (!open) return;
    setName(monitor?.name ?? "");
    setType(monitor?.type ?? "http");
    setTarget(monitor?.target ?? "");
    setSourceNodeId(monitor?.sourceNodeId ?? "");
    setIntervalSeconds(String(monitor?.intervalSeconds ?? 60));
    setTimeoutSeconds(String(monitor?.timeoutSeconds ?? 5));
    setFailureCount(String(monitor?.failureCount ?? 3));
    setRecoveryCount(String(monitor?.recoveryCount ?? 2));
    setChannels(new Set(monitor?.notifyChannels ?? []));
    setLocalError(null);
  }, [open, monitor]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) return setLocalError("名称不能为空。");
    if (!target.trim()) return setLocalError("探测目标不能为空。");
    if (Number(intervalSeconds) < 15) return setLocalError("检查间隔至少 15 秒。");
    if (Number(timeoutSeconds) < 1) return setLocalError("超时至少 1 秒。");
    setLocalError(null);
    const body: AvailabilityMonitor = {
      ...(monitor ?? {}),
      name: name.trim(),
      type,
      target: target.trim(),
      ...(sourceNodeId ? { sourceNodeId } : {}),
      intervalSeconds: Number(intervalSeconds),
      timeoutSeconds: Number(timeoutSeconds),
      failureCount: Number(failureCount),
      recoveryCount: Number(recoveryCount),
      notifyChannels: [...channels],
      state: monitor?.state ?? "pending",
    };
    const result = await action.run(body);
    if (result.ok && result.data) onSaved(result.data.data);
  }

  const disabled = !session.can("monitor.manage") || session.writesLocked;

  return (
    <Modal
      open={open}
      title={monitor ? "编辑服务监控" : "新建服务监控"}
      description="服务端会校验目标授权与 SSRF 约束；客户端目标字符串不构成探测许可。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="monitor-editor" variant="primary" loading={action.pending} disabled={disabled}>保存监控</Button>
        </>
      }
    >
      <form id="monitor-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="名称">{ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required />}</Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="类型">
            {ids => (
              <Select id={ids.inputId} value={type} onChange={event => setType(event.target.value as typeof type)}>
                <option value="http">HTTP/HTTPS</option>
                <option value="tcp">TCP</option>
                <option value="tls">TLS</option>
              </Select>
            )}
          </Field>
          <Field label="探测源节点（可选）">
            {ids => (
              <Select id={ids.inputId} value={sourceNodeId} onChange={event => setSourceNodeId(event.target.value)}>
                <option value="">由服务端选择</option>
                {nodes.map(node => <option key={node.id} value={node.id}>{node.name.trim() || node.id}{node.online ? "" : "（离线）"}</option>)}
              </Select>
            )}
          </Field>
        </div>
        <Field label="探测目标" hint="HTTP(S) URL、host:port；目标授权与 SSRF 由服务端执行。">
          {ids => <TextInput id={ids.inputId} value={target} onChange={event => setTarget(event.target.value)} required />}
        </Field>
        <div className="grid gap-3 sm:grid-cols-4">
          <Field label="间隔（秒）">{ids => <TextInput id={ids.inputId} type="number" min={15} value={intervalSeconds} onChange={event => setIntervalSeconds(event.target.value)} />}</Field>
          <Field label="超时（秒）">{ids => <TextInput id={ids.inputId} type="number" min={1} value={timeoutSeconds} onChange={event => setTimeoutSeconds(event.target.value)} />}</Field>
          <Field label="连续失败判定">{ids => <TextInput id={ids.inputId} type="number" min={1} value={failureCount} onChange={event => setFailureCount(event.target.value)} />}</Field>
          <Field label="连续恢复判定">{ids => <TextInput id={ids.inputId} type="number" min={1} value={recoveryCount} onChange={event => setRecoveryCount(event.target.value)} />}</Field>
        </div>
        <fieldset className="rounded-lg border px-4 py-3">
          <legend className="px-1 text-sm font-medium">通知渠道</legend>
          {(["qqbot", "smtp"] as const).map(channel => (
            <label key={channel} className="mt-1 flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                className="size-4 accent-[var(--primary)]"
                checked={channels.has(channel)}
                onChange={event => {
                  setChannels(current => {
                    const next = new Set(current);
                    if (event.target.checked) next.add(channel);
                    else next.delete(channel);
                    return next;
                  });
                }}
              />
              {channel === "qqbot" ? "QQ Bot" : "SMTP"}
            </label>
          ))}
        </fieldset>
        <p className="text-xs text-muted-foreground">默认建议 60 秒间隔、5 秒超时、3 次失败判异常、2 次成功判恢复；服务端可设更严格上限。</p>
      </form>
    </Modal>
  );
}

export function MonitoringPage() {
  const session = useSession();
  const [tab, setTab] = useState<Tab>("resources");
  const [editor, setEditor] = useState<{ open: boolean; monitor: AvailabilityMonitor | null }>({ open: false, monitor: null });
  const [deleting, setDeleting] = useState<AvailabilityMonitor | null>(null);
  const [checksResult, setChecksResult] = useState<{ name: string; ok: boolean } | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const nodes = useResource(`monitor:nodes:${session.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const risks = useResource(`monitor:risks:${session.viewEpoch}`, signal => trafficApi.risks(signal).then(result => result.data));
  const monitors = useResource(`monitor:list:${session.viewEpoch}`, signal => monitoringApi.list(signal).then(result => result.data));
  const channels = useResource(`monitor:channels:${session.viewEpoch}`, signal => notificationsApi.channels(signal).then(result => result.data));
  const deliveries = useResource(`monitor:deliveries:${session.viewEpoch}`, signal => notificationsApi.deliveries(signal).then(result => result.data));

  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const riskItems: readonly TrafficRisk[] = risks.state.status === "ready" ? risks.state.data : [];
  const monitorItems = monitors.state.status === "ready" ? monitors.state.data : [];
  const channelItems: readonly NotificationChannel[] = channels.state.status === "ready" ? channels.state.data.items : [];
  const deliveryItems = deliveries.state.status === "ready" ? deliveries.state.data.items : [];

  const pauseAction = useAction((monitor: AvailabilityMonitor, paused: boolean) =>
    monitoringApi.update(monitor.id ?? "", { ...monitor, state: paused ? "paused" : "pending" }),
  );
  const deleteAction = useAction((monitorId: string) => monitoringApi.remove(monitorId));
  const checksAction = useAction((monitor: AvailabilityMonitor) => monitoringApi.checks(monitor.id ?? ""));

  async function pause(monitor: AvailabilityMonitor, paused: boolean) {
    const result = await pauseAction.run(monitor, paused);
    if (result.ok) {
      setOutcome(paused ? "监控已请求暂停；以服务端返回状态为准。" : "监控已请求恢复；以服务端返回状态为准。");
      monitors.reload();
    }
  }

  async function remove() {
    if (!deleting?.id) return;
    const result = await deleteAction.run(deleting.id);
    if (result.ok) {
      setDeleting(null);
      setOutcome("监控已删除；审计保留。");
      monitors.reload();
    }
  }

  async function loadChecks(monitor: AvailabilityMonitor) {
    const result = await checksAction.run(monitor);
    if (result.ok && result.data) setChecksResult({ name: monitor.name, ok: result.data.data.ok });
  }

  const canManage = session.can("monitor.manage");

  return (
    <div className="space-y-5">
      <PageHeader
        title="监控告警"
        description="资源当前读数、HTTP/TCP/TLS 可用性监控与通知投递。历史趋势/告警事件受当前契约限制，不能伪造。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">监控</span>}
        actions={<Button type="button" onClick={() => { nodes.reload(); risks.reload(); monitors.reload(); channels.reload(); deliveries.reload(); }}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      <TabBar
        label="监控告警分区"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: "resources", label: "资源监控" },
          { id: "service", label: "服务监控" },
          { id: "alerts", label: "通知与投递" },
        ]}
      />

      {tab === "resources" ? (
        <div className="space-y-5">
          <Notice tone="info" title="资源历史趋势缺口">
            契约只提供节点当前 `cpuPercent/memoryPercent`（无采样时间）与流量风险摘要，没有历史指标/告警规则端点。本页不绘制伪造曲线。
          </Notice>
          <Panel title="节点当前资源" description="GET /api/v1/nodes + GET /api/v1/traffic-risks；离线节点不显示历史百分比。">
            <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="节点资源加载失败" empty={<EmptyState title="没有可见节点" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="节点当前资源与流量风险"
                  rows={nodeItems}
                  rowKey={node => node.id}
                  columns={[
                    { key: "name", header: "节点", render: node => node.name.trim() || node.id },
                    { key: "online", header: "在线", render: node => (node.online ? "在线" : "离线") },
                    { key: "cpu", header: "CPU", align: "right", render: node => <LegacyMetricCell metric={adaptMetric(node.cpuPercent, node.online)} /> },
                    { key: "memory", header: "内存", align: "right", render: node => <LegacyMetricCell metric={adaptMetric(node.memoryPercent, node.online)} /> },
                    {
                      key: "risk",
                      header: "流量/到期风险",
                      render: node => {
                        const list = riskItems.filter(risk => risk.nodeId === node.id);
                        return list.length ? (
                          <span className="text-xs">
                            {list.map(risk => `${risk.kind}(${risk.severity}${risk.estimated ? "，估算" : ""})`).join("、")}
                          </span>
                        ) : (
                          <span className="text-xs text-muted-foreground">无返回风险</span>
                        );
                      },
                    },
                    {
                      key: "actions",
                      header: "操作",
                      render: node => (
                        <a href={`#/nodes/${encodeURIComponent(node.id)}`} className="text-xs text-primary underline-offset-2 hover:underline">
                          详情/配额
                        </a>
                      ),
                    },
                  ]}
                />
              )}
            </AsyncPanel>
            {risks.state.status === "error" ? <div className="border-t px-5 py-3 text-xs text-amber-800">流量风险接口不可用：{risks.state.error.code ?? risks.state.error.kind}</div> : null}
          </Panel>
        </div>
      ) : null}

      {tab === "service" ? (
        <div className="space-y-5">
          {!canManage ? <Notice tone="warning" title="写操作已锁定">当前身份未返回监控管理权限（`monitor.manage`）。</Notice> : null}
          <Panel
            title="服务可用性监控"
            description="GET/POST/PATCH/DELETE /api/v1/availability-monitors；探测源失联只能标记未知，不推断业务宕机。"
            actions={<Button size="sm" variant="primary" disabled={!canManage || session.writesLocked} onClick={() => setEditor({ open: true, monitor: null })}>新建监控</Button>}
          >
            <AsyncPanel
              state={monitors.state}
              onRetry={monitors.reload}
              errorTitle="监控列表加载失败"
              empty={<EmptyState title="暂无服务监控" description="创建后会按服务端计划执行探测。" />}
              isEmpty={data => data.length === 0}
            >
              {() => (
                <DataTable
                  caption="服务可用性监控"
                  rows={monitorItems}
                  rowKey={monitor => monitor.id ?? monitor.name}
                  columns={[
                    { key: "name", header: "名称", render: monitor => monitor.name },
                    { key: "target", header: "目标", render: monitor => `${monitor.type.toUpperCase()} · ${monitor.target}` },
                    { key: "state", header: "状态", render: monitor => MONITOR_STATE_TEXT[monitor.state ?? "unknown"] },
                    { key: "interval", header: "间隔/超时", render: monitor => `${monitor.intervalSeconds}s / ${monitor.timeoutSeconds}s` },
                    { key: "checked", header: "最近检查", render: monitor => formatDateTime(monitor.lastCheckedAt) ?? "—" },
                    { key: "channels", header: "通知", render: monitor => monitor.notifyChannels?.join("、") || "未设置" },
                    {
                      key: "actions",
                      header: "操作",
                      render: monitor => (
                        <div className="flex flex-wrap gap-1">
                          <Button size="sm" onClick={() => setEditor({ open: true, monitor })} disabled={!canManage || session.writesLocked}>编辑</Button>
                          <Button
                            size="sm"
                            onClick={() => void pause(monitor, monitor.state !== "paused")}
                            disabled={!canManage || session.writesLocked || pauseAction.pending}
                          >
                            {monitor.state === "paused" ? "恢复" : "暂停"}
                          </Button>
                          <Button size="sm" onClick={() => void loadChecks(monitor)} loading={checksAction.pending}>检查历史</Button>
                          <Button size="sm" variant="danger" disabled={!canManage || session.writesLocked} onClick={() => setDeleting(monitor)}>删除</Button>
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </AsyncPanel>
            {checksResult ? (
              <div className="border-t px-5 py-3 text-xs">
                监控“{checksResult.name}”检查接口返回 ok={String(checksResult.ok)}；契约没有检查历史/探测源状态字段，因此不展示伪造历史。
              </div>
            ) : null}
          </Panel>
        </div>
      ) : null}

      {tab === "alerts" ? (
        <div className="space-y-5">
          <Notice tone="info" title="告警事件契约缺口">
            当前没有独立“告警事件”查询端点；本页展示通知渠道状态与投递记录。投递“平台已接受”不代表用户已阅读。
          </Notice>
          <Panel title="通知渠道状态" description="GET /api/v1/notifications/channels；密钥只返回配置状态。">
            <AsyncPanel state={channels.state} onRetry={channels.reload} errorTitle="通知渠道加载失败" empty={<EmptyState title="未返回通知渠道" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="通知渠道"
                  rows={channelItems}
                  rowKey={channel => channel.channel}
                  columns={[
                    { key: "channel", header: "渠道", render: channel => (channel.channel === "smtp" ? "SMTP" : "QQ Bot") },
                    { key: "enabled", header: "启用", render: channel => (channel.enabled ? "是" : "否") },
                    { key: "cred", header: "凭据状态", render: channel => channel.credentialStatus },
                    { key: "actions", header: "操作", render: () => <a href="#/settings" className="text-xs text-primary underline-offset-2 hover:underline">前往系统设置</a> },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
          <Panel title="通知投递" description="GET /api/v1/notifications/deliveries；状态是平台语义，不是收件人已读回执。" actions={<Button size="sm" onClick={() => deliveries.reload()} loading={deliveries.state.status === "loading"}>刷新</Button>}>
            <AsyncPanel state={deliveries.state} onRetry={deliveries.reload} errorTitle="投递记录加载失败" empty={<EmptyState title="暂无投递记录" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="通知投递记录"
                  rows={deliveryItems}
                  rowKey={delivery => delivery.id}
                  columns={[
                    { key: "id", header: "记录", render: delivery => <span className="font-mono text-xs">{delivery.id}</span> },
                    { key: "channel", header: "渠道", render: delivery => (delivery.channel === "smtp" ? "SMTP" : "QQ Bot") },
                    { key: "status", header: "状态", render: delivery => DELIVERY_STATUS_TEXT[delivery.status] ?? delivery.status },
                    { key: "code", header: "供应商码", render: delivery => delivery.providerCode ?? "—" },
                    { key: "time", header: "时间", render: delivery => formatDateTime(delivery.createdAt) ?? "—" },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
        </div>
      ) : null}

      <MonitorEditor
        open={editor.open}
        monitor={editor.monitor}
        nodes={nodeItems}
        onClose={() => setEditor({ open: false, monitor: null })}
        onSaved={() => {
          setEditor({ open: false, monitor: null });
          setOutcome("监控已保存（服务端结果）。");
          monitors.reload();
        }}
      />
      <Modal
        open={deleting !== null}
        title="删除服务监控"
        description={deleting ? `将删除“${deleting.name}”的配置；审计保留。` : undefined}
        onClose={() => {
          if (!deleteAction.pending) setDeleting(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setDeleting(null)} disabled={deleteAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={remove} loading={deleteAction.pending}>确认删除</Button>
          </>
        }
      >
        {deleteAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteAction.error)}</Notice> : null}
      </Modal>
    </div>
  );
}
