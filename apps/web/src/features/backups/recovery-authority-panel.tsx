import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";
import { describeApiError } from "@/lib/api/errors";
import { useResource } from "@/lib/data/use-resource";

interface Authority {
 readonly mode: "standard" | "supervised" | "restored_supervised";
 readonly authorityId?: string;
 readonly trustGeneration?: number;
 readonly revocationSequence?: number;
 readonly continuousRecoveryFence: boolean;
 readonly recoveryActivationObserved: boolean;
 readonly agentReenrollmentRequired?: boolean;
}
export function RecoveryAuthorityPanel() {
 const session=useSession();
 const allowed=session.can("backup.manage")||session.can("system.backup")||session.can("settings.manage");
 const status=useResource(`recovery-authority:${session.viewEpoch}`,async signal=>(await apiClient.request<Authority>("/api/v1/system/recovery-authority",{signal})).data,{enabled:allowed});
 const data=status.state.status==="ready"?status.state.data:null;
 return <Panel title="恢复权威状态" description="恢复激活需离线签名授权与独立监督进程的实际隔离证明。此处只读取状态。">
  <div className="space-y-3 px-5 py-4">
   {!allowed?<p>需要管理员权限读取恢复状态。</p>:null}
   {status.state.status==="loading"?<p role="status">正在核验主控状态…</p>:null}
   {status.state.status==="error"?<Notice tone="error" title="恢复状态未确认">{describeApiError(status.state.error)}</Notice>:null}
   {data?<>
    <p>模式：{data.mode==="restored_supervised"?"恢复后的受监督主控":data.mode==="supervised"?"受监督主控":"常规主控"}</p>
    {data.authorityId?<p className="break-all">权威 ID：{data.authorityId}</p>:null}
    {data.recoveryActivationObserved?<Notice tone="info" title="本次读取已通过持续隔离核验">信任代次 {data.trustGeneration}，撤销序列 {data.revocationSequence}。旧节点证书不可继续使用，须由管理员重新注册节点并核对权限。</Notice>:<p>未观察到灾备激活回执，不能据此认定已完成灾备演练。</p>}
   </>:null}
   {allowed?<Button size="sm" onClick={status.reload} disabled={status.state.status==="loading"}>重新核验</Button>:null}
   <p className="text-xs text-muted-foreground">恢复密钥及根私钥留在离线设备；页面不接收私钥，也不能解除隔离。持续证明失效时，主控拒绝执行。</p>
  </div>
 </Panel>;
}
