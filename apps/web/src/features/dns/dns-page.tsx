import { useEffect, useState, type FormEvent } from "react";
import { Globe2, Cloud, ListTree, RefreshCw } from "lucide-react";
import { useSession } from "@/app/session";
import { GlowingEffect } from "@/components/aceternity/glowing-effect";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Checkbox, Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { dnsApi } from "@/lib/api/endpoints-infra";
import type { DNSAccount, DNSProviderZone, DNSRecord, DNSRecordInput, DNSZone } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";

function recordFQDN(name: string, zone: DNSZone | null): string {
  const value = name.trim();
  if (!zone || !value) return value;
  if (value === "@") return zone.name;
  if (value === zone.name || value.endsWith(`.${zone.name}`)) return value;
  return `${value}.${zone.name}`;
}

function validateRecord(type: DNSRecordInput["type"], content: string, priority: string): string | null {
  const value = content.trim();
  if (type === "A" && !/^(\d{1,3}\.){3}\d{1,3}$/.test(value)) return "A 记录内容应为 IPv4 地址。";
  if (type === "A" && value.split(".").some(part => Number(part) > 255)) return "A 记录的 IPv4 地址无效。";
  if (type === "AAAA" && !value.includes(":")) return "AAAA 记录内容应为 IPv6 地址。";
  if ((type === "CNAME" || type === "MX") && !value.includes(".")) return `${type} 记录内容应为域名。`;
  if (type === "MX" && priority.trim() === "") return "MX 记录需要填写优先级。";
  return null;
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
  readonly onSaved: (record: DNSRecord) => void;
}) {
  const session = useSession();
  const [type, setType] = useState<DNSRecordInput["type"]>("A");
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [ttl, setTtl] = useState("auto");
  const [proxied, setProxied] = useState(false);
  const [priority, setPriority] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: DNSRecordInput) =>
    record && zone ? dnsApi.updateRecord(zone.id, record.id, body) : zone ? dnsApi.createRecord(zone.id, body) : Promise.reject(new Error("no zone")),
  );

  useEffect(() => {
    if (!open) return;
    setType(record?.type ?? "A");
    setName(record?.name ?? "@");
    setContent(record?.content ?? "");
    setTtl(record?.ttl === undefined || record.ttl <= 1 ? "auto" : String(record.ttl));
    setProxied(record?.proxied ?? false);
    setPriority(record?.priority === undefined ? "" : String(record.priority));
    setLocalError(null);
    action.reset();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, record]);

  const proxiable = type === "A" || type === "AAAA" || type === "CNAME";

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim() || !content.trim()) return setLocalError("记录名与内容不能为空。");
    const problem = validateRecord(type, content, priority);
    if (problem) return setLocalError(problem);
    const body: DNSRecordInput = {
      type,
      name: name.trim(),
      content: content.trim(),
      ...(ttl !== "auto" ? { ttl: Number(ttl) } : {}),
      proxied: proxiable ? proxied : false,
      ...(priority ? { priority: Number(priority) } : {}),
    };
    setLocalError(null);
    const result = await action.run(body);
    if (result.ok && result.data) onSaved(result.data.data);
  }

  const disabled = !session.can("dns.record.manage") || session.writesLocked;
  const inputsLocked = disabled || action.pending;

  return (
    <Modal
      open={open}
      title={record ? "编辑 DNS 记录" : "添加 DNS 记录"}
      description={zone ? `Zone：${zone.name}` : undefined}
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="dns-record-editor" variant="primary" loading={action.pending} disabled={disabled || !name.trim() || !content.trim()}>
            {record ? "保存记录" : "添加记录"}
          </Button>
        </>
      }
    >
      <form id="dns-record-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="类型">
            {ids => (
              <Select id={ids.inputId} value={type} onChange={event => setType(event.target.value as DNSRecordInput["type"])} disabled={inputsLocked}>
                {["A", "AAAA", "CNAME", "TXT", "MX"].map(value => <option key={value} value={value}>{value}</option>)}
              </Select>
            )}
          </Field>
          <Field label="TTL" hint="自动由 Cloudflare 决定。">
            {ids => (
              <Select id={ids.inputId} value={ttl} onChange={event => setTtl(event.target.value)} disabled={inputsLocked}>
                <option value="auto">自动</option>
                <option value="60">1 分钟</option>
                <option value="300">5 分钟</option>
                <option value="600">10 分钟</option>
                <option value="1200">20 分钟</option>
                <option value="3600">1 小时</option>
              </Select>
            )}
          </Field>
        </div>
        <Field label="名称" hint={`完整域名：${recordFQDN(name, zone) || "—"}；填 @ 表示根域名。`}>
          {ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} placeholder="例如 www 或 @" required disabled={inputsLocked} autoComplete="off" />}
        </Field>
        <Field label="内容" hint={type === "A" ? "IPv4 地址，例如 203.0.113.10。" : type === "AAAA" ? "IPv6 地址。" : type === "CNAME" ? "目标域名，例如 example.com。" : type === "MX" ? "邮件服务器域名。" : "TXT 文本内容。"}>
          {ids => <TextInput id={ids.inputId} value={content} onChange={event => setContent(event.target.value)} required disabled={inputsLocked} autoComplete="off" />}
        </Field>
        <div className="grid gap-3 sm:grid-cols-2">
          {type === "MX" ? (
            <Field label="优先级" hint="数值越小优先级越高。">
              {ids => <TextInput id={ids.inputId} type="number" min={0} max={65535} value={priority} onChange={event => setPriority(event.target.value)} required disabled={inputsLocked} />}
            </Field>
          ) : <div />}
          {proxiable ? (
            <label className="mt-6 flex items-center gap-2 text-sm">
              <Checkbox checked={proxied} onChange={event => setProxied(event.target.checked)} disabled={inputsLocked} />
              经 Cloudflare 代理（橙云）
            </label>
          ) : null}
        </div>
      </form>
    </Modal>
  );
}

function ImportZoneModal({
  open,
  account,
  onClose,
  onImported,
}: {
  readonly open: boolean;
  readonly account: DNSAccount | null;
  readonly onClose: () => void;
  readonly onImported: (zone: DNSZone, recordCount: number | null) => void;
}) {
  const [zones, setZones] = useState<readonly DNSProviderZone[] | null>(null);
  const [providerZoneId, setProviderZoneId] = useState("");
  const [loading, setLoading] = useState(false);
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !account) return;
    let active = true;
    setZones(null);
    setProviderZoneId("");
    setError(null);
    setLoading(true);
    dnsApi.providerZones(account.id)
      .then(result => {
        if (!active) return;
        setZones(result.data.items);
        setProviderZoneId(result.data.items[0]?.providerZoneId ?? "");
      })
      .catch(caught => { if (active) setError(describeApiError(caught)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [open, account]);

  async function submit() {
    if (!account || !providerZoneId || importing) return;
    setImporting(true);
    setError(null);
    try {
      const zoneResult = await dnsApi.syncZones(account.id, { providerZoneId });
      const zone = zoneResult.data.items[0];
      if (!zone) return setError("服务端没有返回已导入的 Zone。");
      let recordCount: number | null = null;
      try {
        const synced = await dnsApi.syncRecords(zone.id);
        recordCount = synced.data.items.length;
      } catch {
        recordCount = null;
      }
      onImported(zone, recordCount);
    } catch (caught) {
      setError(describeApiError(caught));
    } finally {
      setImporting(false);
    }
  }

  return (
    <Modal
      open={open}
      title="从 Cloudflare 导入 Zone"
      description={account ? `账号：${account.displayName}。列表来自 Cloudflare 实时读取，导入后会刷新该 Zone 的记录缓存。` : undefined}
      onClose={() => { if (!importing) onClose(); }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={importing}>取消</Button>
          <Button type="button" variant="primary" onClick={submit} loading={importing} disabled={!providerZoneId || loading}>导入并刷新记录</Button>
        </>
      }
    >
      <div className="space-y-4">
        {error ? <Notice tone="error" title="操作失败">{error}</Notice> : null}
        {loading ? <p className="text-sm text-muted-foreground">正在从 Cloudflare 读取可用 Zone…</p> : null}
        {!loading && zones && zones.length === 0 ? <p className="text-sm text-muted-foreground">Cloudflare 没有返回可导入的 Zone；已导入的 Zone 仍可在页面中选择。</p> : null}
        {zones && zones.length > 0 ? (
          <Field label="可用 Zone" hint="导入只写入本地缓存，不会修改 Cloudflare 上的 Zone。">
            {ids => (
              <Select id={ids.inputId} value={providerZoneId} onChange={event => setProviderZoneId(event.target.value)} disabled={importing}>
                {zones.map(zone => <option key={zone.providerZoneId} value={zone.providerZoneId}>{zone.name}{zone.status ? ` · ${zone.status}` : ""}</option>)}
              </Select>
            )}
          </Field>
        ) : null}
      </div>
    </Modal>
  );
}

export function DnsPage() {
  const session = useSession();
  const canManageAccounts = session.can("dns.account.manage");
  const canManageRecords = session.can("dns.record.manage");
  const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null);
  const [recordEditor, setRecordEditor] = useState<{ open: boolean; record: DNSRecord | null }>({ open: false, record: null });
  const [deletingRecord, setDeletingRecord] = useState<DNSRecord | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [importOpen, setImportOpen] = useState(false);

  const accounts = useResource(`dns:accounts:${session.viewEpoch}`, signal => dnsApi.accounts(signal).then(result => result.data));
  const accountItems = accounts.state.status === "ready" ? accounts.state.data.items : [];
  // 账号管理在系统设置中维护；DNS 页固定使用默认账号（无默认时取第一个）。
  const activeAccount = accountItems.find(account => account.isDefault) ?? accountItems[0] ?? null;
  const zones = useResource(
    `dns:zones:${session.viewEpoch}:${activeAccount?.id ?? "none"}`,
    signal => activeAccount ? dnsApi.zones(activeAccount.id, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(activeAccount) },
  );
  const zoneItems = zones.state.status === "ready" ? zones.state.data.items : [];
  const selectedZone = zoneItems.find(zone => zone.id === selectedZoneId) ?? zoneItems[0] ?? null;
  const records = useResource(
    `dns:records:${session.viewEpoch}:${selectedZone?.id ?? "none"}`,
    signal => selectedZone ? dnsApi.records(selectedZone.id, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(selectedZone) },
  );
  const recordItems = records.state.status === "ready" ? records.state.data.items : [];

  const deleteRecordAction = useAction((record: DNSRecord) => dnsApi.removeRecord(record.zoneId, record.id));
  const recordSyncAction = useAction((zoneId: string) => dnsApi.syncRecords(zoneId));

  async function syncRecords() {
    if (!selectedZone) return;
    const result = await recordSyncAction.run(selectedZone.id);
    if (result.ok) {
      setOutcome(`已从 Cloudflare 刷新 ${selectedZone.name} 的 ${result.data?.data.items.length ?? 0} 条记录（本地缓存，不等于远端改动）。`);
      records.reload();
    } else {
      setOutcome(`记录刷新失败：${describeApiError(result.error)}`);
    }
  }

  async function removeRecord() {
    if (!deletingRecord) return;
    const result = await deleteRecordAction.run(deletingRecord);
    if (result.ok) {
      setOutcome(`已删除记录 ${deletingRecord.type} ${deletingRecord.name}。`);
      setDeletingRecord(null);
      records.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="DNS 管理"
        description="首版仅 Cloudflare；账号、Zone 与记录严格按账号隔离。Token 只写不读，不进入浏览器存储。"
        badge={<Badge tone="primary" dot>Cloudflare</Badge>}
        actions={<Button type="button" onClick={() => { accounts.reload(); zones.reload(); records.reload(); }}><RefreshCw className="size-3.5" />刷新</Button>}
      />

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="group relative overflow-hidden rounded-2xl border border-border/80 bg-card p-4 shadow-xs transition-all hover:-translate-y-0.5 hover:shadow-md">
          <GlowingEffect tone="violet" />
          <div className="relative z-10 flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">Cloudflare 账号</span>
            <Cloud className="size-4 text-primary" />
          </div>
          <p className="relative z-10 mt-1.5 font-display text-2xl font-bold text-foreground">{accountItems.length}</p>
        </div>
        <div className="group relative overflow-hidden rounded-2xl border border-border/80 bg-card p-4 shadow-xs transition-all hover:-translate-y-0.5 hover:shadow-md">
          <GlowingEffect tone="blue" />
          <div className="relative z-10 flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">当前账号可见 Zone</span>
            <Globe2 className="size-4 text-emerald-600" />
          </div>
          <p className="relative z-10 mt-1.5 font-display text-2xl font-bold text-emerald-600">{zoneItems.length}</p>
        </div>
        <div className="group relative overflow-hidden rounded-2xl border border-border/80 bg-card p-4 shadow-xs transition-all hover:-translate-y-0.5 hover:shadow-md">
          <GlowingEffect tone="green" />
          <div className="relative z-10 flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">当前 Zone 解析记录</span>
            <ListTree className="size-4 text-sky-600" />
          </div>
          <p className="relative z-10 mt-1.5 font-display text-2xl font-bold text-sky-600">{recordItems.length}</p>
        </div>
      </div>

      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      {!activeAccount ? (
        <Notice tone="warning" title="还没有 Cloudflare 账号">
          先在 <a className="text-primary underline" href="#/settings">系统设置 → DNS 账号</a> 添加账号，再回到这里导入 Zone。
        </Notice>
      ) : null}
      <div className="space-y-4">
          <Panel
            title="Zone"
            description={activeAccount ? `当前账号：${activeAccount.displayName}（账号与凭据在系统设置中管理）` : "选择要查看的域名；导入会从 Cloudflare 实时读取列表，只写本地缓存。"}
            actions={
              <Button size="sm" disabled={!activeAccount || !canManageAccounts || session.writesLocked} onClick={() => setImportOpen(true)}>
                导入 Zone
              </Button>
            }
          >
            {!activeAccount ? <EmptyState title="请先在系统设置中添加账号" /> : (
              <AsyncPanel state={zones.state} onRetry={zones.reload} errorTitle="Zone 加载失败" empty={<EmptyState title="该账号还没有导入 Zone" description="点击右上角“导入 Zone”，从 Cloudflare 选择域名。" />} isEmpty={data => data.items.length === 0}>
                {data => (
                  <div className="px-5 py-4">
                    <Field label="已导入 Zone">
                      {ids => (
                        <Select
                          id={ids.inputId}
                          value={selectedZone?.id ?? ""}
                          onChange={event => setSelectedZoneId(event.target.value)}
                          className="max-w-md"
                        >
                          {data.items.map(zone => <option key={zone.id} value={zone.id}>{zone.name}{zone.status ? ` · ${zone.status}` : ""}</option>)}
                        </Select>
                      )}
                    </Field>
                    <p className="mt-2 text-xs text-muted-foreground">共 {data.items.length} 个 Zone；账号切换请到系统设置。</p>
                  </div>
                )}
              </AsyncPanel>
            )}
          </Panel>

          <Panel
            title="解析记录"
            description={selectedZone ? `Zone ${selectedZone.name} 的解析记录` : "选择 Zone 后显示记录"}
            actions={
              <>
                <Button size="sm" onClick={() => void syncRecords()} loading={recordSyncAction.pending} disabled={!selectedZone || !canManageRecords || session.writesLocked}>从 Cloudflare 刷新</Button>
                <Button size="sm" variant="primary" disabled={!selectedZone || !canManageRecords || session.writesLocked} onClick={() => setRecordEditor({ open: true, record: null })}>添加记录</Button>
              </>
            }
          >
            {!selectedZone ? <EmptyState title="请选择 Zone" /> : (
              <AsyncPanel state={records.state} onRetry={records.reload} errorTitle="DNS 记录加载失败" empty={<EmptyState title="该 Zone 暂无记录" />} isEmpty={data => data.items.length === 0}>
                {() => (
                  <DataTable
                    caption="DNS 解析记录"
                    rows={recordItems}
                    rowKey={record => record.id}
                    columns={[
                      { key: "type", header: "类型", className: "whitespace-nowrap", render: record => <Badge tone="primary">{record.type}</Badge> },
                      { key: "name", header: "名称", render: record => <span className="block max-w-[16rem] truncate" title={record.name}>{record.name}</span> },
                      { key: "content", header: "内容", render: record => <span className="block max-w-[22rem] truncate font-mono text-xs" title={record.content}>{record.content}</span> },
                      { key: "ttl", header: "TTL", className: "whitespace-nowrap", render: record => (record.ttl === undefined || record.ttl === 1 ? "自动" : record.ttl) },
                      { key: "proxied", header: "代理", className: "whitespace-nowrap", render: record => (record.proxied ? "是" : "否") },
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

      <RecordEditor
        open={recordEditor.open}
        record={recordEditor.record}
        zone={selectedZone}
        onClose={() => setRecordEditor({ open: false, record: null })}
        onSaved={record => {
          setRecordEditor({ open: false, record: null });
          setOutcome(`已保存记录 ${record.type} ${record.name}。`);
          records.reload();
        }}
      />
      <ImportZoneModal
        open={importOpen}
        account={activeAccount}
        onClose={() => setImportOpen(false)}
        onImported={(zone, recordCount) => {
          setImportOpen(false);
          setSelectedZoneId(zone.id);
          zones.reload();
          records.reload();
          setOutcome(`Zone 已导入：${zone.name}${recordCount === null ? "；记录刷新失败，可稍后重试。" : `，共 ${recordCount} 条记录。`}`);
        }}
      />
      <Modal
        open={deletingRecord !== null}
        title="删除 DNS 记录"
        description={deletingRecord ? `${deletingRecord.type} ${deletingRecord.name} → ${deletingRecord.content}` : undefined}
        onClose={() => setDeletingRecord(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeletingRecord(null)} disabled={deleteRecordAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeRecord} loading={deleteRecordAction.pending}>删除记录</Button>
          </>
        }
      >
        {deleteRecordAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteRecordAction.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">将从 Cloudflare 删除该条解析记录，并同步本地缓存。</p>
      </Modal>
    </div>
  );
}
