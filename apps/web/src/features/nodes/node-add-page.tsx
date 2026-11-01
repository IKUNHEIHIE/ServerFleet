import { useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { enrollmentApi } from "@/lib/api/endpoints";
import type { NodeEnrollment } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime, formatDuration } from "@/lib/format";

type Decision = "approve" | "reject";

export function NodeAddPage() {
  const session = useSession();
  const [expiryMinutes, setExpiryMinutes] = useState("10");
  const [maxUses, setMaxUses] = useState("1");
  const [token, setToken] = useState<{ id: string; token: string; expiresAt: string } | null>(null);
  const [copyStatus, setCopyStatus] = useState<string | null>(null);
  const [decision, setDecision] = useState<{ enrollment: NodeEnrollment; decision: Decision } | null>(null);
  const [reason, setReason] = useState("");
  const [outcome, setOutcome] = useState<string | null>(null);

  const canManage = session.can("node.manage") || session.can("node.approve") || session.can("node.enroll");
  const locked = !canManage || session.writesLocked;

  const list = useResource(`enrollments:list:${session.viewEpoch}`, signal =>
    enrollmentApi.list(signal).then(result => result.data),
  );
  const createToken = useAction(() =>
    enrollmentApi.createToken({
      expiryMinutes: Number(expiryMinutes),
      maxUses: Number(maxUses),
    }),
  );
  const decide = useAction((enrollmentId: string, body: { decision: Decision; reason?: string }) =>
    enrollmentApi.decide(enrollmentId, body),
  );

  async function generate() {
    setOutcome(null);
    setCopyStatus(null);
    const result = await createToken.run();
    if (result.ok && result.data) {
      setToken(result.data.data);
    }
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
      ...(reason.trim() ? { reason: reason.trim() } : {}),
    });
    if (result.ok) {
      setOutcome(decision.decision === "approve" ? "已批准该接入请求。" : "已拒绝该接入请求。");
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
        badge={<span className="rounded-md border px-2 py-1 text-xs">接入</span>}
      />

      {!canManage ? (
        <Notice tone="warning" title="操作入口已锁定">
          当前身份没有返回节点接入/审批权限（尝试 `node.manage`、`node.approve`、`node.enroll`）。仍可查看待审批请求，但不会显示可用写操作。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="写入被 CSRF 契约锁定">
          刷新后 Identity 未返回 CSRF 令牌；请先重新登录，否则生成/审批会保持禁用。
        </Notice>
      ) : null}
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="1. 生成短期接入凭证" description="POST /api/v1/node-enrollment-tokens。令牌只在创建响应显示一次，服务端不得支持后续列表读取明文。">
          <div className="space-y-4 px-5 py-4">
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
              <Field label="最大使用次数" hint="服务端允许 1–10；默认 1 次。">
                {ids => (
                  <TextInput
                    id={ids.inputId}
                    type="number"
                    min={1}
                    max={10}
                    value={maxUses}
                    onChange={event => setMaxUses(event.target.value)}
                    disabled={locked || createToken.pending}
                  />
                )}
              </Field>
            </div>
            {createToken.error ? (
              <Notice tone="error" title="凭证生成失败">{describeApiError(createToken.error)}</Notice>
            ) : null}
            <Button type="button" variant="primary" onClick={generate} loading={createToken.pending} disabled={locked}>
              生成一次性凭证
            </Button>

            {token ? (
              <div className="rounded-lg border border-amber-600/30 bg-amber-50 px-4 py-3">
                <p className="text-sm font-medium text-amber-900">接入令牌（仅显示这一次）</p>
                <code className="mt-2 block break-all rounded-md border bg-card px-3 py-2 font-mono text-sm">{token.token}</code>
                <p className="mt-2 text-xs text-amber-900">
                  凭证 ID：{token.id} · 到期：{formatDateTime(token.expiresAt) ?? "无法解析"}
                  {formatDuration(Math.max(0, (Date.parse(token.expiresAt) - Date.now()) / 1000))
                    ? ` · 剩余约 ${formatDuration(Math.max(0, (Date.parse(token.expiresAt) - Date.now()) / 1000))}`
                    : ""}
                </p>
                <div className="mt-3 flex flex-wrap gap-2">
                  <Button type="button" size="sm" onClick={copyToken}>
                    复制令牌
                  </Button>
                  <Button type="button" size="sm" variant="ghost" onClick={() => { setToken(null); setCopyStatus(null); }}>
                    从页面移除
                  </Button>
                </div>
                {copyStatus ? <p role="status" className="mt-2 text-xs">{copyStatus}</p> : null}
              </div>
            ) : null}
          </div>
        </Panel>

        <Panel title="2. 安装与审批" description="安装包与命令由 Agent 正式发行渠道提供；本页面不拼接未登记的下载地址，也不伪造安装成功。">
          <div className="space-y-3 px-5 py-4 text-sm leading-6 text-muted-foreground">
            <ol className="list-decimal space-y-2 pl-5">
              <li>在受控终端以 Agent 官方发行包安装，使用上一步的一次性令牌完成注册。</li>
              <li>若服务端启用人工审批，接入请求会出现在下方“待审批接入请求”。</li>
              <li>批准后节点身份由服务端签发；拒绝或到期令牌不会产生可用节点。</li>
              <li>节点身份被吊销后不可复用；过期任务不会自动恢复。</li>
            </ol>
            <Notice tone="info" title="契约缺口：接入历史与安装命令">
              当前候选契约没有“接入历史/重新安装/按节点吊销历史”查询，也没有安装命令模板返回。页面不显示伪造历史或命令；相关证据保留为 UNTESTED，需求已登记。
            </Notice>
          </div>
        </Panel>
      </div>

      <Panel
        title="待审批接入请求"
        description="GET /api/v1/node-enrollments；批准/拒绝必须由服务端再次校验当前权限。"
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
        <Field label="原因（可选）" hint="服务端会写入审计；不要包含秘密或凭据。">
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
