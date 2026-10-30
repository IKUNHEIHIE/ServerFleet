import { useSession } from "@/app/session";
import { navigate } from "@/app/router";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { auditApi, nodesApi, systemApi, tasksApi } from "@/lib/api/endpoints";
import type { AuditEvent, NodeSummary, TaskSummary } from "@/lib/api/dto";
import { useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const TASK_STATE_TEXT: Record<TaskSummary["state"], string> = {
  awaiting_approval: "等待审批",
  queued: "排队中",
  waiting_node: "等待节点",
  running: "执行中",
  paused: "已暂停",
  completed: "已完成",
  partially_failed: "部分失败",
  failed: "失败",
  canceling: "取消中",
  canceled: "已取消",
  unknown: "未知",
  expired: "已过期",
};

function StatCard({
  label,
  value,
  hint,
}: {
  readonly label: string;
  readonly value: string;
  readonly hint: string;
}) {
  return (
    <div className="rounded-xl border bg-card px-5 py-4">
      <p className="text-xs font-medium text-muted-foreground">{label}</p>
      <p className="mt-2 text-2xl font-semibold tabular-nums">{value}</p>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</p>
    </div>
  );
}

export function OverviewPage() {
  const session = useSession();
  const key = session.viewEpoch;
  const health = useResource(`overview:health:${key}`, () => systemApi.health().then(result => result.data));
  const nodes = useResource(`overview:nodes:${key}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const tasks = useResource(`overview:tasks:${key}`, signal =>
    tasksApi.list({ limit: 5 }, signal).then(result => result.data),
  );
  const audit = useResource(`overview:audit:${key}`, signal =>
    auditApi.events({ limit: 5 }, signal).then(result => result.data),
  );

  const nodeCount =
    nodes.state.status === "ready" ? nodes.state.data.items.length : null;
  const onlineCount =
    nodes.state.status === "ready" ? nodes.state.data.items.filter(node => node.online).length : null;

  return (
    <div className="space-y-6">
      <PageHeader
        title="概览仪表盘"
        description="节点在线状态、资源概况、近期任务与告警。每个卡片独立读取真实接口；失败项单独显示，不以演示数据补齐。"
        actions={
          <>
            <Button type="button" onClick={() => { nodes.reload(); tasks.reload(); audit.reload(); health.reload(); }}>
              全部刷新
            </Button>
            <Button type="button" variant="primary" onClick={() => navigate("/nodes")}>
              查看节点列表
            </Button>
          </>
        }
      />

      {health.state.status === "ready" && health.state.data.status === "scaffold" ? (
        <Notice tone="warning" title="Controller 仍为工程骨架">
          健康接口返回 <code>{health.state.data.status}</code>（{health.state.data.stage ?? "未标注阶段"}）。除健康检查外的管理 API
          可能尚未提供，页面会按实际 HTTP 结果显示不可用，不会伪造成功。
        </Notice>
      ) : null}
      {health.state.status === "error" ? (
        <Notice tone="error" title="健康检查失败">
          无法读取 <code>/healthz</code>：{health.state.error.code ?? health.state.error.kind}。管理台仍会尝试其他只读接口。
        </Notice>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="已授权节点"
          value={nodeCount === null ? "—" : String(nodeCount)}
          hint={
            nodes.state.status === "ready"
              ? `在线 ${onlineCount} / 共 ${nodeCount}`
              : nodes.state.status === "error"
                ? "节点接口不可用"
                : "正在读取节点列表"
          }
        />
        <StatCard
          label="近期任务（本页）"
          value={tasks.state.status === "ready" ? String(tasks.state.data.items.length) : "—"}
          hint={tasks.state.status === "ready" ? "仅统计本页返回条目，不代表全局总数" : "任务接口不可用或正在加载"}
        />
        <StatCard
          label="近期审计（本页）"
          value={audit.state.status === "ready" ? String(audit.state.data.items.length) : "—"}
          hint={audit.state.status === "ready" ? "脱敏摘要；完整审计请进入审计日志" : "审计接口不可用或正在加载"}
        />
        <StatCard
          label="身份权限"
          value={session.identity?.isBootstrapAdmin ? "首位管理员" : String(session.identity?.permissions?.length ?? "未返回")}
          hint={
            session.identity?.permissions
              ? "以服务端返回的权限目录为准；写操作仍需服务端校验"
              : "Identity 未返回 permissions；界面不按角色自行推断权限"
          }
        />
      </div>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="近期任务" description="统一任务中心最近记录。" actions={<Button size="sm" onClick={() => navigate("/tasks")}>任务中心</Button>}>
          <AsyncPanel
            state={tasks.state}
            onRetry={tasks.reload}
            empty={<p className="px-6 py-8 text-sm text-muted-foreground">当前没有可见任务。</p>}
            isEmpty={data => data.items.length === 0}
          >
            {data => (
              <DataTable
                caption="近期任务"
                rows={data.items}
                rowKey={task => task.taskId}
                columns={[
                  { key: "id", header: "任务", render: task => <span className="font-mono text-xs">{task.taskId}</span> },
                  { key: "state", header: "状态", render: task => TASK_STATE_TEXT[task.state] },
                  { key: "targets", header: "目标", align: "right", render: task => `${task.targetNodeIdsSnapshot.length} 台` },
                  { key: "created", header: "创建时间", render: task => formatDateTime(task.createdAt) ?? "—" },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>

        <Panel title="近期审计" description="只读脱敏事件；不以浏览器日志代替服务端审计。" actions={<Button size="sm" onClick={() => navigate("/audit")}>审计日志</Button>}>
          <AsyncPanel
            state={audit.state}
            onRetry={audit.reload}
            empty={<p className="px-6 py-8 text-sm text-muted-foreground">当前没有可见审计事件。</p>}
            isEmpty={data => data.items.length === 0}
          >
            {data => (
              <DataTable
                caption="近期审计事件"
                rows={data.items}
                rowKey={(event: AuditEvent) => event.eventId}
                columns={[
                  { key: "time", header: "时间", render: event => formatDateTime(event.occurredAt) ?? "—" },
                  { key: "action", header: "动作", render: event => event.action },
                  { key: "actor", header: "主体", render: event => `${event.actorType}${event.actorId ? ` · ${event.actorId}` : ""}` },
                  { key: "result", header: "结果", render: event => event.result },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      </div>

      <Panel title="已授权节点" description="来自 GET /api/v1/nodes；仅显示当前身份可见范围。" actions={<Button size="sm" onClick={() => nodes.reload()}>刷新</Button>}>
        <AsyncPanel
          state={nodes.state}
          onRetry={nodes.reload}
          empty={<p className="px-6 py-8 text-sm text-muted-foreground">没有可见节点。</p>}
          isEmpty={data => data.items.length === 0}
        >
          {data => (
            <DataTable
              caption="已授权节点"
              rows={data.items}
              rowKey={(node: NodeSummary) => node.id}
              columns={[
                { key: "name", header: "节点", render: node => node.name },
                { key: "id", header: "ID", render: node => <span className="font-mono text-xs">{node.id}</span> },
                { key: "online", header: "状态", render: node => (node.online ? "在线" : "离线") },
                { key: "region", header: "地区", render: node => node.region ?? "—" },
                { key: "provider", header: "云厂商", render: node => node.provider ?? "—" },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>
    </div>
  );
}
