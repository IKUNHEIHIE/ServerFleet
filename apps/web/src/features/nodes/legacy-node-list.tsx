import { useEffect, useId, useMemo, useRef, useState } from "react";
import { MoreHorizontal, Search, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox, Field, TextArea, TextInput } from "@/components/ui/form";
import { Notice } from "@/components/ui/feedback";
import { Modal } from "@/components/ui/modal";
import type { NodeDetail } from "@/lib/api/dto";
import { nodesApi } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
import { cn } from "@/lib/utils";
import type { LegacyMetric } from "./legacy-types";
import type { LegacyNodeRow } from "./legacy-types";
import { NodeEditor } from "./node-editor";

function NodeRowActions({ row, onChanged, canEdit, canDelete, writesLocked, viewEpoch }: { readonly row: LegacyNodeRow; readonly onChanged: () => void; readonly canEdit: boolean; readonly canDelete: boolean; readonly writesLocked: boolean; readonly viewEpoch: string }) {
  const canEditAction = canEdit && !writesLocked;
  const canDeleteAction = canDelete && !writesLocked;
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!menuOpen) return;
    const close = (event: PointerEvent) => { if (!menuRef.current?.contains(event.target as Node)) setMenuOpen(false); };
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") setMenuOpen(false); };
    document.addEventListener("pointerdown", close);
    document.addEventListener("keydown", escape);
    return () => { document.removeEventListener("pointerdown", close); document.removeEventListener("keydown", escape); };
  }, [menuOpen]);
  const [editor, setEditor] = useState<NodeDetail | null>(null);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [reason, setReason] = useState("");
  const [confirmed, setConfirmed] = useState(false);
  const remove = useAction((body: { reason: string }) => nodesApi.remove(row.id, body));

  async function edit() {
    setMenuOpen(false);
    setDetailError(null);
    setLoadingDetail(true);
    try {
      const result = await nodesApi.detail(row.id);
      setEditor(result.data);
    } catch (error) {
      setDetailError(describeApiError(error));
    } finally {
      setLoadingDetail(false);
    }
  }

  async function deleteNode() {
    if (!reason.trim() || !confirmed || remove.pending) return;
    const result = await remove.run({ reason: reason.trim() });
    if (result.ok) {
      setDeleteOpen(false);
      onChanged();
    }
  }

  return (
    <>
      <td className="rounded-r-3xl px-2 py-2 text-right">
        <div ref={menuRef} className="relative inline-block">
          <Button type="button" variant="ghost" size="sm" className="size-8 p-0" aria-label={`${row.name} 更多操作`} aria-haspopup="menu" aria-expanded={menuOpen} onClick={() => setMenuOpen(value => !value)}>
            <MoreHorizontal aria-hidden="true" className="size-4" />
          </Button>
          {detailError ? <span role="alert" className="block max-w-48 text-left text-xs text-destructive">{detailError}</span> : null}
          {menuOpen ? (
            <div role="menu" aria-label={`${row.name} 节点操作`} className="absolute right-0 z-20 mt-1 min-w-32 rounded-md border border-border bg-card p-1 text-left shadow-lg">
              {canEditAction ? <button type="button" role="menuitem" disabled={loadingDetail} onClick={edit} className="block w-full rounded px-3 py-2 text-sm hover:bg-secondary disabled:opacity-50">编辑</button> : null}
              {canDeleteAction ? <button type="button" role="menuitem" onClick={() => { setMenuOpen(false); setReason(""); setConfirmed(false); remove.reset(); setDeleteOpen(true); }} className="block w-full rounded px-3 py-2 text-left text-sm text-destructive hover:bg-destructive/5">删除</button> : null}
              {!canEditAction && !canDeleteAction ? <span className="block px-3 py-2 text-xs text-muted-foreground">无可用操作</span> : null}
            </div>
          ) : null}
        </div>
      </td>
      {editor ? <NodeEditor key={`editor:${viewEpoch}:${row.id}`} nodeId={row.id} metadata={editor} canManage={canEditAction} writesLocked={writesLocked} onClose={() => setEditor(null)} onRefresh={() => { setEditor(null); void edit(); }} onSaved={() => { setEditor(null); onChanged(); }} /> : null}
      <Modal open={deleteOpen} title="删除节点" description="停止此节点接入并从列表移除，保留历史审计与关联记录；重新使用须重新接入。" onClose={() => { if (!remove.pending) setDeleteOpen(false); }} footer={<>
        <Button type="button" disabled={remove.pending} onClick={() => setDeleteOpen(false)}>取消</Button>
        <Button type="button" variant="danger" loading={remove.pending} disabled={!canDeleteAction || !reason.trim() || !confirmed} onClick={deleteNode}>确认删除</Button>
      </>}>
        {remove.error ? <Notice tone="error" title="操作未完成">{describeApiError(remove.error)}</Notice> : null}
        <div className="space-y-4">
          <Field label="原因" hint="请勿包含秘密或凭据。">{ids => <TextArea id={ids.inputId} value={reason} onChange={event => setReason(event.target.value)} disabled={remove.pending} required />}</Field>
          <label className="flex items-start gap-2 text-sm"><Checkbox checked={confirmed} onChange={event => setConfirmed(event.target.checked)} disabled={remove.pending} />停止此节点接入并从列表移除，保留历史审计；重新使用须重新接入</label>
          <p className="text-xs text-muted-foreground">操作原因会记录到审计中，实际结果以服务端返回为准。</p>
        </div>
      </Modal>
    </>
  );
}

function MonitorMetric({ label, metric, sampledAt }: { readonly label: string; readonly metric: LegacyMetric | undefined; readonly sampledAt?: string | null }) {
  const value = metric?.kind === "value" ? metric.percent : null;
  return <div className="min-w-0 text-left">
    <span className="block text-sm text-muted-foreground">{label}</span>
    <span className="block whitespace-nowrap text-base font-bold tabular-nums leading-6 text-foreground">{value === null ? "未采集" : `${value.toFixed(2)}%`}</span>
    <div aria-hidden="true" className="h-[3px] w-full overflow-hidden rounded-full bg-emerald-50"><div className="h-full bg-emerald-500 transition-[width] duration-300" style={{ width: `${value ?? 0}%` }} /></div>
    {value !== null && !sampledAt ? <span className="text-[10px] text-amber-700">无采样时间</span> : null}
    {metric?.kind === "unavailable" ? <span className="text-[10px] text-muted-foreground">{sampledAt ? "采样过期" : "离线，无当前读数"}</span> : null}
  </div>;
}

function speedText(value: number | null | undefined): string {
  if (value == null) return "—";
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(2)}M/s`;
  return `${(value / 1024).toFixed(2)}K/s`;
}

export function normaliseLegacyQuery(raw: string): string {
  return raw.trim().toLowerCase();
}

export function filterLegacyRows(rows: readonly LegacyNodeRow[], query: string): readonly LegacyNodeRow[] {
  const normalised = normaliseLegacyQuery(query);
  if (!normalised) return rows;
  return rows.filter(
    row => row.name.toLowerCase().includes(normalised) || row.id.toLowerCase().includes(normalised),
  );
}

export interface LegacyNodeListProps {
  readonly rows: readonly LegacyNodeRow[];
  readonly onChanged?: () => void;
  readonly canEdit?: boolean;
  readonly canDelete?: boolean;
  readonly writesLocked?: boolean;
  readonly viewEpoch?: string;
  readonly title?: string;
  readonly description?: string | null;
  readonly className?: string;
}

/**
 * 兼容列表：名称链接进入详情，本地搜索与可见计数。
 *
 * 筛选仅作用于已传入的行，不查询后端、不代表权限变更，也不推断全局节点数。
 * 行本身不绑定点击，名称链接保留标准键盘与浏览器导航行为。
 */
export function LegacyNodeList({ rows, onChanged = () => {}, canEdit = false, canDelete = false, writesLocked = false, viewEpoch = "", title = "兼容旧列表", description, className }: LegacyNodeListProps) {
  const [query, setQuery] = useState("");
  const searchId = useId();
  const titleId = useId();
  const total = rows.length;
  const visible = useMemo(() => filterLegacyRows(rows, query), [rows, query]);
  const filtering = query.trim() !== "";
  const noMatch = total > 0 && visible.length === 0;
  const countLabel = total === 0 ? "0 个节点" : visible.length === total ? `共 ${total} 个节点` : `显示 ${visible.length} / 共 ${total} 个节点`;

  let body;
  if (total === 0) {
    body = (
      <div className="px-6 py-10 text-center">
        <p className="font-medium">暂无可显示节点</p>
        <p className="mt-1 text-sm text-muted-foreground">
          服务端返回了空集合；这不代表系统没有节点，只代表当前身份可见范围为 0。
        </p>
      </div>
    );
  } else if (noMatch) {
    body = (
      <div className="px-6 py-10 text-center">
        <p className="font-medium">没有匹配结果</p>
        <p className="mt-1 text-sm text-muted-foreground">
          当前搜索词没有命中已加载的 {total} 条记录；节点数据没有被修改。
        </p>
      </div>
    );
  } else {
    body = (
      <div className="space-y-3 p-3 sm:p-4">
        <table className="node-monitor-table w-full border-separate border-spacing-y-3 text-sm">
          <caption className="sr-only">兼容旧节点列表</caption>
          <thead className="sr-only"><tr>{["节点", "CPU", "内存", "存储", "上传", "下载", "操作"].map(label => <th key={label} scope="col">{label}</th>)}</tr></thead>
          <tbody>
            {visible.map(row => (
              <tr key={row.id} className="rounded-3xl bg-white align-middle shadow-[0_0_0_1px_rgba(0,0,0,0.08),0_2px_4px_rgba(0,0,0,0.04)]">
                <th scope="row" className="w-[32%] rounded-l-3xl px-5 py-5 text-left font-normal">
                  <div className="flex items-center gap-3">
                    <span aria-label={row.online ? "在线" : "离线"} className={cn("size-2.5 shrink-0 rounded-full", row.online ? "bg-emerald-500" : "bg-slate-300")} />
                    <a href={`#/nodes/${encodeURIComponent(row.id)}`} className="break-all text-base font-bold text-foreground hover:text-primary">{row.name}</a>
                  </div>
                  {row.nameIssue ? <span className="mt-1 block text-xs text-amber-700">{row.nameIssue === "empty" ? "名称为空，显示节点 ID" : "名称字段非法，显示节点 ID"}</span> : null}
                  <span className="sr-only">{row.online ? "在线" : "离线"}</span>
                  {row.sampledAt ? <span className="mt-1 block text-[10px] text-muted-foreground" title={row.sampledAt}>采样 {new Date(row.sampledAt).toLocaleTimeString("zh-CN", { hour12: false })}</span> : null}
                </th>
                <td className="w-[12%] px-2 py-4"><MonitorMetric label="CPU" metric={row.cpu} sampledAt={row.sampledAt} /></td>
                <td className="w-[12%] px-2 py-4"><MonitorMetric label="内存" metric={row.memory} sampledAt={row.sampledAt} /></td>
                <td className="w-[12%] px-2 py-4"><MonitorMetric label="存储" metric={row.disk} sampledAt={row.sampledAt} /></td>
                <td className="w-[13%] px-2 py-4"><span className="block text-sm text-muted-foreground">上传</span><span className="whitespace-nowrap text-base font-bold tabular-nums">{speedText(row.uploadBps)}</span></td>
                <td className="w-[13%] px-2 py-4"><span className="block text-sm text-muted-foreground">下载</span><span className="whitespace-nowrap text-base font-bold tabular-nums">{speedText(row.downloadBps)}</span></td>
                <NodeRowActions row={row} onChanged={onChanged} canEdit={canEdit} canDelete={canDelete} writesLocked={writesLocked} viewEpoch={viewEpoch} />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }

  return (
    <section
      aria-labelledby={titleId}
      className={cn("rounded-xl border border-border/80 bg-card shadow-[var(--shadow-card)]", className)}
    >
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border/70 bg-gradient-to-b from-white to-[#FAFBFD] px-5 py-4">
        <div>
          <h2 id={titleId} className="font-semibold text-foreground">
            {title}
          </h2>
          {description ? <p className="mt-1 max-w-3xl text-sm leading-6 text-muted-foreground">{description}</p> : null}
        </div>
        <Badge tone="primary" role="status" className="py-1">
          {countLabel}
        </Badge>
      </header>
      <div className="border-b border-border/70 bg-[#FAFBFD] px-5 py-3.5">
        <div role="search" aria-label="兼容节点列表搜索" className="flex flex-wrap items-end gap-3">
          <div className="min-w-56 flex-1">
            <label htmlFor={searchId} className="block text-xs font-semibold text-muted-foreground">
              搜索名称或节点 ID
            </label>
            <div className="relative mt-1">
              <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
              <TextInput
                id={searchId}
                type="search"
                value={query}
                onChange={event => setQuery(event.target.value)}
                placeholder="仅搜索已加载的节点"
                disabled={total === 0}
                className="mt-0 h-9 pl-8"
              />
            </div>
          </div>
          <Button
            type="button"
            onClick={() => setQuery("")}
            disabled={!filtering}
          >
            <X aria-hidden="true" className="size-3.5 text-muted-foreground" />
            清空搜索
          </Button>
        </div>
      </div>
      {body}
    </section>
  );
}
