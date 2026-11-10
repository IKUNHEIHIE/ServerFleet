import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";
interface CommandReceipt{readonly taskId:string;readonly status:string;readonly planHash:string;readonly approvalRequired:boolean}
export function CommandProfilePanel(){
 const session=useSession();
 const [node,setNode]=useState("");const [profile,setProfile]=useState("");const [policy,setPolicy]=useState("");const [argumentsText,setArguments]=useState("[]");const [privilege,setPrivilege]=useState("normal");const [seconds,setSeconds]=useState("60");
 const [operation,setOperation]=useState<string|null>(null);const [receipt,setReceipt]=useState<CommandReceipt|null>(null);const [error,setError]=useState<string|null>(null);
 const action=useAction((body:Record<string,string>)=>apiClient.request<CommandReceipt>("/api/v1/commands/tasks",{method:"POST",body}));
 const allowed=session.can("tasks.exec.v1")||session.can("host.root")||session.can("settings.manage");const locked=!allowed||session.writesLocked||action.pending||operation!==null;
 async function submit(event:FormEvent<HTMLFormElement>){
  event.preventDefault();let argumentsJSON:string;
  try{const args:unknown=JSON.parse(argumentsText);if(!Array.isArray(args)||args.length>32||args.some(arg=>typeof arg!=="string"||arg.length>512||/[\x00-\x1f\x7f]/.test(arg)))throw new Error();argumentsJSON=JSON.stringify(args);}catch{setError("参数须是最多 32 项的字符串 JSON 数组，不接受控制字符。");return;}
  if(!/^[A-Za-z0-9_-]{1,128}$/.test(node)||!/^[A-Za-z0-9_-]{1,128}$/.test(profile)||!/^[a-f0-9]{64}$/.test(policy)||!/^[1-9][0-9]{0,2}$/.test(seconds)||Number(seconds)>300||argumentsJSON.length>1024){setError("请填写节点、已配置的程序 profile 与策略摘要；超时为 1—300 秒。");return;}
  setError(null);const key=operation??globalThis.crypto.randomUUID();setOperation(key);
  const result=await action.run({nodeId:node,profileId:profile,policySha256:policy,arguments:argumentsJSON,timeoutSeconds:seconds,privilege,idempotencyKey:key});
  if(result.ok&&result.data){setReceipt(result.data.data);setOperation(null);}else if(result.error&&[400,401,403,422,429].includes(result.error.status))setOperation(null);
 }
 return <Panel title="受控命令" description="使用节点预先配置并校验摘要的程序。普通及提权操作均先登记审批；提权另需节点的 root 执行助手。">
  <div className="space-y-3 px-5 py-4"><form onSubmit={submit} className="grid gap-3 sm:grid-cols-2">
   <Field label="节点 ID">{ids=><TextInput id={ids.inputId} value={node} onChange={event=>setNode(event.target.value)} disabled={locked} required/>}</Field>
   <Field label="程序 profile ID">{ids=><TextInput id={ids.inputId} value={profile} onChange={event=>setProfile(event.target.value)} disabled={locked} required/>}</Field>
   <Field label="节点策略 SHA-256">{ids=><TextInput id={ids.inputId} value={policy} onChange={event=>setPolicy(event.target.value)} disabled={locked} required/>}</Field>
   <Field label="执行权限">{ids=><Select id={ids.inputId} value={privilege} onChange={event=>setPrivilege(event.target.value)} disabled={locked}><option value="normal">普通用户</option><option value="root">受控提权（管理员审批）</option></Select>}</Field>
   <Field label="参数 JSON 数组" hint="参数作为独立 argv 传给已配置程序，不自动展开为 Shell。">{ids=><TextArea id={ids.inputId} value={argumentsText} onChange={event=>setArguments(event.target.value)} disabled={locked} rows={3} maxLength={1024}/>}</Field>
   <Field label="超时秒数">{ids=><TextInput id={ids.inputId} type="number" min={1} max={300} value={seconds} onChange={event=>setSeconds(event.target.value)} disabled={locked} required/>}</Field>
   <Button type="submit" loading={action.pending} disabled={!allowed||session.writesLocked||action.pending}>登记人工审批任务</Button>
  </form>
  {operation?<Notice tone="warning" title="提交结果待核对">再次提交复用同一编号，请先检查任务列表。</Notice>:null}
  {receipt?<Notice tone="info" title="命令任务已登记">{receipt.taskId} · {receipt.status}。<a href="#/tasks" className="ml-2 underline">查看与审批</a></Notice>:null}
  {error?<Notice tone="error" title="输入无效">{error}</Notice>:null}{action.error?<Notice tone="error" title="任务未登记">{describeApiError(action.error)}</Notice>:null}
  <p className="text-xs text-muted-foreground">结果只证明该命令的已观察退出状态，不代表服务健康；此 profile 不把任意程序输出写入任务日志。</p></div>
 </Panel>;
}
