export interface NavItem {
  readonly path: string;
  readonly label: string;
  readonly description: string;
}

export interface NavGroup {
  readonly id: "nodes" | "core" | "extensions" | "system";
  readonly label: string;
  readonly collapsible?: boolean;
  readonly items: readonly NavItem[];
}

export const OVERVIEW: NavItem = {
  path: "/overview",
  label: "概览仪表盘",
  description: "节点在线状态、资源概况、近期任务与告警。",
};

export const NAV_GROUPS: readonly NavGroup[] = [
  {
    id: "nodes",
    label: "节点管理",
    collapsible: true,
    items: [
      { path: "/nodes", label: "节点列表", description: "服务器资产、在线状态、资源与详情。" },
      { path: "/nodes/new", label: "添加节点", description: "短期接入凭证、安装引导、接入审批与历史。" },
      { path: "/groups", label: "分组管理", description: "自定义分组与只读自动分类；与服务组严格区分。" },
    ],
  },
  {
    id: "core",
    label: "核心功能",
    items: [
      { path: "/files", label: "文件互传", description: "双栏目录浏览、上传下载与跨节点复制任务。" },
      { path: "/tasks", label: "批量任务", description: "冻结目标快照、分批/金丝雀、审批与进度。" },
      { path: "/monitoring", label: "监控告警", description: "资源概况、HTTP/TCP/TLS 可用性与告警事件。" },
      { path: "/ai", label: "AI 运维", description: "会话资源授权、工具审批与流式消息。" },
      { path: "/gateways", label: "网关管理", description: "Nginx 应用网关安装、检查、维护与路由。" },
      { path: "/publishing", label: "服务发布", description: "服务组到网关、DNS、证书的一体化发布。" },
      { path: "/service-groups", label: "服务组管理", description: "业务成员集合、健康与调度状态。" },
    ],
  },
  {
    id: "extensions",
    label: "扩展能力",
    items: [
      { path: "/workflows", label: "自动化工作流", description: "触发器、条件、操作、人工审批与通知。" },
      { path: "/plugins", label: "插件中心", description: "来源仓库、目录、安装启停升级与卸载。" },
      { path: "/dns", label: "DNS 管理", description: "多账号、Zone 与解析记录。" },
      { path: "/certificates", label: "证书管理", description: "公网证书与内部 TLS 下发。" },
    ],
  },
  {
    id: "system",
    label: "系统维护",
    items: [
      { path: "/updates", label: "更新中心", description: "主控与 Agent 更新状态、失败恢复。" },
      { path: "/backups", label: "备份与恢复", description: "备份校验与主控灾难恢复。" },
      { path: "/users", label: "用户与权限", description: "用户、角色、资源授权与会话。" },
      { path: "/audit", label: "审计日志", description: "关键操作人、时间、资源与结果。" },
      { path: "/settings", label: "系统设置", description: "账号安全、通知渠道、DNS 供应商与安全凭据。" },
    ],
  },
];

export const ROUTE_META: Readonly<Record<string, { readonly title: string; readonly description?: string }>> = {
  "/overview": { title: OVERVIEW.label },
  "/login": { title: "登录" },
  "/setup": { title: "初始化管理员" },
  "/nodes": { title: "节点列表" },
  "/nodes/new": { title: "添加节点" },
  "/nodes/:nodeId": { title: "节点详情" },
  "/groups": { title: "分组管理" },
  "/files": { title: "文件互传" },
  "/tasks": { title: "批量任务" },
  "/tasks/:taskId": { title: "任务详情" },
  "/monitoring": { title: "监控告警" },
  "/ai": { title: "AI 运维" },
  "/ai/:sessionId": { title: "AI 会话" },
  "/gateways": { title: "网关管理" },
  "/gateways/:gatewayId": { title: "网关详情" },
  "/publishing": { title: "服务发布" },
  "/service-groups": { title: "服务组管理" },
  "/service-groups/:serviceGroupId": { title: "服务组详情" },
  "/workflows": { title: "自动化工作流" },
  "/plugins": { title: "插件中心" },
  "/plugins/xray": { title: "Xray 节点管理器" },
  "/dns": { title: "DNS 管理" },
  "/certificates": { title: "证书管理" },
  "/updates": { title: "更新中心" },
  "/backups": { title: "备份与恢复" },
  "/users": { title: "用户与权限" },
  "/audit": { title: "审计日志" },
  "/settings": { title: "系统设置" },
};

export function findNavItem(path: string): NavItem | null {
  if (path === OVERVIEW.path) return OVERVIEW;
  for (const group of NAV_GROUPS) {
    const item = group.items.find(entry => entry.path === path);
    if (item) return item;
  }
  return null;
}
