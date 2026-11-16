import { useState } from "react";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Select } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { transfersApi } from "@/lib/api/endpoints";
import type { TransferJob, TransferStatus } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { useSession } from "@/app/session";
import { formatBytes, formatDateTime } from "@/lib/format";

const TRANSFER_STATUS_TEXT: Record<TransferStatus, string> = {
  queued: "\u6392\u961f\u4e2d",
  running: "\u6267\u884c\u4e2d",
  completed: "\u5df2\u5b8c\u6210",
  partial_failure: "\u90e8\u5206\u5931\u8d25",
  failed: "\u5931\u8d25",
  cancelling: "\u53d6\u6d88\u4e2d",
  cancelled: "\u5df2\u53d6\u6d88",
};

export function TransferHistoryModal({ open, onClose }: { readonly open: boolean; readonly onClose: () => void }) {
  const session = useSession();
  const [filter, setFilter] = useState("");
  const [detail, setDetail] = useState<TransferJob | null>(null);
  const transfers = useResource(
    `files:transfers:${session.viewEpoch}:${filter}`,
    signal => transfersApi.list({ ...(filter ? { status: filter } : {}), limit: 50 }, signal).then(result => result.data),
    { enabled: open },
  );
  const cancelTransfer = useAction((id: string) => transfersApi.cancel(id));
  const items = transfers.state.status === "ready" ? transfers.state.data.items : [];

  async function cancel(id: string) {
    const result = await cancelTransfer.run(id);
    if (result.ok) transfers.reload();
  }

  if (detail) {
    return (
      <Modal
        open={open}
        title={`\u4f20\u8f93\u4efb\u52a1 ${detail.id}`}
        onClose={() => setDetail(null)}
        wide
        footer={
          <>
            <Button type="button" onClick={() => setDetail(null)}>\u8fd4\u56de\u5217\u8868</Button>
            <Button type="button" onClick={onClose}>\u5173\u95ed</Button>
          </>
        }
      >
        <div className="space-y-3 text-sm">
          <p>\u72b6\u6001\uff1a{TRANSFER_STATUS_TEXT[detail.status] ?? detail.status}</p>
          <p>\u8fdb\u5ea6\uff1a{detail.progressPercent}%</p>
          <p>\u6e90\uff1a{detail.sourceNodeId} \u00b7 {detail.sourcePaths.join("\u3001")}</p>
          <p>\u76ee\u6807\uff1a{detail.destinationNodeId} \u00b7 {detail.destinationDir}</p>
          {detail.error ? <Notice tone="error" title="\u4f20\u8f93\u9519\u8bef">{detail.error.code} \u00b7 {detail.error.message}</Notice> : null}
          {detail.items?.length ? (
            <ul className="divide-y divide-border/75 rounded border">
              {detail.items.map((item, index) => (
                <li key={`${item.path ?? index}`} className="px-3.5 py-2 text-xs">
                  <span className="font-mono">{item.path ?? "\u672a\u77e5\u9879\u76ee"}</span> \u00b7 {item.state ?? "\u72b6\u6001\u672a\u63d0\u4f9b"}
                  {item.error ? ` \u00b7 ${item.error}` : ""}
                </li>
              ))}
            </ul>
          ) : <p className="text-muted-foreground">\u670d\u52a1\u7aef\u672a\u8fd4\u56de\u9010\u9879\u76ee\u7ed3\u679c\uff1b\u4e0d\u80fd\u636e\u6b64\u58f0\u79f0\u5168\u90e8\u6210\u529f\u3002</p>}
        </div>
      </Modal>
    );
  }

  return (
    <Modal
      open={open}
      title="\u4f20\u8f93\u4efb\u52a1"
      description="\u53d6\u6d88\u662f\u8bf7\u6c42\uff0c\u4e0d\u4fdd\u8bc1\u56de\u6eda\u3002"
      wide
      onClose={onClose}
      footer={<Button type="button" onClick={onClose}>\u5173\u95ed</Button>}
    >
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <label className="text-xs text-muted-foreground">
            \u72b6\u6001
            <Select value={filter} onChange={event => setFilter(event.target.value)} className="ml-2 mt-0 inline-block h-8 w-auto px-2 py-1 text-xs">
              <option value="">\u5168\u90e8</option>
              {Object.entries(TRANSFER_STATUS_TEXT).map(([value, text]) => <option key={value} value={value}>{text}</option>)}
            </Select>
          </label>
          <Button size="sm" onClick={transfers.reload} loading={transfers.state.status === "loading"}>\u5237\u65b0</Button>
        </div>
        {cancelTransfer.error ? <Notice tone="error" title="\u53d6\u6d88\u672a\u5b8c\u6210">{describeApiError(cancelTransfer.error)}</Notice> : null}
        <AsyncPanel
          state={transfers.state}
          onRetry={transfers.reload}
          errorTitle="\u4f20\u8f93\u4efb\u52a1\u52a0\u8f7d\u5931\u8d25"
          empty={<EmptyState title="\u6682\u65e0\u4f20\u8f93\u4efb\u52a1" description="\u4ece\u4efb\u4e00\u680f\u9009\u62e9\u6761\u76ee\u53d1\u9001\u540e\uff0c\u4f1a\u521b\u5efa\u771f\u5b9e\u4f20\u8f93\u4efb\u52a1\u3002" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <DataTable
              caption="\u6587\u4ef6\u4f20\u8f93\u4efb\u52a1"
              rows={items}
              rowKey={job => job.id}
              columns={[
                { key: "id", header: "\u4efb\u52a1", render: job => <span className="font-mono text-xs">{job.id}</span> },
                { key: "route", header: "\u8def\u5f84", render: job => `${job.sourceNodeId}:${job.sourcePaths.join(",")} \u2192 ${job.destinationNodeId}:${job.destinationDir}` },
                { key: "status", header: "\u72b6\u6001", render: job => TRANSFER_STATUS_TEXT[job.status] ?? job.status },
                {
                  key: "progress",
                  header: "\u8fdb\u5ea6",
                  align: "right",
                  render: job => (
                    <span className="inline-flex items-center justify-end gap-2">
                      <span className="h-1.5 w-16 overflow-hidden rounded-full bg-secondary">
                        <span className="block h-full rounded-full bg-primary transition-all duration-300" style={{ width: `${Math.max(0, Math.min(100, job.progressPercent))}%` }} />
                      </span>
                      <span className="font-mono text-xs">{`${job.progressPercent}%`}</span>
                    </span>
                  ),
                },
                { key: "bytes", header: "\u5df2\u590d\u5236", render: job => `${formatBytes(job.copiedBytes) ?? "0 B"} / ${formatBytes(job.totalBytes ?? null) ?? "\u672a\u77e5"}` },
                { key: "created", header: "\u521b\u5efa", render: job => formatDateTime(job.createdAt) ?? "\u2014" },
                {
                  key: "actions",
                  header: "\u64cd\u4f5c",
                  render: job => (
                    <div className="flex flex-wrap gap-1">
                      <Button size="sm" onClick={() => setDetail(job)}>\u8be6\u60c5</Button>
                      <Button size="sm" variant="danger" disabled={session.writesLocked || !["queued", "running", "cancelling"].includes(job.status)} onClick={() => void cancel(job.id)} loading={cancelTransfer.pending}>
                        \u53d6\u6d88
                      </Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </div>
    </Modal>
  );
}
