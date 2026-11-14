import { useState, type ChangeEvent, type FormEvent } from "react";
import { RefreshCw, SendToBack } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { controlClassName, Field, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import type { NodeSummary } from "@/lib/api/dto";
import { fileDistributionsApi, type FileDistributionView } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
import { formatBytes } from "@/lib/format";

async function computeSha256Hex(buffer: ArrayBuffer): Promise<string> {
  if (!globalThis.crypto?.subtle) {
    throw new Error("当前浏览器环境不支持 WebCrypto SHA-256 摘要计算。");
  }
  const digest = await globalThis.crypto.subtle.digest("SHA-256", buffer);
  return [...new Uint8Array(digest)].map(b => b.toString(16).padStart(2, "0")).join("");
}

function parseTargets(raw: string): string[] {
  return [...new Set(raw.split(/[\s,，]+/).map(item => item.trim()).filter(Boolean))];
}

export function FileDistributionPanel({ nodes }: { readonly nodes: readonly NodeSummary[] }) {
  const session = useSession();
  const [open, setOpen] = useState(false);
  const [destinationDir, setDestinationDir] = useState("/tmp");
  const [targetsText, setTargetsText] = useState("");
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [lookupId, setLookupId] = useState("");
  const [distribution, setDistribution] = useState<FileDistributionView | null>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [statusMessage, setStatusMessage] = useState<string | null>(null);

  const canManage = session.can("file.write") || session.can("file.manage") || session.can("file.transfer");

  const startAction = useAction(async (file: File, dir: string, targetNodes: readonly string[]) => {
    const buffer = await file.arrayBuffer();
    const sha256 = await computeSha256Hex(buffer);
    const created = await fileDistributionsApi.create({
      destinationDir: dir,
      targetNodes,
      files: [{ name: file.name, sizeBytes: buffer.byteLength, sha256 }],
    });
    const distId = created.data.distributionId;
    setLookupId(distId);
    await fileDistributionsApi.uploadContent(distId, buffer, 0);
    const completed = await fileDistributionsApi.complete(distId);
    return completed.data;
  });

  const inspectAction = useAction((id: string) => fileDistributionsApi.get(id).then(result => result.data));
  const cancelAction = useAction((id: string) => fileDistributionsApi.cancel(id).then(result => result.data));

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0] ?? null;
    setSelectedFile(file);
    setLocalError(null);
  }

  function addNodeShortcut(nodeId: string) {
    const current = parseTargets(targetsText);
    if (current.includes(nodeId)) return;
    setTargetsText([...current, nodeId].join(", "));
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const dir = destinationDir.trim();
    const targetNodes = parseTargets(targetsText);
    if (!dir.startsWith("/") || dir.split("/").some(seg => seg === "..")) {
      setLocalError("目标目录必须为合法的绝对路径（不能包含 ..）。");
      return;
    }
    if (targetNodes.length === 0) {
      setLocalError("请至少指定一个目标节点 ID。");
      return;
    }
    if (!selectedFile) {
      setLocalError("请选择要分发到多台节点的本地文件。");
      return;
    }

    setLocalError(null);
    setStatusMessage(null);
    const result = await startAction.run(selectedFile, dir, targetNodes);
    if (result.ok && result.data) {
      setDistribution(result.data);
      setStatusMessage(
        `分发会话 ${result.data.distributionId} 已提交校验/入队（状态：${result.data.status}${result.data.taskId ? ` · 统一任务 ${result.data.taskId}` : ""}）。`,
      );
    }
  }

  async function handleInspect(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const id = lookupId.trim();
    if (!id) {
      setLocalError("请输入要查询的分发会话 ID。");
      return;
    }
    setLocalError(null);
    const result = await inspectAction.run(id);
    if (result.ok && result.data) {
      setDistribution(result.data);
    }
  }

  async function handleCancel(id: string) {
    setLocalError(null);
    const result = await cancelAction.run(id);
    if (result.ok && result.data) {
      setDistribution(result.data);
      setStatusMessage(`分发会话 ${result.data.distributionId} 已请求取消（当前状态：${result.data.status}）。`);
    }
  }

  return (
    <Panel
      title="多目标文件分发"
      description="POST /api/v1/files/distributions → PUT content → POST complete；单次上传向多台目标节点并发分发。"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="primary" dot>
            <SendToBack aria-hidden="true" className="size-3" />
            Multi-Target
          </Badge>
          <Button
            size="sm"
            variant={open ? "secondary" : "primary"}
            aria-expanded={open}
            onClick={() => setOpen(current => !current)}
          >
            {open ? "收起多目标分发" : "打开多目标文件分发"}
          </Button>
        </div>
      }
    >
      {open ? (
        <div className="space-y-4 px-5 py-4">
          <form onSubmit={handleSubmit} className="neurox-subcard space-y-3.5">
            <p className="text-sm font-semibold text-foreground">创建并发起多目标分发</p>
            {!canManage ? <Notice tone="warning" title="权限受限">当前身份缺少文件写入/传输权限。</Notice> : null}
            {localError ? <Notice tone="error" title="参数无效">{localError}</Notice> : null}
            {startAction.error ? <Notice tone="error" title="分发提交失败">{describeApiError(startAction.error)}</Notice> : null}

            <div className="grid gap-3.5 sm:grid-cols-2">
              <Field label="分发目标绝对目录">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    value={destinationDir}
                    onChange={event => setDestinationDir(event.target.value)}
                    disabled={!canManage || session.writesLocked || startAction.pending}
                    required
                  />
                )}
              </Field>
              <Field label="选择本地分发文件">
                {ids => (
                  <input
                    id={ids.inputId}
                    type="file"
                    onChange={handleFileChange}
                    disabled={!canManage || session.writesLocked || startAction.pending}
                    className={cn(controlClassName, "py-1.5 text-xs file:mr-3 file:rounded-lg file:border-0 file:bg-primary/10 file:px-2.5 file:py-1 file:text-xs file:font-semibold file:text-primary")}
                  />
                )}
              </Field>
            </div>

            <Field label="分发目标节点 ID（逗号或换行分隔）">
              {ids => (
                <TextArea
                  id={ids.inputId}
                  value={targetsText}
                  onChange={event => setTargetsText(event.target.value)}
                  rows={2}
                  placeholder="例如 node-1, node-2"
                  className="font-mono text-xs"
                  disabled={!canManage || session.writesLocked || startAction.pending}
                  required
                />
              )}
            </Field>

            {nodes.length > 0 ? (
              <div className="flex flex-wrap items-center gap-1.5">
                <span className="text-xs text-muted-foreground">快速添加节点：</span>
                {nodes.slice(0, 8).map(node => (
                  <Button
                    key={node.id}
                    type="button"
                    size="sm"
                    variant="secondary"
                    onClick={() => addNodeShortcut(node.id)}
                  >
                    + {node.name}
                  </Button>
                ))}
              </div>
            ) : null}

            <Button
              type="submit"
              size="sm"
              variant="primary"
              loading={startAction.pending}
              disabled={!canManage || session.writesLocked || !selectedFile}
            >
              上传并提交多目标分发
            </Button>
          </form>

          <form onSubmit={handleInspect} className="flex flex-wrap items-end gap-3 rounded-xl border border-border/70 bg-[#FAFBFD] p-3.5">
            <div className="min-w-[14rem] flex-1">
              <Field label="查询分发会话 ID">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    value={lookupId}
                    onChange={event => setLookupId(event.target.value)}
                    placeholder="输入 distributionId 查询进度或取消"
                    className="font-mono text-xs"
                  />
                )}
              </Field>
            </div>
            <Button type="submit" size="sm" variant="secondary" loading={inspectAction.pending}>
              <RefreshCw aria-hidden="true" className="size-3.5" />
              查询分发状态
            </Button>
          </form>

          {inspectAction.error ? <Notice tone="error" title="查询失败">{describeApiError(inspectAction.error)}</Notice> : null}
          {cancelAction.error ? <Notice tone="error" title="取消失败">{describeApiError(cancelAction.error)}</Notice> : null}
          {statusMessage ? <Notice tone="info" title="分发状态更新">{statusMessage}</Notice> : null}

          {distribution ? (
            <div className="neurox-subcard space-y-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="space-y-0.5 text-xs">
                  <p className="font-semibold text-foreground">
                    分发会话：<span className="font-mono">{distribution.distributionId}</span> · 状态{" "}
                    <Badge tone={distribution.status === "completed" ? "success" : "info"} dot>{distribution.status}</Badge>
                  </p>
                  <p className="text-muted-foreground">
                    目标路径：<span className="font-mono">{distribution.destination}</span> · 已接收{" "}
                    {formatBytes(distribution.bytesReceived) ?? `${distribution.bytesReceived} B`}
                    {distribution.sizeBytes !== undefined ? ` / ${formatBytes(distribution.sizeBytes) ?? `${distribution.sizeBytes} B`}` : ""}
                  </p>
                  {distribution.taskId ? (
                    <p className="text-muted-foreground">
                      关联统一任务：
                      <a className="ml-1 font-mono text-primary underline" href={`#/tasks/${encodeURIComponent(distribution.taskId)}`}>
                        {distribution.taskId}
                      </a>
                    </p>
                  ) : null}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    loading={inspectAction.pending}
                    onClick={() => void inspectAction.run(distribution.distributionId).then(r => { if (r.ok && r.data) setDistribution(r.data); })}
                  >
                    刷新会话
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="danger"
                    loading={cancelAction.pending}
                    disabled={!canManage || session.writesLocked || ["completed", "cancelled", "failed"].includes(distribution.status)}
                    onClick={() => void handleCancel(distribution.distributionId)}
                  >
                    取消分发
                  </Button>
                </div>
              </div>
              {distribution.error ? (
                <Notice tone="error" title="分发错误">{distribution.error.code} · {distribution.error.message}</Notice>
              ) : null}
              <DataTable
                caption="逐节点分发状态"
                rows={distribution.targetNodes}
                rowKey={target => target.nodeId}
                columns={[
                  { key: "node", header: "节点", render: target => <span className="font-mono text-xs">{target.nodeId}</span> },
                  { key: "status", header: "状态", render: target => target.status },
                  { key: "bytes", header: "字节", align: "right", render: target => formatBytes(target.bytes) ?? target.bytes },
                  { key: "sha256", header: "SHA-256", render: target => <span className="font-mono text-xs">{target.sha256 || "—"}</span> },
                  { key: "err", header: "错误", render: target => target.error ? `${target.error.code}: ${target.error.message}` : "—" },
                ]}
              />
            </div>
          ) : null}
        </div>
      ) : (
        <div className="px-5 py-4 text-xs leading-relaxed text-muted-foreground">
          上传单个文件至主控经 SHA-256 校验后，通过统一任务框架一次向多个受控节点分发。点击“打开多目标文件分发”展开表单与会话查询。
        </div>
      )}
    </Panel>
  );
}
