import { useState } from "react";
import { useSession } from "@/app/session";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/form";
import { Notice } from "@/components/ui/feedback";
import { Panel } from "@/components/ui/panel";
import { usersApi } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction } from "@/lib/data/use-resource";

const RIGHTS = [
  { key: "nodes.write", label: "编辑节点资料" },
  { key: "nodes.suspend", label: "暂停任务派发" },
  { key: "nodes.revoke", label: "删除节点或吊销身份" },
] as const;

export function NodeManagementAccess({ nodeId, onGranted }: { readonly nodeId: string; readonly onGranted?: () => void }) {
  const session = useSession();
  const [selected, setSelected] = useState<readonly string[]>([]);
  const [done, setDone] = useState(false);
  const grant = useAction(async () => {
    if (!session.identity || selected.length === 0) return;
    await usersApi.createGrant({ principalId: session.identity.userId, resourceType: "node", resourceId: nodeId, permissions: ["nodes.read", ...selected] });
  });
  if (!session.identity?.isBootstrapAdmin && !session.identity?.roles.includes("administrator")) return null;
  return (
    <Panel title="当前账户的节点管理授权" description={session.identity.isBootstrapAdmin ? "首个初始化账户的节点管理权限已覆盖所有当前节点。" : "接入审批只授予读取权限。管理员可在这里明确授予当前节点的管理权限，其他节点不受影响。"}>
      {session.identity.isBootstrapAdmin ? (
        <p className="px-5 py-4 text-sm text-muted-foreground">首个初始化账户拥有所有当前节点的读取、编辑、暂停和删除权限；任务、凭据和危险操作仍遵循各自授权与审批要求。</p>
      ) : <div className="space-y-3 px-5 py-4">
        <fieldset disabled={session.writesLocked || grant.pending} className="flex flex-wrap gap-4">
          <legend className="sr-only">选择此节点的管理权限</legend>
          {RIGHTS.map(right => <label key={right.key} className="inline-flex items-center gap-2 text-sm"><Checkbox checked={selected.includes(right.key)} onChange={event => setSelected(values => event.target.checked ? [...values, right.key] : values.filter(value => value !== right.key))} />{right.label}</label>)}
        </fieldset>
        <Button disabled={session.writesLocked || selected.length === 0 || grant.pending} loading={grant.pending} onClick={async () => { setDone(false); const result = await grant.run(); if (result.ok) { setDone(true); session.revalidate(); onGranted?.(); } }}>授权当前账户管理此节点</Button>
        {done ? <Notice tone="success" title="授权已保存">已为当前账户授予所选权限，仅限本节点。</Notice> : null}
        {grant.error ? <Notice tone="error" title="授权失败">{describeApiError(grant.error)}</Notice> : null}
      </div>}
    </Panel>
  );
}
