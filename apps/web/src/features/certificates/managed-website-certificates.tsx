import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";
import { tasksApi } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { CertificateCleanupPanel } from "./certificate-cleanup-panel";

interface CertificateTask {
  id: string;
  status: string;
  planHash: string;
  targets?: Record<string, string>;
}

interface Website {
  domain: string;
  autoRenew: boolean;
  generation: number;
  issue: CertificateTask | null;
  delivery: CertificateTask | null;
  nodeIds: string[] | null;
  notAfter?: string;
  renewAt?: string;
  installDirectory?: string;
}

interface NodeChoice {
  id: string;
  name: string;
  online: boolean;
  supported: boolean;
}

interface WebsiteList {
  configured: boolean;
  items: Website[];
  nodes: NodeChoice[];
}

const path = "/api/v1/certificates/websites";
const statusLabels: Record<string, string> = {
  awaiting_approval: "待确认",
  pending: "排队中",
  dispatched: "安装中",
  timed_out: "超时",
  queued: "排队中",
  running: "进行中",
  completed: "已完成",
  succeeded: "已安装",
  partial: "部分完成",
  partially_failed: "部分失败",
  failed: "失败",
  expired: "已过期",
  canceled: "已取消",
  cancelled: "已取消",
  unknown: "结果待核对",
};

function active(task: CertificateTask | null) {
  return task && ["awaiting_approval", "queued", "running", "cancelling"].includes(task.status);
}

export function ManagedWebsiteCertificates() {
  const session = useSession();
  const [domain, setDomain] = useState("");
  const [autoRenew, setAutoRenew] = useState(true);
  const [deliveryDomain, setDeliveryDomain] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [query, setQuery] = useState("");
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const websites = useResource(`managed-websites:${session.viewEpoch}`, signal =>
    apiClient.request<WebsiteList>(path, { signal }).then(result => result.data), {
    refreshIntervalMs: 5000,
  });
  const change = useAction((operation: string, hostname: string, data: Record<string, string>) =>
    apiClient.request(path, { method: "POST", body: {
      operation, domain: hostname, idempotencyKey: crypto.randomUUID(), ...data,
    } }),
  );
  const approve = useAction((task: CertificateTask) => tasksApi.approve(task.id, { planDigest: task.planHash, decision: "approve" }));
  const data = websites.state.status === "ready" ? websites.state.data : null;
  const delivery = data?.items.find(site => site.domain === deliveryDomain);
  const visibleNodes = data?.nodes.filter(node => node.name.toLowerCase().includes(query.trim().toLowerCase())) ?? [];
  const readyIds = new Set(data?.nodes.filter(node => node.supported).map(node => node.id));
  const eligibleSelection = selected.filter(id => readyIds.has(id));
  const busy = change.pending || approve.pending || session.writesLocked;
  const canApprove = session.can("task.approve") || session.can("task.manage");
  const error = change.error ?? approve.error;

  async function requestIssue(event: FormEvent) {
    event.preventDefault();
    if (!domain.trim() || busy) return;
    const result = await change.run("issue", domain, { autoRenew: String(autoRenew) });
    if (result.ok) {
      setDomain("");
      websites.refreshSilent();
    }
  }

  async function update(operation: string, site: Website, values: Record<string, string>) {
    const result = await change.run(operation, site.domain, values);
    if (result.ok) {
      websites.refreshSilent();
      if (operation === "deliver") setDeliveryDomain("");
    }
  }

  function confirm(task: CertificateTask) {
    void approve.run(task).then(() => websites.refreshSilent());
  }

  function openDelivery(site: Website) {
    setDeliveryDomain(site.domain);
    setSelected(site.nodeIds ?? []);
    setQuery("");
  }

  function taskStatus(task: CertificateTask, label: string) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span>{label} · {statusLabels[task.status] ?? task.status}</span>
        {task.status === "awaiting_approval" && canApprove
          ? <Button size="sm" disabled={busy} onClick={() => confirm(task)}>确认{label}</Button>
          : null}
      </div>
    );
  }

  return (
    <div className="space-y-5">
      <Panel title="网站证书" description="主控签发和续签。签发完成后可按名称选择节点下发。"
        actions={<Button size="sm" onClick={websites.refreshSilent}>刷新</Button>}>
        <AsyncPanel state={websites.state} onRetry={websites.reload} errorTitle="网站证书加载失败">
          {inventory => <div className="space-y-5 p-5">
            {!inventory.configured ? <Notice tone="warning" title="签发服务尚未配置">请在主控配置 ACME 账号和 DNS 授权。</Notice> : null}
            {error ? <Notice tone="error" title="操作未完成">
              {error.code === "WEBSITE_DNS_ZONE_UNAVAILABLE" ? "找不到可签发该域名的 DNS 区域。请先在 DNS 管理中同步域名。"
                : error.code === "WEBSITE_NODES_UNAVAILABLE" ? "选择中有节点暂不可下发，请刷新后重新选择。"
                : error.code === "WEBSITE_CERTIFICATE_ALREADY_ISSUED" ? "该域名已有有效证书，可直接选择节点下发。"
                : describeApiError(error)}
            </Notice> : null}
            <form className="flex flex-wrap items-end gap-3" onSubmit={requestIssue}>
              <Field label="网站域名">
                {({ inputId }) => <TextInput id={inputId} required value={domain} onChange={event => setDomain(event.target.value)} placeholder="site.example.com" />}
              </Field>
              <label className="flex min-h-9 items-center gap-2 text-sm">
                <Checkbox checked={autoRenew} onChange={event => setAutoRenew(event.target.checked)} />自动续签
              </label>
              <Button type="submit" variant="primary" disabled={busy || !inventory.configured}>申请证书</Button>
            </form>
            {inventory.items.length === 0 ? <EmptyState title="暂无网站证书" /> : null}
            {inventory.items.map(site => (
              <section key={site.domain} className="space-y-3 rounded-lg border border-border p-4">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 space-y-1">
                    <h3 className="break-all font-semibold">{site.domain}</h3>
                    {site.notAfter ? <p className="text-sm text-muted-foreground">
                      到期 {formatDateTime(site.notAfter)}
                      {site.autoRenew && site.renewAt ? `，计划续签 ${formatDateTime(site.renewAt)}` : ""}
                    </p> : null}
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <label className="flex items-center gap-2 text-sm">
                      <Checkbox aria-label={`${site.domain} 自动续签`} checked={site.autoRenew} disabled={busy}
                        onChange={event => void update("auto-renew", site, { autoRenew: String(event.target.checked) })} />
                      自动续签
                    </label>
                    {site.issue && !active(site.issue) && site.issue.status !== "completed" ?
                      <Button size="sm" disabled={busy} onClick={() => void update("issue", site, { autoRenew: String(site.autoRenew) })}>重新申请</Button> : null}
                    <Button size="sm" variant="primary" disabled={!site.notAfter || new Date(site.notAfter).getTime() <= Date.now() || busy || Boolean(active(site.delivery))} onClick={() => openDelivery(site)}>选择节点下发</Button>
                  </div>
                </div>
                {site.issue ? taskStatus(site.issue, "签发") : null}
                {site.delivery ? taskStatus(site.delivery, "下发") : null}
                {site.nodeIds?.length ? <ul className="flex flex-wrap gap-2 text-sm">
                  {site.nodeIds.map(id => {
                    const state = site.delivery?.targets?.[id];
                    return <li key={id} className="rounded border border-border px-2 py-1">
                      {inventory.nodes.find(node => node.id === id)?.name ?? id}{state ? ` · ${statusLabels[state] ?? state}` : ""}
                    </li>;
                  })}
                </ul> : null}
                {site.installDirectory && site.delivery?.status === "completed" ? <p className="break-all text-sm text-muted-foreground">
                  节点证书目录下的 <code>{site.installDirectory}</code> 提供 <code>fullchain.pem</code> 和 <code>privkey.pem</code>。
                </p> : null}
              </section>
            ))}
          </div>}
        </AsyncPanel>
      </Panel>
      <details open={cleanupOpen} onToggle={event => setCleanupOpen(event.currentTarget.open)} className="rounded-lg border border-border p-4">
        <summary className="cursor-pointer text-sm">DNS 验证记录清理</summary>
        {cleanupOpen ? <div className="mt-4"><CertificateCleanupPanel /></div> : null}
      </details>
      <Modal open={Boolean(delivery)} title="选择下发节点" description={delivery?.domain} onClose={() => setDeliveryDomain("")}>
        {delivery ? <form className="space-y-4" onSubmit={event => {
          event.preventDefault();
          if (eligibleSelection.length && !busy) void update("deliver", delivery, { nodeIds: eligibleSelection.join(",") });
        }}>
          {error ? <Notice tone="error" title="下发未提交">{describeApiError(error)}</Notice> : null}
          <Field label="搜索节点">{({ inputId }) => <TextInput id={inputId} value={query} onChange={event => setQuery(event.target.value)} />}</Field>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" size="sm" onClick={() => setSelected([...new Set([...selected, ...visibleNodes.filter(node => node.supported).map(node => node.id)])])}>全选匹配节点</Button>
            <Button type="button" size="sm" onClick={() => setSelected([])}>清空</Button>
            <span className="text-sm text-muted-foreground">已选 {eligibleSelection.length} 台</span>
          </div>
          <div className="max-h-64 space-y-2 overflow-auto rounded-lg border border-border p-3">
            {visibleNodes.length === 0 ? <p className="text-sm text-muted-foreground">暂无匹配节点</p> : null}
            {visibleNodes.map(node => <label key={node.id} className="flex items-center gap-2 text-sm">
              <Checkbox disabled={!node.supported} checked={node.supported && selected.includes(node.id)}
                onChange={event => setSelected(event.target.checked ? [...selected, node.id] : selected.filter(id => id !== node.id))} />
              <span>{node.name}</span>
              {!node.supported ? <span className="text-muted-foreground">{node.online ? "需更新 Agent 或启用证书目录" : "离线"}</span> : null}
            </label>)}
          </div>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" disabled={busy || !eligibleSelection.length}>提交下发</Button>
          </div>
        </form> : null}
      </Modal>
    </div>
  );
}
