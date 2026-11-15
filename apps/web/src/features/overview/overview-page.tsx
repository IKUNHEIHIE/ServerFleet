import { useMemo } from "react";
import {
  Activity,
  ArrowUpRight,
  Bot,
  Cpu,
  FileCode2,
  ListChecks,
  Network,
  PlusCircle,
  RefreshCw,
  Server,
  ShieldCheck,
  Sparkles,
  Wifi,
  WifiOff,
} from "lucide-react";
import { motion } from "motion/react";
import {
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from "recharts";
import { AnimatedNumber, GlowingEffect, Spotlight } from "@/components/aceternity";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button, LinkButton } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { auditApi, nodesApi, tasksApi } from "@/lib/api/endpoints";
import type { AuditEvent, NodeSummary, TaskSummary } from "@/lib/api/dto";
import { useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { LegacyMetricCell } from "@/features/nodes/metric-display";
import { adaptMetric } from "@/features/nodes/legacy-adapter";
import { TASK_STATE_LABEL } from "@/features/tasks/task-state";

function StatCard({
  label,
  numericValue,
  fallbackValue,
  hint,
  icon,
  tone = "primary",
}: {
  readonly label: string;
  readonly numericValue: number | null;
  readonly fallbackValue?: string;
  readonly hint: string;
  readonly icon: React.ReactNode;
  readonly tone?: "primary" | "success" | "warning" | "info";
}) {
  const accentMap = {
    primary: {
      iconBg: "bg-[#F0EEFF] text-[#6658E8] border-[#6658E8]/20",
      bar: "from-[#6658E8] to-[#3984E7]",
      glow: "rgba(102, 88, 232, 0.16)",
    },
    success: {
      iconBg: "bg-[#ECFDF5] text-[#169B68] border-[#169B68]/20",
      bar: "from-[#169B68] to-[#34D399]",
      glow: "rgba(22, 155, 104, 0.16)",
    },
    warning: {
      iconBg: "bg-[#FFFBEB] text-[#D89B2B] border-[#D89B2B]/25",
      bar: "from-[#D89B2B] to-[#F59E0B]",
      glow: "rgba(216, 155, 43, 0.16)",
    },
    info: {
      iconBg: "bg-[#EFF6FF] text-[#3984E7] border-[#3984E7]/20",
      bar: "from-[#3984E7] to-[#6658E8]",
      glow: "rgba(57, 132, 231, 0.16)",
    },
  }[tone];

  return (
    <GlowingEffect glowColor={accentMap.glow} className="rounded-2xl">
      <div className="relative flex h-full flex-col justify-between p-5">
        <div className="flex items-start justify-between gap-3">
          <div>
            <p className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {label}
            </p>
            <p className="mt-2.5 text-3xl font-bold tracking-tight text-foreground tabular-nums">
              {numericValue !== null ? (
                <AnimatedNumber value={numericValue} />
              ) : (
                fallbackValue ?? "—"
              )}
            </p>
          </div>
          <span
            className={`flex size-10 shrink-0 items-center justify-center rounded-xl border ${accentMap.iconBg}`}
          >
            {icon}
          </span>
        </div>
        <div className="mt-4">
          <p className="text-xs text-muted-foreground">{hint}</p>
          <div className="mt-3 h-1 w-full overflow-hidden rounded-full bg-secondary">
            <div className={`h-full w-2/3 rounded-full bg-gradient-to-r ${accentMap.bar}`} />
          </div>
        </div>
      </div>
    </GlowingEffect>
  );
}

function getTaskBadgeVariant(state: TaskSummary["state"]): "default" | "secondary" | "destructive" | "outline" | "success" | "warning" | "info" {
  if (state === "completed") return "success";
  if (state === "failed" || state === "expired" || state === "canceled") return "destructive";
  if (state === "partially_failed" || state === "unknown" || state === "awaiting_approval" || state === "paused") {
    return "warning";
  }
  if (state === "running" || state === "queued" || state === "waiting_node" || state === "canceling") return "info";
  return "secondary";
}

export function OverviewPage() {
  const session = useSession();
  const nodes = useResource(`overview:nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
  );
  const tasks = useResource(`overview:tasks:${session.viewEpoch}`, signal =>
    tasksApi.list({ limit: 8 }, signal).then(result => result.data),
  );
  const audit = useResource(`overview:audit:${session.viewEpoch}`, signal =>
    auditApi.events({ limit: 8 }, signal).then(result => result.data),
  );

  const nodeItems: readonly NodeSummary[] = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const onlineCount = nodeItems.filter(node => node.online).length;
  const offlineCount = nodeItems.length - onlineCount;
  const taskItems: readonly TaskSummary[] = tasks.state.status === "ready" ? tasks.state.data.items : [];
  const runningTasks = taskItems.filter(item =>
    ["queued", "awaiting_approval", "waiting_node", "running", "paused", "canceling"].includes(item.state),
  ).length;

  const onlineRatio = nodeItems.length > 0 ? Math.round((onlineCount / nodeItems.length) * 100) : 0;

  // 真实在线节点瞬时负载（仅包含在线且服务端返回有限数值的节点，不编造离线或历史趋势）
  const telemetryChartData = useMemo(() => {
    return nodeItems
      .map(node => {
        const cpu = adaptMetric(node.cpuPercent, node.online);
        const memory = adaptMetric(node.memoryPercent, node.online);
        return {
          name: (node.name.trim() || node.id).slice(0, 12),
          cpu: cpu.kind === "value" ? Math.round(cpu.percent) : null,
          memory: memory.kind === "value" ? Math.round(memory.percent) : null,
        };
      })
      .filter(node => node.cpu !== null || node.memory !== null)
      .slice(0, 8);
  }, [nodeItems]);

  const regionDistribution = useMemo(() => {
    const counts = new Map<string, number>();
    for (const node of nodeItems) {
      const key = [node.provider, node.region].filter(Boolean).join(" · ") || "未标注区域";
      counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    return [...counts.entries()].slice(0, 5);
  }, [nodeItems]);

  function reloadAll() {
    nodes.reload();
    tasks.reload();
    audit.reload();
  }

  return (
    <div className="space-y-6">
      <div className="relative overflow-hidden rounded-2xl">
        <Spotlight className="-top-24 left-10" />
        <PageHeader
          title="概览仪表盘"
          description="集中查看已授权节点、最近批量任务与审计事件。所有数字来自 Controller 当前响应，不在浏览器推断趋势。"
          badge={
            <Badge variant="outline" className="border-[#6658E8]/30 bg-[#F0EEFF] text-[#6658E8]">
              NEUROX Telemetry v2.0
            </Badge>
          }
          actions={
            <div className="flex flex-wrap items-center gap-2">
              <LinkButton
                href="#/ai"
                className="border-[#6658E8]/30 bg-[#F0EEFF]/80 text-xs font-semibold text-[#6658E8] hover:bg-[#6658E8] hover:text-white"
              >
                <Bot className="size-3.5" aria-hidden="true" />
                AI 运维工作台
              </LinkButton>
              <Button type="button" variant="primary" onClick={reloadAll}>
                <RefreshCw className="size-3.5" aria-hidden="true" />
                刷新全部
              </Button>
            </div>
          }
        />
      </div>

      {/* KPI Cards Grid */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="节点总数"
          numericValue={nodes.state.status === "ready" ? nodeItems.length : null}
          hint="当前返回的节点总数"
          icon={<Server className="size-5" aria-hidden="true" />}
          tone="primary"
        />
        <StatCard
          label="在线节点"
          numericValue={nodes.state.status === "ready" ? onlineCount : null}
          hint="当前在线的节点数量"
          icon={<Wifi className="size-5" aria-hidden="true" />}
          tone="success"
        />
        <StatCard
          label="离线节点"
          numericValue={nodes.state.status === "ready" ? offlineCount : null}
          hint="离线节点的旧指标不作为当前值"
          icon={<WifiOff className="size-5" aria-hidden="true" />}
          tone="warning"
        />
        <StatCard
          label="活动任务"
          numericValue={tasks.state.status === "ready" ? runningTasks : null}
          hint="最近 8 条任务中未完成的数量"
          icon={<Activity className="size-5" aria-hidden="true" />}
          tone="info"
        />
      </div>

      {/* NEUROX Visual Analytics & Quick Action Row */}
      <div className="grid gap-5 lg:grid-cols-3">
        {/* Card 1: Node Sync & Regional Topology */}
        <Panel
          title="集群在线率与区域分布"
          description="基于节点实时快照统计"
        >
          <div className="flex flex-col justify-between gap-4 px-5 py-4">
            <div className="flex items-center gap-5">
              <div className="relative flex size-24 shrink-0 items-center justify-center">
                <svg className="size-full -rotate-90" viewBox="0 0 100 100">
                  <circle
                    cx="50"
                    cy="50"
                    r="40"
                    fill="none"
                    stroke="#EEF0F7"
                    strokeWidth="10"
                  />
                  <motion.circle
                    cx="50"
                    cy="50"
                    r="40"
                    fill="none"
                    stroke="url(#neurox-donut-grad)"
                    strokeWidth="10"
                    strokeLinecap="round"
                    strokeDasharray={251.2}
                    initial={{ strokeDashoffset: 251.2 }}
                    animate={{
                      strokeDashoffset: 251.2 - (251.2 * onlineRatio) / 100,
                    }}
                    transition={{ duration: 0.7, ease: "easeOut" }}
                  />
                  <defs>
                    <linearGradient id="neurox-donut-grad" x1="0%" y1="0%" x2="100%" y2="100%">
                      <stop offset="0%" stopColor="#6658E8" />
                      <stop offset="100%" stopColor="#169B68" />
                    </linearGradient>
                  </defs>
                </svg>
                <div className="absolute inset-0 flex flex-col items-center justify-center text-center">
                  <span className="text-lg font-bold tabular-nums text-foreground">
                    {nodes.state.status === "ready" ? `${onlineRatio}%` : "—"}
                  </span>
                  <span className="text-[10px] text-muted-foreground">在线率</span>
                </div>
              </div>

              <div className="min-w-0 flex-1 space-y-2 text-xs">
                <div className="flex items-center justify-between rounded-lg bg-[#ECFDF5]/80 px-2.5 py-1.5">
                  <span className="flex items-center gap-1.5 font-medium text-[#169B68]">
                    <span className="size-2 rounded-full bg-[#169B68]" />
                    在线受控节点
                  </span>
                  <span className="font-mono font-bold text-[#169B68]">{onlineCount}</span>
                </div>
                <div className="flex items-center justify-between rounded-lg bg-[#FFFBEB]/80 px-2.5 py-1.5">
                  <span className="flex items-center gap-1.5 font-medium text-[#D89B2B]">
                    <span className="size-2 rounded-full bg-[#D89B2B]" />
                    离线或待重连
                  </span>
                  <span className="font-mono font-bold text-[#D89B2B]">{offlineCount}</span>
                </div>
              </div>
            </div>

            <div className="space-y-1.5 border-t border-border/80 pt-3">
              <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                云厂商 / 区域分布
              </p>
              {regionDistribution.length === 0 ? (
                <p className="text-xs text-muted-foreground">暂无节点区域数据</p>
              ) : (
                <div className="space-y-1.5">
                  {regionDistribution.map(([region, count]) => {
                    const pct = nodeItems.length > 0 ? Math.round((count / nodeItems.length) * 100) : 0;
                    return (
                      <div key={region} className="space-y-1 text-xs">
                        <div className="flex justify-between">
                          <span className="truncate text-foreground">{region}</span>
                          <span className="font-mono text-muted-foreground">
                            {count} 台 ({pct}%)
                          </span>
                        </div>
                        <div className="h-1.5 w-full overflow-hidden rounded-full bg-secondary">
                          <div
                            className="h-full rounded-full bg-gradient-to-r from-[#6658E8] to-[#3984E7]"
                            style={{ width: `${pct}%` }}
                          />
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          </div>
        </Panel>

        {/* Card 2: Real-time Node CPU / Memory Snapshot Bar Chart */}
        <Panel
          title="在线节点资源快照"
          description="仅展示在线节点当前上报的 CPU / 内存瞬时百分比（不含历史推演）"
        >
          <div className="px-5 py-4">
            {telemetryChartData.length === 0 ? (
              <div className="flex h-48 flex-col items-center justify-center rounded-xl border border-dashed border-border bg-[#F6F7FB]/60 p-4 text-center">
                <Cpu className="mb-2 size-6 text-[#6658E8]/60" aria-hidden="true" />
                <p className="text-xs font-medium text-foreground">暂无在线节点瞬时指标</p>
                <p className="mt-1 text-[11px] text-muted-foreground">
                  当在线节点返回有效 cpuPercent / memoryPercent 时自动渲染对比图。
                </p>
              </div>
            ) : (
              <div className="space-y-3">
                <div className="h-36 w-full" aria-label="在线节点资源柱状图">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart data={telemetryChartData} margin={{ top: 4, right: 8, left: -20, bottom: 0 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#E8EAF1" vertical={false} />
                      <XAxis dataKey="name" tick={{ fontSize: 11, fill: "#737B8C" }} axisLine={false} tickLine={false} />
                      <YAxis domain={[0, 100]} tick={{ fontSize: 10, fill: "#737B8C" }} axisLine={false} tickLine={false} />
                      <RechartsTooltip
                        contentStyle={{
                          borderRadius: "12px",
                          border: "1px solid #E8EAF1",
                          backgroundColor: "#FFFFFF",
                          fontSize: "12px",
                        }}
                      />
                      <Bar dataKey="cpu" name="CPU %" fill="#6658E8" radius={[4, 4, 0, 0]} maxBarSize={22} />
                      <Bar dataKey="memory" name="内存 %" fill="#3984E7" radius={[4, 4, 0, 0]} maxBarSize={22} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
                <div className="flex items-center justify-between border-t border-border/80 pt-2 text-[11px] text-muted-foreground">
                  <span className="flex items-center gap-3">
                    <span className="inline-flex items-center gap-1">
                      <span className="size-2 rounded-xs bg-[#6658E8]" /> CPU 负载
                    </span>
                    <span className="inline-flex items-center gap-1">
                      <span className="size-2 rounded-xs bg-[#3984E7]" /> 内存占用
                    </span>
                  </span>
                  <span>无采样时间戳 · 瞬时读数</span>
                </div>
              </div>
            )}
          </div>
        </Panel>

        {/* Card 3: Quick Operations & Control Matrix */}
        <Panel
          title="控制面快捷矩阵"
          description="高频运维工作流直达入口"
        >
          <div className="grid grid-cols-2 gap-2.5 px-5 py-4">
            {[
              {
                href: "#/nodes/new",
                label: "注册新节点",
                sub: "签发一次性引导令牌",
                icon: <PlusCircle className="size-4 text-[#6658E8]" />,
              },
              {
                href: "#/tasks",
                label: "批量任务中心",
                sub: "命令执行与审批流",
                icon: <ListChecks className="size-4 text-[#3984E7]" />,
              },
              {
                href: "#/ai",
                label: "AI 运维空间",
                sub: "受控工具调用与审计",
                icon: <Sparkles className="size-4 text-[#6658E8]" />,
              },
              {
                href: "#/gateways",
                label: "网关与拓扑",
                sub: "流量转发与健康状态",
                icon: <Network className="size-4 text-[#169B68]" />,
              },
              {
                href: "#/certificates",
                label: "PKI 证书管理",
                sub: "CA 链与节点凭据",
                icon: <ShieldCheck className="size-4 text-[#169B68]" />,
              },
              {
                href: "#/audit",
                label: "安全审计事件",
                sub: "全链路脱敏追溯",
                icon: <FileCode2 className="size-4 text-[#D89B2B]" />,
              },
            ].map(item => (
              <a
                key={item.href}
                href={item.href}
                className="group flex flex-col justify-between rounded-xl border border-border bg-[#F6F7FB]/70 p-3 transition-all duration-150 hover:-translate-y-0.5 hover:border-[#6658E8]/35 hover:bg-card hover:shadow-xs"
              >
                <div className="flex items-center justify-between">
                  <span className="flex size-7 items-center justify-center rounded-lg bg-card shadow-2xs">
                    {item.icon}
                  </span>
                  <ArrowUpRight className="size-3.5 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:-translate-y-0.5 group-hover:text-[#6658E8]" />
                </div>
                <div className="mt-2.5">
                  <p className="text-xs font-semibold text-foreground group-hover:text-[#6658E8]">
                    {item.label}
                  </p>
                  <p className="mt-0.5 truncate text-[11px] text-muted-foreground">{item.sub}</p>
                </div>
              </a>
            ))}
          </div>
        </Panel>
      </div>

      {/* Recent Tasks & Recent Audit Events */}
      <div className="grid gap-5 xl:grid-cols-2">
        <Panel
          title="最近批量任务"
          description="最近 8 条任务；已受理不代表执行成功。"
          actions={
            <a className="text-xs font-semibold text-primary hover:underline" href="#/tasks">
              查看全部任务 →
            </a>
          }
        >
          <AsyncPanel
            state={tasks.state}
            onRetry={tasks.reload}
            errorTitle="任务列表加载失败"
            empty={<EmptyState title="暂无任务记录" description="服务端尚未返回批量任务。" />}
            isEmpty={data => data.items.length === 0}
          >
            {data => (
              <DataTable
                caption="最近批量任务"
                rows={data.items}
                rowKey={item => item.taskId}
                columns={[
                  {
                    key: "id",
                    header: "任务",
                    render: item => (
                      <a
                        className="font-mono text-xs font-semibold text-primary hover:underline"
                        href={`#/tasks/${encodeURIComponent(item.taskId)}`}
                      >
                        {item.taskId}
                      </a>
                    ),
                  },
                  { key: "targetType", header: "范围", render: item => item.targetType ?? "node_ids" },
                  {
                    key: "state",
                    header: "状态",
                    render: item => (
                      <Badge variant={getTaskBadgeVariant(item.state)}>
                        {TASK_STATE_LABEL[item.state] ?? item.state}
                      </Badge>
                    ),
                  },
                  {
                    key: "targets",
                    header: "目标快照",
                    align: "right",
                    render: item => `${item.targetNodeIdsSnapshot.length} 台`,
                  },
                  {
                    key: "createdAt",
                    header: "创建时间",
                    render: item => formatDateTime(item.createdAt) ?? "—",
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>

        <Panel
          title="最近审计事件"
          description="最近 8 条审计事件。"
          actions={
            <a className="text-xs font-semibold text-primary hover:underline" href="#/audit">
              打开审计日志 →
            </a>
          }
        >
          <AsyncPanel
            state={audit.state}
            onRetry={audit.reload}
            errorTitle="审计事件加载失败"
            empty={<EmptyState title="暂无审计事件" description="当前查询未返回审计记录。" />}
            isEmpty={data => data.items.length === 0}
          >
            {data => (
              <DataTable<AuditEvent>
                caption="最近审计事件"
                rows={data.items}
                rowKey={item => item.eventId}
                columns={[
                  {
                    key: "time",
                    header: "时间",
                    render: item => formatDateTime(item.occurredAt) ?? "—",
                  },
                  {
                    key: "actor",
                    header: "主体",
                    render: item =>
                      `${item.actorType}${item.actorId ? ` · ${item.actorId}` : ""}`,
                  },
                  {
                    key: "action",
                    header: "动作",
                    render: item => <span className="font-mono text-xs">{item.action}</span>,
                  },
                  {
                    key: "result",
                    header: "结果",
                    render: item => (
                      <Badge
                        variant={
                          item.result === "success" || item.result === "allowed"
                            ? "success"
                            : item.result === "denied" || item.result === "failure"
                              ? "destructive"
                              : "outline"
                        }
                      >
                        {item.result}
                      </Badge>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      </div>

      {/* Authorized Nodes Table */}
      <Panel
        title="当前授权节点"
        description="点击节点进入详情，或前往节点管理筛选。"
        actions={
          <div className="flex flex-wrap gap-2">
            <a className="text-xs font-semibold text-primary hover:underline" href="#/nodes">
              打开节点管理
            </a>
            <a className="text-xs font-semibold text-primary hover:underline" href="#/nodes/new">
              添加节点
            </a>
          </div>
        }
      >
        <AsyncPanel
          state={nodes.state}
          onRetry={nodes.reload}
          errorTitle="节点列表加载失败"
          empty={
            <EmptyState
              title="没有可见节点"
              description="当前账号尚未获授任何节点，或尚未完成节点注册。"
            />
          }
          isEmpty={data => data.items.length === 0}
        >
          {data => (
            <DataTable
              caption="概览节点列表"
              rows={data.items.slice(0, 10)}
              rowKey={node => node.id}
              columns={[
                {
                  key: "name",
                  header: "名称",
                  render: node => (
                    <a
                      className="font-semibold text-primary hover:underline"
                      href={`#/nodes/${encodeURIComponent(node.id)}`}
                    >
                      {node.name.trim() || "未命名节点"}
                    </a>
                  ),
                },
                {
                  key: "id",
                  header: "ID",
                  render: node => (
                    <span className="font-mono text-xs text-muted-foreground">{node.id}</span>
                  ),
                },
                {
                  key: "status",
                  header: "状态",
                  render: node => (
                    <Badge tone={node.online ? "success" : "neutral"} dot>
                      {node.online ? "在线" : "离线"}
                    </Badge>
                  ),
                },
                {
                  key: "region",
                  header: "区域 / 云厂商",
                  render: node =>
                    [node.provider, node.region].filter(Boolean).join(" · ") || "—",
                },
                {
                  key: "cpu",
                  header: "CPU",
                  align: "right",
                  render: node => (
                    <LegacyMetricCell metric={adaptMetric(node.cpuPercent, node.online)} />
                  ),
                },
                {
                  key: "memory",
                  header: "内存",
                  align: "right",
                  render: node => (
                    <LegacyMetricCell metric={adaptMetric(node.memoryPercent, node.online)} />
                  ),
                },
                {
                  key: "ip",
                  header: "IP / 标签",
                  render: node => node.ip ?? (node.tags?.length ? node.tags.join(", ") : "—"),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>
    </div>
  );
}
