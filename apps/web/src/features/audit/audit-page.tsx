import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { auditApi } from "@/lib/api/endpoints";
import type { AuditEvent } from "@/lib/api/dto";
import { useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

interface Filters {
  readonly actorType: string;
  readonly action: string;
  readonly resourceType: string;
  readonly result: string;
  readonly from: string;
  readonly to: string;
}

const EMPTY: Filters = { actorType: "", action: "", resourceType: "", result: "", from: "", to: "" };

function isoOrUndefined(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

export function AuditPage() {
  const session = useSession();
  const [draft, setDraft] = useState<Filters>(EMPTY);
  const [filters, setFilters] = useState<Filters>(EMPTY);
  const [cursor, setCursor] = useState<string | null>(null);
  const [stack, setStack] = useState<readonly (string | null)[]>([]);
  const [selected, setSelected] = useState<AuditEvent | null>(null);

  const events = useResource(
    `audit:${session.viewEpoch}:${JSON.stringify(filters)}:${cursor ?? ""}`,
    signal =>
      auditApi
        .events(
          {
            ...(filters.actorType ? { actorType: filters.actorType } : {}),
            ...(filters.action ? { action: filters.action } : {}),
            ...(filters.resourceType ? { resourceType: filters.resourceType } : {}),
            ...(filters.result ? { result: filters.result } : {}),
            ...(isoOrUndefined(filters.from) ? { from: isoOrUndefined(filters.from) } : {}),
            ...(isoOrUndefined(filters.to) ? { to: isoOrUndefined(filters.to) } : {}),
            ...(cursor ? { cursor } : {}),
            limit: 100,
          },
          signal,
        )
        .then(result => result.data),
  );

  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFilters(draft);
    setCursor(null);
    setStack([]);
  }

  function clear() {
    setDraft(EMPTY);
    setFilters(EMPTY);
    setCursor(null);
    setStack([]);
  }

  const items = events.state.status === "ready" ? events.state.data.items : [];
  const hasMore = events.state.status === "ready" && Boolean(events.state.data.page.nextCursor);

  return (
    <div className="space-y-5">
      <PageHeader
        title="审计日志"
        description="查询脱敏的服务端审计事件；不展示秘密、凭据或原始文件内容。浏览器日志不能替代服务端审计。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">只读</span>}
      />
      <Panel title="筛选" description="GET /api/v1/audit/events；时间按 UTC 提交。">
        <form onSubmit={apply} className="grid gap-3 px-5 py-4 sm:grid-cols-2 lg:grid-cols-3">
          <Field label="主体类型">
            {ids => (
              <Select id={ids.inputId} value={draft.actorType} onChange={event => setDraft({ ...draft, actorType: event.target.value })}>
                <option value="">全部</option>
                <option value="user">user</option>
                <option value="agent">agent</option>
                <option value="plugin">plugin</option>
                <option value="ai">ai</option>
                <option value="system">system</option>
              </Select>
            )}
          </Field>
          <Field label="动作">
            {ids => <TextInput id={ids.inputId} value={draft.action} onChange={event => setDraft({ ...draft, action: event.target.value })} placeholder="如 node.revoke" />}
          </Field>
          <Field label="资源类型">
            {ids => <TextInput id={ids.inputId} value={draft.resourceType} onChange={event => setDraft({ ...draft, resourceType: event.target.value })} />}
          </Field>
          <Field label="结果">
            {ids => (
              <Select id={ids.inputId} value={draft.result} onChange={event => setDraft({ ...draft, result: event.target.value })}>
                <option value="">全部</option>
                <option value="allowed">allowed</option>
                <option value="denied">denied</option>
                <option value="success">success</option>
                <option value="failure">failure</option>
                <option value="partial">partial</option>
              </Select>
            )}
          </Field>
          <Field label="起始时间">
            {ids => <TextInput id={ids.inputId} type="datetime-local" value={draft.from} onChange={event => setDraft({ ...draft, from: event.target.value })} />}
          </Field>
          <Field label="结束时间">
            {ids => <TextInput id={ids.inputId} type="datetime-local" value={draft.to} onChange={event => setDraft({ ...draft, to: event.target.value })} />}
          </Field>
          <div className="flex flex-wrap items-end gap-2">
            <Button type="submit" variant="primary">应用筛选</Button>
            <Button type="button" onClick={clear}>清空</Button>
            <Button type="button" onClick={events.reload} loading={events.state.status === "loading"}>刷新</Button>
          </div>
        </form>
      </Panel>

      <Panel title="事件" description="只读脱敏视图；详情不额外请求，避免把敏感字段带入浏览器。">
        <AsyncPanel
          state={events.state}
          onRetry={events.reload}
          errorTitle="审计事件加载失败"
          empty={<EmptyState title="没有匹配的审计事件" description="服务端返回空集合；可调整筛选条件。" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <>
              <DataTable
                caption="审计事件"
                rows={items}
                rowKey={event => event.eventId}
                columns={[
                  { key: "time", header: "时间", render: event => formatDateTime(event.occurredAt) ?? "—" },
                  { key: "actor", header: "主体", render: event => `${event.actorType}${event.actorId ? ` · ${event.actorId}` : ""}` },
                  { key: "action", header: "动作", render: event => <span className="font-mono text-xs">{event.action}</span> },
                  { key: "resource", header: "资源", render: event => (event.resourceType ? `${event.resourceType}${event.resourceId ? ` · ${event.resourceId}` : ""}` : "—") },
                  { key: "result", header: "结果", render: event => event.result },
                  { key: "request", header: "请求", render: event => <span className="font-mono text-xs">{event.requestId}</span> },
                  {
                    key: "actions",
                    header: "操作",
                    render: event => <Button size="sm" onClick={() => setSelected(event)}>详情</Button>,
                  },
                ]}
              />
              <div className="flex items-center justify-between border-t px-5 py-3">
                <Button
                  type="button"
                  size="sm"
                  disabled={stack.length === 0}
                  onClick={() => {
                    const next = [...stack];
                    const previous = next.pop() ?? null;
                    setStack(next);
                    setCursor(previous);
                  }}
                >
                  上一页
                </Button>
                <span className="text-xs text-muted-foreground">本页 {items.length} 条 · 服务端审计为准</span>
                <Button
                  type="button"
                  size="sm"
                  disabled={!hasMore}
                  onClick={() => {
                    if (events.state.status !== "ready" || !events.state.data.page.nextCursor) return;
                    setStack(current => [...current, cursor]);
                    setCursor(events.state.data.page.nextCursor);
                  }}
                >
                  下一页
                </Button>
              </div>
            </>
          )}
        </AsyncPanel>
      </Panel>

      <Modal
        open={selected !== null}
        title="审计事件详情"
        description="只展示服务端返回的脱敏字段。"
        onClose={() => setSelected(null)}
        footer={<Button type="button" onClick={() => setSelected(null)}>关闭</Button>}
      >
        {selected ? (
          <dl className="space-y-3 text-sm">
            {[
              ["事件 ID", selected.eventId],
              ["时间", formatDateTime(selected.occurredAt) ?? "—"],
              ["主体", `${selected.actorType}${selected.actorId ? ` · ${selected.actorId}` : ""}`],
              ["动作", selected.action],
              ["资源", selected.resourceType ? `${selected.resourceType}${selected.resourceId ? ` · ${selected.resourceId}` : ""}` : "—"],
              ["结果", selected.result],
              ["请求 ID", selected.requestId],
              ["任务 ID", selected.taskId ?? "—"],
              ["脱敏摘要", selected.redactedSummary ?? "—"],
            ].map(([term, value]) => (
              <div key={term}>
                <dt className="text-xs text-muted-foreground">{term}</dt>
                <dd className="mt-0.5 break-words">{value}</dd>
              </div>
            ))}
          </dl>
        ) : null}
      </Modal>
    </div>
  );
}
