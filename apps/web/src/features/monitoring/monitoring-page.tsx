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
import type { AvailabilityMonitor, MonitorCheckEntry, MonitorState, MonitorProbeResult, NotificationDelivery, NotificationChannel, NodeSummary, TrafficRisk } from "@/lib/api/dto";
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

const CHECK_STATUS_TEXT: Record<string, string> = {
  up: "up（可达）",
  down: "down（不可达）",
  unknown: "unknown（未判定）",
};

/** 检查历史弹窗：GET /api/v1/availability-monitors/{id}/history，服务端记录为准；无记录如实显示。 */
function CheckHistory({ monitor, onClose }: { readonly monitor: AvailabilityMonitor; readonly onClose: () => void }) {
  const [state, setState] = useState<{ loading: boolean; error: string | null; items: readonly MonitorCheckEntry[] }>({
    loading: true, error: null, items: [],
  });

  useEffect(() => {
    let cancelled = false;
    setState({ loading: true, error: null, items: [] });
    monitoringApi
      .history(monitor.id ?? "")
      .then(result => {
        if (cancelled) return;
        setState({ loading: false, error: null, items: result.data.items });
      })
      .catch(error => {
        if (cancelled) return;
        setState({ loading: false, error: describeApiError(error), items: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [monitor.id]);

  return (
    <Modal
      open
      title={monitor.name ? `检查历史 · ${monitor.name}` : "检查历史"}
      description="服务端持久化的探测采样（新→旧）；探测源失联只标记 unknown，不推断业务宕机。"
      onClose={onClose}
      wide
      footer={<Button type="button" onClick={onClose}>关闭</Button>}
    >
      {state.loading ? <p className="text-sm text-muted-foreground">正在加载…</p> : null}
      {state.error ? <Notice tone="error" title="检查历史加载失败">{state.error}</Notice> : null}
      {!state.loading && !state.error ? (
        state.items.length === 0 ? (
          <EmptyState title="暂无检查记录" description="服务端没有该监控的探测采样；不会伪造历史。" />
        ) : (
          <DataTable
            caption="检查历史"
            rows={state.items}
            rowKey={(check, index) => `${check.checkedAt}-${check.id ?? index}`}
            columns={[
              { key: "checkedAt", header: "检查时间", render: check => formatDateTime(check.checkedAt) ?? check.checkedAt },
              { key: "status", header: "状态", render: check => CHECK_STATUS_TEXT[check.status] ?? check.status },
              { key: "latency", header: "延迟", align: "right", render: check => (typeof check.latencyMs === "number" ? `${check.latencyMs} ms` : "—") },
              { key: "code", header: "状态码", align: "right", render: check => (typeof check.statusCode === "number" ? check.statusCode : "—") },
              { key: "error", header: "错误摘要", render: check => check.error?.trim() || "—" },
              { key: "source", header: "来源", render: check => check.sourceNodeId || "Controller" },
              { key: "task", header: "任务", render: check => check.taskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(check.taskId)}`}>查看探测任务</a> : "—" },
            ]}
          />
        )
      ) : null}
    </Modal>
  );
}

function AlertHistory({ monitor, onClose }: { readonly monitor: AvailabilityMonitor; readonly onClose: () => void }) {
 const session=useSession();
 const events=useResource(`monitor:alert-history:${session.viewEpoch}:${monitor.id}`, signal=>monitoringApi.alerts(monitor.id ?? "",signal).then(result=>result.data),{refreshIntervalMs:5000});
 const labels:Record<string,string>={pending:"等待可用渠道",queued:"排队",submitted:"已提交，结果待确认",provider_accepted:"平台已接受",delivery_unknown:"投递未知，请人工核查",blocked:"配置或权限已变更",failed:"失败",rejected:"平台拒绝",rate_limited:"平台限流"};
 return <Modal open title={`告警历史 · ${monitor.name}`} onClose={onClose} wide footer={<Button onClick={onClose}>关闭</Button>}>
 <p className="mb-3 text-sm text-muted-foreground">告警状态与探测状态独立。UNKNOWN 不自动重发，可在对应任务中进入人工处置。</p>
 <AsyncPanel state={events.state} onRetry={events.reload} errorTitle="告警历史加载失败" empty={<EmptyState title="暂无告警事件" />} isEmpty={data=>data.items.length===0}>
 {data=><DataTable caption="监控告警事件" rows={data.items} rowKey={event=>event.key} columns={[
 {key:"state",header:"状态",render:event=>labels[event.state] ?? event.state},
 {key:"time",header:"发生时间",render:event=>formatDateTime(event.createdAt)},
 {key:"delivery",header:"投递记录",render:event=>event.deliveryId ?? "—"},
 {key:"task",header:"投递任务",render:event=>event.taskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(event.taskId)}`}>查看任务与人工处置</a> : "尚未创建任务"},
 ]}/>}
 </AsyncPanel></Modal>;
}

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
    if(sourceNodeId && Number(timeoutSeconds)>30) return setLocalError("节点探测的目标超时最多30秒。");
    if (Number(timeoutSeconds) < 1) return setLocalError("超时至少 1 秒。");
    setLocalError(null);
    // 真实 Controller 的 PATCH/POST 使用 DisallowUnknownFields：只回传契约字段，
    // 不把列表响应中的只读字段（consecutiveFailures/createdAt/lastError…）原样回传（GM-W4-02）。
    const body: AvailabilityMonitor = {
      ...(monitor?.id ? { id: monitor.id } : {}),
      name: name.trim(),
      type,
      target: target.trim(),
      sourceNodeId,
      intervalSeconds: Number(intervalSeconds),
      timeoutSeconds: Number(timeoutSeconds),
      failureCount: Number(failureCount),
      recoveryCount: Number(recoveryCount),
      notifyChannels: [...channels],
      ...(monitor?.id ? {} : { state: "pending" as const }),
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
          <Field label="探测来源" hint="节点需要支持 monitoring.probe；节点源仅探测公网目标。离线或未取得可信回执时显示未知，不判定业务宕机。">
            {ids => <Select id={ids.inputId} value={sourceNodeId} onChange={event=>setSourceNodeId(event.target.value)} disabled={disabled}>
              <option value="">Controller</option>
              {nodes.map(node=><option key={node.id} value={node.id}>{node.name || node.id}{node.online ? "" : "（离线）"}</option>)}
            </Select>}
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
  const [alertHistory, setAlertHistory] = useState<AvailabilityMonitor | null>(null);
  const [history, setHistory] = useState<AvailabilityMonitor | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const nodes = useResource(`monitor:nodes:${session.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const risks = useResource(`monitor:risks:${session.viewEpoch}`, signal => trafficApi.risks(signal).then(result => result.data));
  const monitors = useResource(`monitor:list:${session.viewEpoch}`, signal => monitoringApi.list(signal).then(result => result.data), { refreshIntervalMs: 10000 });
  const channels = useResource(`monitor:channels:${session.viewEpoch}`, signal => notificationsApi.channels(signal).then(result => result.data));
  const deliveries = useResource(`monitor:deliveries:${session.viewEpoch}`, signal => notificationsApi.deliveries(signal).then(result => result.data));

  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const riskItems: readonly TrafficRisk[] = risks.state.status === "ready" ? risks.state.data : [];
  const monitorItems = monitors.state.status === "ready" ? monitors.state.data : [];
  const channelItems: readonly NotificationChannel[] = channels.state.status === "ready" ? channels.state.data.items : [];
  const deliveryItems = deliveries.state.status === "ready" ? deliveries.state.data.items : [];

  // 真实 Controller 的 PATCH 拒绝未知字段：暂停/恢复只回传白名单契约字段（GM-W4-02）。
  const pauseAction = useAction((monitor: AvailabilityMonitor, paused: boolean) =>
    monitoringApi.update(monitor.id ?? "", {
      id: monitor.id,
      name: monitor.name,
      type: monitor.type,
      target: monitor.target,
      sourceNodeId: monitor.sourceNodeId ?? "",
      intervalSeconds: monitor.intervalSeconds,
      timeoutSeconds: monitor.timeoutSeconds,
      state: paused ? "paused" : "pending",
    }),
  );
  const deleteAction = useAction((monitorId: string) => monitoringApi.remove(monitorId));
  const probeAction = useAction((monitorId: string) => monitoringApi.probe(monitorId));

  async function probe(monitor: AvailabilityMonitor) {
    const result = await probeAction.run(monitor.id ?? "");
    if (result.ok && result.data) {
      const check: MonitorProbeResult = result.data.data;
      const latency = typeof check.latencyMs === "number" ? ` · 延迟 ${check.latencyMs} ms` : "";
      const code = typeof check.statusCode === "number" ? ` · 状态码 ${check.statusCode}` : "";
      setOutcome(
        `立即探测完成：${CHECK_STATUS_TEXT[check.status] ?? check.status}${latency}${code} · 连续失败 ${check.consecutiveFailures}。${check.error ? ` 错误摘要：${check.error}` : ""}`,
      );
      monitors.reload();
    } else if (result.error) {
      setOutcome(
        `探测未执行（HTTP ${result.error.status}${result.error.code ? ` · 错误码 ${result.error.code}` : ""}）：${describeApiError(result.error)} 探测是否发起始终由服务端 SSRF/白名单边界决定。`,
      );
    }
  }

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

  const canManage = session.can("monitor.manage");

  return (
    <div className="space-y-5">
      <PageHeader
        title="监控告警"
        description="资源当前读数、HTTP/TCP/TLS 可用性监控（含真实检查历史与立即探测）与通知投递。资源历史趋势仍受当前契约限制，不能伪造。"
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
            description="GET/POST/PATCH/DELETE /api/v1/availability-monitors；“检查历史”与“立即探测”对接真实历史/探测端点，探测由服务端 SSRF 边界执行。"
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
                    { key: "state", header: "状态", render: monitor => monitor.lastProbeUnknown ? "未知（探测来源或执行异常）" : MONITOR_STATE_TEXT[monitor.state ?? "unknown"] },
                    { key: "source", header: "探测来源", render: monitor => monitor.sourceNodeId ? nodeItems.find(node=>node.id===monitor.sourceNodeId)?.name || monitor.sourceNodeId : "Controller" },
                    { key: "probeTask", header: "探测任务", render: monitor => monitor.lastProbeTaskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(monitor.lastProbeTaskId)}`}>查看任务</a> : "—" },
                    { key: "automation", header: "后台调度", render: monitor => monitor.automationOwnerId ? "已绑定保存者权限" : "请编辑并保存以授权" },
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
                          <Button size="sm" onClick={() => setHistory(monitor)}>检查历史</Button>
                          <Button size="sm" onClick={() => setAlertHistory(monitor)}>告警历史</Button>
                          <Button
                            size="sm"
                            variant="primary"
                            disabled={!canManage || session.writesLocked || probeAction.pending}
                            onClick={() => void probe(monitor)}
                            loading={probeAction.pending}
                          >
                            立即探测
                          </Button>
                          <Button size="sm" variant="danger" disabled={!canManage || session.writesLocked} onClick={() => setDeleting(monitor)}>删除</Button>
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
      {alertHistory ? <AlertHistory monitor={alertHistory} onClose={() => setAlertHistory(null)} /> : null}
      {history ? <CheckHistory monitor={history} onClose={() => setHistory(null)} /> : null}
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
