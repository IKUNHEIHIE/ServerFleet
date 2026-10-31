import { useEffect, useMemo, useState, type FormEvent } from "react";
import { navigate } from "@/app/router";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi, serviceGroupsApi, type ServiceGroupInput } from "@/lib/api/endpoints";
import type { NodeSummary, ServiceGroup, ServiceGroupDispatchPreview } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { NodeSummaryTable } from "@/features/nodes/node-summary-table";

interface EditorState {
  readonly open: boolean;
  readonly group: ServiceGroup | null;
}

function protocolOptions() {
  return (
    <>
      <option value="http">HTTP</option>
      <option value="https">HTTPS</option>
      <option value="tcp">TCP</option>
      <option value="udp">UDP</option>
    </>
  );
}

function ServiceGroupEditor({
  state,
  onClose,
  onSaved,
}: {
  readonly state: EditorState;
  readonly onClose: () => void;
  readonly onSaved: (group: ServiceGroup) => void;
}) {
  const session = useSession();
  const group = state.group;
  const [name, setName] = useState(group?.name ?? "");
  const [description, setDescription] = useState(group?.description ?? "");
  const [port, setPort] = useState(String(group?.service?.port ?? 80));
  const [protocol, setProtocol] = useState(group?.service?.protocol ?? "http");
  const [healthCheck, setHealthCheck] = useState(group?.service?.healthCheck ?? "");
  const [lbEnabled, setLbEnabled] = useState(group?.loadBalancing.enabled ?? false);
  const [strategy, setStrategy] = useState(group?.loadBalancing.strategy ?? "round_robin");
  // 真实 Controller 按成员节点集合授权（internal/servicegroups Service.Create/Update 以
  // members 作为授权范围）；创建时必须显式选择成员，空成员集合会被服务端拒绝。
  const [memberIds, setMemberIds] = useState<ReadonlySet<string>>(new Set(group?.memberNodeIds ?? []));
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction((body: ServiceGroupInput) =>
    group ? serviceGroupsApi.patch(group.id, body) : serviceGroupsApi.create(body),
  );
  const memberOptions = useResource(`service-group-editor:nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data.items),
  );

  useEffect(() => {
    if (!state.open) return;
    setName(group?.name ?? "");
    setDescription(group?.description ?? "");
    setPort(String(group?.service?.port ?? 80));
    setProtocol(group?.service?.protocol ?? "http");
    setHealthCheck(group?.service?.healthCheck ?? "");
    setLbEnabled(group?.loadBalancing.enabled ?? false);
    setStrategy(group?.loadBalancing.strategy ?? "round_robin");
    setMemberIds(new Set(group?.memberNodeIds ?? []));
    setLocalError(null);
  }, [state.open, group]);

  function toggleMember(nodeId: string, checked: boolean) {
    setMemberIds(current => {
      const next = new Set(current);
      if (checked) next.add(nodeId);
      else next.delete(nodeId);
      return next;
    });
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!name.trim()) {
      setLocalError("服务组名称不能为空。");
      return;
    }
    const numericPort = Number(port);
    if (!Number.isInteger(numericPort) || numericPort < 1 || numericPort > 65535) {
      setLocalError("服务端口必须是 1–65535 的整数。");
      return;
    }
    setLocalError(null);
    const body: ServiceGroupInput = {
      name: name.trim(),
      ...(description.trim() ? { description: description.trim() } : {}),
      memberNodeIds: [...memberIds],
      service: { port: numericPort, protocol, ...(healthCheck.trim() ? { healthCheck: healthCheck.trim() } : {}) },
      loadBalancing: { enabled: lbEnabled, strategy },
      ...(group ? { expectedRevision: group.revision } : {}),
    };
    const result = await action.run(body);
    if (result.ok && result.data) onSaved(result.data.data);
  }

  return (
    <Modal
      open={state.open}
      title={group ? "编辑服务组" : "新建服务组"}
      description="服务组是独立业务资源，可用于网关后端、任务目标、证书下发与 AI 授权；不是普通分组。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="service-group-editor" variant="primary" loading={action.pending}>
            {group ? "保存修改" : "创建服务组"}
          </Button>
        </>
      }
    >
      <form id="service-group-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="服务组名称">
          {ids => <TextInput id={ids.inputId} value={name} onChange={event => setName(event.target.value)} required maxLength={100} />}
        </Field>
        <Field label="用途/备注（可选）">
          {ids => <TextArea id={ids.inputId} value={description} onChange={event => setDescription(event.target.value)} maxLength={1000} />}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="服务端口">
            {ids => <TextInput id={ids.inputId} type="number" min={1} max={65535} value={port} onChange={event => setPort(event.target.value)} required />}
          </Field>
          <Field label="协议">
            {ids => (
              <Select id={ids.inputId} value={protocol} onChange={event => setProtocol(event.target.value)}>
                {protocolOptions()}
              </Select>
            )}
          </Field>
        </div>
        <Field label="健康检查说明（可选）">
          {ids => <TextInput id={ids.inputId} value={healthCheck} onChange={event => setHealthCheck(event.target.value)} maxLength={200} />}
        </Field>
        <div className="rounded-lg border px-4 py-3">
          <label className="flex items-center gap-2 text-sm font-medium">
            <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={lbEnabled} onChange={event => setLbEnabled(event.target.checked)} />
            启用流量调度
          </label>
          <p className="mt-1 text-xs text-muted-foreground">未启用时，服务组只是资源集合，不参与网关负载均衡；默认不启用。</p>
          <Field label="调度策略">
            {ids => (
              <Select id={ids.inputId} value={strategy} onChange={event => setStrategy(event.target.value)} disabled={!lbEnabled}>
                <option value="round_robin">轮询</option>
                <option value="least_connections">最少连接</option>
                <option value="none">不调度</option>
              </Select>
            )}
          </Field>
        </div>
        <fieldset className="max-h-56 overflow-y-auto rounded-lg border px-4 py-3">
          <legend className="px-1 text-sm font-medium">成员节点（{memberIds.size} 已选）</legend>
          <p className="mb-1 text-xs text-muted-foreground">
            服务端按成员节点集合校验创建/修改权限；至少选择一个已授权节点。
          </p>
          <AsyncPanel
            state={memberOptions.state}
            onRetry={memberOptions.reload}
            errorTitle="节点列表加载失败"
            empty={<p className="py-2 text-sm text-muted-foreground">当前没有可见节点；请先在节点授权中获得 nodes.read。</p>}
            isEmpty={data => data.length === 0}
          >
            {data => (
              <>
                {data.map(node => (
                  <label key={node.id} className="mt-1 flex items-center gap-2 text-sm">
                    <input
                      type="checkbox"
                      className="size-4 accent-[var(--primary)]"
                      checked={memberIds.has(node.id)}
                      onChange={event => toggleMember(node.id, event.target.checked)}
                    />
                    {node.name.trim() || node.id} {node.online ? "" : "（离线）"}
                  </label>
                ))}
              </>
            )}
          </AsyncPanel>
        </fieldset>
        <p className="text-xs text-muted-foreground">成员创建后仍可在详情页继续管理。</p>
      </form>
    </Modal>
  );
}

function MemberEditor({
  group,
  open,
  canManage,
  writesLocked,
  onClose,
  onSaved,
}: {
  readonly group: ServiceGroup | null;
  readonly open: boolean;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onClose: () => void;
  readonly onSaved: (group: ServiceGroup) => void;
}) {
  const session = useSession();
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [query, setQuery] = useState("");
  const [acknowledge, setAcknowledge] = useState(false);
  const nodes = useResource(`servicegroup:members:nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
    { enabled: open },
  );
  const save = useAction((id: string, memberNodeIds: readonly string[], expectedRevision: number) =>
    serviceGroupsApi.replaceMembers(id, { memberNodeIds, expectedRevision }),
  );

  useEffect(() => {
    if (!open || !group) return;
    setSelected(new Set(group.memberNodeIds));
    setAcknowledge(false);
    setQuery("");
  }, [open, group]);

  const candidates = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const filtered = useMemo(() => {
    const normalised = query.trim().toLowerCase();
    return normalised ? candidates.filter(node => `${node.name} ${node.id}`.toLowerCase().includes(normalised)) : candidates;
  }, [candidates, query]);
  const missing = group?.memberNodeIds.filter(id => !candidates.some(node => node.id === id)) ?? [];
  const disabled = !canManage || writesLocked || !group || nodes.state.status !== "ready" || (missing.length > 0 && !acknowledge);

  function toggle(nodeId: string, checked: boolean) {
    setSelected(current => {
      const next = new Set(current);
      if (checked) next.add(nodeId);
      else next.delete(nodeId);
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
      title={group ? `服务组成员 · ${group.name}` : "服务组成员"}
      description="保存会原子替换完整成员集合并带 expectedRevision；不会删除节点。"
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
      <div className="space-y-3">
        {save.error ? <Notice tone="error" title="保存成员失败">{describeApiError(save.error)}</Notice> : null}
        {!canManage ? <Notice tone="warning" title="缺少服务组管理权限">保存保持禁用。</Notice> : null}
        {writesLocked ? <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能保存。</Notice> : null}
        {missing.length > 0 ? (
          <Notice tone="warning" title="部分现有成员不可见">
            有 {missing.length} 个成员不在当前可见节点集合；继续保存会移除它们。
            <label className="mt-2 flex items-center gap-2 text-xs">
              <input type="checkbox" className="size-4" checked={acknowledge} onChange={event => setAcknowledge(event.target.checked)} />
              我确认移除不可见成员。
            </label>
          </Notice>
        ) : null}
        <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="候选节点加载失败">
          {() => (
            <>
              <Field label="搜索节点">
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
              <p className="text-xs text-muted-foreground">已选 {selected.size} · 当前 {filtered.length} / 共 {candidates.length}</p>
              {filtered.length === 0 ? (
                <EmptyState title="没有匹配节点" description="当前搜索没有命中；不影响已保存成员。" />
              ) : (
                <NodeSummaryTable caption="服务组成员候选" rows={filtered} selectedIds={selected} onToggle={toggle} />
              )}
            </>
          )}
        </AsyncPanel>
      </div>
    </Modal>
  );
}

export function ServiceGroupsPage() {
  const session = useSession();
  const canManage = session.can("servicegroup.manage") || session.can("node.manage");
  const [query, setQuery] = useState("");
  const [editor, setEditor] = useState<EditorState>({ open: false, group: null });
  const groups = useResource(`servicegroups:list:${session.viewEpoch}`, signal =>
    serviceGroupsApi.list(signal).then(result => result.data),
  );
  const items = groups.state.status === "ready" ? groups.state.data.items : [];
  const visible = query.trim()
    ? items.filter(group => `${group.name} ${group.description ?? ""}`.toLowerCase().includes(query.trim().toLowerCase()))
    : items;

  return (
    <div className="space-y-5">
      <PageHeader
        title="服务组管理"
        description="独立业务资源：包含成员、端口、健康与调度状态。与“节点管理 → 分组管理”的普通分组不共用身份与权限。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">业务资源</span>}
        actions={
          <Button
            type="button"
            variant="primary"
            disabled={!canManage || session.writesLocked}
            onClick={() => setEditor({ open: true, group: null })}
          >
            新建服务组
          </Button>
        }
      />
      {!canManage ? (
        <Notice tone="warning" title="写操作已锁定">
          当前身份未返回服务组管理权限（`servicegroup.manage`/`node.manage`）；列表仍按可见范围显示。
        </Notice>
      ) : null}
      <Panel
        title="服务组列表"
        description="GET /api/v1/service-groups。只有明确启用的网关联动服务组才参与调度；默认不启用负载均衡。"
        actions={<Button size="sm" onClick={groups.reload} loading={groups.state.status === "loading"}>刷新</Button>}
      >
        <div className="border-b px-5 py-3">
          <label htmlFor="servicegroup-search" className="block text-xs font-medium text-muted-foreground">本地筛选名称/用途</label>
          <input
            id="servicegroup-search"
            type="search"
            value={query}
            onChange={event => setQuery(event.target.value)}
            className="mt-1 h-9 w-full max-w-md rounded-md border border-input bg-background px-3 text-sm"
            placeholder="仅筛选已加载服务组"
          />
        </div>
        <AsyncPanel
          state={groups.state}
          onRetry={groups.reload}
          errorTitle="服务组加载失败"
          empty={<EmptyState title="暂无服务组" description="服务端返回空集合；创建后可作为网关后端或任务目标。" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <DataTable
              caption="服务组列表"
              rows={visible}
              rowKey={group => group.id}
              empty={<EmptyState title="没有匹配的服务组" description="本地筛选未命中；不改变服务端数据。" />}
              columns={[
                {
                  key: "name",
                  header: "服务组",
                  render: group => (
                    <a href={`#/service-groups/${encodeURIComponent(group.id)}`} className="font-medium text-primary underline-offset-2 hover:underline">
                      {group.name}
                    </a>
                  ),
                },
                { key: "members", header: "成员", render: group => `${group.memberNodeIds.length} 台` },
                {
                  key: "service",
                  header: "服务",
                  render: group =>
                    group.service ? `${group.service.protocol ?? "—"} : ${group.service.port ?? "—"}` : "未配置",
                },
                {
                  key: "lb",
                  header: "调度",
                  render: group =>
                    group.loadBalancing.enabled ? `启用 · ${group.loadBalancing.strategy ?? "未指定"}` : "未启用（仅资源集合）",
                },
                {
                  key: "health",
                  header: "健康/可调度",
                  render: group => `${group.healthyNodeIds?.length ?? 0} / ${group.dispatchEligibleNodeIds?.length ?? 0}`,
                },
                { key: "rev", header: "revision", render: group => group.revision },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <ServiceGroupEditor
        state={editor}
        onClose={() => setEditor({ open: false, group: null })}
        onSaved={group => {
          setEditor({ open: false, group: null });
          groups.reload();
          navigate(`/service-groups/${encodeURIComponent(group.id)}`);
        }}
      />
    </div>
  );
}

export function ServiceGroupDetailPage({ serviceGroupId }: { readonly serviceGroupId: string }) {
  const session = useSession();
  const canManage = session.can("servicegroup.manage") || session.can("node.manage");
  const [editor, setEditor] = useState<EditorState>({ open: false, group: null });
  const [membersOpen, setMembersOpen] = useState(false);
  const [scheduling, setScheduling] = useState<{ nodeId: string; paused: boolean } | null>(null);
  const [reason, setReason] = useState("");
  const [preview, setPreview] = useState<ServiceGroupDispatchPreview | null>(null);
  const [previewError, setPreviewError] = useState<unknown>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);

  const group = useResource(`servicegroup:detail:${session.viewEpoch}:${serviceGroupId}`, signal =>
    serviceGroupsApi.get(serviceGroupId, signal).then(result => result.data),
  );
  const nodes = useResource(`servicegroup:detail:nodes:${session.viewEpoch}`, signal =>
    nodesApi.list({ signal }).then(result => result.data),
  );
  const schedulingAction = useAction((nodeId: string, body: { paused: boolean; reason?: string }) =>
    serviceGroupsApi.setScheduling(serviceGroupId, nodeId, body),
  );
  const deleteAction = useAction(() => serviceGroupsApi.remove(serviceGroupId));

  const nodeItems: readonly NodeSummary[] = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const current = group.state.status === "ready" ? group.state.data : null;
  const memberRows = current
    ? current.memberNodeIds.map(id => {
        const node = nodeItems.find(entry => entry.id === id);
        return {
          id,
          name: node?.name.trim() ? node.name : id,
          online: node?.online ?? null,
          healthy: current.healthyNodeIds?.includes(id) ?? null,
          dispatchEligible: current.dispatchEligibleNodeIds?.includes(id) ?? null,
        };
      })
    : [];

  async function submitScheduling() {
    if (!scheduling) return;
    const result = await schedulingAction.run(scheduling.nodeId, {
      paused: scheduling.paused,
      ...(reason.trim() ? { reason: reason.trim() } : {}),
    });
    if (result.ok) {
      setOutcome(scheduling.paused ? "成员已暂停调度（服务端结果）。" : "成员已恢复调度（服务端结果）。");
      setScheduling(null);
      setReason("");
      group.reload();
    }
  }

  async function loadPreview() {
    setPreviewError(null);
    setPreview(null);
    try {
      const response = await serviceGroupsApi.dispatchPreview(serviceGroupId);
      setPreview(response.data);
    } catch (error) {
      setPreviewError(error);
    }
  }

  async function removeGroup() {
    const result = await deleteAction.run();
    if (result.ok) {
      setDeleteOpen(false);
      setOutcome("服务组已删除；不会删除节点。");
      navigate("/service-groups");
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={current?.name ?? "服务组详情"}
        description="成员、健康与调度资格。暂停成员只影响调度，不注销节点；成员变更不会扩大旧任务或旧 AI 授权。"
        actions={
          <>
            <a href="#/service-groups" className="inline-flex h-9 items-center rounded-md border bg-card px-3 text-sm hover:bg-secondary">返回列表</a>
            <Button
              type="button"
              disabled={!canManage || session.writesLocked || !current}
              onClick={() => current && setMembersOpen(true)}
            >
              管理成员
            </Button>
            <Button
              type="button"
              disabled={!canManage || session.writesLocked || !current}
              onClick={() => current && setEditor({ open: true, group: current })}
            >
              编辑
            </Button>
            <Button type="button" variant="danger" disabled={!canManage || session.writesLocked || !current} onClick={() => setDeleteOpen(true)}>
              删除
            </Button>
          </>
        }
      />
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}
      {deleteAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteAction.error)}</Notice> : null}
      {!canManage ? <Notice tone="warning" title="写操作已锁定">当前身份没有服务组管理权限，详情与成员仍按可见范围显示。</Notice> : null}

      <AsyncPanel
        state={group.state}
        onRetry={group.reload}
        errorTitle="服务组加载失败"
        empty={<EmptyState title="服务组不存在" description="服务端未返回该资源，或当前身份无权查看。" />}
        isEmpty={() => false}
      >
        {data => (
          <div className="space-y-5">
            <Panel title="服务组定义" description="GET /api/v1/service-groups/{serviceGroupId}">
              <div className="grid gap-4 px-5 py-4 text-sm sm:grid-cols-2 lg:grid-cols-3">
                <p>用途：{data.description || "未填写"}</p>
                <p>服务：{data.service ? `${data.service.protocol ?? "—"} : ${data.service.port ?? "—"}` : "未配置"}</p>
                <p>健康检查：{data.service?.healthCheck || "未配置"}</p>
                <p>调度：{data.loadBalancing.enabled ? `启用 · ${data.loadBalancing.strategy ?? "未指定"}` : "未启用（仅资源集合）"}</p>
                <p>关联网关：{data.gatewayIds?.join("、") || "无"}</p>
                <p>revision：{data.revision}</p>
              </div>
            </Panel>

            <Panel
              title="成员与调度"
              description="成员状态来自服务端；离线、维护或健康失败不参与调度，但不会自动注销节点。"
              actions={
                <>
                  <Button size="sm" onClick={() => { group.reload(); nodes.reload(); }}>刷新</Button>
                  <Button size="sm" onClick={loadPreview}>调度预演</Button>
                </>
              }
            >
              {previewError ? <Notice tone="error" title="预演失败">{describeApiError(previewError)}</Notice> : null}
              {preview ? (
                <div className="border-b px-5 py-3 text-sm">
                  <p>网关可调度：{preview.gatewayEligible ? "是" : "否"}</p>
                  <p className="mt-1">可调度节点：{preview.eligibleNodeIds.length > 0 ? preview.eligibleNodeIds.join("、") : "无"}</p>
                  {preview.excludedNodes?.length ? (
                    <ul className="mt-1 list-disc pl-5 text-xs text-muted-foreground">
                      {preview.excludedNodes.map(item => <li key={item.nodeId}>{item.nodeId}：{item.reason ?? "原因未提供"}</li>)}
                    </ul>
                  ) : null}
                  <p className="mt-1 text-xs text-muted-foreground">预演结果不执行任务，不改变真实调度状态。</p>
                </div>
              ) : null}
              <DataTable
                caption="服务组成员与调度状态"
                rows={memberRows}
                rowKey={row => row.id}
                empty={<EmptyState title="服务组暂无成员" description="成员为空时不能作为调度后端；请先添加成员。" />}
                columns={[
                  { key: "name", header: "节点", render: row => <span>{row.name}<span className="block font-mono text-xs text-muted-foreground">{row.id}</span></span> },
                  { key: "online", header: "在线", render: row => (row.online === null ? "未知" : row.online ? "在线" : "离线") },
                  { key: "healthy", header: "健康", render: row => (row.healthy === null ? "未知" : row.healthy ? "健康" : "不健康") },
                  { key: "dispatch", header: "可调度", render: row => (row.dispatchEligible === null ? "未知" : row.dispatchEligible ? "是" : "否") },
                  {
                    key: "action",
                    header: "操作",
                    render: row => (
                      <Button
                        size="sm"
                        disabled={!canManage || session.writesLocked || schedulingAction.pending}
                        onClick={() => { setScheduling({ nodeId: row.id, paused: row.dispatchEligible !== false }); setReason(""); }}
                      >
                        {row.dispatchEligible === false ? "恢复调度" : "暂停调度"}
                      </Button>
                    ),
                  },
                ]}
              />
            </Panel>
          </div>
        )}
      </AsyncPanel>

      <ServiceGroupEditor
        state={editor}
        onClose={() => setEditor({ open: false, group: null })}
        onSaved={() => {
          setEditor({ open: false, group: null });
          setOutcome("服务组定义已更新（服务端结果）。");
          group.reload();
        }}
      />
      <MemberEditor
        group={current}
        open={membersOpen}
        canManage={canManage}
        writesLocked={session.writesLocked}
        onClose={() => setMembersOpen(false)}
        onSaved={() => {
          setMembersOpen(false);
          setOutcome("服务组成员已保存（服务端结果）。");
          group.reload();
        }}
      />
      <Modal
        open={scheduling !== null}
        title={scheduling?.paused ? "暂停成员调度" : "恢复成员调度"}
        description="调度暂停不注销节点、不删除成员；已有任务不会因此扩大或回滚。"
        onClose={() => {
          if (!schedulingAction.pending) setScheduling(null);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setScheduling(null)} disabled={schedulingAction.pending}>取消</Button>
            <Button type="button" variant="primary" onClick={submitScheduling} loading={schedulingAction.pending} disabled={!reason.trim()}>
              确认
            </Button>
          </>
        }
      >
        {schedulingAction.error ? <Notice tone="error" title="操作失败">{describeApiError(schedulingAction.error)}</Notice> : null}
        <Field label="原因" hint="服务端会记录审计；请勿包含秘密。">
          {ids => <TextArea id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} />}
        </Field>
      </Modal>

      <Modal
        open={deleteOpen}
        title="删除服务组"
        description={current ? `将删除服务组“${current.name}”。成员节点不会被删除；已引用的网关路由、证书任务或 AI 旧授权需要按服务端影响清单处置。` : undefined}
        onClose={() => {
          if (!deleteAction.pending) setDeleteOpen(false);
        }}
        footer={
          <>
            <Button type="button" onClick={() => setDeleteOpen(false)} disabled={deleteAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeGroup} loading={deleteAction.pending}>确认删除</Button>
          </>
        }
      >
        {deleteAction.error ? <Notice tone="error" title="删除失败">{describeApiError(deleteAction.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">服务端会校验当前权限并记录审计；删除后不能用旧 revision 恢复。</p>
      </Modal>
    </div>
  );
}
