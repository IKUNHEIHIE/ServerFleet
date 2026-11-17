import { useEffect, useState } from "react";
import { ArrowLeft, Bot, LogOut, RefreshCw, Server } from "lucide-react";
import { matchPath, useLocation } from "@/app/router";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { AsyncPanel } from "@/components/ui/async-panel";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { nodesApi } from "@/lib/api/endpoints";
import { useResource } from "@/lib/data/use-resource";
import { describeApiError } from "@/lib/api/errors";
import { formatBytes, formatDateTime } from "@/lib/format";
import { adaptLegacyNodes } from "@/features/nodes/legacy-adapter";
import type { LegacyNodeRow, LegacyMetric } from "@/features/nodes/legacy-types";
import { AIPage } from "@/features/ai/ai-pages";
import "./mobile-app.css";

function Metric({ label, metric }: { readonly label: string; readonly metric: LegacyMetric | undefined }) {
  const value = metric?.kind === "value" ? metric.percent : null;
  return <div className="mobile-metric"><div><span>{label}</span><strong>{value === null ? "—" : value.toFixed(1) + "%"}</strong></div>
    <div className="mobile-meter" aria-label={label + "占用"}><span style={{ width: value === null ? 0 : value + "%", background: value !== null && value >= 85 ? "#dc625d" : undefined }} /></div></div>;
}

function NodeCard({ node }: { readonly node: LegacyNodeRow }) {
  return <a href={"#/app/nodes/" + encodeURIComponent(node.id)} className="mobile-node">
    <div className="mobile-node-title"><span className="mobile-node-icon"><Server size={19} /></span><div><h2>{node.name}</h2><p>{[node.region, node.provider].filter(Boolean).join(" · ") || node.id}</p></div><span className={"mobile-status " + (node.online ? "is-online" : "")}>{node.online ? "在线" : "失联"}</span></div>
    <div className="mobile-metrics"><Metric label="CPU" metric={node.cpu} /><Metric label="内存" metric={node.memory} /><Metric label="磁盘" metric={node.disk} /></div>
    <div className="mobile-network"><span>↓ {formatBytes(node.downloadBps ?? undefined) ?? "—"}/s</span><span>↑ {formatBytes(node.uploadBps ?? undefined) ?? "—"}/s</span><span>{node.sampledAt ? "采样 " + (formatDateTime(node.sampledAt) ?? "—") : "暂无采样"}</span></div>
  </a>;
}

function MobileNodes({ nodeId }: { readonly nodeId?: string }) {
  const session = useSession();
  const [query, setQuery] = useState("");
  const list = useResource("mobile:nodes:" + session.viewEpoch, signal => nodesApi.list({ signal }).then(result => result.data), { refreshIntervalMs: 5000 });
  const detail = useResource("mobile:detail:" + session.viewEpoch + ":" + nodeId, async signal => {
    const [info, hardware] = await Promise.all([nodesApi.detail(nodeId!, signal), nodesApi.capabilities(nodeId!, signal)]);
    return { info: info.data, hardware: hardware.data };
  }, { enabled: Boolean(nodeId), refreshIntervalMs: 5000 });
  const adapted = list.state.status === "ready" ? adaptLegacyNodes(list.state.data) : null;
  const rows = adapted?.ok ? adapted.rows : [];
  const selected = nodeId ? rows.find(row => row.id === nodeId) : null;
  return <div className="mobile-nodes">
    <div className="mobile-section-title"><div>{nodeId ? <a className="mobile-back" href="#/app/nodes"><ArrowLeft size={16} />所有节点</a> : <><p className="mobile-eyebrow">YOUR FLEET</p><h1>节点概览</h1></>}<p>{nodeId ? "节点信息与实时资源" : "每 5 秒刷新 · " + rows.filter(row => row.online).length + " 个在线"}</p></div><Button aria-label="刷新节点" variant="ghost" onClick={() => { list.reload(); if (nodeId) detail.reload(); }}><RefreshCw size={18} /></Button></div>
    {!nodeId ? <input className="mobile-search" aria-label="搜索节点" placeholder="搜索名称、地区或云厂商" value={query} onChange={event => setQuery(event.target.value)} /> : null}
    <AsyncPanel state={list.state} onRetry={list.reload} errorTitle="节点加载失败" empty={<EmptyState title="暂无可见节点" />} isEmpty={data => data.items.length === 0}>
      {() => <>{adapted && !adapted.ok ? <Notice tone="error" title="节点数据格式错误">{adapted.message}</Notice> : null}
        {list.state.status === "ready" && list.state.refreshError ? <Notice tone="warning" title="刷新失败，正在显示上次数据">{describeApiError(list.state.refreshError)}</Notice> : null}
        {nodeId ? selected ? <NodeCard node={selected} /> : <EmptyState title="节点不可见" description="节点已被移除，或当前账号无权查看。" /> : <>{rows.filter(row => [row.name, row.region, row.provider].join(" ").toLowerCase().includes(query.trim().toLowerCase())).map(row => <NodeCard key={row.id} node={row} />)}{rows.length > 0 && !rows.some(row => [row.name, row.region, row.provider].join(" ").toLowerCase().includes(query.trim().toLowerCase())) ? <EmptyState title="没有匹配的节点" /> : null}</>}
      </>}
    </AsyncPanel>
    {nodeId ? <AsyncPanel state={detail.state} onRetry={detail.reload} errorTitle="节点详情加载失败" isEmpty={() => false}>
      {({ info, hardware }) => <section className="mobile-info"><h2>节点信息</h2><dl>{[
        ["地址", info.address], ["系统", info.platform], ["Agent", info.agentVersion], ["CPU", hardware.cpuModel],
        ["核心数", hardware.cpuCores ? hardware.cpuCores + " 核" : undefined], ["内存", formatBytes(hardware.memoryTotalBytes)],
        ["存储", formatBytes(hardware.diskTotalBytes)], ["最近在线", formatDateTime(info.lastSeenAt)], ["节点 ID", info.nodeId],
      ].map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value || "未采集"}</dd></div>)}</dl></section>}
    </AsyncPanel> : null}
  </div>;
}

export function MobileApp() {
  const location = useLocation();
  const session = useSession();
  const isAI = location.path === "/app/ai";
  const detail = matchPath("/app/nodes/:nodeId", location.path);
  const known = isAI || location.path === "/app" || location.path === "/app/nodes" || detail !== null;
  const [logoutError, setLogoutError] = useState(false);
  useEffect(() => { document.title = (isAI ? "AI 运维" : "节点") + " · Server Fleet"; }, [isAI]);
  return <div className="mobile-app">
    <header className="mobile-header"><div className="mobile-brand"><span>SF</span><div><strong>Server Fleet</strong><small>{window.location.host}</small></div></div><Button aria-label="退出登录" title="退出登录" variant="ghost" onClick={() => { setLogoutError(false); void session.logout().then(result => setLogoutError(!result.ok)).catch(() => setLogoutError(true)); }}><LogOut size={18} /></Button></header>
    {logoutError ? <Notice tone="error" title="退出失败">请检查连接后重试。</Notice> : null}
    <main className={isAI ? "mobile-main mobile-ai-main" : "mobile-main"}>{!known ? <EmptyState title="页面不存在" description="请从底部选择节点或 AI 运维。" /> : isAI ? <AIPage mobile /> : <MobileNodes nodeId={detail?.params.nodeId} />}</main>
    <nav className="mobile-nav" aria-label="App 导航"><a href="#/app/nodes" aria-current={!isAI ? "page" : undefined}><Server size={21} /><span>节点</span></a><a href="#/app/ai" aria-current={isAI ? "page" : undefined}><Bot size={22} /><span>AI 运维</span></a></nav>
  </div>;
}
