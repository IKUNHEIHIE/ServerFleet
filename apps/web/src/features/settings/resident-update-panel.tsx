import { useState } from "react";
import { ShieldCheck } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";

// Uses the same authenticated REST/CSRF adapter as the surrounding settings.
export function ResidentUpdatePanel() {
  const session = useSession();
  const allowed = session.can("system.update") || session.can("settings.manage") || session.can("update.manage");
  const [uncertain, setUncertain] = useState(false);
  const post = async (path: string, body: unknown) => (await apiClient.request<unknown>(path, { method: "POST", body })).data;
  const get = async (path: string) => (await apiClient.request<unknown>(path)).data;
  const [component, setComponent] = useState("agent");
  const [nodes, setNodes] = useState("");
  const [version, setVersion] = useState("");
  const [keyId, setKeyId] = useState("");
  const [releaseSha256, setReleaseSHA] = useState("");
  const [policySha256, setPolicySHA] = useState("");
  const [taskId, setTask] = useState("");
  const [message, setMessage] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit() {
    setBusy(true);
    try {
      const common = { version, keyId, releaseSha256, policySha256, idempotencyKey: crypto.randomUUID() };
      const result = (await post(
        component === "agent" ? "/api/v1/system/agent-fleet-updates" : "/api/v1/system/controller-update-tasks",
        component === "agent" ? { ...common, nodeIds: nodes.split(/[\s,]+/).filter(Boolean) } : common,
      )) as { taskId?: string; status?: string };
      setTask(result.taskId ?? "");
      setMessage(`已登记 ${result.status ?? ""}，需要独立真人管理员审批；这不代表已安装。`);
    } catch {
      setUncertain(true);
      setMessage("更新请求被拒绝或结果未确认，请先检查任务记录，勿重复提交未知执行。");
    } finally {
      setBusy(false);
    }
  }

  async function receipt() {
    setBusy(true);
    try {
      setMessage(JSON.stringify(await get(`/api/v1/system/controller-update-receipts/${encodeURIComponent(taskId)}`), null, 2));
    } catch {
      setMessage("尚无可核验的 Controller 安装回执；保持 UNKNOWN，勿重跑。");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-label="受监督的签名更新" className="min-w-0">
      <Panel
        title="签名组件更新"
        headingLevel={3}
        description="仅支持已配置独立监督进程的 Linux 主机。Agent 先 1 台再每批最多 5 台，非成功即停止扩批；不同架构或不同策略应分开审批。Controller 重启后的任务 UNKNOWN 与安装回执分别展示。"
        actions={
          <Badge tone="primary" dot>
            <ShieldCheck aria-hidden="true" className="size-3" />
            Supervised
          </Badge>
        }
      >
        <div className="space-y-4 px-5 py-4">
          {!allowed ? (
            <Notice tone="warning" title="需要管理员更新权限">
              <p role="alert">需要管理员更新权限。</p>
            </Notice>
          ) : null}

          <div className="grid gap-3.5 sm:grid-cols-2">
            <Field label="组件">
              {ids => (
                <Select id={ids.inputId} value={component} onChange={e => setComponent(e.target.value)}>
                  <option value="agent">远程 Agent 批次</option>
                  <option value="controller">Controller 自身</option>
                </Select>
              )}
            </Field>
            {component === "agent" ? (
              <Field label="按灰度顺序的节点 ID" hint="多个节点以逗号或空白分隔（1 → 5 分批）">
                {ids => <TextInput id={ids.inputId} value={nodes} onChange={e => setNodes(e.target.value)} placeholder="node-canary, node-prod-1" />}
              </Field>
            ) : (
              <Field label="升级目标">
                {() => (
                  <div className="flex h-9 items-center rounded-lg border border-border/80 bg-[#FAFBFD] px-3 text-xs font-medium text-muted-foreground">
                    主控进程（独立监督器核验与重启）
                  </div>
                )}
              </Field>
            )}
            <Field label="签名版本">
              {ids => <TextInput id={ids.inputId} value={version} onChange={e => setVersion(e.target.value)} placeholder="0.9.3" />}
            </Field>
            <Field label="已配置信任 Key ID">
              {ids => <TextInput id={ids.inputId} value={keyId} onChange={e => setKeyId(e.target.value)} placeholder="release-key-2026" />}
            </Field>
            <Field label="发行文件 SHA-256">
              {ids => <TextInput id={ids.inputId} value={releaseSha256} onChange={e => setReleaseSHA(e.target.value)} className="font-mono text-xs" />}
            </Field>
            <Field label="监督策略 SHA-256">
              {ids => <TextInput id={ids.inputId} value={policySha256} onChange={e => setPolicySHA(e.target.value)} className="font-mono text-xs" />}
            </Field>
          </div>

          <div className="flex flex-wrap items-center gap-2.5">
            <Button
              type="button"
              variant="primary"
              disabled={busy || uncertain || session.writesLocked || !allowed}
              onClick={() => void submit()}
            >
              创建人工审批更新任务
            </Button>
          </div>

          <div className="neurox-subcard space-y-3">
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-[14rem] flex-1">
                <Field label="任务 ID">
                  {ids => <TextInput id={ids.inputId} value={taskId} onChange={e => setTask(e.target.value)} placeholder="输入已创建的更新任务 ID" />}
                </Field>
              </div>
              {component === "controller" ? (
                <Button type="button" disabled={busy || !taskId} onClick={() => void receipt()}>
                  读取历史安装回执
                </Button>
              ) : null}
              {taskId ? (
                <a
                  href="#/tasks"
                  className="inline-flex h-9 items-center rounded-lg border border-primary/25 bg-primary-light px-3 text-xs font-semibold text-primary transition-colors hover:bg-primary/15"
                >
                  查看任务与逐节点结果
                </a>
              ) : null}
            </div>
            <pre
              role="status"
              className="min-h-[2.25rem] whitespace-pre-wrap break-all rounded-lg border border-border/70 bg-card px-3 py-2 font-mono text-xs text-muted-foreground"
            >
              {message || "等待登记或查询签名更新任务…"}
            </pre>
          </div>
        </div>
      </Panel>
    </section>
  );
}
