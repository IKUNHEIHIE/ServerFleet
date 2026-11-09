import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
interface Candidate{readonly executionId:string;readonly nodeId:string;readonly destination:string;readonly confirmedOffset:number;readonly size:number;readonly sha256:string}
export function AgentResumePanel(){
 const session=useSession();const allowed=session.can("files.resume")||session.can("files.write")||session.can("settings.manage");
 const [oldTask,setOldTask]=useState("");const [candidates,setCandidates]=useState<readonly Candidate[]>([]);const [keys,setKeys]=useState<Record<string,string>>({});const [receipt,setReceipt]=useState<string|null>(null);
 const read=useAction((id:string)=>apiClient.request<{items:readonly Candidate[]}>(`/api/v1/files/resume-candidates/${encodeURIComponent(id)}`));
 const resume=useAction((executionId:string,idempotencyKey:string)=>apiClient.request<{taskId:string;status:string}>("/api/v1/files/resume-tasks",{method:"POST",body:{executionId,idempotencyKey}}));
 async function inspect(event:FormEvent<HTMLFormElement>){event.preventDefault();setCandidates([]);setReceipt(null);if(!/^[A-Za-z0-9_-]{1,128}$/.test(oldTask))return;const result=await read.run(oldTask);if(result.ok&&result.data)setCandidates(result.data.data.items);}
 async function submit(candidate:Candidate){const key=keys[candidate.executionId]??globalThis.crypto.randomUUID();setKeys(previous=>({...previous,[candidate.executionId]:key}));const result=await resume.run(candidate.executionId,key);if(result.ok&&result.data){setReceipt(`${result.data.data.taskId} · ${result.data.data.status}`);setKeys(previous=>{const next={...previous};delete next[candidate.executionId];return next;});}}
 return <Panel title="节点传输恢复" description="先读取中断任务的已确认偏移。恢复使用新的审批任务；节点还须证明暂存前缀完整且从未开始最终落盘。">
  <div className="space-y-3 px-5 py-4"><form onSubmit={inspect} className="flex flex-wrap items-end gap-3"><Field label="原文件任务 ID">{ids=><TextInput id={ids.inputId} value={oldTask} onChange={event=>setOldTask(event.target.value)} disabled={!allowed||read.pending} required/>}</Field><Button type="submit" loading={read.pending} disabled={!allowed||read.pending}>读取可恢复片段</Button></form>
   {candidates.length?<ul className="space-y-2">{candidates.map(candidate=><li key={candidate.executionId} className="space-y-1 rounded-lg border p-3 text-sm"><p className="break-all">{candidate.nodeId} · {candidate.destination}</p><p>已确认 {candidate.confirmedOffset} / {candidate.size} 字节</p><Button size="sm" onClick={()=>void submit(candidate)} loading={resume.pending} disabled={!allowed||session.writesLocked||resume.pending}>登记恢复审批任务</Button></li>)}</ul>:<p className="text-xs text-muted-foreground">尚无可恢复片段；最终落盘不明、缺少已确认字节或未保留完整源文件时，不能自动恢复。</p>}
   {receipt?<Notice tone="info" title="恢复任务已登记">{receipt}。<a href="#/tasks" className="ml-2 underline">查看与审批</a></Notice>:null}
   {read.error?<Notice tone="error" title="未读取恢复信息">{describeApiError(read.error)}</Notice>:null}{resume.error?<Notice tone="error" title="恢复任务未登记">{describeApiError(resume.error)}</Notice>:null}
  </div>
 </Panel>;
}
