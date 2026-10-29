import type { ReactNode } from "react";
import { useEffect } from "react";
import { matchPath, useLocation } from "@/app/router";
import { ROUTE_META } from "@/app/navigation";
import { useSession } from "@/app/session";
import { Shell } from "@/app/shell";
import { SessionChecking, SessionUnavailable } from "@/app/session-screens";
import { BootstrapPage, LoginPage } from "@/features/auth/auth-pages";
import { OverviewPage } from "@/features/overview/overview-page";
import { NodesPage } from "@/features/nodes/nodes-page";
import { NodeAddPage } from "@/features/nodes/node-add-page";
import { NodeDetailPage } from "@/features/nodes/node-detail-page";
import { GroupsPage } from "@/features/groups/groups-page";
import { ServiceGroupDetailPage, ServiceGroupsPage } from "@/features/service-groups/service-groups-page";
import { UsersPage } from "@/features/users/users-page";
import { AuditPage } from "@/features/audit/audit-page";
import { SettingsPage } from "@/features/settings/settings-page";
import { NotFoundPage, PendingPage } from "@/features/pending/pending-page";

export interface RouteContext {
  readonly params: Readonly<Record<string, string>>;
  readonly search: URLSearchParams;
}

interface RouteEntry {
  readonly path: string;
  readonly render: (context: RouteContext) => ReactNode;
}

function pending(title: string, description: string) {
  return () => <PendingPage title={title} description={description} />;
}

export const ROUTES: readonly RouteEntry[] = [
  { path: "/overview", render: () => <OverviewPage /> },
  { path: "/nodes", render: () => <NodesPage /> },
  { path: "/nodes/new", render: () => <NodeAddPage /> },
  { path: "/nodes/:nodeId", render: ({ params }) => <NodeDetailPage nodeId={params.nodeId} /> },
  { path: "/groups", render: () => <GroupsPage /> },
  { path: "/files", render: pending("文件互传", "双栏目录浏览、上传下载与跨节点复制任务。") },
  { path: "/tasks", render: pending("批量任务", "冻结目标快照、分批/金丝雀、审批与进度。") },
  { path: "/tasks/:taskId", render: pending("任务详情", "任务状态、逐节点执行、SSE 进度与审批。") },
  { path: "/monitoring", render: pending("监控告警", "资源概况、HTTP/TCP/TLS 可用性与告警事件。") },
  { path: "/ai", render: pending("AI 运维", "会话资源授权、工具审批与流式消息。") },
  { path: "/ai/:sessionId", render: pending("AI 会话", "资源授权、审批模式、消息与工具事件。") },
  { path: "/gateways", render: pending("网关管理", "Nginx 应用网关安装、检查、维护与路由。") },
  { path: "/gateways/:gatewayId", render: pending("网关详情", "安装状态、健康检查与调度资格。") },
  { path: "/publishing", render: pending("服务发布", "服务组到网关、DNS、证书的一体化发布。") },
  { path: "/service-groups", render: () => <ServiceGroupsPage /> },
  { path: "/service-groups/:serviceGroupId", render: ({ params }) => <ServiceGroupDetailPage serviceGroupId={params.serviceGroupId} /> },
  { path: "/workflows", render: pending("自动化工作流", "触发器、条件、操作、人工审批与通知。") },
  { path: "/plugins", render: pending("插件中心", "来源仓库、目录、安装启停升级与卸载。") },
  { path: "/plugins/xray", render: pending("Xray 节点管理器", "声明式插件界面；仅安装并启用后可操作。") },
  { path: "/dns", render: pending("DNS 管理", "多账号、Zone 与解析记录。") },
  { path: "/certificates", render: pending("证书管理", "公网证书与内部 TLS 下发。") },
  { path: "/updates", render: pending("更新中心", "主控与 Agent 更新状态、失败恢复。") },
  { path: "/backups", render: pending("备份与恢复", "备份校验与主控灾难恢复。") },
  { path: "/users", render: () => <UsersPage /> },
  { path: "/audit", render: () => <AuditPage /> },
  { path: "/settings", render: () => <SettingsPage /> },
];

function routeTitle(path: string): string {
  if (ROUTE_META[path]) return ROUTE_META[path].title;
  for (const [pattern, meta] of Object.entries(ROUTE_META)) {
    if (pattern.includes(":") && matchPath(pattern, path)) return meta.title;
  }
  return "页面不存在";
}

export function RouterContent() {
  const location = useLocation();
  const route = ROUTES.find(entry => matchPath(entry.path, location.path) !== null) ?? null;
  const match = route ? matchPath(route.path, location.path) : null;
  const title = routeTitle(location.path);

  useEffect(() => {
    document.title = `${title} · Server Fleet`;
    const frame = requestAnimationFrame(() => {
      const heading = document.getElementById("page-title");
      if (heading instanceof HTMLElement) heading.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, [title, location.path]);

  if (!route || !match) return <NotFoundPage path={location.path} />;
  return <>{route.render({ params: match.params, search: new URLSearchParams(location.search) })}</>;
}

export function AppFrame() {
  const session = useSession();
  const location = useLocation();
  const next = `${location.path}${location.search}`;

  useEffect(() => {
    if (session.state.kind === "authenticated" && location.path === "/setup") {
      window.history.replaceState(null, "", "#/overview");
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    }
  }, [session.state.kind, location.path]);

  if (session.state.kind === "checking") return <SessionChecking />;
  if (session.state.kind === "unavailable") return <SessionUnavailable />;
  if (session.state.kind === "bootstrap") return <BootstrapPage available />;
  if (session.state.kind === "anonymous") {
    if (location.path === "/setup") return <BootstrapPage available={false} />;
    return <LoginPage next={next === "/login" ? null : next} />;
  }
  if (location.path === "/login") return <LoginPage next={null} />;
  if (location.path === "/setup") return <SessionChecking />;
  return (
    <Shell>
      <RouterContent />
    </Shell>
  );
}
