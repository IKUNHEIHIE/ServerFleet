import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { Panel } from "@/components/ui/panel";
import { logCleanupApi, type LogCleanupPolicy } from "@/lib/api/endpoints-operations";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

export function LogCleanupPanel() {
  const session = useSession();
  const settings = useResource(
    `settings:log-cleanup:${session.viewEpoch}`,
    signal => logCleanupApi.get(signal).then(result => result.data),
    {
      refreshIntervalMs: 2500,
      shouldPoll: data => {
        const run = (data as LogCleanupPolicy).lastRun;
        return Boolean(run && !run.finishedAt);
      },
    },
  );

  return (
    <AsyncPanel state={settings.state} onRetry={settings.reload} errorTitle="日志清理设置加载失败">
      {policy => <LogCleanupForm key={policy.revision} policy={policy} locked={session.writesLocked} refresh={settings.refreshSilent} />}
    </AsyncPanel>
  );
}

function LogCleanupForm({ policy, locked, refresh }: { policy: LogCleanupPolicy; locked: boolean; refresh: () => void }) {
  const [days, setDays] = useState(String(policy.retentionDays));
  const [scheduled, setScheduled] = useState(policy.scheduled);
  const [confirm, setConfirm] = useState(false);
  const save = useAction((retentionDays: number) => logCleanupApi.configure(policy.revision, retentionDays, scheduled));
  const cleanup = useAction(() => logCleanupApi.run(policy.revision));
  const lastRun = policy.lastRun;
  const running = Boolean(lastRun && !lastRun.finishedAt);
  const disabled = locked || running || save.pending || cleanup.pending;
  const changed = Number(days) !== policy.retentionDays || scheduled !== policy.scheduled;
  const error = save.error ?? cleanup.error;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await save.run(Number(days));
    if (result.ok || result.error?.code === "LOG_CLEANUP_CONFLICT") refresh();
  }

  async function run() {
    const result = await cleanup.run();
    if (result.ok || result.error?.code === "LOG_CLEANUP_CONFLICT") {
      setConfirm(false);
      refresh();
    }
  }

  return (
    <div className="space-y-4">
      {error && !confirm ? <Notice tone="error" title="操作未完成">
        {error.code === "LOG_CLEANUP_CONFLICT" ? "设置已更新或正在清理，请确认当前状态后重试。" : describeApiError(error)}
      </Notice> : null}
      <Panel title="定时清理">
        <form onSubmit={submit} className="space-y-4 p-5">
          <label className="flex items-center gap-2 text-sm font-medium">
            <Checkbox checked={scheduled} disabled={disabled} onChange={event => setScheduled(event.target.checked)} />
            每天自动清理
          </label>
          <div className="max-w-48">
            <Field label="保留天数" hint="手动与定时清理使用同一设置。">
              {ids => <TextInput id={ids.inputId} aria-describedby={ids.hintId} type="number" min={1} max={3650}
                value={days} onChange={event => setDays(event.target.value)} required disabled={disabled} />}
            </Field>
          </div>
          {policy.nextRunAt ? <p className="text-sm text-muted-foreground">下次清理 {formatDateTime(policy.nextRunAt)}</p> : null}
          <Button type="submit" variant="primary" loading={save.pending} disabled={disabled || !changed}>保存设置</Button>
        </form>
      </Panel>
      <Panel title="手动清理" description={`保留最近 ${policy.retentionDays} 天的日志。`}
        actions={<Button variant="danger" disabled={disabled} onClick={() => setConfirm(true)}>一键清理日志</Button>}>
        <div className="px-5 py-4 text-sm" role="status">
          {lastRun?.finishedAt
            ? <p>{formatDateTime(lastRun.finishedAt)} 清理完成，已删除 {lastRun.auditEvents} 条审计事件和 {lastRun.taskLogs} 条任务日志。</p>
            : lastRun
              ? <p>正在清理，已删除 {lastRun.auditEvents} 条审计事件和 {lastRun.taskLogs} 条任务日志。</p>
              : <p className="text-muted-foreground">暂无清理记录。</p>}
        </div>
      </Panel>
      <Modal open={confirm} title="清理过期日志"
        description={`将永久删除 ${policy.retentionDays} 天前的审计事件和已结束任务的执行日志。`}
        onClose={() => { if (!cleanup.pending) setConfirm(false); }}
        footer={<>
          <Button disabled={cleanup.pending} onClick={() => setConfirm(false)}>取消</Button>
          <Button variant="danger" disabled={disabled} loading={cleanup.pending} onClick={run}>确认清理</Button>
        </>}>
        {cleanup.error ? <Notice tone="error" title="清理请求失败">{describeApiError(cleanup.error)}</Notice> : null}
      </Modal>
    </div>
  );
}
