import { useMemo, useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Notice } from "@/components/ui/feedback";
import { Button } from "@/components/ui/button";
import { PageHeader, Panel } from "@/components/ui/panel";
import { TabBar } from "@/components/ui/table";
import { nodesApi } from "@/lib/api/endpoints";
import type { NodeDetail, NodeSummary } from "@/lib/api/dto";
import { ApiError } from "@/lib/api/errors";
import { useResource } from "@/lib/data/use-resource";
import { NodeListWorkspace } from "./node-list-workspace";
import { adaptLegacyNodes } from "./legacy-adapter";
import { LegacyNodeList } from "./legacy-node-list";
import { toRichNodes } from "./rich-adapter";

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
}: {
  readonly summaries: readonly NodeSummary[];
  readonly listKey: string;
  readonly enabled: boolean;
  readonly onListRetry: () => void;
}) {
  const summaryKey = summaries.map(summary => summary.id).join(",");
  const rich = useResource(
    `nodes:rich:${listKey}:${summaryKey}`,
    async signal => {
      const details = await loadDetails(summaries, signal);
      const items = toRichNodes(summaries, details);
      if (!items) throw new ApiError({ kind: "invalid", status: 200 });
      return items;
    },
    { enabled },
  );

  return (
    <div className="space-y-4">
      <Notice tone="warning" title="候选详情契约">
        详情字段来自 <code>GET /api/v1/nodes/{"{nodeId}"}</code>（G2 候选，D02–D04 未冻结）。列表 DTO 没有采样时间，
        富化视图不会把 CPU/内存数值显示为当前读数；兼容视图才按“无采样时间”展示这些合法数值。
      </Notice>
      <Panel title="富化节点视图" description="列表 + 只读详情；详情字段全部来自服务端，不补造状态或版本。">
        <AsyncPanel
          state={rich.state}
          onRetry={() => {
            onListRetry();
            rich.reload();
          }}
          loadingLabel="正在读取节点详情…"
          errorTitle="节点详情加载失败"
          empty={<p className="px-6 py-8 text-sm text-muted-foreground">没有可见节点。</p>}
          isEmpty={items => items.length === 0}
        >
          {items => (
            <div className="px-4 py-4">
              <NodeListWorkspace
                key={`${listKey}:${summaryKey}`}
                state={{ status: "ready", nodes: items }}
                title="节点列表（富化）"
                onRetry={() => {
                  onListRetry();
                  rich.reload();
                }}
              />
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
  const list = useResource(`nodes:list:${listKey}`, signal => nodesApi.list({ onlineOnly, signal }).then(result => result.data));

  const adapted = useMemo(() => {
    if (list.state.status !== "ready") return null;
    return adaptLegacyNodes(list.state.data);
  }, [list.state]);

  const items = list.state.status === "ready" ? list.state.data.items : [];
  const online = items.filter(node => node.online).length;

  return (
    <div className="space-y-5">
      <PageHeader
        title="节点列表"
        description="服务器资产、地区、云厂商、在线状态与资源。默认使用兼容接口字段；富化视图需要候选详情接口。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">只读</span>}
        actions={
          <>
            <label className="inline-flex h-9 items-center gap-2 rounded-md border bg-card px-3 text-sm">
              <input
                type="checkbox"
                checked={onlineOnly}
                onChange={event => setOnlineOnly(event.target.checked)}
                className="size-4 accent-[var(--primary)]"
              />
              仅在线节点（服务端过滤）
            </label>
            <Button type="button" onClick={list.reload} loading={list.state.status === "loading"}>
              刷新
            </Button>
          </>
        }
      />

      <Panel title="列表来源与范围" description="GET /api/v1/nodes；onlineOnly 直接作为查询参数，由服务端按当前身份返回可见范围。">
        <div className="grid gap-3 px-5 py-4 text-sm sm:grid-cols-3">
          <p>
            当前返回：<strong>{list.state.status === "ready" ? `${items.length} 个节点` : "尚未取得"}</strong>
          </p>
          <p>
            在线：<strong>{list.state.status === "ready" ? `${online} 个` : "—"}</strong>
          </p>
          <p className="text-muted-foreground">{onlineOnly ? "已请求仅在线集合；总数不代表全部资产。" : "未限制在线状态。"}</p>
        </div>
      </Panel>

      <Panel>
        <TabBar
          label="节点列表视图"
          active={view}
          onChange={setView}
          tabs={[
            { id: "legacy", label: "兼容视图（旧字段）" },
            { id: "rich", label: "富化视图（候选详情）" },
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
                    title="节点列表"
                    description="兼容接口只提供 id/name/online 与可空指标。离线行不显示历史百分比；在线数值标记“无采样时间”。"
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
              {() => (
                <RichNodesView
                  summaries={items}
                  listKey={listKey}
                  enabled={view === "rich"}
                  onListRetry={list.reload}
                />
              )}
            </AsyncPanel>
          )}
        </div>
      </Panel>
    </div>
  );
}
