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
import { dnsApi, dnsRecordTasksApi, type NodeCSRTask } from "@/lib/api/endpoints-infra";
import type { DNSAccount, DNSProviderZone, DNSRecord, DNSRecordInput, DNSZone } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";

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
  readonly onSaved: (task:NodeCSRTask) => void;
}) {
  const session = useSession();
  const [type, setType] = useState<DNSRecordInput["type"]>("A");
  const [name, setName] = useState("");
  const [content, setContent] = useState("");
  const [ttl, setTtl] = useState("");
  const [proxied, setProxied] = useState(false);
  const [priority, setPriority] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [operationKey,setOperationKey] = useState<string|null>(null);
  const action = useAction((body: DNSRecordInput,key:string) =>
    record && zone ? dnsRecordTasksApi.update(zone.id, record.id, body,key) : zone ? dnsRecordTasksApi.create(zone.id, body,key) : Promise.reject(new Error("no zone")),
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
    setOperationKey(null);
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
    const key=operationKey??globalThis.crypto.randomUUID();setOperationKey(key);
    const result = await action.run(body,key);
    if(result.ok&&result.data){setOperationKey(null);onSaved(result.data.data);}
    else if(result.error&&[400,401,403,422,429].includes(result.error.status))setOperationKey(null);
  }

  const disabled = !session.can("dns.record.manage") || session.writesLocked;
  const inputsLocked=disabled||action.pending||operationKey!==null;

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
          <Button type="submit" form="dns-record-editor" variant="primary" loading={action.pending} disabled={disabled}>登记审批任务</Button>
        </>
      }
    >
      <form id="dns-record-editor" onSubmit={submit} className="space-y-4">
        {operationKey?<Notice tone="warning" title="任务提交结果待核对">再次提交复用当前编号；请先在任务列表核对，不要重建未知结果操作。</Notice>:null}
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
          <Field label="TTL（可选）">{ids => <TextInput id={ids.inputId} type="number" min={1} value={ttl} onChange={event => setTtl(event.target.value)} disabled={inputsLocked} />}</Field>
        </div>
        <Field label="名称">{ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required disabled={inputsLocked} />}</Field>
        <Field label="内容">{ids => <TextInput id={ids.inputId} value={content} onChange={event => setContent(event.target.value)} required disabled={inputsLocked} />}</Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="优先级（MX 等，可选）">{ids => <TextInput id={ids.inputId} type="number" min={0} value={priority} onChange={event => setPriority(event.target.value)} disabled={inputsLocked} />}</Field>
          <label className="mt-6 flex items-center gap-2 text-sm">
            <Checkbox checked={proxied} onChange={event => setProxied(event.target.checked)} disabled={inputsLocked} />
            Cloudflare 代理（供应商支持时）
          </label>
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
  const [deleteKeys,setDeleteKeys]=useState<Record<string,string>>({});
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

  const deleteRecordAction = useAction((record: DNSRecord,key:string) => dnsRecordTasksApi.remove(record.zoneId, record.id,key));
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
    const scope=`${deletingRecord.zoneId}:${deletingRecord.id}`;
    const key=deleteKeys[scope]??globalThis.crypto.randomUUID();setDeleteKeys(previous=>({...previous,[scope]:key}));
    const result = await deleteRecordAction.run(deletingRecord,key);
    if (result.ok) {
      setDeletingRecord(null);
      setDeleteKeys(previous=>{const next={...previous};delete next[scope];return next;});
      setOutcome(`DNS 删除审批任务已登记：${result.data?.data.taskId??""}；审批及执行前记录尚未删除。`);
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
          <GlowingEffect />
          <div className="relative z-10 flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">Cloudflare 账号</span>
            <Cloud className="size-4 text-primary" />
          </div>
          <p className="relative z-10 mt-1.5 font-display text-2xl font-bold text-foreground">{accountItems.length}</p>
        </div>
        <div className="group relative overflow-hidden rounded-2xl border border-border/80 bg-card p-4 shadow-xs transition-all hover:-translate-y-0.5 hover:shadow-md">
          <GlowingEffect />
          <div className="relative z-10 flex items-center justify-between">
            <span className="text-xs font-medium text-muted-foreground">当前账号可见 Zone</span>
            <Globe2 className="size-4 text-emerald-600" />
          </div>
          <p className="relative z-10 mt-1.5 font-display text-2xl font-bold text-emerald-600">{zoneItems.length}</p>
        </div>
        <div className="group relative overflow-hidden rounded-2xl border border-border/80 bg-card p-4 shadow-xs transition-all hover:-translate-y-0.5 hover:shadow-md">
          <GlowingEffect />
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
        onSaved={task => {
          setRecordEditor({ open: false, record: null });
          setOutcome(`DNS 修改审批任务已登记：${task.taskId}；当前 ${task.status}，执行后再刷新记录。`);
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
            <Button type="button" variant="danger" onClick={removeRecord} loading={deleteRecordAction.pending}>登记删除审批任务</Button>
          </>
        }
      >
        {deleteRecordAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteRecordAction.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">登记后需先完成人工审批，再由统一任务删除所选记录；请核对实际执行结果。</p>
      </Modal>
    </div>
  );
}
