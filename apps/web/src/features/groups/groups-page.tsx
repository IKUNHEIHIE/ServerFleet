import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { groupsApi, nodesApi } from "@/lib/api/endpoints";
import type { NodeGroup, NodeGroupResults } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { NodeSummaryTable } from "@/features/nodes/node-summary-table";

type ViewMode = "manual" | "automatic";
type Dimension = "region" | "provider" | "tag";
type StatusFilter = "all" | "online" | "offline";

const DIMENSION_LABEL: Record<Dimension, string> = { region: "地区", provider: "云厂商", tag: "标签" };

function GroupEditor({
  open,
  group,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly group: NodeGroup | null;
  readonly onClose: () => void;
  readonly onSaved: (group: NodeGroup) => void;
}) {
  const [name, setName] = useState(group?.name ?? "");
  const [description, setDescription] = useState(group?.description ?? "");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction(async (body: { name: string; description?: string }) => {
    if (group) {
      return groupsApi.update(group.id, { ...body, expectedRevision: group.revision });
    }
    return groupsApi.create(body);
  });

  useEffect(() => {
    setName(group?.name ?? "");
    setDescription(group?.description ?? "");
    setLocalError(null);
  }, [group, open]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) {
      setLocalError("分组名称不能为空。");
      return;
    }
    setLocalError(null);
    const result = await action.run({
      name: name.trim(),
      ...(description.trim() ? { description: description.trim() } : {}),
    });
    if (result.ok && result.data) onSaved(result.data.data);
  }

  return (
    <Modal
      open={open}
      title={group ? "编辑自定义分组" : "新建自定义分组"}
      description="普通分组只用于整理和筛选节点，不是服务组，不会影响网关调度、证书或 AI 授权。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="group-editor-form" variant="primary" loading={action.pending}>
            {group ? "保存修改" : "创建分组"}
          </Button>
        </>
      }
    >
      <form id="group-editor-form" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="分组名称" hint="同一管理范围内名称由服务端校验唯一性。">
          {ids => (
            <TextInput
              id={ids.inputId}
              value={name}
              onChange={event => setName(event.target.value)}
              maxLength={100}
              required
              disabled={action.pending}
            />
          )}
        </Field>
        <Field label="备注（可选）">
          {ids => (
            <TextArea
              id={ids.inputId}
              value={description}
              onChange={event => setDescription(event.target.value)}
              maxLength={1000}
              disabled={action.pending}
            />
          )}
        </Field>
        {group ? <p className="text-xs text-muted-foreground">提交会带 expectedRevision={group.revision}；并发修改由服务端拒绝。</p> : null}
      </form>
    </Modal>
  );
}

function DeleteGroup({
  group,
  onClose,
  onDeleted,
}: {
  readonly group: NodeGroup | null;
  readonly onClose: () => void;
  readonly onDeleted: () => void;
}) {
  const action = useAction((groupId: string) => groupsApi.remove(groupId));
  async function remove() {
    if (!group) return;
    const result = await action.run(group.id);
    if (result.ok) onDeleted();
  }
  return (
    <Modal
      open={group !== null}
      title="删除自定义分组"
      description={group ? `将删除分组“${group.name}”及其成员关系；不会删除任何节点，也不影响服务组。` : undefined}
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="button" variant="danger" onClick={remove} loading={action.pending}>确认删除</Button>
        </>
      }
    >
      {action.error ? <Notice tone="error" title="删除失败">{describeApiError(action.error)}</Notice> : null}
      <p className="text-sm text-muted-foreground">服务端会校验当前权限并记录审计。删除后成员关系不可恢复。</p>
    </Modal>
  );
}

function MembersDrawer({
  open,
  group,
  canManage,
  writesLocked,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly group: NodeGroup | null;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: (group: NodeGroup) => void;
}) {
  const session = useSession();
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [query, setQuery] = useState("");
  const [region, setRegion] = useState("all");
  const [provider, setProvider] = useState("all");
  const [online, setOnline] = useState<"all" | "online" | "offline">("all");
  const [acknowledgeRemoval, setAcknowledgeRemoval] = useState(false);
  const nodes = useResource(`group:members:candidates:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
    { enabled: open },
  );
  const save = useAction((groupId: string, memberNodeIds: readonly string[], expectedRevision: number) =>
    groupsApi.replaceMembers(groupId, { memberNodeIds, expectedRevision }),
  );

  useEffect(() => {
    if (!open || !group) return;
    setSelected(new Set(group.memberNodeIds ?? []));
    setQuery("");
    setRegion("all");
    setProvider("all");
    setOnline("all");
    setAcknowledgeRemoval(false);
  }, [open, group]);

  const candidates = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const regions = useMemo(() => [...new Set(candidates.map(node => node.region).filter((value): value is string => Boolean(value)))].sort(), [candidates]);
  const providers = useMemo(() => [...new Set(candidates.map(node => node.provider).filter((value): value is string => Boolean(value)))].sort(), [candidates]);
  const filtered = useMemo(() => {
    const normalised = query.trim().toLowerCase();
    return candidates.filter(node => {
      if (online !== "all" && (online === "online") !== node.online) return false;
      if (region !== "all" && node.region !== region) return false;
      if (provider !== "all" && node.provider !== provider) return false;
      if (normalised && !(`${node.name} ${node.id}`.toLowerCase().includes(normalised))) return false;
      return true;
    });
  }, [candidates, online, provider, query, region]);
  const missing = group?.memberNodeIds?.filter(id => !candidates.some(node => node.id === id)) ?? [];
  const allFilteredSelected = filtered.length > 0 && filtered.every(node => selected.has(node.id));
  const disabled = !canManage || writesLocked || !group || nodes.state.status !== "ready" || (missing.length > 0 && !acknowledgeRemoval);

  function toggle(nodeId: string, checked: boolean) {
    setSelected(current => {
      const next = new Set(current);
      if (checked) next.add(nodeId);
      else next.delete(nodeId);
      return next;
    });
  }

  function toggleAll() {
    setSelected(current => {
      const next = new Set(current);
      if (allFilteredSelected) {
        for (const node of filtered) next.delete(node.id);
      } else {
        for (const node of filtered) next.add(node.id);
      }
      return next;
    });
  }

  async function submit() {
    if (!group) return;
    const result = await save.run(group.id, [...selected], group.revision);
    if (result.ok && result.data) onSaved(result.data.data);
  }

  return (
    <Modal
      open={open}
      title={group ? `管理成员 · ${group.name}` : "管理成员"}
      description="保存时提交完整成员集合并带 expectedRevision。取消不改变成员关系；从分组移除不会删除节点。"
      onClose={() => {
        if (!save.pending) onClose();
      }}
      side
      wide
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={save.pending}>取消</Button>
          <Button type="button" variant="primary" onClick={submit} loading={save.pending} disabled={disabled}>
            保存成员（{selected.size}）
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {save.error ? <Notice tone="error" title="保存成员失败">{describeApiError(save.error)}</Notice> : null}
        {!canManage ? <Notice tone="warning" title="缺少成员管理权限">当前身份未返回分组管理权限，保存保持禁用。</Notice> : null}
        {writesLocked ? <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能保存。</Notice> : null}
        {missing.length > 0 ? (
          <Notice tone="warning" title="部分现有成员不在当前可见节点集合">
            有 {missing.length} 个成员 ID 未出现在当前返回的节点中（可能无权查看或已被移除）。继续保存会移除它们。
            <label className="mt-2 flex items-start gap-2 text-xs">
              <input
                type="checkbox"
                className="mt-0.5 size-4 accent-[var(--primary)]"
                checked={acknowledgeRemoval}
                onChange={event => setAcknowledgeRemoval(event.target.checked)}
              />
              我确认保存将移除这些不可见成员。
            </label>
          </Notice>
        ) : null}
        <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="候选节点加载失败">
          {() => (
            <>
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
                <Field label="搜索">
                  {ids => (
                    <TextInput
                      id={ids.inputId}
                      type="search"
                      value={query}
                      onChange={event => setQuery(event.target.value)}
                      placeholder="名称或 ID"
                    />
                  )}
                </Field>
                <Field label="地区">
                  {ids => (
                    <Select id={ids.inputId} value={region} onChange={event => setRegion(event.target.value)}>
                      <option value="all">全部地区</option>
                      {regions.map(value => <option key={value} value={value}>{value}</option>)}
                    </Select>
                  )}
                </Field>
                <Field label="云厂商">
                  {ids => (
                    <Select id={ids.inputId} value={provider} onChange={event => setProvider(event.target.value)}>
                      <option value="all">全部厂商</option>
                      {providers.map(value => <option key={value} value={value}>{value}</option>)}
                    </Select>
                  )}
                </Field>
                <Field label="在线状态">
                  {ids => (
                    <Select id={ids.inputId} value={online} onChange={event => setOnline(event.target.value as typeof online)}>
                      <option value="all">全部</option>
                      <option value="online">在线</option>
                      <option value="offline">离线</option>
                    </Select>
                  )}
                </Field>
              </div>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-xs text-muted-foreground">
                  已选 {selected.size} · 当前筛选 {filtered.length} / 共 {candidates.length}
                </p>
                <Button type="button" size="sm" onClick={toggleAll} disabled={filtered.length === 0}>
                  {allFilteredSelected ? "取消全选当前筛选" : "全选当前筛选"}
                </Button>
              </div>
              {filtered.length === 0 ? (
                <EmptyState title="没有匹配的节点" description="当前筛选条件下没有可选择的节点；不影响已保存的成员关系。" />
              ) : (
                <NodeSummaryTable
                  caption="分组成员候选节点"
                  rows={filtered}
                  selectedIds={selected}
                  onToggle={toggle}
                />
              )}
            </>
          )}
        </AsyncPanel>
      </div>
    </Modal>
  );
}

export function GroupsPage() {
  const session = useSession();
  const canManage = session.can("group.manage") || session.can("node.manage");
  const [view, setView] = useState<ViewMode>("manual");
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [dimension, setDimension] = useState<Dimension>("region");
  const [facetValue, setFacetValue] = useState<string | null>(null);
  const [searchDraft, setSearchDraft] = useState("");
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");
  const [cursor, setCursor] = useState<string | null>(null);
  const [cursorStack, setCursorStack] = useState<readonly (string | null)[]>([]);
  const [editor, setEditor] = useState<{ open: boolean; group: NodeGroup | null }>({ open: false, group: null });
  const [deleting, setDeleting] = useState<NodeGroup | null>(null);
  const [membersFor, setMembersFor] = useState<NodeGroup | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const groups = useResource(`groups:list:${session.viewEpoch}`, signal =>
    groupsApi.list(signal).then(result => result.data),
  );
  const facets = useResource(
    `groups:facets:${session.viewEpoch}:${dimension}`,
    signal => groupsApi.classifications(dimension, signal).then(result => result.data),
    { enabled: view === "automatic" },
  );

  const groupItems = groups.state.status === "ready" ? groups.state.data.items : [];
  const effectiveGroupId =
    selectedGroupId && groupItems.some(group => group.id === selectedGroupId)
      ? selectedGroupId
      : (groupItems[0]?.id ?? null);
  const selectedGroup = groupItems.find(group => group.id === effectiveGroupId) ?? null;
  const facetItems = facets.state.status === "ready" ? facets.state.data.items : [];
  const effectiveFacet = facetValue ?? facetItems[0]?.value ?? null;

  useEffect(() => {
    setCursor(null);
    setCursorStack([]);
  }, [view, effectiveGroupId, dimension, effectiveFacet]);

  const mode: "manual" | "automatic" | null =
    view === "manual" ? (effectiveGroupId ? "manual" : null) : effectiveFacet ? "automatic" : null;
  const results = useResource(
    `groups:results:${session.viewEpoch}:${mode ?? "none"}:${effectiveGroupId ?? ""}:${dimension}:${effectiveFacet ?? ""}:${search}:${status}:${cursor ?? ""}`,
    signal => {
      const query =
        mode === "manual"
          ? { mode: "manual" as const, groupId: effectiveGroupId ?? undefined }
          : { mode: "automatic" as const, dimension, value: effectiveFacet ?? undefined };
      return groupsApi
        .results(
          {
            ...query,
            ...(search ? { search } : {}),
            status,
            ...(cursor ? { cursor } : {}),
            limit: 50,
          },
          signal,
        )
        .then(response => response.data);
    },
    { enabled: mode !== null },
  );

  function resetPage() {
    setCursor(null);
    setCursorStack([]);
  }

  function switchView(next: ViewMode) {
    setView(next);
    resetPage();
  }

  function selectGroup(id: string) {
    setSelectedGroupId(id);
    resetPage();
  }

  function selectFacet(value: string) {
    setFacetValue(value);
    resetPage();
  }

  function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSearch(searchDraft.trim());
    resetPage();
  }

  function clearSearch() {
    setSearchDraft("");
    setSearch("");
    resetPage();
  }

  function nextPage() {
    if (results.state.status !== "ready" || !results.state.data.nextCursor) return;
    setCursorStack(stack => [...stack, cursor]);
    setCursor(results.state.data.nextCursor);
  }

  function previousPage() {
    if (cursorStack.length === 0) return;
    const stack = [...cursorStack];
    const previous = stack.pop() ?? null;
    setCursorStack(stack);
    setCursor(previous);
  }

  const total = results.state.status === "ready" ? results.state.data.total : null;
  const canGoNext = results.state.status === "ready" && Boolean(results.state.data.nextCursor);

  return (
    <div className="space-y-5">
      <PageHeader
        title="分组管理"
        description="自定义分组用于整理与筛选，可编辑成员；自动分类按节点属性实时计算，只读。二者都不是业务服务组。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">普通分组</span>}
        actions={
          <Button
            type="button"
            variant="primary"
            disabled={!canManage || session.writesLocked}
            onClick={() => setEditor({ open: true, group: null })}
          >
            新建自定义分组
          </Button>
        }
      />
      {outcome ? <Notice tone="success" title="操作完成" actions={<Button size="sm" onClick={() => setOutcome(null)}>知道了</Button>}>{outcome}</Notice> : null}
      {!canManage ? (
        <Notice tone="warning" title="成员写操作已锁定">
          当前身份未返回分组管理权限（`group.manage`/`node.manage`）。列表与结果仍按服务端可见范围显示。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能新建/编辑/删除分组或保存成员。</Notice>
      ) : null}

      <TabBar
        label="分组视图"
        active={view}
        onChange={switchView}
        tabs={[
          { id: "manual", label: "自定义分组" },
          { id: "automatic", label: "自动分类（只读）" },
        ]}
      />

      <div className="grid min-w-0 gap-5 lg:grid-cols-[19rem_minmax(0,1fr)]">
        <div className="min-w-0 space-y-4">
          {view === "manual" ? (
            <Panel
              title="自定义分组"
              description="GET /api/v1/groups；只返回人工建立的分组。"
              actions={<Button size="sm" onClick={groups.reload} loading={groups.state.status === "loading"}>刷新</Button>}
            >
              <AsyncPanel
                state={groups.state}
                onRetry={groups.reload}
                empty={<p className="px-5 py-6 text-sm text-muted-foreground">还没有自定义分组。</p>}
                isEmpty={data => data.items.length === 0}
              >
                {data => (
                  <ul className="divide-y">
                    {data.items.map(group => {
                      const active = group.id === effectiveGroupId;
                      return (
                        <li key={group.id}>
                          <button
                            type="button"
                            onClick={() => selectGroup(group.id)}
                            aria-current={active ? "true" : undefined}
                            className={`w-full px-4 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "bg-primary/10" : "hover:bg-secondary"}`}
                          >
                            <span className="flex items-center justify-between gap-2">
                              <span className="truncate font-medium">{group.name}</span>
                              <span className="text-xs text-muted-foreground">{group.memberCount ?? group.memberNodeIds?.length ?? 0} 台</span>
                            </span>
                            {group.description ? <span className="mt-0.5 block truncate text-xs text-muted-foreground">{group.description}</span> : null}
                            <span className="mt-1 block text-xs text-muted-foreground">revision {group.revision}</span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </AsyncPanel>
            </Panel>
          ) : (
            <Panel title="自动分类" description="GET /api/v1/groups/classifications；只读、按可见节点实时计算。">
              <div className="px-3 pt-3">
                <TabBar
                  label="自动分类维度"
                  active={dimension}
                  onChange={value => {
                    setDimension(value);
                    setFacetValue(null);
                    resetPage();
                  }}
                  tabs={[
                    { id: "region", label: "地区" },
                    { id: "provider", label: "云厂商" },
                    { id: "tag", label: "标签" },
                  ]}
                />
              </div>
              <AsyncPanel
                state={facets.state}
                onRetry={facets.reload}
                empty={<p className="px-5 py-6 text-sm text-muted-foreground">当前维度没有分类值。</p>}
                isEmpty={data => data.items.length === 0}
              >
                {data => (
                  <ul className="divide-y">
                    {data.items.map(item => {
                      const active = item.value === effectiveFacet;
                      return (
                        <li key={item.value}>
                          <button
                            type="button"
                            onClick={() => selectFacet(item.value)}
                            aria-current={active ? "true" : undefined}
                            className={`flex w-full items-center justify-between gap-2 px-4 py-3 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "bg-primary/10" : "hover:bg-secondary"}`}
                          >
                            <span className="truncate">{item.label ?? item.value}</span>
                            <span className="text-xs text-muted-foreground">{item.nodeCount} 台</span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                )}
              </AsyncPanel>
              <p className="border-t px-4 py-3 text-xs text-muted-foreground">分类值不可创建、重命名或删除；修改归类请编辑节点属性。</p>
            </Panel>
          )}
        </div>

        <Panel
          title={
            view === "manual"
              ? selectedGroup
                ? `成员节点 · ${selectedGroup.name}`
                : "成员节点"
              : effectiveFacet
                ? `${DIMENSION_LABEL[dimension]} · ${facetItems.find(item => item.value === effectiveFacet)?.label ?? effectiveFacet}`
                : "自动分类节点"
          }
          description={
            view === "manual"
              ? "统一读取 GET /api/v1/groups/node-results；搜索与状态筛选均由服务端执行。"
              : "自动分类只读；结果按当前用户可见节点范围计算。"
          }
          actions={
            view === "manual" && selectedGroup ? (
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={!canManage || session.writesLocked}
                  onClick={() => setMembersFor(selectedGroup)}
                >
                  管理成员
                </Button>
                <Button
                  size="sm"
                  disabled={!canManage || session.writesLocked}
                  onClick={() => setEditor({ open: true, group: selectedGroup })}
                >
                  编辑
                </Button>
                <Button
                  size="sm"
                  variant="danger"
                  disabled={!canManage || session.writesLocked}
                  onClick={() => setDeleting(selectedGroup)}
                >
                  删除
                </Button>
              </div>
            ) : null
          }
        >
          <div className="border-b px-5 py-3">
            <form onSubmit={applySearch} className="flex flex-wrap items-end gap-2">
              <div className="min-w-48 flex-1">
                <label htmlFor="group-node-search" className="block text-xs font-medium text-muted-foreground">
                  搜索名称或 IP
                </label>
                <input
                  id="group-node-search"
                  type="search"
                  value={searchDraft}
                  onChange={event => setSearchDraft(event.target.value)}
                  placeholder="服务端搜索当前分组"
                  className="mt-1 h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                />
              </div>
              <div>
                <label htmlFor="group-node-status" className="block text-xs font-medium text-muted-foreground">状态</label>
                <select
                  id="group-node-status"
                  value={status}
                  onChange={event => {
                    setStatus(event.target.value as StatusFilter);
                    resetPage();
                  }}
                  className="mt-1 h-9 rounded-md border border-input bg-background px-3 text-sm"
                >
                  <option value="all">全部</option>
                  <option value="online">在线</option>
                  <option value="offline">离线</option>
                </select>
              </div>
              <Button type="submit" size="sm">搜索</Button>
              <Button type="button" size="sm" variant="ghost" onClick={clearSearch} disabled={!search && !searchDraft}>
                清空
              </Button>
            </form>
          </div>

          {mode === null ? (
            <EmptyState
              title={view === "manual" ? "请选择或新建一个分组" : "请选择一个分类值"}
              description={view === "manual" ? "右侧只在选中分组后读取成员结果。" : "自动分类值来自服务端可见节点；没有分类时不发请求。"}
            />
          ) : (
            <>
              <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-2 text-xs text-muted-foreground">
                <span>本页 {results.state.status === "ready" ? results.state.data.items.length : "—"} 条 · 服务端总数 {total ?? "—"}</span>
                <span>{cursor ? "使用游标翻页" : "第一页"}</span>
              </div>
              <AsyncPanel
                state={results.state}
                onRetry={results.reload}
                errorTitle="分组节点加载失败"
                empty={<EmptyState title="该分组暂无节点" description="服务端返回空集合；不是本地搜索无命中。" />}
                isEmpty={(data: NodeGroupResults) => data.items.length === 0}
              >
                {data => (
                  <NodeSummaryTable
                    caption={view === "manual" ? "自定义分组成员节点" : "自动分类节点"}
                    rows={data.items}
                    empty={<EmptyState title="该分组暂无节点" description="服务端返回空集合；不是本地搜索无命中。" />}
                  />
                )}
              </AsyncPanel>
              <div className="flex items-center justify-between border-t px-5 py-3">
                <Button type="button" size="sm" onClick={previousPage} disabled={cursorStack.length === 0}>上一页</Button>
                <span className="text-xs text-muted-foreground">搜索与状态由服务端过滤；客户端选中不代表授权。</span>
                <Button type="button" size="sm" onClick={nextPage} disabled={!canGoNext}>下一页</Button>
              </div>
            </>
          )}
        </Panel>
      </div>

      <GroupEditor
        open={editor.open}
        group={editor.group}
        onClose={() => setEditor({ open: false, group: null })}
        onSaved={group => {
          setEditor({ open: false, group: null });
          setSelectedGroupId(group.id);
          setOutcome(editor.group ? "分组已更新，已重新读取服务端结果。" : "分组已创建，已选择新分组。");
          groups.reload();
          resetPage();
        }}
      />
      <DeleteGroup
        group={deleting}
        onClose={() => setDeleting(null)}
        onDeleted={() => {
          setDeleting(null);
          setSelectedGroupId(null);
          setOutcome("分组已删除；节点和服务组未受影响。");
          groups.reload();
          results.reload();
        }}
      />
      <MembersDrawer
        open={membersFor !== null}
        group={membersFor}
        canManage={canManage}
        writesLocked={session.writesLocked}
        onClose={() => setMembersFor(null)}
        onSaved={group => {
          setMembersFor(null);
          setSelectedGroupId(group.id);
          setOutcome("成员集合已保存；服务端返回的 revision 已更新。");
          groups.reload();
          results.reload();
        }}
      />
    </div>
  );
}
