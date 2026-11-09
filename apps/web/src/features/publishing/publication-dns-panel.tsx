import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { publicationsApi } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

export function PublicationDNSPanel({publicationId}:{readonly publicationId:string}){
 const session=useSession();
 const evidence=useResource(`publication-dns:${session.viewEpoch}:${publicationId}`,signal=>publicationsApi.dnsTasks(publicationId,signal).then(response=>response.data));
 if(evidence.state.status==="loading"||evidence.state.status==="idle")return <p role="status" className="text-xs text-muted-foreground">正在读取 DNS 任务观察…</p>;
 if(evidence.state.status==="error")return <div className="space-y-2"><Notice tone="warning" title="DNS 任务观察未就绪">{describeApiError(evidence.state.error)}</Notice><Button size="sm" onClick={evidence.reload}>重新读取 DNS 状态</Button></div>;
 const batch=evidence.state.data;
 return <section aria-label="发布 DNS 任务观察" className="space-y-2 rounded-lg border p-3">
  <div className="flex flex-wrap items-center justify-between gap-2"><p>DNS 观察：{batch.state} · {formatDateTime(batch.updatedAt)??"—"}</p><Button size="sm" onClick={evidence.reload}>刷新 DNS 状态</Button></div>
  {batch.applyTaskId?<p className="break-all text-xs">执行任务：{batch.applyTaskId}</p>:null}
  {batch.rollbackTaskId?<p className="break-all text-xs">补偿任务：{batch.rollbackTaskId}</p>:null}
  <a href="#/tasks" className="text-xs underline">查看任务、完成人工审批并核对执行结果</a>
  {batch.state==="unknown"||batch.state==="partial"?<Notice tone="warning" title="DNS 结果需要核对">已有记录身份会保留；不会自动重放未知创建或删除。</Notice>:null}
  <ol className="space-y-1 text-xs">{batch.descriptor.records.map((record,index)=><li key={`${record.type}:${record.name}:${record.content}`} className="break-all">{record.type} {record.name} → {record.content} · {batch.observations[index]?.state??"尚未观察"}{batch.observations[index]?.recordId?` · ${batch.observations[index]?.recordId}`:""}</li>)}</ol>
  <p className="text-xs text-muted-foreground">审批后使用“继续发布”读取同一任务并推进。未知结果需先核对；复用的记录不会被发布回滚删除。</p>
  {evidence.state.refreshError?<Notice tone="warning" title="刷新失败">{describeApiError(evidence.state.refreshError)}</Notice>:null}
 </section>;
}
