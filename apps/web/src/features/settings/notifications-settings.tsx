import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { credentialsApi } from "@/lib/api/endpoints";
import { notificationsApi } from "@/lib/api/endpoints-infra";
import type { CredentialMetadata, NotificationChannel, NotificationDelivery, NotificationSettings } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const DELIVERY_TEXT: Record<NotificationDelivery["status"], string> = {
  queued: "排队",
  submitted: "已提交",
  provider_accepted: "平台已接受（不代表已读）",
  rejected: "被拒绝",
  rate_limited: "被限流",
  failed: "失败",
  delivery_unknown: "投递未知",
};

function ChannelForm({ channel, credentials, onChanged, onOpenCredentials }: { readonly channel: NotificationChannel; readonly credentials: readonly CredentialMetadata[]; readonly onChanged: () => void; readonly onOpenCredentials?: () => void }) {
  const session = useSession();
  const [enabled, setEnabled] = useState(channel.enabled);
  const [smtp, setSmtp] = useState<NonNullable<NotificationChannel["smtp"]>>(channel.smtp ?? {});
  const [qqbot, setQqbot] = useState<NonNullable<NotificationChannel["qqbot"]>>(channel.qqbot ?? {});
  const [credentialId, setCredentialId] = useState(channel.credentialId ?? "");
  const [testResult, setTestResult] = useState<NotificationDelivery | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const save = useAction((body: { enabled: boolean; smtp?: NonNullable<NotificationChannel["smtp"]>; qqbot?: NonNullable<NotificationChannel["qqbot"]> }) =>
    notificationsApi.putChannel(channel.channel, body),
  );
  const saveSecret = useAction((value: string) => notificationsApi.putSecret(channel.channel, value));
  const test = useAction(() => notificationsApi.test(channel.channel));

  useEffect(() => {
    setEnabled(channel.enabled);
    setSmtp(channel.smtp ?? {});
    setQqbot(channel.qqbot ?? {});
  }, [channel]);

  const disabled = !session.can("notify.channel.manage") || session.writesLocked;
  const canTest = session.can("notify.test") && !session.writesLocked;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setOutcome(null);
    const result = await save.run({
      enabled,
      ...(channel.channel === "smtp" ? { smtp } : { qqbot }),
    });
    if (result.ok) { setOutcome("渠道配置已保存；修改配置后请重新绑定凭据以授权后台投递。"); onChanged(); }
  }

  async function submitSecret() {
    if (!credentialId) return;
    setOutcome(null);
    const result = await saveSecret.run(credentialId);
    if (result.ok) { setOutcome("已绑定加密凭据及当前修订，后台投递授权已保存。"); onChanged(); }
  }

  async function runTest() {
    setOutcome(null);
    setTestResult(null);
    const result = await test.run();
    if (result.ok && result.data) { setTestResult(result.data.data); onChanged(); }
  }

  return (
    <Panel
      title={channel.channel === "smtp" ? "SMTP 邮箱" : "QQ 官方机器人"}
      description={`凭据状态：${channel.credentialStatus}。${channel.backgroundReady ? "后台投递已授权" : "请保存配置后绑定凭据"}。`}
    >
      <form onSubmit={submit} className="space-y-4 px-5 py-4">
        {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
        {save.error || saveSecret.error ? <Notice tone="error" title="保存失败">{describeApiError(save.error ?? saveSecret.error)}</Notice> : null}
        {test.error ? <Notice tone="error" title="测试失败">{describeApiError(test.error)}</Notice> : null}
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={disabled} />
          启用该渠道
        </label>
        {channel.channel === "smtp" ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="SMTP 服务器">{ids => <TextInput id={ids.inputId} value={smtp.host ?? ""} onChange={event => setSmtp({ ...smtp, host: event.target.value })} disabled={disabled} />}</Field>
            <Field label="端口">{ids => <TextInput id={ids.inputId} type="number" min={1} max={65535} value={smtp.port ?? ""} onChange={event => setSmtp({ ...smtp, port: event.target.value ? Number(event.target.value) : undefined })} disabled={disabled} />}</Field>
            <Field label="加密">
              {ids => (
                <Select id={ids.inputId} value={smtp.security ?? "tls"} onChange={event => setSmtp({ ...smtp, security: event.target.value as "tls" | "starttls" })} disabled={disabled}>
                  <option value="tls">TLS</option>
                  <option value="starttls">STARTTLS</option>
                </Select>
              )}
            </Field>
            <Field label="发件邮箱">{ids => <TextInput id={ids.inputId} type="email" value={smtp.sender ?? ""} onChange={event => setSmtp({ ...smtp, sender: event.target.value })} disabled={disabled} />}</Field>
            <Field label="收件邮箱">{ids => <TextInput id={ids.inputId} type="email" value={smtp.recipient ?? ""} onChange={event => setSmtp({ ...smtp, recipient: event.target.value })} disabled={disabled} />}</Field>
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="AppID">{ids => <TextInput id={ids.inputId} value={qqbot.appId ?? ""} onChange={event => setQqbot({ ...qqbot, appId: event.target.value })} disabled={disabled} />}</Field>
            <Field label="接收类型">
              {ids => (
                <Select id={ids.inputId} value={qqbot.targetType ?? "group"} onChange={event => setQqbot({ ...qqbot, targetType: event.target.value as "private" | "group" })} disabled={disabled}>
                  <option value="group">群</option>
                  <option value="private">私聊</option>
                </Select>
              )}
            </Field>
            <Field label="接收对象 OpenID">{ids => <TextInput id={ids.inputId} value={qqbot.targetId ?? ""} onChange={event => setQqbot({ ...qqbot, targetId: event.target.value })} disabled={disabled} />}</Field>
          </div>
        )}
        <div className="flex flex-wrap items-end gap-2">
          <Button type="submit" variant="primary" loading={save.pending} disabled={disabled}>保存配置</Button>
          <Button type="button" onClick={runTest} loading={test.pending} disabled={!canTest}>测试消息</Button>
        </div>
        <div className="rounded-lg border bg-secondary/30 px-4 py-3">
          <Field label="加密凭据" hint="先在凭据管理创建 SMTP 密码或 QQ Bot Secret；此处只绑定凭据 ID。">
            {ids => <Select id={ids.inputId} value={credentialId} onChange={event => setCredentialId(event.target.value)} disabled={disabled}>
              <option value="">请选择凭据</option>
              {credentials.filter(item => item.kind === (channel.channel === "smtp" ? "smtp_password" : "qqbot_secret") && item.status !== "revoked" && item.status !== "expired").map(item => <option key={item.id} value={item.id}>{item.name} · 修订 {item.revision}</option>)}
            </Select>}
          </Field>
          <div className="mt-2 flex gap-2"><Button type="button" onClick={submitSecret} loading={saveSecret.pending} disabled={disabled || !credentialId}>绑定凭据</Button>
          {onOpenCredentials ? <Button type="button" onClick={onOpenCredentials}>前往凭据管理</Button> : null}</div>
        </div>
        {testResult ? (
          <Notice tone={testResult.status === "provider_accepted" ? "info" : "warning"} title="测试投递结果">
            {DELIVERY_TEXT[testResult.status]} · 渠道 {testResult.channel}
            {testResult.taskId ? <a className="ml-2 underline" href={`#/tasks/${encodeURIComponent(testResult.taskId)}`}>查看投递任务</a> : null}
            {testResult.providerCode ? ` · 供应商码 ${testResult.providerCode}` : ""}
            {testResult.message ? ` · ${testResult.message}` : ""}
          </Notice>
        ) : null}
      </form>
    </Panel>
  );
}

export function NotificationsSettings({ onOpenCredentials }: { readonly onOpenCredentials?: () => void }) {
  const session = useSession();
  const channels = useResource(`settings:notification:channels:${session.viewEpoch}`, signal =>
    notificationsApi.channels(signal).then(result => result.data),
  );
  const credentials = useResource(`settings:notification:credentials:${session.viewEpoch}`, signal => credentialsApi.list(signal).then(result => result.data));
  const settings = useResource(`settings:notification:settings:${session.viewEpoch}`, signal =>
    notificationsApi.settings(signal).then(result => result.data),
  );
  const deliveries = useResource(`settings:notification:deliveries:${session.viewEpoch}`, signal =>
    notificationsApi.deliveries(signal).then(result => result.data),
    { refreshIntervalMs: 5000 },
  );
  const [draft, setDraft] = useState<NotificationSettings | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const saveSettings = useAction((body: NotificationSettings) => notificationsApi.putSettings(body));

  useEffect(() => {
    if (settings.state.status === "ready") setDraft(settings.state.data);
  }, [settings.state]);

  const canManage = session.can("notify.channel.manage") || session.can("settings.manage");
  const savedItems = channels.state.status === "ready" ? channels.state.data.items : [];
  const items = (["smtp", "qqbot"] as const).map(channel => savedItems.find(item => item.channel === channel) ?? { channel, enabled: false, credentialStatus: "not_configured" as const });
  const credentialItems = credentials.state.status === "ready" ? credentials.state.data : [];
  const changed = () => { channels.reload(); deliveries.reload(); };
  const deliveryItems = deliveries.state.status === "ready" ? deliveries.state.data.items : [];

  async function submitSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft) return;
    const result = await saveSettings.run(draft);
    if (result.ok) {
      setOutcome("通知策略已保存（服务端结果）。");
      settings.reload();
    }
  }

  return (
    <div className="space-y-5">
      <Notice tone="info" title="投递语义">
        “平台已接受”不等于收件人已读；QQ 失败时 SMTP 备用、重试上限和去重窗口以服务端策略执行。测试按钮不会把模拟结果说成真实送达。
      </Notice>
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      {credentials.state.status === "error" ? <Notice tone="error" title="凭据列表加载失败">{describeApiError(credentials.state.error)}<Button size="sm" onClick={credentials.reload}>重试</Button></Notice> : null}
      <AsyncPanel state={channels.state} onRetry={channels.reload} errorTitle="通知渠道加载失败">
        {() => (
          <div className="grid gap-5 xl:grid-cols-2">
            {items.map(channel => <ChannelForm key={channel.channel} channel={channel} credentials={credentialItems} onChanged={changed} onOpenCredentials={onOpenCredentials} />)}
          </div>
        )}
      </AsyncPanel>

      <Panel title="通知策略" description="GET/PUT /api/v1/notifications/settings">
        <form onSubmit={submitSettings} className="space-y-3 px-5 py-4">
          {saveSettings.error ? <Notice tone="error" title="保存失败">{describeApiError(saveSettings.error)}</Notice> : null}
          {draft ? (
            <>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="size-4 accent-[var(--primary)]"
                  checked={draft.smtpFallbackOnQQFailure}
                  onChange={event => setDraft({ ...draft, smtpFallbackOnQQFailure: event.target.checked })}
                  disabled={!canManage || session.writesLocked}
                />
                QQ 失败时允许 SMTP 备用
              </label>
              <Field label="最大重试次数（0–5）">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    type="number"
                    min={0}
                    max={5}
                    value={draft.maxRetryAttempts}
                    onChange={event => setDraft({ ...draft, maxRetryAttempts: Number(event.target.value) })}
                    disabled={!canManage || session.writesLocked}
                  />
                )}
              </Field>
              <Field label="去重窗口（秒，可选）">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    type="number"
                    min={0}
                    max={86400}
                    value={draft.deduplicationWindowSeconds ?? ""}
                    onChange={event => setDraft({ ...draft, deduplicationWindowSeconds: event.target.value ? Number(event.target.value) : undefined })}
                    disabled={!canManage || session.writesLocked}
                  />
                )}
              </Field>
              <Button type="submit" variant="primary" loading={saveSettings.pending} disabled={!canManage || session.writesLocked}>保存策略</Button>
            </>
          ) : (
            <p className="text-sm text-muted-foreground">通知策略尚未加载。</p>
          )}
        </form>
      </Panel>

      <Panel title="投递记录" description="GET /api/v1/notifications/deliveries；只读状态。" actions={<Button size="sm" onClick={deliveries.reload} loading={deliveries.state.status === "loading"}>刷新</Button>}>
        <AsyncPanel state={deliveries.state} onRetry={deliveries.reload} errorTitle="投递记录加载失败" empty={<EmptyState title="暂无投递记录" />} isEmpty={data => data.items.length === 0}>
          {() => (
            <DataTable
              caption="通知投递记录"
              rows={deliveryItems}
              rowKey={delivery => delivery.id}
              columns={[
                { key: "id", header: "记录", render: delivery => <span className="font-mono text-xs">{delivery.id}</span> },
                { key: "channel", header: "渠道", render: delivery => (delivery.channel === "smtp" ? "SMTP" : "QQ Bot") },
                { key: "status", header: "状态", render: delivery => DELIVERY_TEXT[delivery.status] },
                { key: "task", header: "投递任务", render: delivery => delivery.taskId ? <a className="underline" href={`#/tasks/${encodeURIComponent(delivery.taskId)}`}>查看任务</a> : "—" },
                { key: "code", header: "供应商码", render: delivery => delivery.providerCode ?? "—" },
                { key: "message", header: "脱敏说明", render: delivery => delivery.message ?? "—" },
                { key: "time", header: "时间", render: delivery => formatDateTime(delivery.createdAt) ?? "—" },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>
    </div>
  );
}
