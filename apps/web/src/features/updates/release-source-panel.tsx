import { useState } from "react";
import { CloudDownload, Shield } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Panel } from "@/components/ui/panel";
import { releaseSourceApi, type BackupTaskAdmission } from "@/lib/api/endpoints-system";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";

export function ReleaseSourcePanel() {
  const session = useSession();
  const allowed = session.can("system.update") || session.can("settings.manage") || session.can("update.manage");
  const source = useResource(`release-source:${session.viewEpoch}`, async signal => (await releaseSourceApi.get(signal)).data, { enabled: allowed });
  const acquire = useAction((key: string) => releaseSourceApi.acquire(key));
  const [key, setKey] = useState<string | null>(null);
  const [task, setTask] = useState<BackupTaskAdmission | null>(null);

  async function submit() {
    const operationKey = key ?? globalThis.crypto.randomUUID();
    setKey(operationKey);
    const result = await acquire.run(operationKey);
    if (result.ok && result.data) {
      setTask(result.data.data);
      setKey(null);
    }
  }

  return (
    <Panel
      title="可信 Agent 发行工件"
      description="从管理员固定的 HTTPS 来源拉取已签名工件。拉取通过统一任务执行，不自动安装。"
      actions={
        <Badge tone="info" dot>
          <Shield aria-hidden="true" className="size-3" />
          Signed Artifact
        </Badge>
      }
    >
      <div className="space-y-4 px-5 py-4">
        {!allowed ? <Notice tone="warning" title="需要管理员权限">当前身份不能拉取更新工件。</Notice> : null}
        {source.state.status === "loading" ? <p role="status" className="text-sm text-muted-foreground">正在读取可信来源…</p> : null}
        {source.state.status === "error" ? (
          <Notice tone="info" title="可信在线来源不可用">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span>{describeApiError(source.state.error)}</span>
              <Button size="sm" onClick={source.reload}>重新读取</Button>
            </div>
          </Notice>
        ) : null}
        {source.state.status === "ready" ? (
          <div className="neurox-subcard space-y-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-border/70 bg-card p-3">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">可信来源主机</p>
                <p className="mt-1 font-mono text-sm font-semibold text-foreground">来源 {source.state.data.host}</p>
              </div>
              <div className="rounded-lg border border-border/70 bg-card p-3">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">候选发行版本</p>
                <p className="mt-1 font-mono text-sm font-semibold text-primary">候选版本 {source.state.data.version}</p>
              </div>
            </div>
            <div className="rounded-lg border border-border/70 bg-card p-3">
              <p className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">来源配置摘要 (SHA-256)</p>
              <p className="mt-1 break-all font-mono text-xs text-foreground">来源配置摘要 {source.state.data.sourceSha256}</p>
            </div>
            <div className="flex flex-wrap items-center gap-2.5">
              <Button variant="primary" onClick={() => void submit()} loading={acquire.pending} disabled={session.writesLocked || !allowed}>
                <CloudDownload aria-hidden="true" className="size-4" />
                登记签名工件拉取任务
              </Button>
            </div>
            {source.state.refreshError ? <Notice tone="warning" title="来源刷新失败">{describeApiError(source.state.refreshError)}</Notice> : null}
          </div>
        ) : null}
        {key ? <Notice tone="warning" title="请求结果待核对">再次提交会复用当前请求编号。先查看任务状态；页面不会自动重试未知结果。</Notice> : null}
        {acquire.error ? <Notice tone="error" title="拉取任务未完成">{describeApiError(acquire.error)}</Notice> : null}
        {task ? (
          <Notice tone="info" title="已登记统一任务">
            任务 {task.taskId} · {task.status}。{task.approvalRequired ? "请先在任务页完成人工审批。" : "下载、签名和字节校验结果请在任务页查看。"}
            <a href="#/tasks" className="ml-2 font-semibold text-primary underline">查看任务</a>
          </Notice>
        ) : null}
        <p className="text-xs leading-5 text-muted-foreground">
          拉取成功后仍需单独准备更新计划、审批安装并核对版本、健康与回滚结果。来源地址、信任公钥和落盘目录均不接受网页输入。
        </p>
      </div>
    </Panel>
  );
}
