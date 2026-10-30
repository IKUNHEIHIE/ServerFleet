import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { dnsApi } from "@/lib/api/endpoints-infra";
import type { DNSAccount, DNSConnectionTest, DNSRecord, DNSRecordInput, DNSZone } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

function AccountEditor({
  open,
  account,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly account: DNSAccount | null;
  readonly onClose: () => void;
  readonly onSaved: () => void;
}) {
  const session = useSession();
  const [displayName, setDisplayName] = useState("");
  const [note, setNote] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [apiToken, setApiToken] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: { displayName?: string; note?: string; enabled?: boolean; apiToken?: string }) =>
    account
      ? dnsApi.updateAccount(account.id, { displayName: body.displayName, note: body.note, enabled: body.enabled })
      : dnsApi.createAccount({
          providerCode: "cloudflare",
          displayName: body.displayName ?? "",
          note: body.note,
          enabled: body.enabled,
          apiToken: body.apiToken ?? "",
        }),
  );

  useEffect(() => {
    if (!open) return;
    setDisplayName(account?.displayName ?? "");
    setNote(account?.note ?? "");
    setEnabled(account?.enabled ?? true);
    setApiToken("");
    setLocalError(null);
  }, [open, account]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!displayName.trim()) return setLocalError("显示名称不能为空。");
    if (!account && !apiToken) return setLocalError("创建账号必须输入 Cloudflare API Token。");
    setLocalError(null);
    const result = await action.run({
      displayName: displayName.trim(),
      ...(note.trim() ? { note: note.trim() } : {}),
      enabled,
      ...(!account ? { apiToken } : {}),
    });
    setApiToken("");
    if (result.ok) onSaved();
  }

  const disabled = !session.can("dns.account.manage") || session.writesLocked;

  return (
    <Modal
      open={open}
      title={account ? "编辑 DNS 账号" : "添加 Cloudflare 账号"}
      description="API Token 只通过 HTTPS 安全写入；列表与日志不返回明文。首版仅支持 Cloudflare。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="dns-account-editor" variant="primary" loading={action.pending} disabled={disabled}>保存账号</Button>
        </>
      }
    >
      <form id="dns-account-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="显示名称">{ids => <TextInput id={ids.inputId} value={displayName} onChange={event => setDisplayName(event.target.value)} required disabled={disabled} />}</Field>
        <Field label="备注（可选）">{ids => <TextInput id={ids.inputId} value={note} onChange={event => setNote(event.target.value)} disabled={disabled} />}</Field>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={disabled} />
          启用账号
        </label>
        {!account ? (
          <Field label="Cloudflare API Token" hint="只写入后端凭证服务；提交后立即清空。">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="new-password"
                value={apiToken}
                onChange={event => setApiToken(event.target.value)}
                disabled={disabled}
              />
            )}
          </Field>
        ) : null}
      </form>
    </Modal>
  );
}

function RecordEditor({
  open,
  record,
  zone,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly record: DNSRecord | null;
  readonly zone: DNSZone | null;
  readonly onClose: () => void;
  readonly onSaved: () => void;
}) {
  const session = useSession();
  const [type, setType] = useState<DNSRecordInput["type"]>("A");
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [ttl, setTtl] = useState("");
  const [proxied, setProxied] = useState(false);
  const [priority, setPriority] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: DNSRecordInput) =>
    record && zone ? dnsApi.updateRecord(zone.id, record.id, body) : zone ? dnsApi.createRecord(zone.id, body) : Promise.reject(new Error("no zone")),
  );

  useEffect(() => {
    if (!open) return;
    setType(record?.type ?? "A");
    setName(record?.name ?? "");
    setContent(record?.content ?? "");
    setTtl(record?.ttl === undefined ? "" : String(record.ttl));
    setProxied(record?.proxied ?? false);
    setPriority(record?.priority === undefined ? "" : String(record.priority));
    setLocalError(null);
  }, [open, record]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim() || !content.trim()) return setLocalError("记录名与内容不能为空。");
    const body: DNSRecordInput = {
      type,
      name: name.trim(),
      content: content.trim(),
      ...(ttl ? { ttl: Number(ttl) } : {}),
      proxied,
      ...(priority ? { priority: Number(priority) } : {}),
    };
    setLocalError(null);
    const result = await action.run(body);
    if (result.ok) onSaved();
  }

  const disabled = !session.can("dns.record.manage") || session.writesLocked;

  return (
    <Modal
      open={open}
      title={record ? "编辑 DNS 记录" : "添加 DNS 记录"}
      description={zone ? `Zone：${zone.name}（account ${zone.accountId}）` : undefined}
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="dns-record-editor" variant="primary" loading={action.pending} disabled={disabled}>保存记录</Button>
        </>
      }
    >
      <form id="dns-record-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="类型">
            {ids => (
              <Select id={ids.inputId} value={type} onChange={event => setType(event.target.value as DNSRecordInput["type"])} disabled={disabled}>
                {["A", "AAAA", "CNAME", "TXT", "MX"].map(value => <option key={value} value={value}>{value}</option>)}
              </Select>
            )}
          </Field>
          <Field label="TTL（可选）">{ids => <TextInput id={ids.inputId} type="number" min={1} value={ttl} onChange={event => setTtl(event.target.value)} disabled={disabled} />}</Field>
        </div>
        <Field label="名称">{ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required disabled={disabled} />}</Field>
        <Field label="内容">{ids => <TextInput id={ids.inputId} value={content} onChange={event => setContent(event.target.value)} required disabled={disabled} />}</Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="优先级（MX 等，可选）">{ids => <TextInput id={ids.inputId} type="number" min={0} value={priority} onChange={event => setPriority(event.target.value)} disabled={disabled} />}</Field>
          <label className="mt-6 flex items-center gap-2 text-sm">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={proxied} onChange={event => setProxied(event.target.checked)} disabled={disabled} />
            Cloudflare 代理（供应商支持时）
          </label>
        </div>
      </form>
    </Modal>
  );
}

export function DnsPage() {
  const session = useSession();
  const canManageAccounts = session.can("dns.account.manage");
  const canManageRecords = session.can("dns.record.manage");
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(null);
  const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null);
  const [accountEditor, setAccountEditor] = useState<{ open: boolean; account: DNSAccount | null }>({ open: false, account: null });
  const [deletingAccount, setDeletingAccount] = useState<DNSAccount | null>(null);
  const [rotating, setRotating] = useState<DNSAccount | null>(null);
  const [rotateToken, setRotateToken] = useState("");
  const [testResult, setTestResult] = useState<{ accountId: string; result: DNSConnectionTest } | null>(null);
  const [recordEditor, setRecordEditor] = useState<{ open: boolean; record: DNSRecord | null }>({ open: false, record: null });
  const [deletingRecord, setDeletingRecord] = useState<DNSRecord | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const providers = useResource(`dns:providers:${session.viewEpoch}`, signal => dnsApi.providers(signal).then(result => result.data));
  const accounts = useResource(`dns:accounts:${session.viewEpoch}`, signal => dnsApi.accounts(signal).then(result => result.data));
  const accountItems = accounts.state.status === "ready" ? accounts.state.data.items : [];
  const selectedAccount = accountItems.find(account => account.id === selectedAccountId) ?? accountItems.find(account => account.isDefault) ?? accountItems[0] ?? null;
  const zones = useResource(
    `dns:zones:${session.viewEpoch}:${selectedAccount?.id ?? "none"}`,
    signal => selectedAccount ? dnsApi.zones(selectedAccount.id, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(selectedAccount) },
  );
  const zoneItems = zones.state.status === "ready" ? zones.state.data.items : [];
  const selectedZone = zoneItems.find(zone => zone.id === selectedZoneId) ?? zoneItems[0] ?? null;
  const records = useResource(
    `dns:records:${session.viewEpoch}:${selectedZone?.id ?? "none"}`,
    signal => selectedZone ? dnsApi.records(selectedZone.id, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(selectedZone) },
  );
  const recordItems = records.state.status === "ready" ? records.state.data.items : [];

  const deleteAccountAction = useAction((id: string) => dnsApi.removeAccount(id));
  const rotateAction = useAction((id: string, token: string) => dnsApi.rotateCredential(id, token));
  const defaultAction = useAction((id: string) => dnsApi.setDefault(id));
  const testAction = useAction((id: string) => dnsApi.test(id));
  const syncAction = useAction((id: string) => dnsApi.syncZones(id));
  const deleteRecordAction = useAction((record: DNSRecord) => dnsApi.removeRecord(record.zoneId, record.id));

  async function removeAccount() {
    if (!deletingAccount) return;
    const result = await deleteAccountAction.run(deletingAccount.id);
    if (result.ok) {
      setDeletingAccount(null);
      setSelectedAccountId(null);
      setSelectedZoneId(null);
      setOutcome("账号绑定已解除；不会删除 Cloudflare 上的真实 Zone/记录。");
      accounts.reload();
    }
  }

  async function rotateCredential() {
    if (!rotating || !rotateToken) return;
    const result = await rotateAction.run(rotating.id, rotateToken);
    setRotateToken("");
    if (result.ok) {
      setRotating(null);
      setOutcome("API Token 已轮换写入（服务端结果）；不会回显。");
      accounts.reload();
    }
  }

  async function setDefault(account: DNSAccount) {
    const result = await defaultAction.run(account.id);
    if (result.ok) {
      setOutcome(`默认账号已切换为 ${account.displayName}。`);
      accounts.reload();
    }
  }

  async function test(account: DNSAccount) {
    setTestResult(null);
    const result = await testAction.run(account.id);
    if (result.ok && result.data) setTestResult({ accountId: account.id, result: result.data.data });
  }

  async function sync(account: DNSAccount) {
    const result = await syncAction.run(account.id);
    if (result.ok) {
      setOutcome("Zone 清单同步请求已完成（服务端结果）。");
      zones.reload();
    }
  }

  async function removeRecord() {
    if (!deletingRecord) return;
    const result = await deleteRecordAction.run(deletingRecord);
    if (result.ok) {
      setDeletingRecord(null);
      setOutcome("DNS 记录已删除；服务端会记录变更。");
      records.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="DNS 管理"
        description="首版仅 Cloudflare；账号、Zone 与记录严格按账号隔离。Token 只写不读，不进入浏览器存储。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">Cloudflare</span>}
        actions={<Button type="button" onClick={() => { accounts.reload(); zones.reload(); records.reload(); }}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      <Panel title="供应商" description="GET /api/v1/dns/providers；只启用服务端声明支持的供应商。">
        <AsyncPanel state={providers.state} onRetry={providers.reload} errorTitle="供应商加载失败">
          {data => <p className="px-5 py-4 text-sm">{data.items.map(provider => `${provider.displayName}（${provider.code}）· Zone 同步 ${provider.capabilities.supportsZoneSync ? "支持" : "未声明"}`).join("；") || "没有供应商"}</p>}
        </AsyncPanel>
      </Panel>
      <div className="grid gap-5 lg:grid-cols-[20rem_minmax(0,1fr)]">
        <Panel
          title="账号"
          description="GET /api/v1/dns/accounts；不返回 Token。"
          actions={<Button size="sm" variant="primary" disabled={!canManageAccounts || session.writesLocked} onClick={() => setAccountEditor({ open: true, account: null })}>添加账号</Button>}
        >
          <AsyncPanel state={accounts.state} onRetry={accounts.reload} errorTitle="账号加载失败" empty={<EmptyState title="暂无 Cloudflare 账号" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <ul className="divide-y">
                {accountItems.map(account => (
                  <li key={account.id} className={account.id === selectedAccount?.id ? "bg-primary/10" : ""}>
                    <button type="button" onClick={() => { setSelectedAccountId(account.id); setSelectedZoneId(null); setTestResult(null); }} className="w-full px-4 py-3 text-left hover:bg-secondary">
                      <span className="flex items-center justify-between gap-2">
                        <span className="font-medium">{account.displayName}</span>
                        <span className="text-xs text-muted-foreground">{account.enabled ? "启用" : "停用"}</span>
                      </span>
                      <span className="mt-0.5 block text-xs text-muted-foreground">
                        凭据 {account.credentialStatus}{account.isDefault ? " · 默认" : ""} · {account.zoneCount ?? "—"} Zone
                      </span>
                    </button>
                    <div className="flex flex-wrap gap-1 px-3 pb-2">
                      <Button size="sm" onClick={() => setAccountEditor({ open: true, account })} disabled={!canManageAccounts || session.writesLocked}>编辑</Button>
                      <Button size="sm" onClick={() => void setDefault(account)} disabled={!canManageAccounts || session.writesLocked || account.isDefault}>默认</Button>
                      <Button size="sm" onClick={() => void test(account)} loading={testAction.pending}>检查</Button>
                      <Button size="sm" onClick={() => setRotating(account)} disabled={!canManageAccounts || session.writesLocked}>轮换</Button>
                      <Button size="sm" variant="danger" onClick={() => setDeletingAccount(account)} disabled={!canManageAccounts || session.writesLocked}>删除</Button>
                    </div>
                    {testResult?.accountId === account.id ? (
                      <div className="mx-3 mb-3 rounded-md border bg-secondary/30 px-3 py-2 text-xs">
                        连接检查：{testResult.result.status} · Zone 读取 {testResult.result.zoneRead ? "通过" : "失败"} · 记录写入 {testResult.result.recordWrite ? "通过" : "失败"}
                        {testResult.result.message ? ` · ${testResult.result.message}` : ""}
                      </div>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </AsyncPanel>
        </Panel>

        <div className="space-y-4">
          <Panel
            title="Zone"
            description="按所选账号隔离；切换账号不会继承上一账号的 Zone。"
            actions={
              <Button size="sm" onClick={() => selectedAccount && void sync(selectedAccount)} loading={syncAction.pending} disabled={!selectedAccount || !canManageAccounts || session.writesLocked}>
                同步 Zone
              </Button>
            }
          >
            {!selectedAccount ? <EmptyState title="请选择账号" /> : (
              <AsyncPanel state={zones.state} onRetry={zones.reload} errorTitle="Zone 加载失败" empty={<EmptyState title="该账号没有可见 Zone" description="可尝试同步；未授权 Zone 不会显示。" />} isEmpty={data => data.items.length === 0}>
                {data => (
                  <div className="px-5 py-4">
                    <label className="text-xs font-medium text-muted-foreground">
                      Zone
                      <select
                        value={selectedZone?.id ?? ""}
                        onChange={event => setSelectedZoneId(event.target.value)}
                        className="mt-1 block h-9 w-full max-w-md rounded-md border border-input bg-background px-3 text-sm"
                      >
                        {data.items.map(zone => <option key={zone.id} value={zone.id}>{zone.name}{zone.status ? ` · ${zone.status}` : ""}</option>)}
                      </select>
                    </label>
                  </div>
                )}
              </AsyncPanel>
            )}
          </Panel>

          <Panel
            title="解析记录"
            description={selectedZone ? `Zone ${selectedZone.name}；GET /api/v1/dns/zones/${selectedZone.id}/records` : "选择 Zone 后显示记录"}
            actions={<Button size="sm" variant="primary" disabled={!selectedZone || !canManageRecords || session.writesLocked} onClick={() => setRecordEditor({ open: true, record: null })}>添加记录</Button>}
          >
            {!selectedZone ? <EmptyState title="请选择 Zone" /> : (
              <AsyncPanel state={records.state} onRetry={records.reload} errorTitle="DNS 记录加载失败" empty={<EmptyState title="该 Zone 暂无记录" />} isEmpty={data => data.items.length === 0}>
                {() => (
                  <DataTable
                    caption="DNS 解析记录"
                    rows={recordItems}
                    rowKey={record => record.id}
                    columns={[
                      { key: "type", header: "类型", render: record => record.type },
                      { key: "name", header: "名称", render: record => record.name },
                      { key: "content", header: "内容", render: record => <span className="break-all font-mono text-xs">{record.content}</span> },
                      { key: "ttl", header: "TTL", render: record => record.ttl ?? "自动" },
                      { key: "proxied", header: "代理", render: record => (record.proxied ? "是" : "否") },
                      { key: "modified", header: "修改", render: record => formatDateTime(record.modifiedAt) ?? "—" },
                      {
                        key: "actions",
                        header: "操作",
                        render: record => (
                          <div className="flex gap-1">
                            <Button size="sm" onClick={() => setRecordEditor({ open: true, record })} disabled={!canManageRecords || session.writesLocked}>编辑</Button>
                            <Button size="sm" variant="danger" onClick={() => setDeletingRecord(record)} disabled={!canManageRecords || session.writesLocked}>删除</Button>
                          </div>
                        ),
                      },
                    ]}
                  />
                )}
              </AsyncPanel>
            )}
          </Panel>
        </div>
      </div>

      <AccountEditor
        open={accountEditor.open}
        account={accountEditor.account}
        onClose={() => setAccountEditor({ open: false, account: null })}
        onSaved={() => {
          setAccountEditor({ open: false, account: null });
          setOutcome("DNS 账号已保存（服务端结果）。");
          accounts.reload();
        }}
      />
      <RecordEditor
        open={recordEditor.open}
        record={recordEditor.record}
        zone={selectedZone}
        onClose={() => setRecordEditor({ open: false, record: null })}
        onSaved={() => {
          setRecordEditor({ open: false, record: null });
          setOutcome("DNS 记录已保存（服务端结果）。");
          records.reload();
        }}
      />
      <Modal
        open={deletingAccount !== null}
        title="解除账号绑定"
        description={deletingAccount ? `解除“${deletingAccount.displayName}”。不会删除 Cloudflare 上的真实 Zone 或记录。` : undefined}
        onClose={() => setDeletingAccount(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeletingAccount(null)} disabled={deleteAccountAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeAccount} loading={deleteAccountAction.pending}>确认解除</Button>
          </>
        }
      >
        {deleteAccountAction.error ? <Notice tone="error" title="解除失败">{describeApiError(deleteAccountAction.error)}</Notice> : null}
      </Modal>
      <Modal
        open={rotating !== null}
        title="轮换 API Token"
        description={rotating ? `账号 ${rotating.displayName}；新 Token 只写不读。` : undefined}
        onClose={() => { if (!rotateAction.pending) setRotating(null); }}
        footer={
          <>
            <Button type="button" onClick={() => setRotating(null)} disabled={rotateAction.pending}>取消</Button>
            <Button type="button" variant="primary" onClick={rotateCredential} loading={rotateAction.pending} disabled={!rotateToken}>提交轮换</Button>
          </>
        }
      >
        {rotateAction.error ? <Notice tone="error" title="轮换失败">{describeApiError(rotateAction.error)}</Notice> : null}
        <Field label="新的 Cloudflare API Token">
          {ids => <TextInput id={ids.inputId} type="password" autoComplete="new-password" value={rotateToken} onChange={event => setRotateToken(event.target.value)} />}
        </Field>
      </Modal>
      <Modal
        open={deletingRecord !== null}
        title="删除 DNS 记录"
        description={deletingRecord ? `${deletingRecord.type} ${deletingRecord.name} → ${deletingRecord.content}` : undefined}
        onClose={() => setDeletingRecord(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeletingRecord(null)} disabled={deleteRecordAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeRecord} loading={deleteRecordAction.pending}>确认删除</Button>
          </>
        }
      >
        {deleteRecordAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteRecordAction.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">该操作直接作用于所选账号与 Zone；不会跨账号删除。</p>
      </Modal>
    </div>
  );
}
