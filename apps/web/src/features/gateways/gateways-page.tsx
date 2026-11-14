import { useEffect, useState, type FormEvent } from "react";
import { Globe, Network, RefreshCw, Server } from "lucide-react";
import { useSession } from "@/app/session";
import { GlowingEffect } from "@/components/aceternity/glowing-effect";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable, DefinitionList, TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi, serviceGroupsApi } from "@/lib/api/endpoints";
import { gatewaysApi } from "@/lib/api/endpoints-infra";
import type { GatewayHealthChecks, GatewayNode, GatewayProvisionJob, GatewayRoute, GatewayRouteInput, NodeSummary, ServiceGroup } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const GATEWAY_STATE: Record<GatewayNode["status"], string> = {
  pending: "待安装",
  installing: "安装中",
  verifying: "检查中",
  ready: "就绪",
  failed: "失败",
  maintenance: "维护中",
};

function GatewayTopologyCanvas({
  gateways,
  routes,
  serviceGroups,
}: {
  readonly gateways: readonly GatewayNode[];
  readonly routes: readonly GatewayRoute[];
  readonly serviceGroups: readonly ServiceGroup[];
}) {
  if (gateways.length === 0 && routes.length === 0) {
    return (
      <div className="px-6 py-8 text-center text-sm text-muted-foreground">
        当前尚无已登记网关或路由数据；接入网关并配置路由后将在此实时绘制真实流量拓扑。
      </div>
    );
  }

  const eligibleCount = gateways.filter(gw => gw.schedulingEligible).length;
  const activeRouteCount = routes.filter(rt => rt.enabled).length;

  return (
    <div className="neurox-dot-bg relative overflow-hidden px-5 py-5">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-3 text-xs">
          <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/25 bg-emerald-500/10 px-2.5 py-1 font-semibold text-emerald-800">
            <span className="size-2 animate-pulse rounded-full bg-emerald-500" />
            可调度网关: {eligibleCount} / {gateways.length}
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-primary/20 bg-primary-light px-2.5 py-1 font-semibold text-primary">
            已启用路由: {activeRouteCount} / {routes.length}
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-full border border-border/80 bg-card px-2.5 py-1 font-medium text-muted-foreground">
            关联服务组: {serviceGroups.length}
          </span>
        </div>
        <span className="text-[11px] text-muted-foreground">
          拓扑连线 100% 由服务端真实网关、路由与服务组关系生成
        </span>
      </div>

      <div className="relative grid gap-6 lg:grid-cols-[1fr_5rem_1.15fr_5rem_1fr] lg:items-center">
        {/* Column 1: Ingress Routes */}
        <div className="space-y-2.5">
          <div className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            <Globe className="size-3.5 text-primary" />
            <span>入口域名与路由 ({routes.length})</span>
          </div>
          {routes.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border bg-card/80 p-3.5 text-xs text-muted-foreground">
              暂无路由规则（请在“服务路由”标签页新建）
            </div>
          ) : (
            routes.slice(0, 6).map(route => (
              <div
                key={route.id}
                className="flex items-center justify-between gap-2 rounded-xl border border-border/80 bg-card px-3.5 py-2.5 shadow-2xs"
              >
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs font-semibold text-foreground">
                    {route.https ? "https://" : "http://"}
                    {route.domain}
                  </p>
                  <p className="mt-0.5 truncate text-[11px] text-muted-foreground">
                    归属网关: {route.gatewayId ?? "默认"}
                  </p>
                </div>
                <Badge variant={route.enabled ? "success" : "outline"}>
                  {route.enabled ? "流量生效" : "已停用"}
                </Badge>
              </div>
            ))
          )}
        </div>

        {/* Animated SVG Connector 1: Ingress -> Gateways */}
        <div aria-hidden="true" className="hidden lg:flex lg:items-center lg:justify-center">
          <svg viewBox="0 0 80 120" className="h-28 w-20 overflow-visible">
            <defs>
              <linearGradient id="gw-flow-1" x1="0%" y1="0%" x2="100%" y2="0%">
                <stop offset="0%" stopColor="#6658E8" />
                <stop offset="100%" stopColor="#3984E7" />
              </linearGradient>
            </defs>
            <path d="M 0 30 C 40 30, 40 60, 80 60" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            <path d="M 0 60 C 40 60, 40 60, 80 60" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            <path d="M 0 90 C 40 90, 40 60, 80 60" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            {eligibleCount > 0 ? (
              <>
                <path
                  d="M 0 30 C 40 30, 40 60, 80 60"
                  fill="none"
                  stroke="url(#gw-flow-1)"
                  strokeWidth="2.2"
                  className="animate-neurox-dash"
                />
                <path
                  d="M 0 60 C 40 60, 40 60, 80 60"
                  fill="none"
                  stroke="url(#gw-flow-1)"
                  strokeWidth="2.2"
                  className="animate-neurox-dash"
                />
              </>
            ) : null}
          </svg>
        </div>

        {/* Column 2: Nginx Gateway Nodes */}
        <div className="space-y-2.5">
          <div className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            <Network className="size-3.5 text-primary" />
            <span>Nginx 网关集群 ({gateways.length})</span>
          </div>
          {gateways.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border bg-card/80 p-3.5 text-xs text-muted-foreground">
              暂无网关节点
            </div>
          ) : (
            gateways.map(gw => (
              <GlowingEffect
                key={gw.id}
                glowColor={gw.schedulingEligible ? "rgba(22, 155, 104, 0.16)" : "rgba(216, 155, 43, 0.15)"}
                className="rounded-xl border border-border/80 bg-card p-3.5 shadow-[var(--shadow-card)]"
              >
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <span
                      className={`size-2.5 rounded-full ${
                        gw.schedulingEligible
                          ? "bg-emerald-500 ring-4 ring-emerald-500/15"
                          : gw.online
                            ? "bg-amber-500"
                            : "bg-slate-400"
                      }`}
                    />
                    <span className="font-semibold text-foreground">{`${gw.name} (${gw.id})`}</span>
                  </div>
                  <Badge variant={gw.schedulingEligible ? "success" : gw.online ? "warning" : "outline"}>
                    {gw.schedulingEligible ? "调度就绪" : GATEWAY_STATE[gw.status]}
                  </Badge>
                </div>
                <div className="mt-2 flex flex-wrap items-center gap-3 text-[11px] text-muted-foreground">
                  <span className="font-mono">节点: {gw.nodeId}</span>
                  <span>Nginx: {gw.nginxVersion ?? "未知"}</span>
                  <span>健康探测: {gw.checksPassed ? "通过" : "未通过"}</span>
                </div>
              </GlowingEffect>
            ))
          )}
        </div>

        {/* Animated SVG Connector 2: Gateways -> Backends */}
        <div aria-hidden="true" className="hidden lg:flex lg:items-center lg:justify-center">
          <svg viewBox="0 0 80 120" className="h-28 w-20 overflow-visible">
            <defs>
              <linearGradient id="gw-flow-2" x1="0%" y1="0%" x2="100%" y2="0%">
                <stop offset="0%" stopColor="#3984E7" />
                <stop offset="100%" stopColor="#169B68" />
              </linearGradient>
            </defs>
            <path d="M 0 60 C 40 60, 40 30, 80 30" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            <path d="M 0 60 C 40 60, 40 60, 80 60" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            <path d="M 0 60 C 40 60, 40 90, 80 90" fill="none" stroke="#E8EAF1" strokeWidth="2" />
            {eligibleCount > 0 && (serviceGroups.length > 0 || routes.length > 0) ? (
              <>
                <path
                  d="M 0 60 C 40 60, 40 30, 80 30"
                  fill="none"
                  stroke="url(#gw-flow-2)"
                  strokeWidth="2.2"
                  className="animate-neurox-dash"
                />
                <path
                  d="M 0 60 C 40 60, 40 90, 80 90"
                  fill="none"
                  stroke="url(#gw-flow-2)"
                  strokeWidth="2.2"
                  className="animate-neurox-dash"
                />
              </>
            ) : null}
          </svg>
        </div>

        {/* Column 3: Target Service Groups & Upstreams */}
        <div className="space-y-2.5">
          <div className="flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            <Server className="size-3.5 text-emerald-600" />
            <span>后端服务组与上游 ({serviceGroups.length + routes.filter(r => Boolean(r.upstream)).length})</span>
          </div>
          {serviceGroups.length === 0 && routes.filter(r => Boolean(r.upstream)).length === 0 ? (
            <div className="rounded-xl border border-dashed border-border bg-card/80 p-3.5 text-xs text-muted-foreground">
              暂无关联后端服务组或直接 upstream
            </div>
          ) : (
            <>
              {serviceGroups.slice(0, 4).map(sg => (
                <div
                  key={sg.id}
                  className="flex items-center justify-between gap-2 rounded-xl border border-border/80 bg-card px-3.5 py-2.5 shadow-2xs"
                >
                  <div className="min-w-0">
                    <p className="truncate text-xs font-semibold text-foreground">{`服务组: ${sg.name} (${sg.id})`}</p>
                    <p className="mt-0.5 text-[11px] text-muted-foreground">
                      健康实例 {sg.healthyNodeIds?.length ?? 0} / 成员 {sg.memberNodeIds.length} 台
                    </p>
                  </div>
                  <Badge variant={sg.loadBalancing.enabled ? "info" : "outline"}>
                    {sg.service ? `${sg.service.protocol}:${sg.service.port}` : "未配端口"}
                  </Badge>
                </div>
              ))}
              {routes
                .filter(r => Boolean(r.upstream))
                .slice(0, 3)
                .map(r => (
                  <div
                    key={`up-${r.id}`}
                    className="flex items-center justify-between gap-2 rounded-xl border border-border/80 bg-card px-3.5 py-2.5 shadow-2xs"
                  >
                    <span className="truncate font-mono text-xs text-foreground">{`直连: ${r.upstream}`}</span>
                    <Badge variant="outline">Upstream</Badge>
                  </div>
                ))}
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function ChecksBody({ checks }: { readonly checks: GatewayHealthChecks }) {
  const items = [
    ["Nginx 已安装", checks.nginxInstalled],
    ["服务运行", checks.serviceRunning],
    ["配置有效", checks.configValid],
    ["健康探测", checks.healthProbePassed],
  ] as const;
  return (
    <div className="space-y-2 px-5 py-4 text-sm">
      <ul className="space-y-1">
        {items.map(([label, passed]) => (
          <li key={label}>{label}：{passed ? "通过" : "未通过"}</li>
        ))}
      </ul>
      <p className="text-xs text-muted-foreground">检查时间：{formatDateTime(checks.checkedAt) ?? "未提供"}{checks.message ? ` · ${checks.message}` : ""}</p>
    </div>
  );
}

function RouteEditor({
  open,
  route,
  serviceGroups,
  gateways,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly route: GatewayRoute | null;
  readonly serviceGroups: readonly ServiceGroup[];
  readonly gateways: readonly GatewayNode[];
  readonly onClose: () => void;
  readonly onSaved: () => void;
}) {
  const session = useSession();
  const [mode, setMode] = useState<"upstream" | "serviceGroup">("upstream");
  const [domain, setDomain] = useState("");
  const [upstream, setUpstream] = useState("");
  const [serviceGroupId, setServiceGroupId] = useState("");
  // 真实 Controller 的路由创建强制要求 gatewayId（internal/gateway/http.go 校验非空）。
  const [gatewayId, setGatewayId] = useState("");
  const [https, setHttps] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: GatewayRouteInput) =>
    route ? gatewaysApi.updateRoute(route.id, body) : gatewaysApi.createRoute({ ...body, gatewayId }),
  );

  useEffect(() => {
    if (!open) return;
    setMode(route?.serviceGroupId ? "serviceGroup" : "upstream");
    setDomain(route?.domain ?? "");
    setUpstream(route?.upstream ?? "");
    setServiceGroupId(route?.serviceGroupId ?? "");
    setGatewayId(route?.gatewayId ?? "");
    setHttps(route?.https ?? true);
    setEnabled(route?.enabled ?? true);
    setLocalError(null);
  }, [open, route]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!route && !gatewayId) return setLocalError("必须选择所属网关（真实 Controller 按网关归属校验路由）。");
    if (!domain.trim()) return setLocalError("域名不能为空。");
    if (mode === "upstream" && !upstream.trim()) return setLocalError("上游地址不能为空。");
    if (mode === "serviceGroup" && !serviceGroupId) return setLocalError("必须选择服务组。");
    setLocalError(null);
    const result = await action.run({
      domain: domain.trim(),
      https,
      enabled,
      ...(mode === "upstream" ? { upstream: upstream.trim() } : { serviceGroupId }),
    });
    if (result.ok) onSaved();
  }

  const disabled = !session.can("gateway.manage") || session.writesLocked;

  return (
    <Modal
      open={open}
      title={route ? "编辑网关路由" : "新建网关路由"}
      description="后端只能二选一：直接 upstream 字符串或服务组 ID；服务端会检查网关准入和引用有效性。"
      onClose={() => { if (!action.pending) onClose(); }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="route-editor" variant="primary" loading={action.pending} disabled={disabled}>保存路由</Button>
        </>
      }
    >
      <form id="route-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        {!route ? (
          <Field label="所属网关" hint="真实 Controller 的路由创建按网关归属校验；编辑时不可更换。">
            {ids => (
              <Select id={ids.inputId} value={gatewayId} onChange={event => setGatewayId(event.target.value)} required disabled={disabled}>
                <option value="">请选择网关</option>
                {gateways.map(gateway => <option key={gateway.id} value={gateway.id}>{gateway.name.trim() || gateway.id}</option>)}
              </Select>
            )}
          </Field>
        ) : null}
        <Field label="访问域名">{ids => <TextInput id={ids.inputId} value={domain} onChange={event => setDomain(event.target.value)} required disabled={disabled} />}</Field>
        <Field label="后端类型">
          {ids => (
            <Select id={ids.inputId} value={mode} onChange={event => setMode(event.target.value as typeof mode)} disabled={disabled}>
              <option value="upstream">直接 upstream</option>
              <option value="serviceGroup">服务组</option>
            </Select>
          )}
        </Field>
        {mode === "upstream" ? (
          <Field label="上游地址" hint="例如 node-01:8080。">
            {ids => <TextInput id={ids.inputId} value={upstream} onChange={event => setUpstream(event.target.value)} disabled={disabled} />}
          </Field>
        ) : (
          <Field label="服务组">
            {ids => (
              <Select id={ids.inputId} value={serviceGroupId} onChange={event => setServiceGroupId(event.target.value)} disabled={disabled}>
                <option value="">请选择</option>
                {serviceGroups.map(group => <option key={group.id} value={group.id}>{group.name}</option>)}
              </Select>
            )}
          </Field>
        )}
        <div className="flex flex-wrap gap-4">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={https} onChange={event => setHttps(event.target.checked)} disabled={disabled} />
            HTTPS
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={disabled} />
            启用
          </label>
        </div>
      </form>
    </Modal>
  );
}

export function GatewaysPage() {
  const session = useSession();
  const [tab, setTab] = useState<"gateways" | "routes">("gateways");
  const [createOpen, setCreateOpen] = useState(false);
  const [ack, setAck] = useState(false);
  const [nodeId, setNodeId] = useState("");
  const [job, setJob] = useState<GatewayProvisionJob | null>(null);
  const [checksFor, setChecksFor] = useState<{ gateway: GatewayNode; checks: GatewayHealthChecks | null } | null>(null);
  const [routeEditor, setRouteEditor] = useState<{ open: boolean; route: GatewayRoute | null }>({ open: false, route: null });
  const [deletingRoute, setDeletingRoute] = useState<GatewayRoute | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);

  const gateways = useResource(`gateways:list:${session.viewEpoch}`, signal => gatewaysApi.list(signal).then(result => result.data));
  const routes = useResource(`gateways:routes:${session.viewEpoch}`, signal => gatewaysApi.routes(signal).then(result => result.data));
  const nodes = useResource(`gateways:nodes:${session.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const serviceGroups = useResource(`gateways:groups:${session.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data));

  const gatewayItems = gateways.state.status === "ready" ? gateways.state.data.items : [];
  const routeItems = routes.state.status === "ready" ? routes.state.data.items : [];
  const nodeItems: readonly NodeSummary[] = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const groupItems: readonly ServiceGroup[] = serviceGroups.state.status === "ready" ? serviceGroups.state.data.items : [];
  const canInstall = session.can("gateway.install") || session.can("gateway.manage");
  const canManage = session.can("gateway.manage");

  const createAction = useAction((body: { nodeId: string; acknowledgeInstallImpact: true }) => gatewaysApi.create(body));
  const dispatchAction = useAction((gatewayId: string, enabled: boolean) => gatewaysApi.setDispatch(gatewayId, enabled));
  const retryAction = useAction((gatewayId: string) => gatewaysApi.provision(gatewayId));
  const checksAction = useAction((gateway: GatewayNode) => gatewaysApi.checks(gateway.id));
  const jobRefresh = useAction((jobId: string) => gatewaysApi.provisionJob(jobId));
  const deleteRouteAction = useAction((routeId: string) => gatewaysApi.removeRoute(routeId));

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!nodeId) return setLocalError("必须选择在线节点。");
    if (!ack) return setLocalError("必须确认安装影响。");
    setLocalError(null);
    const result = await createAction.run({ nodeId, acknowledgeInstallImpact: true });
    if (result.ok && result.data) {
      setCreateOpen(false);
      setAck(false);
      setJob(result.data.data);
      gateways.reload();
    }
  }

  async function toggle(gateway: GatewayNode) {
    const result = await dispatchAction.run(gateway.id, !gateway.enabled);
    if (result.ok) {
      setOutcome(gateway.enabled ? "已请求进入维护/停用调度。" : "已请求启用调度；是否可调度以服务端返回为准。");
      gateways.reload();
    }
  }

  async function retry(gateway: GatewayNode) {
    const result = await retryAction.run(gateway.id);
    if (result.ok && result.data) {
      setJob(result.data.data);
      gateways.reload();
    }
  }

  async function loadChecks(gateway: GatewayNode) {
    setChecksFor({ gateway, checks: null });
    const result = await checksAction.run(gateway);
    if (result.ok && result.data) setChecksFor({ gateway, checks: result.data.data });
  }

  async function refreshJob() {
    if (!job) return;
    const result = await jobRefresh.run(job.id);
    if (result.ok && result.data) setJob(result.data.data);
  }

  async function removeRoute() {
    if (!deletingRoute) return;
    const result = await deleteRouteAction.run(deletingRoute.id);
    if (result.ok) {
      setDeletingRoute(null);
      setOutcome("路由已删除。");
      routes.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="网关管理"
        description="Nginx 应用网关：安装/检测、健康检查、维护与调度资格。安装失败或检查未通过不可调度。"
        badge={<span className="rounded-full border border-primary/20 bg-primary-light px-2.5 py-0.5 text-xs font-semibold text-primary">应用网关</span>}
        actions={
          <Button type="button" onClick={() => { gateways.reload(); routes.reload(); nodes.reload(); }}>
            <RefreshCw aria-hidden="true" className="size-3.5" />
            刷新
          </Button>
        }
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}

      <Panel
        title="实时网络拓扑视图"
        description="基于当前服务端返回的网关、路由与后端服务组真实数据渲染动态流量路径。"
      >
        <GatewayTopologyCanvas gateways={gatewayItems} routes={routeItems} serviceGroups={groupItems} />
      </Panel>

      <TabBar
        label="网关管理分区"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: "gateways", label: "网关节点" },
          { id: "routes", label: "服务路由" },
        ]}
      />

      {tab === "gateways" ? (
        <Panel
          title="网关节点"
          description="GET /api/v1/gateways；schedulingEligible 由服务端综合在线、就绪、检查与启用状态计算。"
          actions={<Button size="sm" variant="primary" disabled={!canInstall || session.writesLocked} onClick={() => setCreateOpen(true)}>新建网关</Button>}
        >
          <AsyncPanel state={gateways.state} onRetry={gateways.reload} errorTitle="网关列表加载失败" empty={<EmptyState title="暂无网关" description="从在线节点选择并授权安装 Nginx。" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="Nginx 网关节点"
                rows={gatewayItems}
                rowKey={gateway => gateway.id}
                columns={[
                  { key: "name", header: "网关", render: gateway => <a href={`#/gateways/${encodeURIComponent(gateway.id)}`} className="text-primary underline-offset-2 hover:underline">{gateway.name}</a> },
                  { key: "node", header: "节点", render: gateway => <span className="font-mono text-xs">{gateway.nodeId}</span> },
                  { key: "state", header: "状态", render: gateway => GATEWAY_STATE[gateway.status] },
                  { key: "online", header: "在线", render: gateway => (gateway.online ? "是" : "否") },
                  { key: "checks", header: "检查", render: gateway => (gateway.checksPassed ? "通过" : "未通过") },
                  { key: "eligible", header: "可调度", render: gateway => (gateway.schedulingEligible ? "是" : "否") },
                  { key: "version", header: "Nginx", render: gateway => gateway.nginxVersion ?? "—" },
                  {
                    key: "actions",
                    header: "操作",
                    render: gateway => (
                      <div className="flex flex-wrap gap-1">
                        <Button size="sm" onClick={() => void loadChecks(gateway)}>检查</Button>
                        <Button size="sm" onClick={() => void toggle(gateway)} disabled={!canManage || session.writesLocked} loading={dispatchAction.pending}>
                          {gateway.enabled ? "停用调度" : "启用调度"}
                        </Button>
                        <Button size="sm" onClick={() => void retry(gateway)} disabled={!canInstall || session.writesLocked} loading={retryAction.pending}>重试安装</Button>
                      </div>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      ) : (
        <Panel
          title="服务路由"
          description="GET /api/v1/gateway-routes；后端为直接 upstream 或服务组 ID（二选一）。"
          actions={<Button size="sm" variant="primary" disabled={!canManage || session.writesLocked} onClick={() => setRouteEditor({ open: true, route: null })}>新建路由</Button>}
        >
          <AsyncPanel state={routes.state} onRetry={routes.reload} errorTitle="路由加载失败" empty={<EmptyState title="暂无网关路由" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="网关服务路由"
                rows={routeItems}
                rowKey={route => route.id}
                columns={[
                  { key: "domain", header: "域名", render: route => route.domain },
                  { key: "backend", header: "后端", render: route => route.serviceGroupId ? `服务组 ${route.serviceGroupId}` : route.upstream ?? "—" },
                  { key: "https", header: "HTTPS", render: route => (route.https ? "是" : "否") },
                  { key: "enabled", header: "启用", render: route => (route.enabled ? "是" : "否") },
                  {
                    key: "actions",
                    header: "操作",
                    render: route => (
                      <div className="flex gap-1">
                        <Button size="sm" onClick={() => setRouteEditor({ open: true, route })} disabled={!canManage || session.writesLocked}>编辑</Button>
                        <Button size="sm" variant="danger" onClick={() => setDeletingRoute(route)} disabled={!canManage || session.writesLocked}>删除</Button>
                      </div>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      )}

      <Modal
        open={createOpen}
        title="新建网关"
        description="只允许选择在线且未作为网关登记的节点；安装前必须确认影响。"
        onClose={() => { if (!createAction.pending) setCreateOpen(false); }}
        footer={
          <>
            <Button type="button" onClick={() => setCreateOpen(false)} disabled={createAction.pending}>取消</Button>
            <Button type="submit" form="gateway-create" variant="primary" loading={createAction.pending}>创建安装任务</Button>
          </>
        }
      >
        <form id="gateway-create" onSubmit={create} className="space-y-4">
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          {createAction.error ? <Notice tone="error" title="创建失败">{describeApiError(createAction.error)}</Notice> : null}
          <Field label="节点">
            {ids => (
              <Select id={ids.inputId} value={nodeId} onChange={event => setNodeId(event.target.value)}>
                <option value="">请选择在线节点</option>
                {nodeItems.filter(entry => entry.online).map(entry => <option key={entry.id} value={entry.id}>{entry.name.trim() || entry.id}</option>)}
              </Select>
            )}
          </Field>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1 size-4 accent-[var(--primary)]" checked={ack} onChange={event => setAck(event.target.checked)} />
            我确认安装 Nginx 可能影响节点现有服务；服务端会保留现有配置并先检测。
          </label>
        </form>
      </Modal>

      <Modal
        open={job !== null}
        title={job ? `网关安装作业 ${job.id}` : "网关安装作业"}
        description="作业异步执行；只有全部检查通过才可调度。"
        onClose={() => setJob(null)}
        footer={
          <>
            <Button type="button" onClick={() => void refreshJob()} loading={jobRefresh.pending}>刷新作业</Button>
            <Button type="button" onClick={() => setJob(null)}>关闭</Button>
          </>
        }
      >
        {job ? (
          <div className="space-y-2 text-sm">
            <p>状态：{job.status}{job.phase ? ` · ${job.phase}` : ""}</p>
            {job.error ? <Notice tone="error" title="安装错误">{job.error.code} · {job.error.message}</Notice> : null}
            {job.checks ? <ChecksBody checks={job.checks} /> : <p className="text-muted-foreground">服务端尚未返回检查明细。</p>}
          </div>
        ) : null}
      </Modal>

      <Modal
        open={checksFor !== null}
        title={checksFor ? `网关检查 · ${checksFor.gateway.name}` : "网关检查"}
        onClose={() => setChecksFor(null)}
        footer={<Button type="button" onClick={() => setChecksFor(null)}>关闭</Button>}
      >
        {checksAction.error ? <Notice tone="error" title="检查失败">{describeApiError(checksAction.error)}</Notice> : null}
        {checksFor?.checks ? <ChecksBody checks={checksFor.checks} /> : <p className="px-5 py-4 text-sm text-muted-foreground">正在读取检查结果…</p>}
      </Modal>

      <RouteEditor
        open={routeEditor.open}
        route={routeEditor.route}
        serviceGroups={groupItems}
        gateways={gatewayItems}
        onClose={() => setRouteEditor({ open: false, route: null })}
        onSaved={() => {
          setRouteEditor({ open: false, route: null });
          setOutcome("路由已保存（服务端结果）。");
          routes.reload();
        }}
      />

      <Modal
        open={deletingRoute !== null}
        title="删除网关路由"
        description={deletingRoute ? `域名 ${deletingRoute.domain}` : undefined}
        onClose={() => setDeletingRoute(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeletingRoute(null)} disabled={deleteRouteAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeRoute} loading={deleteRouteAction.pending}>确认删除</Button>
          </>
        }
      >
        {deleteRouteAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteRouteAction.error)}</Notice> : null}
      </Modal>
    </div>
  );
}


export function GatewayDetailPage({ gatewayId }: { readonly gatewayId: string }) {
  const session = useSession();
  const gateway = useResource(`gateway:detail:${session.viewEpoch}:${gatewayId}`, signal =>
    gatewaysApi.get(gatewayId, signal).then(result => result.data),
  );
  const checks = useResource(`gateway:checks:${session.viewEpoch}:${gatewayId}`, signal =>
    gatewaysApi.checks(gatewayId, signal).then(result => result.data),
    { enabled: gateway.state.status === "ready" },
  );
  const toggle = useAction((enabled: boolean) => gatewaysApi.setDispatch(gatewayId, enabled));
  const retry = useAction(() => gatewaysApi.provision(gatewayId));
  const [outcome, setOutcome] = useState<string | null>(null);
  const canManage = session.can("gateway.manage") || session.can("gateway.install");

  async function dispatch(enable: boolean) {
    const result = await toggle.run(enable);
    if (result.ok) {
      setOutcome(enable ? "已请求启用调度；以服务端 eligibility 为准。" : "已请求停用调度。");
      gateway.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={gateway.state.status === "ready" ? gateway.state.data.name : "网关详情"}
        description="网关准入由在线、就绪、Nginx 安装、配置/健康检查和管理员启用共同决定。"
        actions={
          <>
            <a href="#/gateways" className="inline-flex h-9 items-center rounded-md border bg-card px-3 text-sm hover:bg-secondary">返回网关列表</a>
            <Button type="button" disabled={!canManage || session.writesLocked || !gateway.state.status || gateway.state.status !== "ready"} onClick={() => gateway.state.status === "ready" && void dispatch(!gateway.state.data.enabled)}>
              {gateway.state.status === "ready" && gateway.state.data.enabled ? "停用调度" : "启用调度"}
            </Button>
            <Button type="button" disabled={!canManage || session.writesLocked} onClick={async () => { const result = await retry.run(); if (result.ok) setOutcome("安装/检查作业已提交。"); }} loading={retry.pending}>重试安装/检查</Button>
          </>
        }
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      {toggle.error ? <Notice tone="error" title="调度操作失败">{describeApiError(toggle.error)}</Notice> : null}
      {retry.error ? <Notice tone="error" title="重试失败">{describeApiError(retry.error)}</Notice> : null}
      <AsyncPanel state={gateway.state} onRetry={gateway.reload} errorTitle="网关加载失败" empty={<EmptyState title="网关不存在" />} isEmpty={() => false}>
        {data => (
          <Panel title="网关快照" description="GET /api/v1/gateways/{gatewayId}">
            <DefinitionList
              items={[
                { term: "节点", value: <span className="font-mono text-xs">{data.nodeId}</span> },
                { term: "状态", value: GATEWAY_STATE[data.status] },
                { term: "在线", value: data.online ? "是" : "否" },
                { term: "Nginx 已安装", value: data.nginxInstalled ? "是" : "否" },
                { term: "检查通过", value: data.checksPassed ? "是" : "否" },
                { term: "管理员启用", value: data.enabled ? "是" : "否" },
                { term: "可调度", value: data.schedulingEligible ? "是" : "否" },
                { term: "Nginx 版本", value: data.nginxVersion ?? "未知" },
                { term: "最近错误", value: data.lastError ?? "无" },
              ]}
            />
          </Panel>
        )}
      </AsyncPanel>
      <Panel title="健康检查" description="GET /api/v1/gateways/{gatewayId}/checks；失败原因以服务端为准。">
        <AsyncPanel state={checks.state} onRetry={checks.reload} errorTitle="检查结果加载失败">
          {data => <ChecksBody checks={data} />}
        </AsyncPanel>
      </Panel>
    </div>
  );
}
