import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import {
  Activity,
  Bot,
  Command,
  CornerDownLeft,
  Cpu,
  FileCode2,
  FolderSync,
  Globe,
  HardDriveDownload,
  KeyRound,
  Layers,
  LayoutDashboard,
  ListChecks,
  Network,
  PackagePlus,
  PlusCircle,
  RefreshCw,
  Rocket,
  Search,
  Server,
  Settings,
  ShieldCheck,
  Sparkles,
  Users,
  Workflow,
} from "lucide-react";
import { NAV_GROUPS, OVERVIEW } from "./navigation";
import { navigate } from "./router";
import { useSession } from "./session";

export interface CommandItem {
  readonly id: string;
  readonly label: string;
  readonly group: string;
  readonly subtitle: string;
  readonly keywords: string;
  readonly path?: string;
  readonly onSelect: () => void;
}

export function getRouteIcon(path: string) {
  switch (path) {
    case "/overview":
      return <LayoutDashboard className="size-4" aria-hidden="true" />;
    case "/nodes":
      return <Server className="size-4" aria-hidden="true" />;
    case "/nodes/new":
      return <PlusCircle className="size-4" aria-hidden="true" />;
    case "/groups":
      return <Layers className="size-4" aria-hidden="true" />;
    case "/files":
      return <FolderSync className="size-4" aria-hidden="true" />;
    case "/tasks":
      return <ListChecks className="size-4" aria-hidden="true" />;
    case "/monitoring":
      return <Activity className="size-4" aria-hidden="true" />;
    case "/ai":
      return <Bot className="size-4" aria-hidden="true" />;
    case "/gateways":
      return <Network className="size-4" aria-hidden="true" />;
    case "/publishing":
      return <Rocket className="size-4" aria-hidden="true" />;
    case "/service-groups":
      return <Cpu className="size-4" aria-hidden="true" />;
    case "/workflows":
      return <Workflow className="size-4" aria-hidden="true" />;
    case "/plugins":
    case "/plugins/xray":
      return <PackagePlus className="size-4" aria-hidden="true" />;
    case "/dns":
      return <Globe className="size-4" aria-hidden="true" />;
    case "/certificates":
      return <KeyRound className="size-4" aria-hidden="true" />;
    case "/updates":
      return <RefreshCw className="size-4" aria-hidden="true" />;
    case "/backups":
      return <HardDriveDownload className="size-4" aria-hidden="true" />;
    case "/users":
      return <Users className="size-4" aria-hidden="true" />;
    case "/audit":
      return <FileCode2 className="size-4" aria-hidden="true" />;
    case "/settings":
      return <Settings className="size-4" aria-hidden="true" />;
    default:
      return <ShieldCheck className="size-4" aria-hidden="true" />;
  }
}

export function CommandPalette({
  open,
  onClose,
  pluginLinks = [],
}: {
  readonly open: boolean;
  readonly onClose: () => void;
  readonly pluginLinks?: readonly { readonly path: string; readonly label: string }[];
}) {
  const session = useSession();
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const items = useMemo<readonly CommandItem[]>(() => {
    const navItems: CommandItem[] = [
      {
        id: `nav:${OVERVIEW.path}`,
        label: OVERVIEW.label,
        group: "概览",
        subtitle: `跳转至 ${OVERVIEW.path}`,
        keywords: `${OVERVIEW.label} dashboard overview 仪表盘 概览`,
        path: OVERVIEW.path,
        onSelect: () => {
          navigate(OVERVIEW.path);
          onClose();
        },
      },
      ...NAV_GROUPS.flatMap(group =>
        group.items.map(item => ({
          id: `nav:${item.path}`,
          label: item.label,
          group: group.label,
          subtitle: `路由 ${item.path}`,
          keywords: `${item.label} ${group.label} ${item.path}`,
          path: item.path,
          onSelect: () => {
            navigate(item.path);
            onClose();
          },
        })),
      ),
      ...pluginLinks.map(link => ({
        id: `nav:${link.path}`,
        label: link.label,
        group: "已启用插件",
        subtitle: `插件路由 ${link.path}`,
        keywords: `${link.label} plugin xray 插件`,
        path: link.path,
        onSelect: () => {
          navigate(link.path);
          onClose();
        },
      })),
    ];

    const quickActions: CommandItem[] = [
      {
        id: "act:revalidate",
        label: "同步并复核服务端身份",
        group: "快捷操作",
        subtitle: "刷新当前身份与权限",
        keywords: "revalidate refresh auth session 同步身份 权限",
        onSelect: () => {
          session.revalidate();
          onClose();
        },
      },
    ];

    if (session.can("node.enroll") || session.can("node.write")) {
      quickActions.push({
        id: "act:add-node",
        label: "新建节点注册令牌（Bootstrap Token）",
        group: "快捷操作",
        subtitle: "前往添加节点向导",
        keywords: "add node enroll token 添加节点 注册",
        path: "/nodes/new",
        onSelect: () => {
          navigate("/nodes/new");
          onClose();
        },
      });
    }

    if (session.can("ai.use") || session.can("ai.manage")) {
      quickActions.push({
        id: "act:ai-workspace",
        label: "进入 AI 运维工作空间",
        group: "快捷操作",
        subtitle: "管理 AI 会话、资源授权与审批策略",
        keywords: "ai ops agent session 运维 智能体 审批",
        path: "/ai",
        onSelect: () => {
          navigate("/ai");
          onClose();
        },
      });
    }

    return [...quickActions, ...navItems];
  }, [onClose, pluginLinks, session]);

  const filtered = useMemo(() => {
    const trimmed = query.trim().toLowerCase();
    if (!trimmed) return items;
    return items.filter(
      item =>
        item.label.toLowerCase().includes(trimmed) ||
        item.group.toLowerCase().includes(trimmed) ||
        item.subtitle.toLowerCase().includes(trimmed) ||
        item.keywords.toLowerCase().includes(trimmed),
    );
  }, [items, query]);

  useEffect(() => {
    if (!open) return;
    setQuery("");
    setActiveIndex(0);
    const id = requestAnimationFrame(() => inputRef.current?.focus());
    return () => cancelAnimationFrame(id);
  }, [open]);

  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  if (!open) return null;

  function onKeyDown(event: ReactKeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      onClose();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setActiveIndex(current => (filtered.length === 0 ? 0 : (current + 1) % filtered.length));
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setActiveIndex(current =>
        filtered.length === 0 ? 0 : (current - 1 + filtered.length) % filtered.length,
      );
      return;
    }
    if (event.key === "Enter" && filtered[activeIndex]) {
      event.preventDefault();
      filtered[activeIndex].onSelect();
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-[#1C2030]/45 px-4 pt-[12vh] backdrop-blur-xs"
      onMouseDown={event => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="全局命令面板"
        onKeyDown={onKeyDown}
        className="relative w-full max-w-xl overflow-hidden rounded-2xl border border-border bg-card text-card-foreground shadow-[0_24px_64px_-12px_rgba(28,32,48,0.3)]"
      >
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 top-0 h-[3px] bg-gradient-to-r from-[#6658E8] via-[#3984E7] to-[#169B68]"
        />
        <div className="flex items-center gap-3 border-b border-border px-4 py-3.5">
          <Search className="size-4 shrink-0 text-[#6658E8]" aria-hidden="true" />
          <input
            ref={inputRef}
            type="search"
            value={query}
            onChange={event => setQuery(event.target.value)}
            placeholder="搜索功能页面、路由或快捷运维操作…"
            aria-label="搜索命令或页面"
            className="w-full bg-transparent text-sm text-foreground placeholder:text-muted-foreground focus:outline-none"
          />
          <kbd className="hidden rounded border border-border bg-[#F6F7FB] px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground sm:inline-block">
            ESC
          </kbd>
        </div>

        <div className="max-h-80 overflow-y-auto p-2">
          {filtered.length === 0 ? (
            <div className="px-4 py-8 text-center text-xs text-muted-foreground">
              未找到匹配的页面或受权快捷操作
            </div>
          ) : (
            <ul role="listbox" aria-label="可用命令列表" className="space-y-1">
              {filtered.map((item, index) => {
                const selected = index === activeIndex;
                return (
                  <li key={item.id} role="option" aria-selected={selected}>
                    <button
                      type="button"
                      onMouseEnter={() => setActiveIndex(index)}
                      onClick={item.onSelect}
                      className={`flex w-full items-center justify-between gap-3 rounded-xl px-3 py-2.5 text-left text-sm transition-colors ${
                        selected
                          ? "bg-[#F0EEFF] text-[#6658E8]"
                          : "text-foreground hover:bg-[#F6F7FB]"
                      }`}
                    >
                      <div className="flex min-w-0 items-center gap-3">
                        <span
                          className={`flex size-8 shrink-0 items-center justify-center rounded-lg border ${
                            selected
                              ? "border-[#6658E8]/30 bg-white text-[#6658E8]"
                              : "border-border bg-[#F6F7FB] text-muted-foreground"
                          }`}
                        >
                          {item.path ? getRouteIcon(item.path) : <Sparkles className="size-4" aria-hidden="true" />}
                        </span>
                        <div className="min-w-0">
                          <div className="truncate font-semibold">{item.label}</div>
                          <div className="truncate text-xs text-muted-foreground">
                            {item.group} · {item.subtitle}
                          </div>
                        </div>
                      </div>
                      <CornerDownLeft
                        className={`size-3.5 shrink-0 ${
                          selected ? "text-[#6658E8]" : "text-muted-foreground/50"
                        }`}
                        aria-hidden="true"
                      />
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>

        <footer className="flex items-center justify-between border-t border-border bg-[#F6F7FB]/80 px-4 py-2 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <Command className="size-3 text-[#6658E8]" aria-hidden="true" />
            <span>方向键选择 · Enter 跳转 · 权限与服务端保持一致</span>
          </span>
          <span className="font-mono">Ctrl + K</span>
        </footer>
      </div>
    </div>
  );
}
