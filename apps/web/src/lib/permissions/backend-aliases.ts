/**
 * UI 权限键与真实 Controller 权限目录（`apps/controller/server/server.go` ExtraPermissions
 * 与 `internal/auth` 内置 5 键）的兼容映射（GM-W3-01）。
 *
 * 严格性不变式：
 * - 只在服务端 `permissions` 数组**显式包含**某个后端权限键时才判定通过；
 *   本模块不根据角色名、isBootstrapAdmin 或其他字段推断权限。
 * - `isBootstrapAdmin` 的放行逻辑保留在会话层（服务端在身份中显式声明该事实）。
 * - 候选契约/验收桩继续直接使用 UI 键：映射只做“额外接受”，不改变原有匹配。
 */

/** UI 权限键 → 可满足它的真实后端权限键集合（后端键均来自服务端权限目录，逐项核对过）。 */
const BACKEND_ALIASES: Readonly<Record<string, readonly string[]>> = {
  "task.submit": ["tasks.submit"],
  "task.approve": ["tasks.approve"],
  "task.cancel": ["tasks.cancel"],
  "task.read": ["tasks.read"],
  "task.manage": ["tasks.approve", "tasks.cancel", "tasks.revise"],
  "node.read": ["nodes.read"],
  "node.manage": ["nodes.write", "node.create"],
  "node.suspend": ["nodes.write"],
  "node.revoke": ["nodes.write"],
  "group.manage": ["groups.write"],
  "servicegroup.manage": ["servicegroups.write"],
  "servicegroup.read": ["servicegroups.read"],
  "gateway.read": ["gateway.read"],
  "gateway.manage": ["gateway.write"],
  "gateway.install": ["gateway.provision"],
  "file.read": ["files.read"],
  "file.write": ["files.write"],
  "file.transfer": ["files.transfer"],
  "dns.account.manage": ["dns.account.create", "dns.account.update", "dns.account.default"],
  "dns.account.test": ["dns.account.test"],
  "dns.record.manage": ["dns.record.create", "dns.record.update", "dns.record.delete"],
  "dns.read": ["dns.accounts.read", "dns.zones.read", "dns.records.read"],
  "notify.channel.manage": ["notifications.channel", "notifications.settings", "notifications.secret", "notifications.write"],
  "notify.test": ["notifications.test"],
  "audit.read": ["audit.read"],
  "credential.manage": ["credentials.create", "credentials.rotate", "credentials.revoke"],
};

/**
 * 判定已授予的权限集合是否满足 UI 权限键。
 * `granted` 必须是服务端返回的权限结果；缺失（undefined）时返回 false，不得推断。
 */
export function hasPermission(granted: readonly string[] | undefined, uiKey: string): boolean {
  if (!granted) return false;
  if (granted.includes(uiKey)) return true;
  const aliases = BACKEND_ALIASES[uiKey];
  if (!aliases) return false;
  return aliases.some(alias => granted.includes(alias));
}
