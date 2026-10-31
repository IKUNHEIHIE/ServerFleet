import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable, TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { authApi, groupsApi, nodesApi, serviceGroupsApi, usersApi } from "@/lib/api/endpoints";
import type { Grant, NodeSummary, Permission, Role, ServiceGroup, Session, User } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

type Tab = "users" | "roles" | "grants" | "sessions";

function UserEditor({
  open,
  user,
  roles,
  canManage,
  writesLocked,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly user: User | null;
  readonly roles: readonly Role[];
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: (user: User) => void;
}) {
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [roleIds, setRoleIds] = useState<ReadonlySet<string>>(new Set());
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction(async (body: {
    username: string;
    displayName?: string;
    password?: string;
    roleIds: readonly string[];
    enabled: boolean;
  }) =>
    user
      // 真实 Controller 的用户修改强制 If-Match 乐观锁（缺失返回 428 PRECONDITION_REQUIRED）。
      ? usersApi.patch(user.userId, { displayName: body.displayName, roleIds: body.roleIds, enabled: body.enabled }, user.revision)
      : usersApi.create(body),
  );

  useEffect(() => {
    if (!open) return;
    setUsername(user?.username ?? "");
    setDisplayName(user?.displayName ?? "");
    setPassword("");
    setEnabled(user?.enabled ?? true);
    setRoleIds(new Set(user?.roleIds ?? []));
    setLocalError(null);
  }, [open, user]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!username.trim()) return setLocalError("用户名不能为空。");
    if (!user && !password) return setLocalError("创建用户必须设置初始密码。");
    if (roleIds.size === 0) return setLocalError("至少选择一个角色。");
    setLocalError(null);
    const result = await action.run({
      username: username.trim(),
      ...(displayName.trim() ? { displayName: displayName.trim() } : {}),
      ...(password ? { password } : {}),
      roleIds: [...roleIds],
      enabled,
    });
    if (result.ok && result.data) onSaved(result.data.data);
  }

  const disabled = !canManage || writesLocked;

  return (
    <Modal
      open={open}
      title={user ? "编辑用户" : "创建用户"}
      description={user ? "更新角色与启用状态。当前契约没有密码重置端点；不得在本页伪造成功。" : "创建后密码不会回显；角色决定服务端能力目录。"}
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="user-editor" variant="primary" loading={action.pending} disabled={disabled || roles.length === 0}>
            保存用户
          </Button>
        </>
      }
    >
      <form id="user-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        {roles.length === 0 ? <Notice tone="warning" title="角色目录不可用">读取不到角色列表，无法安全选择角色；保存保持禁用。</Notice> : null}
        <Field label="用户名">
          {ids => <TextInput id={ids.inputId} value={username} onChange={event => setUsername(event.target.value)} disabled={Boolean(user) || disabled} required />}
        </Field>
        <Field label="显示名称（可选）">
          {ids => <TextInput id={ids.inputId} value={displayName} onChange={event => setDisplayName(event.target.value)} disabled={disabled} />}
        </Field>
        {!user ? (
          <Field label="初始密码" hint="只通过 HTTPS 提交；不写入浏览器存储或页面回显。">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="new-password"
                value={password}
                onChange={event => setPassword(event.target.value)}
                disabled={disabled}
              />
            )}
          </Field>
        ) : null}
        <fieldset className="rounded-lg border px-4 py-3">
          <legend className="px-1 text-sm font-medium">角色</legend>
          {roles.map(role => (
            <label key={role.roleId} className="mt-1 flex items-start gap-2 text-sm">
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-[var(--primary)]"
                checked={roleIds.has(role.roleId)}
                onChange={event => {
                  setRoleIds(current => {
                    const next = new Set(current);
                    if (event.target.checked) next.add(role.roleId);
                    else next.delete(role.roleId);
                    return next;
                  });
                }}
                disabled={disabled}
              />
              <span>
                {role.label} <span className="font-mono text-xs text-muted-foreground">{role.roleId}</span>
              </span>
            </label>
          ))}
        </fieldset>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={disabled} />
          启用该用户
        </label>
      </form>
    </Modal>
  );
}

function GrantEditor({
  open,
  canManage,
  writesLocked,
  permissions,
  nodes,
  groups,
  serviceGroups,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly permissions: readonly Permission[];
  readonly nodes: readonly NodeSummary[];
  readonly groups: readonly { id: string; name: string }[];
  readonly serviceGroups: readonly ServiceGroup[];
  readonly onClose: () => void;
  readonly onSaved: (grant: Grant) => void;
}) {
  const [principalId, setPrincipalId] = useState("");
  const [resourceType, setResourceType] = useState<"node" | "node_group" | "service_group">("node");
  const [resourceId, setResourceId] = useState("");
  const [selectedPermissions, setSelectedPermissions] = useState<ReadonlySet<string>>(new Set());
  const [permissionText, setPermissionText] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: {
    principalId: string;
    resourceType: "node" | "node_group" | "service_group";
    resourceId: string;
    permissions: readonly string[];
    expiresAt?: string;
  }) => usersApi.createGrant(body));

  useEffect(() => {
    if (!open) return;
    setPrincipalId("");
    setResourceType("node");
    setResourceId("");
    setSelectedPermissions(new Set());
    setPermissionText("");
    setExpiresAt("");
    setLocalError(null);
  }, [open]);

  const resourceOptions =
    resourceType === "node"
      ? nodes.map(node => ({ id: node.id, label: node.name }))
      : resourceType === "node_group"
        ? groups.map(group => ({ id: group.id, label: group.name }))
        : serviceGroups.map(group => ({ id: group.id, label: group.name }));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const permissionsList = permissions.length > 0 ? [...selectedPermissions] : permissionText.split(",").map(value => value.trim()).filter(Boolean);
    if (!principalId.trim()) return setLocalError("必须填写授权主体 ID。");
    if (!resourceId.trim()) return setLocalError("必须选择或填写资源 ID。");
    if (permissionsList.length === 0) return setLocalError("至少选择或填写一项权限。");
    let expiry: string | undefined;
    if (expiresAt) {
      const parsed = new Date(expiresAt);
      if (Number.isNaN(parsed.getTime())) return setLocalError("到期时间无法解析。");
      expiry = parsed.toISOString();
    }
    setLocalError(null);
    const result = await action.run({
      principalId: principalId.trim(),
      resourceType,
      resourceId: resourceId.trim(),
      permissions: permissionsList,
      ...(expiry ? { expiresAt: expiry } : {}),
    });
    if (result.ok && result.data) onSaved(result.data.data);
  }

  return (
    <Modal
      open={open}
      title="创建资源授权"
      description="客户端选择不构成授权；服务端会再次校验主体、资源与操作权限，并记录审计。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="grant-editor" variant="primary" loading={action.pending} disabled={!canManage || writesLocked}>创建授权</Button>
        </>
      }
    >
      <form id="grant-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="授权失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="主体 ID" hint="通常是用户 ID；不能使用客户端自报身份代替服务端鉴权。">
          {ids => <TextInput id={ids.inputId} value={principalId} onChange={event => setPrincipalId(event.target.value)} required />}
        </Field>
        <Field label="资源类型">
          {ids => (
            <Select id={ids.inputId} value={resourceType} onChange={event => { setResourceType(event.target.value as typeof resourceType); setResourceId(""); }}>
              <option value="node">节点</option>
              <option value="node_group">普通分组</option>
              <option value="service_group">服务组</option>
            </Select>
          )}
        </Field>
        <Field label="资源 ID">
          {ids => (
            <>
              <TextInput id={ids.inputId} list="grant-resource-options" value={resourceId} onChange={event => setResourceId(event.target.value)} required />
              <datalist id="grant-resource-options">
                {resourceOptions.map(option => <option key={option.id} value={option.id}>{option.label}</option>)}
              </datalist>
            </>
          )}
        </Field>
        {permissions.length > 0 ? (
          <fieldset className="max-h-56 overflow-y-auto rounded-lg border px-4 py-3">
            <legend className="px-1 text-sm font-medium">权限</legend>
            {permissions.map(permission => (
              <label key={permission.key} className="mt-1 flex items-start gap-2 text-sm">
                <input
                  type="checkbox"
                  className="mt-0.5 size-4 accent-[var(--primary)]"
                  checked={selectedPermissions.has(permission.key)}
                  onChange={event => {
                    setSelectedPermissions(current => {
                      const next = new Set(current);
                      if (event.target.checked) next.add(permission.key);
                      else next.delete(permission.key);
                      return next;
                    });
                  }}
                />
                <span>
                  <span className="font-mono text-xs">{permission.key}</span>
                  <span className="block text-xs text-muted-foreground">{permission.description}</span>
                </span>
              </label>
            ))}
          </fieldset>
        ) : (
          <Field label="权限（逗号分隔）" hint="权限目录不可用时的降级输入；服务端仍会校验。">
            {ids => <TextInput id={ids.inputId} value={permissionText} onChange={event => setPermissionText(event.target.value)} />}
          </Field>
        )}
        <Field label="到期时间（可选）">
          {ids => <TextInput id={ids.inputId} type="datetime-local" value={expiresAt} onChange={event => setExpiresAt(event.target.value)} />}
        </Field>
      </form>
    </Modal>
  );
}

export function UsersPage() {
  const session = useSession();
  const [tab, setTab] = useState<Tab>("users");
  const [userEditor, setUserEditor] = useState<{ open: boolean; user: User | null }>({ open: false, user: null });
  const [grantEditorOpen, setGrantEditorOpen] = useState(false);
  const [revokingGrant, setRevokingGrant] = useState<Grant | null>(null);
  const [revokingSession, setRevokingSession] = useState<Session | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const canManageUsers = session.can("user.manage");
  const canManageGrants = session.can("grant.manage") || session.can("user.manage");

  const users = useResource(`users:list:${session.viewEpoch}`, signal =>
    usersApi.list({ limit: 100 }, signal).then(result => result.data),
    { enabled: tab === "users" || tab === "grants" },
  );
  const roles = useResource(`users:roles:${session.viewEpoch}`, signal =>
    usersApi.roles(signal).then(result => result.data),
    { enabled: tab === "users" || tab === "roles" },
  );
  const permissions = useResource(`users:permissions:${session.viewEpoch}`, signal =>
    usersApi.permissions(signal).then(result => result.data),
    { enabled: tab === "roles" || tab === "grants" },
  );
  const grants = useResource(`users:grants:${session.viewEpoch}`, signal =>
    usersApi.grants({ limit: 100 }, signal).then(result => result.data),
    { enabled: tab === "grants" },
  );
  const sessions = useResource(`users:sessions:${session.viewEpoch}`, signal =>
    authApi.sessions({ limit: 100 }, signal).then(result => result.data),
    { enabled: tab === "sessions" },
  );
  const nodes = useResource(`users:grant-nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
    { enabled: tab === "grants" },
  );
  const groups = useResource(`users:grant-groups:${session.viewEpoch}`, signal =>
    groupsApi.list(signal).then(result => result.data),
    { enabled: tab === "grants" },
  );
  const serviceGroups = useResource(`users:grant-servicegroups:${session.viewEpoch}`, signal =>
    serviceGroupsApi.list(signal).then(result => result.data),
    { enabled: tab === "grants" },
  );

  const revokeGrant = useAction((grantId: string) => usersApi.revokeGrant(grantId));
  const revokeSession = useAction((sessionId: string) => authApi.revokeSession(sessionId));

  const userItems = users.state.status === "ready" ? users.state.data.items : [];
  const roleItems = roles.state.status === "ready" ? roles.state.data.items : [];
  const permissionItems = permissions.state.status === "ready" ? permissions.state.data.items : [];
  const grantItems = grants.state.status === "ready" ? grants.state.data.items : [];
  const sessionItems = sessions.state.status === "ready" ? sessions.state.data.items : [];
  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const groupItems = groups.state.status === "ready" ? groups.state.data.items : [];
  const serviceGroupItems = serviceGroups.state.status === "ready" ? serviceGroups.state.data.items : [];

  async function submitRevokeGrant() {
    if (!revokingGrant) return;
    const result = await revokeGrant.run(revokingGrant.grantId);
    if (result.ok) {
      setRevokingGrant(null);
      setOutcome("资源授权已撤销。");
      grants.reload();
    }
  }

  async function submitRevokeSession() {
    if (!revokingSession) return;
    const result = await revokeSession.run(revokingSession.sessionId);
    if (result.ok) {
      setRevokingSession(null);
      setOutcome("会话已撤销。");
      sessions.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="用户与权限"
        description="用户、角色目录、资源授权与会话。客户端隐藏按钮不构成安全控制；所有访问由服务端重新校验。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">RBAC</span>}
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      <TabBar
        label="用户与权限视图"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: "users", label: "用户" },
          { id: "roles", label: "角色与权限目录" },
          { id: "grants", label: "资源授权" },
          { id: "sessions", label: "我的会话" },
        ]}
      />

      {tab === "users" ? (
        <Panel
          title="用户"
          description="GET /api/v1/users；修改角色/停用需服务端权限。"
          actions={
            <Button size="sm" variant="primary" disabled={!canManageUsers || session.writesLocked} onClick={() => setUserEditor({ open: true, user: null })}>
              创建用户
            </Button>
          }
        >
          <AsyncPanel state={users.state} onRetry={users.reload} errorTitle="用户列表加载失败" empty={<EmptyState title="暂无用户" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="用户列表"
                rows={userItems}
                rowKey={user => user.userId}
                columns={[
                  { key: "username", header: "用户名", render: user => <span>{user.username}<span className="block font-mono text-xs text-muted-foreground">{user.userId}</span></span> },
                  { key: "display", header: "显示名", render: user => user.displayName ?? "—" },
                  { key: "roles", header: "角色", render: user => user.roleIds.join("、") || "无" },
                  { key: "enabled", header: "状态", render: user => (user.enabled ? "启用" : "停用") },
                  { key: "revision", header: "revision", render: user => user.revision },
                  {
                    key: "actions",
                    header: "操作",
                    render: user => (
                      <Button size="sm" disabled={!canManageUsers || session.writesLocked} onClick={() => setUserEditor({ open: true, user })}>
                        编辑
                      </Button>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
          <p className="border-t px-5 py-3 text-xs text-muted-foreground">
            契约缺口：没有密码重置、二次验证或删除用户端点；本页不提供伪造入口。UserPatch 也未携带 expectedRevision，服务端需自行处理并发。
          </p>
        </Panel>
      ) : null}

      {tab === "roles" ? (
        <div className="grid gap-5 xl:grid-cols-2">
          <Panel title="角色目录" description="GET /api/v1/roles；当前契约没有角色 CRUD，目录只读。">
            <AsyncPanel state={roles.state} onRetry={roles.reload} errorTitle="角色加载失败" empty={<EmptyState title="暂无角色" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="角色目录"
                  rows={roleItems}
                  rowKey={role => role.roleId}
                  columns={[
                    { key: "label", header: "角色", render: role => role.label },
                    { key: "id", header: "ID", render: role => <span className="font-mono text-xs">{role.roleId}</span> },
                    { key: "builtin", header: "内置", render: role => (role.builtIn ? "是" : "否") },
                    { key: "count", header: "权限数", align: "right", render: role => role.permissions.length },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
          <Panel title="权限目录" description="GET /api/v1/permissions；供授权选择与审计核对。">
            <AsyncPanel state={permissions.state} onRetry={permissions.reload} errorTitle="权限目录加载失败" empty={<EmptyState title="暂无权限项" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="权限目录"
                  rows={permissionItems}
                  rowKey={permission => permission.key}
                  columns={[
                    { key: "key", header: "权限", render: permission => <span className="font-mono text-xs">{permission.key}</span> },
                    { key: "desc", header: "说明", render: permission => permission.description },
                    { key: "resource", header: "资源类型", render: permission => permission.resourceType ?? "—" },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
        </div>
      ) : null}

      {tab === "grants" ? (
        <Panel
          title="资源授权"
          description="GET /api/v1/resource-grants；选中资源不代表授权，服务端会按主体/资源/权限再次校验。"
          actions={<Button size="sm" variant="primary" disabled={!canManageGrants || session.writesLocked} onClick={() => setGrantEditorOpen(true)}>创建授权</Button>}
        >
          <AsyncPanel state={grants.state} onRetry={grants.reload} errorTitle="资源授权加载失败" empty={<EmptyState title="暂无资源授权" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="资源授权记录"
                rows={grantItems}
                rowKey={grant => grant.grantId}
                columns={[
                  { key: "principal", header: "主体", render: grant => <span className="font-mono text-xs">{grant.principalId}</span> },
                  { key: "resource", header: "资源", render: grant => `${grant.resourceType} · ${grant.resourceId}` },
                  { key: "permissions", header: "权限", render: grant => grant.permissions.join("、") },
                  { key: "expires", header: "到期", render: grant => formatDateTime(grant.expiresAt) ?? "长期" },
                  {
                    key: "actions",
                    header: "操作",
                    render: grant => (
                      <Button size="sm" variant="danger" disabled={!canManageGrants || session.writesLocked} onClick={() => setRevokingGrant(grant)}>
                        撤销
                      </Button>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      ) : null}

      {tab === "sessions" ? (
        <Panel
          title="我的会话"
          description="GET /api/v1/auth/sessions；撤销他人会话不在本页范围，当前会话请使用退出。"
          actions={<Button size="sm" onClick={sessions.reload} loading={sessions.state.status === "loading"}>刷新</Button>}
        >
          <AsyncPanel state={sessions.state} onRetry={sessions.reload} errorTitle="会话列表加载失败" empty={<EmptyState title="没有活跃会话" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="我的活跃会话"
                rows={sessionItems}
                rowKey={item => item.sessionId}
                columns={[
                  { key: "id", header: "会话 ID", render: item => <span className="font-mono text-xs">{item.sessionId}</span> },
                  { key: "created", header: "创建", render: item => formatDateTime(item.createdAt) ?? "—" },
                  { key: "seen", header: "最近使用", render: item => formatDateTime(item.lastSeenAt) ?? "—" },
                  { key: "expires", header: "到期", render: item => formatDateTime(item.expiresAt) ?? "—" },
                  { key: "current", header: "当前", render: item => (item.current ? "当前会话" : "其他设备") },
                  {
                    key: "actions",
                    header: "操作",
                    render: item => (
                      <Button size="sm" variant="danger" disabled={item.current} onClick={() => setRevokingSession(item)}>
                        撤销
                      </Button>
                    ),
                  },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      ) : null}

      <UserEditor
        open={userEditor.open}
        user={userEditor.user}
        roles={roleItems}
        canManage={canManageUsers}
        writesLocked={session.writesLocked}
        onClose={() => setUserEditor({ open: false, user: null })}
        onSaved={() => {
          setUserEditor({ open: false, user: null });
          setOutcome(userEditor.user ? "用户已更新。" : "用户已创建。");
          users.reload();
        }}
      />
      <GrantEditor
        open={grantEditorOpen}
        canManage={canManageGrants}
        writesLocked={session.writesLocked}
        permissions={permissionItems}
        nodes={nodeItems}
        groups={groupItems}
        serviceGroups={serviceGroupItems}
        onClose={() => setGrantEditorOpen(false)}
        onSaved={() => {
          setGrantEditorOpen(false);
          setOutcome("资源授权已创建。");
          grants.reload();
        }}
      />
      <Modal
        open={revokingGrant !== null}
        title="撤销资源授权"
        description={revokingGrant ? `撤销 ${revokingGrant.principalId} 对 ${revokingGrant.resourceType} ${revokingGrant.resourceId} 的授权。` : undefined}
        onClose={() => {
          if (!revokeGrant.pending) setRevokingGrant(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setRevokingGrant(null)} disabled={revokeGrant.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={submitRevokeGrant} loading={revokeGrant.pending}>确认撤销</Button>
          </>
        }
      >
        {revokeGrant.error ? <Notice tone="error" title="撤销失败">{describeApiError(revokeGrant.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">撤销后依赖该授权的操作会被服务端阻止；不会自动回滚已执行任务。</p>
      </Modal>
      <Modal
        open={revokingSession !== null}
        title="撤销会话"
        description={revokingSession ? `将撤销会话 ${revokingSession.sessionId}。` : undefined}
        onClose={() => {
          if (!revokeSession.pending) setRevokingSession(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setRevokingSession(null)} disabled={revokeSession.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={submitRevokeSession} loading={revokeSession.pending}>确认撤销</Button>
          </>
        }
      >
        {revokeSession.error ? <Notice tone="error" title="撤销失败">{describeApiError(revokeSession.error)}</Notice> : null}
      </Modal>
    </div>
  );
}
