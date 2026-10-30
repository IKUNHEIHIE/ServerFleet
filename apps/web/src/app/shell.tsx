import { useState, type ReactNode } from "react";
import { useLocation } from "@/app/router";
import { NAV_GROUPS, OVERVIEW, ROUTE_META, findNavItem } from "@/app/navigation";
import { useSession } from "@/app/session";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Modal } from "@/components/ui/modal";
import { Notice } from "@/components/ui/feedback";
import { describeApiError } from "@/lib/api/errors";
import { formatDateTime } from "@/lib/format";
import { pluginsApi } from "@/lib/api/endpoints-ai";
import { useResource } from "@/lib/data/use-resource";

function isActive(path: string, target: string): boolean {
  if (path === target) return true;
  if (target === "/nodes") return path.startsWith("/nodes/") && path !== "/nodes/new";
  if (target === "/tasks") return path.startsWith("/tasks/");
  if (target === "/ai") return path.startsWith("/ai/");
  if (target === "/service-groups") return path.startsWith("/service-groups/");
  if (target === "/gateways") return path.startsWith("/gateways/");
  return false;
}

function NavLink({
  path,
  label,
  current,
  onNavigate,
}: {
  readonly path: string;
  readonly label: string;
  readonly current: string;
  readonly onNavigate?: () => void;
}) {
  const active = isActive(current, path);
  return (
    <li>
      <a
        href={`#${path}`}
        aria-current={active ? "page" : undefined}
        onClick={onNavigate}
        className={cn(
          "flex items-center rounded-md px-3 py-2 text-sm transition-colors",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          active ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-secondary hover:text-foreground",
        )}
      >
        {label}
      </a>
    </li>
  );
}

export function NavList({ onNavigate, pluginLinks = [] }: { readonly onNavigate?: () => void; readonly pluginLinks?: readonly { readonly path: string; readonly label: string }[] }) {
  const location = useLocation();
  const [nodesOpen, setNodesOpen] = useState(true);
  return (
    <nav aria-label="主导航" className="flex flex-col gap-1">
      <ul className="space-y-0.5">
        <NavLink path={OVERVIEW.path} label={OVERVIEW.label} current={location.path} onNavigate={onNavigate} />
      </ul>
      {NAV_GROUPS.map(group => {
        const collapsed = group.collapsible === true && !nodesOpen;
        return (
          <div key={group.id} className="pt-3">
            {group.collapsible ? (
              <button
                type="button"
                aria-expanded={nodesOpen}
                aria-controls="nav-group-nodes"
                onClick={() => setNodesOpen(value => !value)}
                className="flex w-full items-center justify-between rounded-md px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                {group.label}
                <span aria-hidden="true" className="text-[10px]">
                  {nodesOpen ? "−" : "+"}
                </span>
              </button>
            ) : (
              <p className="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{group.label}</p>
            )}
            <ul
              id={group.collapsible ? "nav-group-nodes" : undefined}
              hidden={collapsed}
              className="mt-0.5 space-y-0.5"
            >
              {group.items.map(item => (
                <NavLink key={item.path} path={item.path} label={item.label} current={location.path} onNavigate={onNavigate} />
              ))}
            </ul>
          </div>
        );
      })}
      {pluginLinks.length > 0 ? (
        <div className="pt-3">
          <p className="px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-muted-foreground">已启用插件</p>
          <ul className="mt-0.5 space-y-0.5">
            {pluginLinks.map(link => <NavLink key={link.path} path={link.path} label={link.label} current={location.path} onNavigate={onNavigate} />)}
          </ul>
        </div>
      ) : null}
      <div className="pt-4 text-xs leading-5 text-muted-foreground">
        <p>可选插件入口仅在服务端确认已安装并启用后显示。</p>
      </div>
    </nav>
  );
}

function Sidebar({ pluginLinks }: { readonly pluginLinks: readonly { readonly path: string; readonly label: string }[] }) {
  return (
    <aside className="fixed inset-y-0 left-0 z-30 hidden w-64 flex-col border-r bg-card lg:flex">
      <div className="border-b px-5 py-4">
        <a href="#/overview" className="flex items-center gap-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <span aria-hidden="true" className="grid size-9 place-items-center rounded-md bg-primary text-sm font-bold text-primary-foreground">
            SF
          </span>
          <span>
            <span className="block font-semibold leading-5">Server Fleet</span>
            <span className="block text-xs text-muted-foreground">v0.9 管理台</span>
          </span>
        </a>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
        <NavList pluginLinks={pluginLinks} />
      </div>
      <p className="border-t px-5 py-3 text-xs text-muted-foreground">Web 仅调用 Controller REST/SSE</p>
    </aside>
  );
}

export function Shell({ children }: { readonly children: ReactNode }) {
  const session = useSession();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [logoutError, setLogoutError] = useState<string | null>(null);
  const [loggingOut, setLoggingOut] = useState(false);
  const routeMeta = ROUTE_META[location.path];
  const navItem = findNavItem(location.path);
  const plugins = useResource(`shell:plugins:${session.viewEpoch}:${session.pluginEpoch}`, signal =>
    pluginsApi.installed(signal).then(result => result.data),
  );
  const pluginLinks =
    plugins.state.status === "ready"
      ? plugins.state.data.items
          .filter(plugin => plugin.id === "xray-manager" && plugin.enabled && plugin.status === "enabled")
          .map(() => ({ path: "/plugins/xray", label: "Xray 节点管理器" }))
      : [];
  const notice =
    session.state.kind === "authenticated" || session.state.kind === "anonymous" ? session.state.notice : null;
  const warning = session.state.kind === "authenticated" ? session.state.warning : null;

  async function handleLogout() {
    setLoggingOut(true);
    const result = await session.logout();
    setLoggingOut(false);
    if (!result.ok && result.error) setLogoutError(describeApiError(result.error));
  }

  return (
    <div className="min-h-screen bg-background">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:z-50 focus:rounded-md focus:bg-card focus:px-4 focus:py-2 focus:text-sm focus:shadow"
      >
        跳到主要内容
      </a>
      <Sidebar pluginLinks={pluginLinks} />
      <div className="lg:pl-64">
        <header className="sticky top-0 z-20 border-b bg-card/95 backdrop-blur">
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 lg:px-8">
            <div className="flex min-w-0 items-center gap-3">
              <button
                type="button"
                className="inline-flex h-9 items-center rounded-md border px-3 text-sm lg:hidden"
                aria-expanded={mobileOpen}
                onClick={() => setMobileOpen(true)}
              >
                菜单
              </button>
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{navItem ? navItem.label : routeMeta?.title ?? "Server Fleet"}</p>
                <p className="truncate text-xs text-muted-foreground">
                  {session.identity ? `${session.identity.displayName} · ${session.identity.roles.join("、") || "无角色"}` : ""}
                </p>
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <span className="hidden text-xs text-muted-foreground sm:inline">
                会话到期：
                {session.state.kind === "authenticated" && session.state.sessionExpiresAt
                  ? formatDateTime(session.state.sessionExpiresAt) ?? "时间无法解析"
                  : "刷新后未知"}
              </span>
              <Button type="button" size="sm" onClick={() => session.revalidate()}>
                同步身份
              </Button>
              <a
                href="#/login"
                className="inline-flex h-8 items-center rounded-md border border-input bg-card px-2.5 text-xs font-medium hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                重新登录
              </a>
              <Button type="button" size="sm" variant="outline" onClick={handleLogout} loading={loggingOut}>
                退出
              </Button>
            </div>
          </div>
        </header>
        <div className="mx-auto w-full max-w-[1600px] px-4 py-6 lg:px-8">
          <div className="space-y-3">
            {notice ? (
              <Notice tone="info" actions={<Button size="sm" onClick={session.clearNotice}>知道了</Button>}>
                {notice}
              </Notice>
            ) : null}
            {warning ? <Notice tone="warning" title="身份状态提醒">{warning}</Notice> : null}
            {logoutError ? (
              <Notice tone="error" title="退出未确认" actions={<Button size="sm" onClick={() => setLogoutError(null)}>关闭</Button>}>
                {logoutError}
              </Notice>
            ) : null}
            {session.writesLocked ? (
              <Notice
                tone="warning"
                title="安全写入已锁定"
                actions={
                  <a
                    href="#/login"
                    className="inline-flex h-8 items-center rounded-md border border-input bg-card px-2.5 text-xs font-medium hover:bg-secondary"
                  >
                    重新登录以获取 CSRF 令牌
                  </a>
                }
              >
                页面刷新后，服务端会话仍有效，但当前 Identity 契约未返回 CSRF 令牌。只读页面可用；创建、修改、审批、退出等写操作会保持禁用，直到重新登录。
              </Notice>
            ) : null}
          </div>
          <main id="main-content" tabIndex={-1} className="mt-5 focus:outline-none">
            {children}
          </main>
          <footer className="mt-10 border-t py-4 text-xs leading-5 text-muted-foreground">
            <p>Server Fleet · 完整 v0.9 管理台（GM）。所有管理操作经 Controller REST/SSE；不在浏览器保存会话令牌、凭据或生产秘密。</p>
            <p className="mt-1">G2 契约仍为候选；未实现或失败接口会明确显示，不以演示数据替代。</p>
          </footer>
        </div>
      </div>
      <Modal open={mobileOpen} title="导航" onClose={() => setMobileOpen(false)} side>
        <NavList pluginLinks={pluginLinks} onNavigate={() => setMobileOpen(false)} />
      </Modal>
    </div>
  );
}
