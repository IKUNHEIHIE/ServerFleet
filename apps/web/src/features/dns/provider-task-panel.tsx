import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { publicCertificateApi, type NodeCSRTask } from "@/lib/api/endpoints-infra";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";

export function DNSProviderTaskPanel(){
 const session=useSession();
 const allowed=session.can("settings.manage");
 const [operation,setOperation]=useState<"test"|"import-zone">("test");
 const [account,setAccount]=useState("");const [zone,setZone]=useState("");const [provider,setProvider]=useState("");
 const [key,setKey]=useState<string|null>(null);const [task,setTask]=useState<NodeCSRTask|null>(null);const [error,setError]=useState<string|null>(null);
 const action=useAction((body:Parameters<typeof publicCertificateApi.inspectDNS>[0])=>publicCertificateApi.inspectDNS(body));
 const locked=!allowed||session.writesLocked||action.pending||key!==null;
 async function submit(event:FormEvent<HTMLFormElement>){
  event.preventDefault();const ids=operation==="test"?[account]:[account,zone,provider];if(ids.some(id=>!/^[a-zA-Z0-9_-]{1,128}$/.test(id))){setError("请填写账号 ID；导入时还需填写新的本地 Zone ID 与供应商 Zone ID。");return;}
  setError(null);const idempotencyKey=key??globalThis.crypto.randomUUID();setKey(idempotencyKey);
  const result=await action.run({operation,accountId:account,...(operation==="import-zone"?{zoneId:zone,providerZoneId:provider}:{}),idempotencyKey});
  if(result.ok&&result.data){setTask(result.data.data);setKey(null);}else if(result.error&&[400,401,403,422,429].includes(result.error.status))setKey(null);
 }
 return <Panel title="DNS 供应商任务" description="检查已登记账号，或导入供应商已有 Zone。这些任务读取供应商元数据，不创建或删除远端 Zone。">
  <div className="space-y-3 px-5 py-4">
   <form onSubmit={submit} className="space-y-3">
    <Field label="操作">{ids=><Select id={ids.inputId} value={operation} onChange={event=>setOperation(event.target.value as "test"|"import-zone")} disabled={locked}><option value="test">检查账号凭据</option><option value="import-zone">导入已有 Zone</option></Select>}</Field>
    <Field label="已登记的 DNS 账号 ID">{ids=><TextInput id={ids.inputId} value={account} onChange={event=>setAccount(event.target.value)} disabled={locked} required/>}</Field>
    {operation==="import-zone"?<><Field label="新的本地 Zone ID">{ids=><TextInput id={ids.inputId} value={zone} onChange={event=>setZone(event.target.value)} disabled={locked} required/>}</Field><Field label="供应商 Zone ID">{ids=><TextInput id={ids.inputId} value={provider} onChange={event=>setProvider(event.target.value)} disabled={locked} required/>}</Field></>:null}
    <Button type="submit" disabled={!allowed||session.writesLocked||action.pending} loading={action.pending}>登记 DNS 任务</Button>
   </form>
   {key?<Notice tone="warning" title="提交结果待核对">再次提交复用当前编号；先在任务列表核对结果。</Notice>:null}
   {task?<Notice tone="info" title="DNS 任务已登记">{task.taskId} · {task.status}。<a href="#/tasks" className="ml-2 underline">查看实际任务结果</a> 完成后刷新账号与 Zone 列表。</Notice>:null}
   {error?<Notice tone="error" title="输入无效">{error}</Notice>:null}
   {action.error?<Notice tone="error" title="任务未登记">{describeApiError(action.error)}</Notice>:null}
  </div>
 </Panel>;
}
