import { useEffect, useState, type ReactNode } from "react";
import {
  Bell,
  ChevronLeft,
  ChevronRight,
  Command,
  PanelLeft,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
} from "lucide-react";
import { motion } from "motion/react";
import { useLocation } from "@/app/router";
import { NAV_GROUPS, OVERVIEW, ROUTE_META, findNavItem } from "@/app/navigation";
import { useSession } from "@/app/session";
import { CommandPalette, getRouteIcon } from "./command-palette";
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
  collapsed = false,
  onNavigate,
}: {
  readonly path: string;
  readonly label: string;
  readonly current: string;
  readonly collapsed?: boolean;
  readonly onNavigate?: () => void;
}) {
  const active = isActive(current, path);
  return (
    <li>
      <a
        href={`#${path}`}
        aria-current={active ? "page" : undefined}
        title={collapsed ? label : undefined}
        onClick={onNavigate}
        className={cn(
          "group relative flex items-center gap-2.5 rounded-xl px-3 py-2 text-sm transition-all duration-150",
          "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          active
            ? "bg-[#F0EEFF] font-semibold text-[#6658E8] shadow-[inset_0_0_0_1px_rgba(102,88,232,0.22)]"
            : "text-muted-foreground hover:bg-[#F6F7FB] hover:text-foreground",
        )}
      >
        {active ? (
          <motion.span
            layoutId="neurox-sidebar-active"
            transition={{ type: "spring", stiffness: 380, damping: 30 }}
            className="pointer-events-none absolute top-1.5 bottom-1.5 left-0 w-1 rounded-r-full bg-[#6658E8]"
          />
        ) : null}
        <span
          className={cn(
            "shrink-0 transition-colors",
            active ? "text-[#6658E8]" : "text-muted-foreground group-hover:text-[#6658E8]",
          )}
        >
          {getRouteIcon(path)}
        </span>
        <span className={collapsed ? "sr-only" : "truncate"}>{label}</span>
      </a>
    </li>
  );
}

export function NavList({
  onNavigate,
  pluginLinks = [],
  collapsed = false,
}: {
  readonly onNavigate?: () => void;
  readonly pluginLinks?: readonly { readonly path: string; readonly label: string }[];
  readonly collapsed?: boolean;
}) {
  const location = useLocation();
  const [nodesOpen, setNodesOpen] = useState(true);
  return (
    <nav aria-label="主导航" className="flex flex-col gap-1">
      <ul className="space-y-0.5">
        <NavLink
          path={OVERVIEW.path}
          label={OVERVIEW.label}
          current={location.path}
          collapsed={collapsed}
          onNavigate={onNavigate}
        />
      </ul>
      {NAV_GROUPS.map(group => {
        const groupCollapsed = group.collapsible === true && !nodesOpen;
        return (
          <div key={group.id} className="pt-3">
            {group.collapsible ? (
              <button
                type="button"
                aria-expanded={nodesOpen}
                aria-controls="nav-group-nodes"
                onClick={() => setNodesOpen(value => !value)}
                className={cn(
                  "flex w-full items-center justify-between rounded-lg px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground hover:bg-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  collapsed && "sr-only",
                )}
              >
                {group.label}
                <span aria-hidden="true" className="text-[10px]">
                  {nodesOpen ? "−" : "+"}
                </span>
              </button>
            ) : (
              <p
                className={cn(
                  "px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/85",
                  collapsed && "sr-only",
                )}
              >
                {group.label}
              </p>
            )}
            <ul
              id={group.collapsible ? "nav-group-nodes" : undefined}
              hidden={groupCollapsed}
              className="mt-0.5 space-y-0.5"
            >
              {group.items.map(item => (
                <NavLink
                  key={item.path}
                  path={item.path}
                  label={item.label}
                  current={location.path}
                  collapsed={collapsed}
                  onNavigate={onNavigate}
                />
              ))}
            </ul>
          </div>
        );
      })}
      {pluginLinks.length > 0 ? (
        <div className="pt-3">
          <p
            className={cn(
              "px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/85",
              collapsed && "sr-only",
            )}
          >
            已启用插件
          </p>
          <ul className="mt-0.5 space-y-0.5">
            {pluginLinks.map(link => (
              <NavLink
                key={link.path}
                path={link.path}
                label={link.label}
                current={location.path}
                collapsed={collapsed}
                onNavigate={onNavigate}
              />
            ))}
          </ul>
        </div>
      ) : null}
      <div className={cn("pt-4 text-xs leading-5 text-muted-foreground", collapsed && "sr-only")}>
        <p>可选插件入口仅在服务端确认已安装并启用后显示。</p>
      </div>
    </nav>
  );
}

function Sidebar({
  pluginLinks,
  collapsed,
  onToggleCollapse,
}: {
  readonly pluginLinks: readonly { readonly path: string; readonly label: string }[];
  readonly collapsed: boolean;
  readonly onToggleCollapse: () => void;
}) {
  return (
    <aside
      className={cn(
        "fixed inset-y-0 left-0 z-30 hidden flex-col border-r border-border bg-card/95 backdrop-blur-md transition-[width] duration-200 lg:flex",
        collapsed ? "w-20" : "w-64",
      )}
    >
      <div className="flex items-center justify-between border-b border-border/80 px-4 py-4">
        <a
          href="#/overview"
          className="flex min-w-0 items-center gap-3 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          <span
            aria-hidden="true"
            className="relative grid size-9 shrink-0 place-items-center rounded-xl bg-gradient-to-br from-[#6658E8] to-[#3984E7] text-sm font-bold text-primary-foreground shadow-[0_4px_14px_-2px_rgba(102,88,232,0.45)]"
          >
            SF
            <span className="absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full border-2 border-card bg-[#169B68]" />
          </span>
          <span className={cn("min-w-0", collapsed && "sr-only")}>
            <span className="flex items-center gap-1.5 font-bold leading-5 tracking-tight text-foreground">
              <span>Server Fleet</span>
              <span className="rounded-full bg-[#F0EEFF] px-1.5 py-0.5 font-mono text-[10px] font-semibold text-[#6658E8]">
                v2.0
              </span>
            </span>
            <span className="block truncate text-xs text-muted-foreground">NEUROX 管理台</span>
          </span>
        </a>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
        <NavList pluginLinks={pluginLinks} collapsed={collapsed} />
      </div>
      <div className="border-t border-border/80 bg-[#F6F7FB]/70 px-3 py-2.5">
        {!collapsed ? (
          <p className="mb-2 px-2 text-[11px] text-muted-foreground">Web 仅调用 Controller REST/SSE</p>
        ) : null}
        <button
          type="button"
          onClick={onToggleCollapse}
          aria-label={collapsed ? "展开侧边栏" : "折叠侧边栏"}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-border bg-card py-1.5 text-xs font-medium text-muted-foreground transition-colors hover:border-[#6658E8]/35 hover:text-[#6658E8]"
        >
          {collapsed ? (
            <ChevronRight className="size-3.5" aria-hidden="true" />
          ) : (
            <>
              <ChevronLeft className="size-3.5" aria-hidden="true" />
              <span>折叠侧边栏</span>
            </>
          )}
        </button>
      </div>
    </aside>
  );
}

export function Shell({ children }: { readonly children: ReactNode }) {
  const session = useSession();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
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
    session.state.kind === "authenticated" || session.state.kind === "anonymous"
      ? session.state.notice
      : null;
  const warning = session.state.kind === "authenticated" ? session.state.warning : null;

  useEffect(() => {
    function handleKeyDown(event: KeyboardEvent) {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setCommandOpen(current => !current);
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  async function handleLogout() {
    setLoggingOut(true);
    const result = await session.logout();
    setLoggingOut(false);
    if (!result.ok && result.error) setLogoutError(describeApiError(result.error));
  }

  return (
    <div className="min-h-screen bg-background text-foreground">
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-50 focus:rounded-lg focus:bg-[#6658E8] focus:px-4 focus:py-2 focus:text-sm focus:font-medium focus:text-white focus:shadow-lg"
      >
        跳到主要内容
      </a>
      <Sidebar
        pluginLinks={pluginLinks}
        collapsed={collapsed}
        onToggleCollapse={() => setCollapsed(value => !value)}
      />
      <div className={cn("transition-[padding] duration-200", collapsed ? "lg:pl-20" : "lg:pl-64")}>
        <header className="sticky top-0 z-20 border-b border-border/85 bg-card/85 backdrop-blur-md">
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 lg:px-8">
            <div className="flex min-w-0 items-center gap-3">
              <button
                type="button"
                className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border bg-card px-3 text-sm font-medium hover:bg-secondary lg:hidden"
                aria-expanded={mobileOpen}
                onClick={() => setMobileOpen(true)}
              >
                <PanelLeft className="size-4 text-[#6658E8]" aria-hidden="true" />
                菜单
              </button>
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <Sparkles className="size-3.5 shrink-0 text-[#6658E8]" aria-hidden="true" />
                  <p className="truncate text-sm font-bold tracking-tight text-foreground">
                    {navItem ? navItem.label : routeMeta?.title ?? "Server Fleet"}
                  </p>
                </div>
                <p className="truncate text-xs text-muted-foreground">
                  {session.identity
                    ? `${session.identity.displayName} · ${session.identity.roles.join("、") || "无角色"}`
                    : ""}
                </p>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={() => setCommandOpen(true)}
                aria-label="打开全局命令面板"
                className="inline-flex h-8 items-center gap-2 rounded-lg border border-border bg-[#F6F7FB] px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:border-[#6658E8]/40 hover:bg-[#F0EEFF]/50 hover:text-[#6658E8]"
              >
                <Command className="size-3.5 text-[#6658E8]" aria-hidden="true" />
                <span className="hidden md:inline">快捷命令</span>
                <kbd className="rounded border border-border bg-card px-1.5 py-0.5 font-mono text-[10px] text-foreground">
                  Ctrl+K
                </kbd>
              </button>

              <div className="relative">
                <button
                  type="button"
                  onClick={() => setStatusOpen(open => !open)}
                  aria-label="通知与状态中心"
                  aria-expanded={statusOpen}
                  className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border bg-card px-2.5 text-xs font-medium text-foreground transition-colors hover:border-[#6658E8]/40 hover:bg-[#F6F7FB]"
                >
                  <Bell className="size-3.5 text-[#6658E8]" aria-hidden="true" />
                  <span
                    className={cn(
                      "size-2 rounded-full",
                      session.writesLocked ? "bg-[#D89B2B]" : "bg-[#169B68]",
                    )}
                  />
                </button>
                {statusOpen ? (
                  <div
                    role="region"
                    aria-label="通知与安全状态摘要"
                    className="absolute right-0 mt-2 w-80 rounded-2xl border border-border bg-card p-4 text-xs shadow-[0_20px_50px_-12px_rgba(28,32,48,0.22)]"
                  >
                    <div className="flex items-center justify-between border-b border-border pb-2">
                      <span className="font-bold text-foreground">安全与会话中心</span>
                      <button
                        type="button"
                        onClick={() => setStatusOpen(false)}
                        className="text-muted-foreground hover:text-foreground"
                      >
                        关闭
                      </button>
                    </div>
                    <div className="mt-3 space-y-2">
                      <div className="flex items-start gap-2 rounded-xl border border-border bg-[#F6F7FB]/80 p-2.5">
                        {session.writesLocked ? (
                          <ShieldAlert className="mt-0.5 size-4 shrink-0 text-[#D89B2B]" />
                        ) : (
                          <ShieldCheck className="mt-0.5 size-4 shrink-0 text-[#169B68]" />
                        )}
                        <div>
                          <p className="font-semibold text-foreground">
                            {session.writesLocked ? "安全写入已锁定" : "CSRF 写保护凭证有效"}
                          </p>
                          <p className="mt-0.5 text-[11px] text-muted-foreground">
                            {session.writesLocked
                              ? "刷新页面后需重新登录获取内存级 CSRF 令牌。"
                              : "当前会话具备完整服务端写操作保护。"}
                          </p>
                        </div>
                      </div>
                      <div className="flex justify-between pt-1">
                        <a
                          href="#/monitoring"
                          onClick={() => setStatusOpen(false)}
                          className="font-medium text-[#6658E8] hover:underline"
                        >
                          前往监控告警 →
                        </a>
                        <a
                          href="#/audit"
                          onClick={() => setStatusOpen(false)}
                          className="font-medium text-[#6658E8] hover:underline"
                        >
                          查看审计日志 →
                        </a>
                      </div>
                    </div>
                  </div>
                ) : null}
              </div>

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
                className="inline-flex h-8 items-center rounded-lg border border-input bg-card px-2.5 text-xs font-medium hover:border-[#6658E8]/35 hover:bg-[#F6F7FB] hover:text-[#6658E8] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
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
              <Notice
                tone="error"
                title="退出未确认"
                actions={<Button size="sm" onClick={() => setLogoutError(null)}>关闭</Button>}
              >
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
                    className="inline-flex h-8 items-center rounded-lg border border-input bg-card px-2.5 text-xs font-medium hover:bg-secondary"
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
          <footer className="mt-10 border-t border-border/80 py-4 text-xs leading-5 text-muted-foreground">
            <p>
              Server Fleet · 完整 v0.9 管理台（GM · NEUROX UI v2.0）。所有管理操作经 Controller
              REST/SSE；不在浏览器保存会话令牌、凭据或生产秘密。
            </p>
            <p className="mt-1">G2 契约仍为候选；未实现或失败接口会明确显示，不以演示数据替代。</p>
          </footer>
        </div>
      </div>
      <Modal open={mobileOpen} title="导航" onClose={() => setMobileOpen(false)} side>
        <NavList pluginLinks={pluginLinks} onNavigate={() => setMobileOpen(false)} />
      </Modal>
      <CommandPalette
        open={commandOpen}
        onClose={() => setCommandOpen(false)}
        pluginLinks={pluginLinks}
      />
    </div>
  );
}
