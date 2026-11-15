import { useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { enrollmentApi } from "@/lib/api/endpoints";
import type { NodeEnrollment } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime, formatDuration } from "@/lib/format";

import { InstallationPanel } from "./installation-panel";

type Decision = "approve" | "reject";

function randomNodeName() {
  return `node-${crypto.randomUUID().replaceAll("-", "").slice(0, 8)}`;
}

export function NodeAddPage() {
  const session = useSession();
  const [nodeName, setNodeName] = useState(randomNodeName);
  const [customName, setCustomName] = useState(false);
  const [expiryMinutes, setExpiryMinutes] = useState("10");
  const maxUses = 1;
  const [replaceNodeId, setReplaceNodeId] = useState("");
  const [token, setToken] = useState<{ id: string; token: string; expiresAt: string; name: string; replaceNodeId?: string } | null>(null);
  const [copyStatus, setCopyStatus] = useState<string | null>(null);
  const [decision, setDecision] = useState<{ enrollment: NodeEnrollment; decision: Decision } | null>(null);
  const [reason, setReason] = useState("");
  const [grantRead, setGrantRead] = useState(true);
  const [outcome, setOutcome] = useState<string | null>(null);

  const canManage = session.can("node.manage") || session.can("node.approve") || session.can("node.enroll");
  const locked = !canManage || session.writesLocked;
  const validName = nodeName.trim().length > 0 && nodeName.length <= 128 && !/[\x00-\x1f\x7f]/.test(nodeName);

  const list = useResource(`enrollments:list:${session.viewEpoch}`, signal =>
    enrollmentApi.list(signal).then(result => result.data),
  );
  const createToken = useAction((name: string) =>
    enrollmentApi.createToken({
      name,
      expiryMinutes: Number(expiryMinutes),
      maxUses: Number(maxUses),
      ...(replaceNodeId.trim() ? { replaceNodeId: replaceNodeId.trim() } : {}),
    }),
  );
  const decide = useAction((enrollmentId: string, body: { decision: Decision; reason?: string; grantRead?: boolean }) =>
    enrollmentApi.decide(enrollmentId, body),
  );

  async function generate() {
    if (locked || createToken.pending || !validName) return;
    const name = token && !customName ? randomNodeName() : nodeName.trim();
    setOutcome(null);
    setCopyStatus(null);
    const result = await createToken.run(name);
    if (result.ok && result.data) {
      setNodeName(name);
      setToken({ ...result.data.data, name, ...(replaceNodeId.trim() ? { replaceNodeId: replaceNodeId.trim() } : {}) });
    }
  }

  function removeToken() {
    setToken(null);
    setCopyStatus(null);
    if (!customName) setNodeName(randomNodeName());
  }

  async function copyToken() {
    if (!token) return;
    try {
      await navigator.clipboard.writeText(token.token);
      setCopyStatus("已复制到剪贴板。请立即交给安装流程，不要写入聊天或日志。");
    } catch {
      setCopyStatus("浏览器拒绝剪贴板访问；请手动选择并在受控终端使用，避免粘贴到不受信页面。");
    }
  }

  async function submitDecision() {
    if (!decision) return;
    const result = await decide.run(decision.enrollment.id, {
      decision: decision.decision,
      ...(decision.decision === "approve" ? { grantRead } : {}),
      ...(reason.trim() ? { reason: reason.trim() } : {}),
    });
    if (result.ok) {
      setOutcome(decision.decision === "approve" ? grantRead ? "已批准该接入请求，并授予当前账户该节点的读取权限。" : "已批准该接入请求；读取节点前请配置资源授权。" : "已拒绝该接入请求。");
      setDecision(null);
      setReason("");
      list.reload();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="添加节点"
        description="生成一次性短期接入凭证、审批接入请求并查看状态。凭证与节点身份不可永久共用。"
        badge={<Badge tone="primary">接入</Badge>}
      />

      {!canManage ? (
        <Notice tone="warning" title="操作入口已锁定">
          当前账户没有节点接入或审批权限，仍可查看请求，请联系管理员授权。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="会话安全验证未完成">
          请重新登录后再生成凭证或审批接入请求。
        </Notice>
      ) : null}
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}

      <div className="space-y-5">
        <Panel title="1. 命名与接入凭证" description="为节点命名并生成一次性短期令牌；安装时使用同一名称。">
          <div className="space-y-4 px-5 py-4">
            <Field label="节点名称" hint={token ? "当前令牌的名称已固定；移除令牌后可修改名称。" : "必填，最多 128 字符。自定义名称会在重新生成令牌时保留。"}>
              {ids => (
                <TextInput
                  id={ids.inputId}
                  required
                  maxLength={128}
                  value={nodeName}
                  readOnly={token !== null}
                  onChange={event => { setNodeName(event.target.value); setCustomName(true); }}
                  disabled={locked || createToken.pending}
                />
              )}
            </Field>
            {!validName ? <p role="alert" className="text-xs text-destructive">请填写有效节点名称，不能包含控制字符。</p> : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="有效期（分钟）" hint="有效期为 1–10 分钟。">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    type="number"
                    min={1}
                    max={10}
                    value={expiryMinutes}
                    onChange={event => setExpiryMinutes(event.target.value)}
                    disabled={locked || createToken.pending}
                  />
                )}
              </Field>
              <Field label="最大使用次数" hint="一次性令牌固定仅使用 1 次。">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    type="number"
                    min={1}
                    max={1}
                    value={maxUses}
                    readOnly
                    disabled={locked || createToken.pending}
                  />
                )}
              </Field>
            </div>
            <Field label="恢复已有节点 ID（可选）" hint="仅用于已核验的身份恢复；批准后旧身份会被吊销。">{ids => <TextInput id={ids.inputId} value={replaceNodeId} onChange={event => setReplaceNodeId(event.target.value)} disabled={locked || createToken.pending} />}</Field>
            {createToken.error ? (
              <Notice tone="error" title="凭证生成失败">{describeApiError(createToken.error)}</Notice>
            ) : null}
            <Button type="button" variant="primary" onClick={generate} loading={createToken.pending} disabled={locked || !validName || !Number.isInteger(Number(expiryMinutes)) || Number(expiryMinutes) < 1 || Number(expiryMinutes) > 10}>
              生成一次性凭证
            </Button>

            {token ? (
              <div className="relative overflow-hidden rounded-xl border border-amber-500/35 bg-gradient-to-br from-amber-500/[0.08] via-card to-primary/[0.04] px-4 py-4 shadow-[var(--shadow-card)]">
                <p className="text-sm font-semibold text-amber-900">接入令牌（仅显示这一次）</p>
                <code className="mt-2 block break-all rounded-lg border border-amber-500/25 bg-card px-3.5 py-2.5 font-mono text-sm font-medium text-foreground shadow-2xs">
                  {token.token}
                </code>
                <p className="mt-2 text-xs text-amber-900">
                  凭证 ID：{token.id} · 到期：{formatDateTime(token.expiresAt) ?? "无法解析"}
                  {formatDuration(Math.max(0, (Date.parse(token.expiresAt) - Date.now()) / 1000))
                    ? ` · 剩余约 ${formatDuration(Math.max(0, (Date.parse(token.expiresAt) - Date.now()) / 1000))}`
                    : ""}
                </p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button type="button" size="sm" variant="primary" onClick={copyToken}>
                    复制令牌
                  </Button>
                  <Button type="button" size="sm" variant="ghost" onClick={removeToken} disabled={createToken.pending}>
                    从页面移除
                  </Button>
                </div>
                {copyStatus ? <p role="status" className="mt-2 text-xs font-medium text-amber-950">{copyStatus}</p> : null}
              </div>
            ) : null}
          </div>
        </Panel>
      </div>

      <InstallationPanel key={session.viewEpoch} nodeName={token?.name ?? nodeName.trim()} />

      <Panel
        title="3. 接入审批"
        description="核对待审批接入请求的节点名称与安装来源后再批准；批准不代表节点已在线。"
        actions={<Button size="sm" onClick={list.reload} loading={list.state.status === "loading"}>刷新</Button>}
      >
        <AsyncPanel
          state={list.state}
          onRetry={list.reload}
          empty={<p className="px-6 py-8 text-sm text-muted-foreground">当前没有接入请求。</p>}
          isEmpty={data => data.items.length === 0}
        >
          {data => (
            <DataTable
              caption="节点接入请求"
              rows={data.items}
              rowKey={row => row.id}
              columns={[
                { key: "id", header: "请求", render: row => <span className="font-mono text-xs">{row.id}</span> },
                { key: "name", header: "节点", render: row => row.name },
                { key: "region", header: "地区", render: row => row.region ?? "—" },
                {
                  key: "status",
                  header: "状态",
                  render: row =>
                    row.status === "pending" ? "待审批" : row.status === "approved" ? "已批准" : "已拒绝",
                },
                {
                  key: "actions",
                  header: "操作",
                  render: row =>
                    row.status === "pending" ? (
                      <div className="flex flex-wrap gap-2">
                        <Button
                          size="sm"
                          disabled={locked || decide.pending}
                          onClick={() => {
                            setDecision({ enrollment: row, decision: "approve" });
                            setReason("");
                          }}
                        >
                          批准
                        </Button>
                        <Button
                          size="sm"
                          variant="danger"
                          disabled={locked || decide.pending}
                          onClick={() => {
                            setDecision({ enrollment: row, decision: "reject" });
                            setReason("");
                          }}
                        >
                          拒绝
                        </Button>
                      </div>
                    ) : (
                      <span className="text-xs text-muted-foreground">无待处理操作</span>
                    ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Modal
        open={decision !== null}
        title={decision?.decision === "approve" ? "批准节点接入" : "拒绝节点接入"}
        description={
          decision
            ? `请求 ${decision.enrollment.id} · ${decision.enrollment.name}。决定会提交到服务端并记录审计；批准不代表节点已在线。`
            : undefined
        }
        onClose={() => {
          if (decide.pending) return;
          setDecision(null);
        }}
        footer={
          <>
            <Button type="button" disabled={decide.pending} onClick={() => setDecision(null)}>
              取消
            </Button>
            <Button
              type="button"
              variant={decision?.decision === "approve" ? "primary" : "danger"}
              onClick={submitDecision}
              loading={decide.pending}
            >
              {decision?.decision === "approve" ? "确认批准" : "确认拒绝"}
            </Button>
          </>
        }
      >
        {decide.error ? <Notice tone="error" title="审批未完成">{describeApiError(decide.error)}</Notice> : null}
        {decision?.decision === "approve" ? (
          <label className="mb-4 flex items-center gap-2 text-sm">
            <Checkbox checked={grantRead} onChange={event => setGrantRead(event.target.checked)} disabled={decide.pending} />
            授予当前账户该节点的读取权限（不授予执行权限）
          </label>
        ) : null}
        <Field label="原因（可选）" hint="请勿包含秘密或凭据。">
          {ids => (
            <TextArea
              id={ids.inputId}
              value={reason}
              onChange={event => setReason(event.target.value)}
              maxLength={500}
              disabled={decide.pending}
            />
          )}
        </Field>
      </Modal>
    </div>
  );
}
