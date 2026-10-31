import { useEffect, useMemo, useState, type ChangeEvent, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { filesApi, nodesApi, transfersApi } from "@/lib/api/endpoints";
import type { ConflictPolicy, FileEntry, NodeSummary, TransferJob, TransferStatus } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatBytes, formatDateTime } from "@/lib/format";

const TRANSFER_STATUS_TEXT: Record<TransferStatus, string> = {
  queued: "排队中",
  running: "执行中",
  completed: "已完成",
  partial_failure: "部分失败",
  failed: "失败",
  cancelling: "取消中",
  cancelled: "已取消",
};

function parentPath(path: string): string {
  if (path === "/") return "/";
  const trimmed = path.replace(/\/+$/, "");
  const index = trimmed.lastIndexOf("/");
  return index <= 0 ? "/" : trimmed.slice(0, index);
}

function isValidAbsolutePath(path: string): boolean {
  return path.startsWith("/") && !path.split("/").some(segment => segment === "..");
}

async function sha256Hex(file: File): Promise<string | undefined> {
  if (!globalThis.crypto?.subtle) return undefined;
  try {
    const digest = await crypto.subtle.digest("SHA-256", await file.arrayBuffer());
    return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, "0")).join("");
  } catch {
    return undefined;
  }
}

interface PaneState {
  readonly nodeId: string | null;
  readonly path: string;
  readonly search: string;
  readonly cursor: string | null;
  readonly stack: readonly (string | null)[];
}

const INITIAL_PANE: PaneState = { nodeId: null, path: "/", search: "", cursor: null, stack: [] };

function FilePane({
  label,
  state,
  setState,
  nodes,
  other,
  onTransferCreated,
}: {
  readonly label: string;
  readonly state: PaneState;
  readonly setState: (updater: (current: PaneState) => PaneState) => void;
  readonly nodes: readonly NodeSummary[];
  readonly other: PaneState;
  readonly onTransferCreated: () => void;
}) {
  const session = useSession();
  const [pathDraft, setPathDraft] = useState(state.path);
  const [searchDraft, setSearchDraft] = useState(state.search);
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [createOpen, setCreateOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<FileEntry | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<FileEntry | null>(null);
  const [sizeResult, setSizeResult] = useState<string | null>(null);
  const [uploadOutcome, setUploadOutcome] = useState<string | null>(null);
  const [transferOpen, setTransferOpen] = useState(false);
  const [conflictPolicy, setConflictPolicy] = useState<ConflictPolicy>("skip");
  const [outcome, setOutcome] = useState<string | null>(null);

  const canWrite = session.can("file.write") || session.can("file.manage");
  const canRename = session.can("file.rename") || session.can("file.manage");
  const canDelete = session.can("file.delete") || session.can("file.manage");
  const canTransfer = session.can("file.transfer") || session.can("file.manage");

  const node = nodes.find(entry => entry.id === state.nodeId) ?? null;
  const list = useResource(
    `files:${session.viewEpoch}:${state.nodeId ?? "none"}:${state.path}:${state.search}:${state.cursor ?? ""}`,
    signal =>
      filesApi
        .list(
          state.nodeId ?? "",
          {
            path: state.path,
            ...(state.search ? { search: state.search } : {}),
            ...(state.cursor ? { cursor: state.cursor } : {}),
            limit: 50,
          },
          signal,
        )
        .then(result => result.data),
    { enabled: Boolean(state.nodeId) },
  );

  useEffect(() => {
    setPathDraft(state.path);
    setSearchDraft(state.search);
    setSelected(new Set());
    setSizeResult(null);
    setUploadOutcome(null);
  }, [state.nodeId, state.path, state.search, state.cursor]);

  const entries = list.state.status === "ready" ? list.state.data.entries : [];
  const orderedEntries = useMemo(() => [...entries].sort((a, b) => Number(b.type === "directory") - Number(a.type === "directory")), [entries]);
  const selectedEntries = orderedEntries.filter(entry => selected.has(entry.path));
  const otherNode = nodes.find(entry => entry.id === other.nodeId) ?? null;
  const sameTarget = state.nodeId !== null && state.nodeId === other.nodeId && state.path === other.path;
  const sendDisabled =
    selectedEntries.length === 0 ||
    !canTransfer ||
    session.writesLocked ||
    !otherNode?.online ||
    !node?.online ||
    sameTarget ||
    selectedEntries.some(entry => entry.type === "symlink");

  function resetInteraction(patch: Partial<PaneState>) {
    setState(current => ({ ...current, ...patch, cursor: null, stack: [] }));
    setSelected(new Set());
    setSizeResult(null);
    setUploadOutcome(null);
  }

  function selectNode(nodeId: string) {
    resetInteraction({ nodeId, path: "/", search: "" });
    setPathDraft("/");
    setSearchDraft("");
  }

  function applyPath(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const next = pathDraft.trim();
    if (!isValidAbsolutePath(next)) {
      setOutcome("路径必须是以 / 开头的绝对路径，且不能包含 ..。");
      return;
    }
    setOutcome(null);
    resetInteraction({ path: next || "/" });
  }

  function openEntry(entry: FileEntry) {
    if (entry.type !== "directory") return;
    resetInteraction({ path: entry.path });
  }

  function applySearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    resetInteraction({ search: searchDraft.trim() });
  }

  function clearSearch() {
    setSearchDraft("");
    resetInteraction({ search: "" });
  }

  function toggle(entry: FileEntry, checked: boolean) {
    setSelected(current => {
      const next = new Set(current);
      if (checked) next.add(entry.path);
      else next.delete(entry.path);
      return next;
    });
  }

  const createFolder = useAction((body: { parentPath: string; name: string }) => filesApi.createDirectory(state.nodeId ?? "", body));
  const rename = useAction((body: { path: string; newName: string; expectedVersion?: string }) => filesApi.rename(state.nodeId ?? "", body));
  const remove = useAction((path: string, recursive: boolean) => filesApi.remove(state.nodeId ?? "", path, recursive));
  const size = useAction((path: string) => filesApi.size(state.nodeId ?? "", path));
  const upload = useAction((body: Parameters<typeof filesApi.createUpload>[1]) => filesApi.createUpload(state.nodeId ?? "", body));
  const uploadBytes = useAction((uploadId: string, bytes: ArrayBuffer) => filesApi.uploadContent(state.nodeId ?? "", uploadId, bytes, 0));
  const complete = useAction((uploadId: string) => filesApi.completeUpload(state.nodeId ?? "", uploadId));
  const transfer = useAction((body: Parameters<typeof transfersApi.create>[0], key: string) => transfersApi.create(body, key));

  // GM-W5-01b：单文件上传会话 → PUT 字节流（offset=0）→ POST complete 触发服务端
  // 字节数与 SHA-256 校验。传输通道未注入时服务端返回 503 unavailable，此时字节已
  // 落地暂存并通过校验，界面如实区分“校验通过”与“未送达远端节点”。
  async function handleFiles(event: ChangeEvent<HTMLInputElement>) {
    const files = Array.from(event.target.files ?? []);
    event.target.value = "";
    if (files.length === 0 || !state.nodeId) return;
    setUploadOutcome(null);
    const lines: string[] = [];
    for (const file of files) {
      const sha256 = await sha256Hex(file);
      const created = await upload.run({
        destinationDir: state.path,
        conflictPolicy: "skip",
        files: [{ name: file.name, sizeBytes: file.size, ...(sha256 ? { sha256 } : {}) }],
      });
      if (!created.ok || !created.data) {
        lines.push(`${file.name}：上传会话创建失败。`);
        continue;
      }
      const uploadId = created.data.data.uploadId;
      const bytes = await file.arrayBuffer();
      const content = await uploadBytes.run(uploadId, bytes);
      if (!content.ok || !content.data) {
        lines.push(`${file.name}：字节流上传失败（uploadId ${uploadId}）。`);
        continue;
      }
      const received = content.data.data.bytesReceived;
      const completed = await complete.run(uploadId);
      if (completed.ok && completed.data && completed.data.data.status === "completed") {
        lines.push(`${file.name}：上传完成，服务端校验通过（${received} B）。`);
      } else if (completed.ok && completed.data) {
        lines.push(`${file.name}：服务端返回 status=${completed.data.data.status}；不声称完成。`);
      } else if (completed.error && completed.error.status === 503) {
        lines.push(
          `文件字节流（${received} B）已上传并通过服务端 SHA-256 校验；当前节点文件传输通道未注入（503 unavailable）。`,
        );
      } else if (completed.error) {
        lines.push(`${file.name}：完成校验失败（${completed.error.status} ${completed.error.code ?? completed.error.kind}）。`);
      }
    }
    setUploadOutcome(lines.join(" "));
    list.reload();
  }

  async function submitCreate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    const name = String(form.get("name") ?? "").trim();
    if (!name || name.includes("/") || name === "." || name === "..") {
      setOutcome("文件夹名称不能为空，也不能包含 /、. 或 ..。");
      return;
    }
    const result = await createFolder.run({ parentPath: state.path, name });
    if (result.ok) {
      setCreateOpen(false);
      setOutcome("文件夹已创建。");
      list.reload();
    }
  }

  async function submitRename(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!renameTarget) return;
    const form = new FormData(event.currentTarget);
    const newName = String(form.get("newName") ?? "").trim();
    if (!newName || newName.includes("/") || newName === "." || newName === "..") {
      setOutcome("新名称不能为空，也不能包含 /、. 或 ..。");
      return;
    }
    const result = await rename.run({
      path: renameTarget.path,
      newName,
      ...(renameTarget.version ? { expectedVersion: renameTarget.version } : {}),
    });
    if (result.ok) {
      setRenameTarget(null);
      setOutcome("重命名已完成。");
      list.reload();
    }
  }

  async function submitDelete() {
    if (!deleteTarget) return;
    const result = await remove.run(deleteTarget.path, deleteTarget.type === "directory");
    if (result.ok) {
      setDeleteTarget(null);
      setOutcome(deleteTarget.type === "directory" ? "文件夹已删除；子级影响由服务端确认。" : "文件已删除。");
      list.reload();
    }
  }

  async function calculateSize(entry: FileEntry) {
    setSizeResult(null);
    const result = await size.run(entry.path);
    if (result.ok && result.data) {
      setSizeResult(`${entry.path}：${formatBytes(result.data.data.sizeBytes) ?? `${result.data.data.sizeBytes} B`}`);
    }
  }

  async function submitTransfer(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!state.nodeId || !other.nodeId || selectedEntries.length === 0) return;
    const key = globalThis.crypto?.randomUUID?.() ?? `transfer-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const result = await transfer.run(
      {
        sourceNodeId: state.nodeId,
        sourcePaths: selectedEntries.map(entry => entry.path),
        destinationNodeId: other.nodeId,
        destinationDir: other.path,
        conflictPolicy,
        followSymlinks: false,
        dryRun: false,
      },
      key,
    );
    if (result.ok && result.data) {
      setTransferOpen(false);
      setOutcome(`传输任务 ${result.data.data.id} 已接受；这不表示文件已复制完成。`);
      setSelected(new Set());
      onTransferCreated();
    }
  }

  return (
    <div className="min-w-0 rounded-xl border bg-card">
      <div className="border-b px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="font-semibold">{label}</h2>
          {node ? <span className="text-xs text-muted-foreground">{node.online ? "在线" : "离线"}</span> : null}
        </div>
        <div className="mt-2 grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
          <label className="text-xs font-medium text-muted-foreground">
            节点
            <Select
              className="mt-1"
              value={state.nodeId ?? ""}
              onChange={event => selectNode(event.target.value)}
              aria-label={`${label}节点`}
            >
              <option value="">请选择节点</option>
              {nodes.map(entry => (
                <option key={entry.id} value={entry.id}>
                  {entry.name.trim() || entry.id} {entry.online ? "" : "（离线）"}
                </option>
              ))}
            </Select>
          </label>
          <div className="flex items-end gap-2">
            <Button
              type="button"
              size="sm"
              disabled={!state.nodeId || state.path === "/"}
              onClick={() => resetInteraction({ path: parentPath(state.path) })}
            >
              上级
            </Button>
            <Button type="button" size="sm" onClick={list.reload} disabled={!state.nodeId} loading={list.state.status === "loading"}>
              刷新
            </Button>
          </div>
        </div>
        <form onSubmit={applyPath} className="mt-2 flex gap-2">
          <TextInput aria-label={`${label}路径`} value={pathDraft} onChange={event => setPathDraft(event.target.value)} placeholder="/absolute/path" disabled={!state.nodeId} />
          <Button type="submit" size="sm" disabled={!state.nodeId}>跳转</Button>
        </form>
        <nav aria-label={`${label}面包屑`} className="mt-2 flex flex-wrap items-center gap-1 text-xs">
          <button type="button" onClick={() => resetInteraction({ path: "/" })} disabled={!state.nodeId} className="rounded px-1 py-0.5 hover:bg-secondary disabled:opacity-50">
            /
          </button>
          {state.path.split("/").filter(Boolean).map((segment, index, parts) => {
            const target = `/${parts.slice(0, index + 1).join("/")}`;
            return (
              <span key={target} className="flex items-center gap-1">
                <span aria-hidden="true">/</span>
                <button type="button" onClick={() => resetInteraction({ path: target })} className="rounded px-1 py-0.5 hover:bg-secondary">
                  {segment}
                </button>
              </span>
            );
          })}
        </nav>
        <form onSubmit={applySearch} className="mt-2 flex flex-wrap items-end gap-2">
          <div className="min-w-40 flex-1">
            <label htmlFor={`${label}-search`} className="block text-xs font-medium text-muted-foreground">当前目录搜索</label>
            <input
              id={`${label}-search`}
              type="search"
              value={searchDraft}
              onChange={event => setSearchDraft(event.target.value)}
              className="mt-1 h-8 w-full rounded-md border border-input bg-background px-2 text-sm"
              disabled={!state.nodeId}
            />
          </div>
          <Button type="submit" size="sm" disabled={!state.nodeId}>搜索</Button>
          <Button type="button" size="sm" variant="ghost" onClick={clearSearch} disabled={!state.search && !searchDraft}>清空</Button>
        </form>
      </div>

      <div className="space-y-2 border-b px-4 py-3">
        {outcome ? <Notice tone="info" title="操作结果" actions={<Button size="sm" onClick={() => setOutcome(null)}>关闭</Button>}>{outcome}</Notice> : null}
        {!node ? <Notice tone="info" title="请选择节点">选择节点后才会读取目录；不会显示本地假文件。</Notice> : null}
        {node && !node.online ? <Notice tone="warning" title="节点离线">目录与写操作会被服务端拒绝；不展示缓存文件。</Notice> : null}
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            size="sm"
            disabled={!state.nodeId || !node?.online || !canWrite || session.writesLocked}
            onClick={() => setCreateOpen(true)}
          >
            新建文件夹
          </Button>
          <label className={`inline-flex h-8 items-center rounded-md border px-2.5 text-xs font-medium ${!state.nodeId || !node?.online || !canWrite || session.writesLocked ? "cursor-not-allowed opacity-50" : "cursor-pointer hover:bg-secondary"}`}>
            上传文件
            <input
              type="file"
              multiple
              className="sr-only"
              disabled={!state.nodeId || !node?.online || !canWrite || session.writesLocked}
              onChange={handleFiles}
            />
          </label>
          <Button type="button" size="sm" onClick={() => setTransferOpen(true)} disabled={sendDisabled}>
            发送到另一侧（{selectedEntries.length}）
          </Button>
        </div>
        {!canWrite ? <p className="text-xs text-muted-foreground">当前身份没有文件写入权限（`file.write`/`file.manage`）。</p> : null}
        {uploadOutcome ? (
          <Notice tone="info" title="上传结果" actions={<Button size="sm" onClick={() => setUploadOutcome(null)}>关闭</Button>}>
            {uploadOutcome}
          </Notice>
        ) : null}
        {sizeResult ? <p role="status" className="text-xs text-muted-foreground">{sizeResult}</p> : null}
      </div>

      {!state.nodeId ? (
        <EmptyState title="尚未选择节点" description="左右两栏各自独立选择节点与目录。" />
      ) : (
        <AsyncPanel
          state={list.state}
          onRetry={list.reload}
          errorTitle="目录读取失败"
          empty={<EmptyState title="当前目录为空" description="服务端返回空集合；不是本地缓存。" />}
          isEmpty={data => data.entries.length === 0}
        >
          {data => (
            <>
              <DataTable
                caption={`${label}目录内容`}
                rows={orderedEntries}
                rowKey={entry => entry.path}
                empty={<EmptyState title="当前目录为空" description="服务端返回空集合。" />}
                columns={[
                  {
                    key: "select",
                    header: "选择",
                    render: entry => (
                      <input
                        type="checkbox"
                        aria-label={`选择 ${entry.name}`}
                        checked={selected.has(entry.path)}
                        disabled={entry.type === "symlink"}
                        onChange={event => toggle(entry, event.target.checked)}
                        className="size-4 accent-[var(--primary)]"
                      />
                    ),
                  },
                  {
                    key: "name",
                    header: "名称",
                    render: entry =>
                      entry.type === "directory" ? (
                        <button type="button" className="text-left text-primary underline-offset-2 hover:underline" onClick={() => openEntry(entry)}>
                          {entry.name}/
                        </button>
                      ) : (
                        <span>{entry.name}</span>
                      ),
                  },
                  {
                    key: "type",
                    header: "类型",
                    render: entry => (entry.type === "directory" ? "目录" : entry.type === "symlink" ? "符号链接（不跟随）" : "文件"),
                  },
                  { key: "size", header: "大小", render: entry => (entry.type === "directory" ? "—" : formatBytes(entry.sizeBytes ?? null) ?? "—") },
                  { key: "modified", header: "修改时间", render: entry => formatDateTime(entry.modifiedAt) ?? "—" },
                  {
                    key: "actions",
                    header: "操作",
                    render: entry => (
                      <div className="flex flex-wrap gap-1">
                        {entry.type === "directory" ? <Button size="sm" onClick={() => openEntry(entry)}>打开</Button> : null}
                        {entry.type !== "symlink" ? <Button size="sm" onClick={() => calculateSize(entry)} loading={size.pending}>大小</Button> : null}
                        <Button size="sm" disabled={!node?.online || !canRename || session.writesLocked} onClick={() => setRenameTarget(entry)}>重命名</Button>
                        <Button size="sm" variant="danger" disabled={!node?.online || !canDelete || session.writesLocked} onClick={() => setDeleteTarget(entry)}>删除</Button>
                      </div>
                    ),
                  },
                ]}
              />
              <div className="flex items-center justify-between border-t px-4 py-2">
                <Button
                  type="button"
                  size="sm"
                  disabled={state.stack.length === 0}
                  onClick={() => {
                    const stack = [...state.stack];
                    const previous = stack.pop() ?? null;
                    setState(current => ({ ...current, stack, cursor: previous }));
                    setSelected(new Set());
                  }}
                >
                  上一页
                </Button>
                <span className="text-xs text-muted-foreground">目录：{data.path}</span>
                <Button
                  type="button"
                  size="sm"
                  disabled={!data.nextCursor}
                  onClick={() => {
                    setState(current => ({ ...current, stack: [...current.stack, current.cursor], cursor: data.nextCursor ?? null }));
                    setSelected(new Set());
                  }}
                >
                  下一页
                </Button>
              </div>
            </>
          )}
        </AsyncPanel>
      )}

      <Modal
        open={createOpen}
        title="新建文件夹"
        description={`父目录：${state.path}`}
        onClose={() => setCreateOpen(false)}
        footer={<Button type="submit" form="create-folder" variant="primary" loading={createFolder.pending}>创建</Button>}
      >
        <form id="create-folder" onSubmit={async event => { await submitCreate(event); }} className="space-y-3">
          {createFolder.error ? <Notice tone="error" title="创建失败">{describeApiError(createFolder.error)}</Notice> : null}
          <Field label="文件夹名称" hint="不能包含 /、NUL、单独的 . 或 ..">
            {ids => <TextInput id={ids.inputId} name="name" required />}
          </Field>
        </form>
      </Modal>

      <Modal
        open={renameTarget !== null}
        title="重命名"
        description={renameTarget ? `当前路径：${renameTarget.path}` : undefined}
        onClose={() => setRenameTarget(null)}
        footer={<Button type="submit" form="rename-entry" variant="primary" loading={rename.pending}>保存名称</Button>}
      >
        <form id="rename-entry" onSubmit={async event => { await submitRename(event); }} className="space-y-3">
          {rename.error ? <Notice tone="error" title="重命名失败">{describeApiError(rename.error)}</Notice> : null}
          <Field label="新名称">
            {ids => <TextInput id={ids.inputId} name="newName" defaultValue={renameTarget?.name ?? ""} required />}
          </Field>
        </form>
      </Modal>

      <Modal
        open={deleteTarget !== null}
        title="删除条目"
        description={deleteTarget ? `${deleteTarget.path}（${deleteTarget.type === "directory" ? "目录" : "文件"}）` : undefined}
        onClose={() => setDeleteTarget(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeleteTarget(null)} disabled={remove.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={submitDelete} loading={remove.pending}>确认删除</Button>
          </>
        }
      >
        {remove.error ? <Notice tone="error" title="删除失败">{describeApiError(remove.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">
          删除目录时服务端会递归处理；这不是可回滚操作。系统路径是否允许删除由服务端授权策略决定。
        </p>
      </Modal>

      <Modal
        open={transferOpen}
        title="发送到另一侧"
        description={`源：${node?.name ?? state.nodeId} · ${state.path}；目标：${otherNode?.name ?? other.nodeId ?? "未选择"} · ${other.path}`}
        onClose={() => setTransferOpen(false)}
        footer={
          <>
            <Button type="button" onClick={() => setTransferOpen(false)} disabled={transfer.pending}>取消</Button>
            <Button type="submit" form="transfer-form" variant="primary" loading={transfer.pending} disabled={sendDisabled}>确认发送</Button>
          </>
        }
      >
        <form id="transfer-form" onSubmit={submitTransfer} className="space-y-3">
          {transfer.error ? <Notice tone="error" title="传输未提交">{describeApiError(transfer.error)}</Notice> : null}
          {sameTarget ? <Notice tone="warning" title="源与目标相同">同节点同目录不能复制到自身。</Notice> : null}
          {selectedEntries.some(entry => entry.type === "symlink") ? <Notice tone="warning" title="包含符号链接">默认不跟随符号链接；请取消选择后再提交。</Notice> : null}
          <p className="text-sm">将发送 {selectedEntries.length} 个项目：{selectedEntries.map(entry => entry.name).join("、")}</p>
          <Field label="同名冲突策略">
            {ids => (
              <Select id={ids.inputId} value={conflictPolicy} onChange={event => setConflictPolicy(event.target.value as ConflictPolicy)}>
                <option value="skip">跳过（默认）</option>
                <option value="overwrite">覆盖</option>
                <option value="rename">自动重命名</option>
              </Select>
            )}
          </Field>
          <p className="text-xs text-muted-foreground">提交只创建异步复制任务，不表示文件已复制；取消不保证回滚已落地数据。</p>
        </form>
      </Modal>
    </div>
  );
}

export function FilesPage() {
  const session = useSession();
  const [paneA, setPaneA] = useState<PaneState>(INITIAL_PANE);
  const [paneB, setPaneB] = useState<PaneState>(INITIAL_PANE);
  const [transferDetail, setTransferDetail] = useState<TransferJob | null>(null);
  const [transferFilter, setTransferFilter] = useState<string>("");
  const [outcome, setOutcome] = useState<string | null>(null);

  const nodes = useResource(`files:nodes:${session.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const transfers = useResource(
    `files:transfers:${session.viewEpoch}:${transferFilter}`,
    signal => transfersApi.list({ ...(transferFilter ? { status: transferFilter } : {}), limit: 50 }, signal).then(result => result.data),
  );
  const cancelTransfer = useAction((id: string) => transfersApi.cancel(id));
  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];

  useEffect(() => {
    if (nodeItems.length === 0) return;
    setPaneA(current => current.nodeId ? current : { ...current, nodeId: nodeItems[0].id });
    setPaneB(current => current.nodeId ? current : { ...current, nodeId: (nodeItems[1] ?? nodeItems[0]).id });
  }, [nodeItems]);

  const transferItems = transfers.state.status === "ready" ? transfers.state.data.items : [];

  async function cancel(id: string) {
    const result = await cancelTransfer.run(id);
    if (result.ok) {
      setOutcome("取消请求已受理；已复制的部分数据不会被自动回滚。");
      transfers.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="文件互传"
        description="双栏独立浏览目录，跨节点复制通过唯一传输任务接口。符号链接默认不跟随；上传走真实字节通道（POST uploads → PUT content → POST complete 校验）。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">异步复制</span>}
        actions={<Button type="button" onClick={() => { nodes.reload(); transfers.reload(); }}>刷新</Button>}
      />
      <Notice tone="info" title="能力边界">
        上传会话会真实传输文件字节并由服务端校验字节数与 SHA-256（`PUT …/content` → `POST …/complete`）；
        远端节点传输通道未注入时服务端返回 503，界面如实区分“校验通过”与“未送达远端”。
        当前契约仍**没有下载端点**，本页不伪造下载成功。
      </Notice>
      {outcome ? <Notice tone="success" title="操作结果" actions={<Button size="sm" onClick={() => setOutcome(null)}>关闭</Button>}>{outcome}</Notice> : null}

      <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="节点列表加载失败" empty={<EmptyState title="没有可用节点" />} isEmpty={data => data.items.length === 0}>
        {() => (
          <div className="grid gap-4 xl:grid-cols-2">
            <FilePane
              label="左侧"
              state={paneA}
              setState={setPaneA}
              nodes={nodeItems}
              other={paneB}
              onTransferCreated={transfers.reload}
            />
            <FilePane
              label="右侧"
              state={paneB}
              setState={setPaneB}
              nodes={nodeItems}
              other={paneA}
              onTransferCreated={transfers.reload}
            />
          </div>
        )}
      </AsyncPanel>

      <Panel
        title="传输任务"
        description="GET /api/v1/transfers；进度与项目级结果以服务端为准。取消是请求，不保证回滚。"
        actions={
          <>
            <label className="text-xs text-muted-foreground">
              状态
              <select
                value={transferFilter}
                onChange={event => setTransferFilter(event.target.value)}
                className="ml-2 h-8 rounded-md border border-input bg-background px-2 text-xs"
              >
                <option value="">全部</option>
                {Object.entries(TRANSFER_STATUS_TEXT).map(([value, text]) => <option key={value} value={value}>{text}</option>)}
              </select>
            </label>
            <Button size="sm" onClick={transfers.reload} loading={transfers.state.status === "loading"}>刷新</Button>
          </>
        }
      >
        <AsyncPanel
          state={transfers.state}
          onRetry={transfers.reload}
          errorTitle="传输任务加载失败"
          empty={<EmptyState title="暂无传输任务" description="从任一栏选择条目发送后，会创建真实传输任务。" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <DataTable
              caption="文件传输任务"
              rows={transferItems}
              rowKey={job => job.id}
              columns={[
                { key: "id", header: "任务", render: job => <span className="font-mono text-xs">{job.id}</span> },
                { key: "route", header: "路径", render: job => `${job.sourceNodeId}:${job.sourcePaths.join(",")} → ${job.destinationNodeId}:${job.destinationDir}` },
                { key: "status", header: "状态", render: job => TRANSFER_STATUS_TEXT[job.status] ?? job.status },
                { key: "progress", header: "进度", align: "right", render: job => `${job.progressPercent}%` },
                { key: "bytes", header: "已复制", render: job => `${formatBytes(job.copiedBytes) ?? "0 B"} / ${formatBytes(job.totalBytes ?? null) ?? "未知"}` },
                { key: "created", header: "创建", render: job => formatDateTime(job.createdAt) ?? "—" },
                {
                  key: "actions",
                  header: "操作",
                  render: job => (
                    <div className="flex flex-wrap gap-1">
                      <Button size="sm" onClick={() => setTransferDetail(job)}>详情</Button>
                      <Button
                        size="sm"
                        variant="danger"
                        disabled={session.writesLocked || !["queued", "running", "cancelling"].includes(job.status)}
                        onClick={() => void cancel(job.id)}
                        loading={cancelTransfer.pending}
                      >
                        取消
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Modal
        open={transferDetail !== null}
        title={transferDetail ? `传输任务 ${transferDetail.id}` : "传输任务"}
        onClose={() => setTransferDetail(null)}
        wide
        footer={<Button type="button" onClick={() => setTransferDetail(null)}>关闭</Button>}
      >
        {transferDetail ? (
          <div className="space-y-3 text-sm">
            <p>状态：{TRANSFER_STATUS_TEXT[transferDetail.status] ?? transferDetail.status}</p>
            <p>进度：{transferDetail.progressPercent}%</p>
            <p>源：{transferDetail.sourceNodeId} · {transferDetail.sourcePaths.join("、")}</p>
            <p>目标：{transferDetail.destinationNodeId} · {transferDetail.destinationDir}</p>
            {transferDetail.error ? <Notice tone="error" title="传输错误">{transferDetail.error.code} · {transferDetail.error.message}</Notice> : null}
            {transferDetail.items?.length ? (
              <ul className="divide-y rounded-md border">
                {transferDetail.items.map((item, index) => (
                  <li key={`${item.path ?? index}`} className="px-3 py-2 text-xs">
                    <span className="font-mono">{item.path ?? "未知项目"}</span> · {item.state ?? "状态未提供"}
                    {item.error ? ` · ${item.error}` : ""}
                  </li>
                ))}
              </ul>
            ) : <p className="text-muted-foreground">服务端未返回逐项目结果；不能据此声称全部成功。</p>}
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
