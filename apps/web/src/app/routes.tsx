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
import { TasksPage } from "@/features/tasks/tasks-page";
import { TaskDetailPage } from "@/features/tasks/task-detail-page";
import { FilesPage } from "@/features/files/files-page";
import { MonitoringPage } from "@/features/monitoring/monitoring-page";
import { DnsPage } from "@/features/dns/dns-page";
import { GatewayDetailPage, GatewaysPage } from "@/features/gateways/gateways-page";
import { CertificatesPage } from "@/features/certificates/certificates-page";
import { PublishingPage } from "@/features/publishing/publishing-page";
import { AIPage, AISessionPage } from "@/features/ai/ai-pages";
import { PluginsPage } from "@/features/plugins/plugins-page";
import { XrayPage } from "@/features/plugins/xray-page";
import { WorkflowsPage } from "@/features/workflows/workflows-page";
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
  { path: "/files", render: () => <FilesPage /> },
  { path: "/tasks", render: () => <TasksPage /> },
  { path: "/tasks/:taskId", render: ({ params }) => <TaskDetailPage taskId={params.taskId} /> },
  { path: "/monitoring", render: () => <MonitoringPage /> },
  { path: "/ai", render: () => <AIPage /> },
  { path: "/ai/:sessionId", render: ({ params }) => <AISessionPage sessionId={params.sessionId} /> },
  { path: "/gateways", render: () => <GatewaysPage /> },
  { path: "/gateways/:gatewayId", render: ({ params }) => <GatewayDetailPage gatewayId={params.gatewayId} /> },
  { path: "/publishing", render: () => <PublishingPage /> },
  { path: "/service-groups", render: () => <ServiceGroupsPage /> },
  { path: "/service-groups/:serviceGroupId", render: ({ params }) => <ServiceGroupDetailPage serviceGroupId={params.serviceGroupId} /> },
  { path: "/workflows", render: () => <WorkflowsPage /> },
  { path: "/plugins", render: () => <PluginsPage /> },
  { path: "/plugins/xray", render: () => <XrayPage /> },
  { path: "/dns", render: () => <DnsPage /> },
  { path: "/certificates", render: () => <CertificatesPage /> },
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
