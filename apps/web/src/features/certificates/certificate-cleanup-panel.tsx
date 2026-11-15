import { useState } from "react";
import { RefreshCw, Trash2 } from "lucide-react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { certificateCleanupApi, type CertificateCleanupRecord } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

function stateTone(state: string): "success" | "warning" | "danger" | "neutral" | "info" {
  switch (state) {
    case "cleaned":
    case "succeeded":
    case "completed":
      return "success";
    case "pending":
    case "queued":
    case "retrying":
      return "warning";
    case "failed":
    case "exhausted":
      return "danger";
    case "canceled":
    case "cancelled":
      return "neutral";
    default:
      return "info";
  }
}

function CertificateCleanupContent() {
  const session = useSession();
  const list = useResource(
    `certs:cleanup:${session.viewEpoch}`,
    signal => certificateCleanupApi.list(signal).then(result => result.data),
  );
  const retryAction = useAction((cleanupId: number | string) => certificateCleanupApi.retry(cleanupId));
  const cancelAction = useAction((cleanupId: number | string) => certificateCleanupApi.cancel(cleanupId));
  const [busyId, setBusyId] = useState<number | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const records: readonly CertificateCleanupRecord[] = list.state.status === "ready" ? list.state.data : [];

  async function handleRetry(id: number) {
    setBusyId(id);
    setNotice(null);
    const result = await retryAction.run(id);
    setBusyId(null);
    if (result.ok && result.data) {
      setNotice(`清理记录 #${id} 重试请求已提交（当前状态：${result.data.data.state}）。`);
      list.reload();
    }
  }

  async function handleCancel(id: number) {
    setBusyId(id);
    setNotice(null);
    const result = await cancelAction.run(id);
    setBusyId(null);
    if (result.ok && result.data) {
      setNotice(`清理记录 #${id} 已取消后续自动重试（当前状态：${result.data.data.state}）。`);
      list.reload();
    }
  }

  return (
    <div className="space-y-4 px-5 py-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">
          查看 ACME DNS-01 验证 TXT 记录的持久化清理状态；存在重试耗尽或冲突时，可在此重试或取消清理任务。
        </p>
        <Button size="sm" variant="secondary" onClick={list.reload} loading={list.state.status === "loading"}>
          <RefreshCw aria-hidden="true" className="size-3.5" />
          刷新队列
        </Button>
      </div>

      {notice ? <Notice tone="info" title="清理队列已更新">{notice}</Notice> : null}
      {retryAction.error ? <Notice tone="error" title="重试清理失败">{describeApiError(retryAction.error)}</Notice> : null}
      {cancelAction.error ? <Notice tone="error" title="取消清理失败">{describeApiError(cancelAction.error)}</Notice> : null}

      <AsyncPanel
        state={list.state}
        onRetry={list.reload}
        errorTitle="ACME 清理队列加载失败"
        empty={<EmptyState title="暂无 ACME 清理记录" description="所有已完成的 ACME 验证记录均已清理，或当前身份暂无记录。" />}
        isEmpty={data => data.length === 0}
      >
        {() => (
          <DataTable
            caption="ACME 验证记录清理队列"
            rows={records}
            rowKey={item => String(item.id)}
            columns={[
              { key: "id", header: "ID", render: item => <span className="font-mono text-xs">#{item.id}</span> },
              { key: "fqdn", header: "FQDN / 域名", render: item => <span className="font-mono text-xs">{item.fqdn}</span> },
              { key: "recordId", header: "记录 ID", render: item => <span className="font-mono text-xs">{item.recordId}</span> },
              {
                key: "state",
                header: "状态",
                render: item => (
                  <Badge tone={stateTone(item.state)} dot>
                    {item.state}
                  </Badge>
                ),
              },
              { key: "attempts", header: "尝试次数", align: "right", render: item => item.attempts },
              { key: "error", header: "最近错误", render: item => item.lastErrorCode || "—" },
              { key: "updated", header: "更新时间", render: item => formatDateTime(item.updatedAt) ?? item.updatedAt },
              {
                key: "ops",
                header: "操作",
                render: item => (
                  <div className="flex flex-wrap gap-1.5">
                    <Button
                      size="sm"
                      variant="primary"
                      loading={retryAction.pending && busyId === item.id}
                      disabled={session.writesLocked || retryAction.pending || cancelAction.pending}
                      onClick={() => void handleRetry(item.id)}
                    >
                      重试清理
                    </Button>
                    <Button
                      size="sm"
                      variant="secondary"
                      loading={cancelAction.pending && busyId === item.id}
                      disabled={session.writesLocked || retryAction.pending || cancelAction.pending}
                      onClick={() => void handleCancel(item.id)}
                    >
                      取消清理
                    </Button>
                  </div>
                ),
              },
            ]}
          />
        )}
      </AsyncPanel>
    </div>
  );
}

export function CertificateCleanupPanel() {
  const session = useSession();
  const [open, setOpen] = useState(false);

  return (
    <Panel
      title="ACME 验证记录清理队列"
      description="跟踪并清理遗留的 TXT 记录。"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="neutral" dot>
            <Trash2 aria-hidden="true" className="size-3" />
            ACME Cleanup
          </Badge>
          <Button
            size="sm"
            variant={open ? "secondary" : "primary"}
            aria-expanded={open}
            onClick={() => setOpen(current => !current)}
          >
            {open ? "收起清理队列" : "查看 ACME 清理队列"}
          </Button>
        </div>
      }
    >
      {open ? (
        <CertificateCleanupContent key={session.viewEpoch} />
      ) : (
        <div className="px-5 py-4 text-xs leading-relaxed text-muted-foreground">
          当公开证书 ACME 签发完成后若 TXT 记录尚未删除（如 <code>issued_cleanup_pending</code>），可展开队列查看尝试次数、错误码并触发重试或取消清理。
        </div>
      )}
    </Panel>
  );
}
