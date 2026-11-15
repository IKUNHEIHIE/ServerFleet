import { useMemo, useState } from "react";
import { Activity, Filter, Plus, RefreshCw, Server } from "lucide-react";
import { useSession } from "@/app/session";
import { GlowingEffect } from "@/components/aceternity/glowing-effect";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/form";
import { Button, LinkButton } from "@/components/ui/button";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DefinitionList, TabBar } from "@/components/ui/table";
import { nodesApi } from "@/lib/api/endpoints";
import type { NodeDetail, NodeSummary } from "@/lib/api/dto";
import { ApiError } from "@/lib/api/errors";
import { useResource } from "@/lib/data/use-resource";
import { adaptLegacyNodes } from "./legacy-adapter";
import { LegacyNodeList } from "./legacy-node-list";

async function loadDetails(
  summaries: readonly NodeSummary[],
  signal: AbortSignal,
): Promise<readonly NodeDetail[]> {
  const results: NodeDetail[] = [];
  let cursor = 0;
  const workers = Array.from({ length: Math.min(6, summaries.length) }, async () => {
    while (cursor < summaries.length) {
      const index = cursor++;
      const summary = summaries[index];
      if (!summary || signal.aborted) return;
      const response = await nodesApi.detail(summary.id, signal);
      results[index] = response.data;
    }
  });
  await Promise.all(workers);
  return results;
}

function RichNodesView({
  summaries,
  listKey,
  enabled,
  onListRetry,
  updatedAt,
}: {
  readonly summaries: readonly NodeSummary[];
  readonly listKey: string;
  readonly enabled: boolean;
  readonly onListRetry: () => void;
  readonly updatedAt: number;
}) {
  const summaryKey = JSON.stringify([updatedAt, summaries]);
  const rich = useResource(
    `nodes:rich:${listKey}:${summaryKey}`,
    async signal => {
      const details = await loadDetails(summaries, signal);
      const ids = new Set(details.map(detail => detail.nodeId));
      if (ids.size !== summaries.length || summaries.some(summary => !ids.has(summary.id))) {
        throw new ApiError({ kind: "invalid", status: 200 });
      }
      return details;
    },
    { enabled },
  );

  return (
    <div className="space-y-4">
      <Panel title="节点详细状态" description="节点连接、平台与资源信息每 1 秒刷新。">
        <AsyncPanel
          state={rich.state}
          onRetry={() => {
            onListRetry();
            rich.reload();
          }}
          loadingLabel="正在读取节点详情…"
          errorTitle="节点详情加载失败"
          empty={<p className="px-6 py-8 text-sm text-muted-foreground">当前没有可见节点；节点已接入时，请检查当前账户的资源读取授权。</p>}
          isEmpty={items => items.length === 0}
        >
          {items => (
            <div className="grid gap-4 px-4 py-4 lg:grid-cols-2">
              {items.map(detail => (
                <Panel key={detail.nodeId} title={
                  <a className="text-primary hover:underline" href={`#/nodes/${encodeURIComponent(detail.nodeId)}`}>
                    {detail.displayName || detail.nodeId}
                  </a>
                }>
                  <DefinitionList items={[
                    { term: "状态", value: detail.status },
                    { term: "平台", value: detail.platform ?? "未知" },
                    { term: "Agent 版本", value: detail.agentVersion ?? "未知" },
                    { term: "最近在线", value: detail.lastSeenAt ?? "未知" },
                  ]} />
                </Panel>
              ))}
            </div>
          )}
        </AsyncPanel>
      </Panel>
    </div>
  );
}

export function NodesPage() {
  const session = useSession();
  const [onlineOnly, setOnlineOnly] = useState(false);
  const [view, setView] = useState<"legacy" | "rich">("legacy");
  const listKey = `${session.viewEpoch}:${onlineOnly}`;
  const list = useResource(
    `nodes:list:${listKey}`,
    signal => nodesApi.list({ onlineOnly, signal }).then(result => result.data),
    { refreshIntervalMs: 1000 },
  );

  const adapted = useMemo(() => {
    if (list.state.status !== "ready") return null;
    return adaptLegacyNodes(list.state.data);
  }, [list.state]);

  const items = list.state.status === "ready" && adapted?.ok ? list.state.data.items : [];
  const online = items.filter(node => node.online).length;

  return (
    <div className="space-y-5">
      <PageHeader
        title="节点列表"
        description="服务器资产、地区、云厂商与在线状态每 1 秒刷新；未接入或失联不等于关机。点击名称管理节点。"
        badge={<Badge tone="primary">自动刷新 · 1 秒</Badge>}
        actions={
          <>
            {session.can("node.manage") && !session.writesLocked ? (
              <LinkButton href="#/nodes/new" variant="primary"><Plus aria-hidden="true" className="size-3.5" />添加节点</LinkButton>
            ) : (
              <Button type="button" variant="primary" disabled>添加节点</Button>
            )}
            <label className="inline-flex h-9 cursor-pointer items-center gap-2 rounded-lg border border-border/80 bg-card px-3 text-sm font-medium shadow-2xs transition-colors hover:bg-secondary/70">
              <Checkbox
                checked={onlineOnly}
                onChange={event => setOnlineOnly(event.target.checked)}
              />
              仅在线节点（服务端过滤）
            </label>
            <Button type="button" onClick={list.reload} loading={list.state.status === "loading"}>
              <RefreshCw aria-hidden="true" className="size-3.5" />
              刷新
            </Button>
          </>
        }
      />

      <Panel title="节点概览" description="当前账户可见的节点，每 1 秒自动更新。">
        <div className="grid gap-3.5 px-5 py-4 text-sm sm:grid-cols-3">
          <GlowingEffect className="flex items-center gap-3 rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
            <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-primary-light text-primary">
              <Server className="size-4" />
            </span>
            <p>
              当前返回：<strong className="font-semibold text-foreground">{list.state.status === "ready" && adapted?.ok ? `${items.length} 个节点` : "尚未取得"}</strong>
            </p>
          </GlowingEffect>
          <GlowingEffect glowColor="rgba(22, 155, 104, 0.14)" className="flex items-center gap-3 rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
            <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-emerald-500/10 text-emerald-700">
              <Activity className="size-4" />
            </span>
            <p>
              在线：<strong className="font-semibold text-emerald-700">{list.state.status === "ready" && adapted?.ok ? `${online} 个` : "—"}</strong>
            </p>
          </GlowingEffect>
          <GlowingEffect glowColor="rgba(57, 132, 231, 0.14)" className="flex items-center gap-3 rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
            <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-xl bg-blue-500/10 text-blue-700">
              <Filter className="size-4" />
            </span>
            <p className="text-muted-foreground">{onlineOnly ? "已请求仅在线集合；总数不代表全部资产。" : "未限制在线状态。"}</p>
          </GlowingEffect>
        </div>
      </Panel>

      <Panel>
        <TabBar
          label="节点列表视图"
          active={view}
          onChange={setView}
          tabs={[
            { id: "legacy", label: "节点概览" },
            { id: "rich", label: "详细状态" },
          ]}
        />
        <div className="px-4 py-4">
          {view === "legacy" ? (
            <AsyncPanel
              state={list.state}
              onRetry={list.reload}
              loadingLabel="正在读取节点列表…"
              errorTitle="节点列表加载失败"
              isEmpty={() => false}
            >
              {() =>
                adapted && adapted.ok ? (
                  <LegacyNodeList
                    key={listKey}
                    rows={adapted.rows}
                    onChanged={list.reload}
                    canEdit={session.can("node.manage")}
                    canDelete={session.can("node.revoke")}
                    writesLocked={session.writesLocked}
                    viewEpoch={session.viewEpoch}
                    title="节点列表"
                  />
                ) : (
                  <div role="alert" className="rounded-lg border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
                    <p className="font-medium text-destructive">节点数据格式不符合兼容约定</p>
                    <p className="mt-1 text-muted-foreground">{adapted?.ok === false ? adapted.message : "响应未能解析。"}</p>
                    <p className="mt-2 text-xs text-muted-foreground">为避免展示部分/错误列表，本次不渲染任何行或计数。</p>
                  </div>
                )
              }
            </AsyncPanel>
          ) : (
            <AsyncPanel state={list.state} onRetry={list.reload} errorTitle="节点列表加载失败">
              {() => adapted?.ok ? (
                <RichNodesView
                  summaries={items}
                  updatedAt={list.state.status === "ready" ? list.state.updatedAt : 0}
                  listKey={listKey}
                  enabled={view === "rich"}
                  onListRetry={list.reload}
                />
              ) : (
                <p role="alert" className="px-4 py-3 text-sm text-destructive">节点数据格式不符合兼容约定</p>
              )}
            </AsyncPanel>
          )}
        </div>
      </Panel>
    </div>
  );
}
