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
import { authApi, credentialsApi } from "@/lib/api/endpoints";
import { NotificationsSettings } from "./notifications-settings";
import type { CredentialMetadata, Session } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

type Tab = "credentials" | "security" | "notifications";

const KIND_LABEL: Record<CredentialMetadata["kind"], string> = {
  cloudflare_token: "Cloudflare API Token",
  smtp_password: "SMTP 密码/授权码",
  qqbot_secret: "QQ Bot AppSecret",
  ai_api_key: "AI 模型 Key",
  other: "其他",
};

const STATUS_TEXT: Record<CredentialMetadata["status"], string> = {
  unverified: "未验证",
  valid: "有效",
  invalid: "无效",
  expired: "已过期",
  revoked: "已撤销",
};

function CredentialEditor({
  open,
  mode,
  credential,
  canManage,
  writesLocked,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly mode: "create" | "rotate";
  readonly credential: CredentialMetadata | null;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: (message: string) => void;
}) {
  const [name, setName] = useState("");
  const [kind, setKind] = useState<CredentialMetadata["kind"]>("cloudflare_token");
  const [secret, setSecret] = useState("");
  const [usageScope, setUsageScope] = useState("");
  const [expiresAt, setExpiresAt] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction(async (body: { name: string; kind: string; secret: string; usageScope: readonly string[]; expiresAt?: string }): Promise<unknown> => {
    if (mode === "rotate" && credential) {
      const response = await credentialsApi.rotate(credential.id, body);
      return response.data;
    }
    const response = await credentialsApi.create(body);
    return response.data;
  });

  useEffect(() => {
    if (!open) return;
    setName(credential?.name ?? "");
    setKind(credential?.kind ?? "cloudflare_token");
    setSecret("");
    setUsageScope(credential?.usageScope.join(",") ?? "");
    setExpiresAt(credential?.expiresAt ? credential.expiresAt.slice(0, 16) : "");
    setLocalError(null);
  }, [open, credential]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) return setLocalError("凭据名称不能为空。");
    if (!secret) return setLocalError("必须输入秘密值；它只在本次请求中提交。");
    const scopes = usageScope.split(",").map(value => value.trim()).filter(Boolean);
    if (scopes.length === 0) return setLocalError("至少填写一个允许使用的模块范围。");
    let expires: string | undefined;
    if (expiresAt) {
      const parsed = new Date(expiresAt);
      if (Number.isNaN(parsed.getTime())) return setLocalError("到期时间无法解析。");
      expires = parsed.toISOString();
    }
    setLocalError(null);
    const result = await action.run({
      name: name.trim(),
      kind,
      secret,
      usageScope: scopes,
      ...(expires ? { expiresAt: expires } : {}),
    });
    setSecret("");
    if (result.ok) {
      onSaved(mode === "rotate" ? "轮换请求已被服务端接受（后台异步执行，不代表已完成）。" : "凭据已创建；服务端不会在列表中回显秘密。");
    }
  }

  return (
    <Modal
      open={open}
      title={mode === "rotate" ? "轮换凭据" : "添加安全凭据"}
      description="秘密只通过 HTTPS 写入；本页不写入浏览器存储、不写日志，也不在提交后保留输入值。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="credential-editor" variant="primary" loading={action.pending} disabled={!canManage || writesLocked || !secret}>
            {mode === "rotate" ? "提交轮换" : "保存凭据"}
          </Button>
        </>
      }
    >
      <form id="credential-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="操作失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="名称">
          {ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required />}
        </Field>
        <Field label="类型">
          {ids => (
            <Select id={ids.inputId} value={kind} onChange={event => setKind(event.target.value as CredentialMetadata["kind"])}>
              {Object.entries(KIND_LABEL).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
            </Select>
          )}
        </Field>
        <Field label="秘密值" hint="仅提交，不回显；轮换时也需要重新输入。">
          {ids => (
            <TextInput
              id={ids.inputId}
              type="password"
              autoComplete="new-password"
              value={secret}
              onChange={event => setSecret(event.target.value)}
              required
            />
          )}
        </Field>
        <Field label="允许使用的模块" hint="逗号分隔；由服务端校验最小权限。例如 dns,smtp,qqbot,ai。">
          {ids => <TextInput id={ids.inputId} value={usageScope} onChange={event => setUsageScope(event.target.value)} />}
        </Field>
        <Field label="到期时间（可选）">
          {ids => <TextInput id={ids.inputId} type="datetime-local" value={expiresAt} onChange={event => setExpiresAt(event.target.value)} />}
        </Field>
      </form>
    </Modal>
  );
}

export function SettingsPage() {
  const session = useSession();
  const [tab, setTab] = useState<Tab>("credentials");
  const [editor, setEditor] = useState<{ open: boolean; mode: "create" | "rotate"; credential: CredentialMetadata | null }>({
    open: false,
    mode: "create",
    credential: null,
  });
  const [revoking, setRevoking] = useState<CredentialMetadata | null>(null);
  const [references, setReferences] = useState<{ credential: CredentialMetadata; result: string } | null>(null);
  const [revokingSession, setRevokingSession] = useState<Session | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const canManage = session.can("credential.manage") || session.can("settings.manage") || session.can("user.manage");

  const credentials = useResource(`settings:credentials:${session.viewEpoch}`, signal =>
    credentialsApi.list(signal).then(result => result.data),
    { enabled: tab === "credentials" },
  );
  const sessions = useResource(`settings:sessions:${session.viewEpoch}`, signal =>
    authApi.sessions({ limit: 100 }, signal).then(result => result.data),
    { enabled: tab === "security" },
  );
  const revokeAction = useAction((id: string) => credentialsApi.revoke(id));
  const referencesAction = useAction((id: string) => credentialsApi.references(id));
  const revokeSessionAction = useAction((id: string) => authApi.revokeSession(id));

  const credentialItems = credentials.state.status === "ready" ? credentials.state.data : [];
  const sessionItems = sessions.state.status === "ready" ? sessions.state.data.items : [];

  async function submitRevoke() {
    if (!revoking) return;
    const result = await revokeAction.run(revoking.id);
    if (result.ok) {
      setRevoking(null);
      setOutcome("撤销请求已被服务端接受（后台异步执行）；引用该凭据的操作将被阻止或需要修复。");
      credentials.reload();
    }
  }

  async function viewReferences(credential: CredentialMetadata) {
    const result = await referencesAction.run(credential.id);
    if (result.ok && result.data) {
      setReferences({
        credential,
        result: result.data.data.ok ? "服务端检查完成；当前响应只返回 ok，没有引用清单字段。" : "服务端返回 ok=false；未能确认引用状态。",
      });
    }
  }

  async function submitRevokeSession() {
    if (!revokingSession) return;
    const result = await revokeSessionAction.run(revokingSession.sessionId);
    if (result.ok) {
      setRevokingSession(null);
      setOutcome("会话已撤销。");
      sessions.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="系统设置"
        description="安全凭据、账号会话与系统级配置。秘密只写不读；页面不把隐藏按钮当作安全边界。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">设置</span>}
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      <TabBar
        label="系统设置分区"
        active={tab}
        onChange={setTab}
        tabs={[
          { id: "credentials", label: "安全凭据" },
          { id: "security", label: "账号安全与会话" },
          { id: "notifications", label: "通知渠道" },
        ]}
      />

      {tab === "credentials" ? (
        <div className="space-y-5">
          {!canManage ? <Notice tone="warning" title="写操作已锁定">当前身份未返回凭据管理权限（`credential.manage`/`settings.manage`/`user.manage`）。</Notice> : null}
          <Panel
            title="凭据元数据"
            description="GET /api/v1/credentials；只返回元数据，不返回秘密。撤销前列出引用由服务端负责。"
            actions={
              <Button
                size="sm"
                variant="primary"
                disabled={!canManage || session.writesLocked}
                onClick={() => setEditor({ open: true, mode: "create", credential: null })}
              >
                添加凭据
              </Button>
            }
          >
            <AsyncPanel
              state={credentials.state}
              onRetry={credentials.reload}
              errorTitle="凭据列表加载失败"
              empty={<EmptyState title="暂无安全凭据" description="凭据由服务端加密托管；浏览器只保存元数据视图。" />}
              isEmpty={data => data.length === 0}
            >
              {() => (
                <DataTable
                  caption="安全凭据元数据"
                  rows={credentialItems}
                  rowKey={credential => credential.id}
                  columns={[
                    { key: "name", header: "名称", render: credential => credential.name },
                    { key: "kind", header: "类型", render: credential => KIND_LABEL[credential.kind] ?? credential.kind },
                    { key: "status", header: "状态", render: credential => STATUS_TEXT[credential.status] ?? credential.status },
                    { key: "scope", header: "使用范围", render: credential => credential.usageScope.join("、") || "未限定" },
                    { key: "validated", header: "最近验证", render: credential => formatDateTime(credential.lastValidatedAt) ?? "从未" },
                    { key: "expires", header: "到期", render: credential => formatDateTime(credential.expiresAt) ?? "长期" },
                    {
                      key: "actions",
                      header: "操作",
                      render: credential => (
                        <div className="flex flex-wrap gap-1">
                          <Button size="sm" onClick={() => viewReferences(credential)} loading={referencesAction.pending}>引用</Button>
                          <Button
                            size="sm"
                            disabled={!canManage || session.writesLocked}
                            onClick={() => setEditor({ open: true, mode: "rotate", credential })}
                          >
                            轮换
                          </Button>
                          <Button size="sm" variant="danger" disabled={!canManage || session.writesLocked} onClick={() => setRevoking(credential)}>
                            撤销
                          </Button>
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </AsyncPanel>
            <p className="border-t px-5 py-3 text-xs text-muted-foreground">
              契约缺口：GET references 目前只返回 <code>{`{ok}`}</code>，没有引用清单；轮换/撤销返回后台作业，不能在此声称已立即生效。
            </p>
          </Panel>
        </div>
      ) : null}

      {tab === "notifications" ? <NotificationsSettings /> : null}

      {tab === "security" ? (
        <div className="space-y-5">
          <Panel title="账号安全" description="当前契约没有密码修改、二次验证或凭据轮换端点；本页不创建假入口。">
            <div className="space-y-2 px-5 py-4 text-sm">
              <p>当前身份：<strong>{session.identity?.displayName}</strong>（{session.identity?.userId}）</p>
              <p>角色：{session.identity?.roles.join("、") || "无"}</p>
              <p className="text-muted-foreground">
                密码修改、二次验证登记与恢复码管理需要服务端专用端点与再认证流程；在契约补齐前标记为 UNTESTED。
              </p>
            </div>
          </Panel>
          <Panel
            title="我的会话"
            description="GET /api/v1/auth/sessions；只撤销自己的会话。当前会话使用顶部“退出”。"
            actions={<Button size="sm" onClick={sessions.reload} loading={sessions.state.status === "loading"}>刷新</Button>}
          >
            <AsyncPanel state={sessions.state} onRetry={sessions.reload} errorTitle="会话加载失败" empty={<EmptyState title="没有活跃会话" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="我的会话"
                  rows={sessionItems}
                  rowKey={item => item.sessionId}
                  columns={[
                    { key: "id", header: "会话 ID", render: item => <span className="font-mono text-xs">{item.sessionId}</span> },
                    { key: "created", header: "创建", render: item => formatDateTime(item.createdAt) ?? "—" },
                    { key: "seen", header: "最近使用", render: item => formatDateTime(item.lastSeenAt) ?? "—" },
                    { key: "expires", header: "到期", render: item => formatDateTime(item.expiresAt) ?? "—" },
                    { key: "current", header: "状态", render: item => (item.current ? "当前会话" : "其他设备") },
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
        </div>
      ) : null}

      <CredentialEditor
        open={editor.open}
        mode={editor.mode}
        credential={editor.credential}
        canManage={canManage}
        writesLocked={session.writesLocked}
        onClose={() => setEditor({ open: false, mode: "create", credential: null })}
        onSaved={message => {
          setEditor({ open: false, mode: "create", credential: null });
          setOutcome(message);
          credentials.reload();
        }}
      />
      <Modal
        open={revoking !== null}
        title="撤销安全凭据"
        description={revoking ? `将撤销“${revoking.name}”。引用该凭据的业务操作会被阻止或进入需修复状态。` : undefined}
        onClose={() => {
          if (!revokeAction.pending) setRevoking(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setRevoking(null)} disabled={revokeAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={submitRevoke} loading={revokeAction.pending}>确认撤销</Button>
          </>
        }
      >
        {revokeAction.error ? <Notice tone="error" title="撤销失败">{describeApiError(revokeAction.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">紧急撤销优先；普通轮换应先验证新秘密再切换引用。</p>
      </Modal>
      <Modal
        open={references !== null}
        title="凭据引用检查"
        onClose={() => setReferences(null)}
        footer={<Button type="button" onClick={() => setReferences(null)}>关闭</Button>}
      >
        {references ? (
          <div className="space-y-2 text-sm">
            <p>凭据：{references.credential.name}</p>
            <p>{references.result}</p>
            <Notice tone="warning" title="契约限制">服务端没有返回引用清单，因此不能在本页展示引用详情或声称可安全删除。</Notice>
          </div>
        ) : null}
      </Modal>
      <Modal
        open={revokingSession !== null}
        title="撤销会话"
        description={revokingSession ? `将撤销会话 ${revokingSession.sessionId}。` : undefined}
        onClose={() => {
          if (!revokeSessionAction.pending) setRevokingSession(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setRevokingSession(null)} disabled={revokeSessionAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={submitRevokeSession} loading={revokeSessionAction.pending}>确认撤销</Button>
          </>
        }
      >
        {revokeSessionAction.error ? <Notice tone="error" title="撤销失败">{describeApiError(revokeSessionAction.error)}</Notice> : null}
      </Modal>
    </div>
  );
}
