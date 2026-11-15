import { useState } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextArea } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { enrollmentApi } from "@/lib/api/endpoints";
import type { NodeEnrollment } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";

import { InstallationPanel } from "./installation-panel";

type Decision = "approve" | "reject";

export function NodeAddPage() {
  const session = useSession();
  const [decision, setDecision] = useState<{ enrollment: NodeEnrollment; decision: Decision } | null>(null);
  const [reason, setReason] = useState("");
  const [grantRead, setGrantRead] = useState(true);
  const [outcome, setOutcome] = useState<string | null>(null);

  const canManage = session.can("node.manage") || session.can("node.approve") || session.can("node.enroll");
  const locked = !canManage || session.writesLocked;

  const list = useResource(`enrollments:list:${session.viewEpoch}`, signal =>
    enrollmentApi.list(signal).then(result => result.data),
  );
  const decide = useAction((enrollmentId: string, body: { decision: Decision; reason?: string; grantRead?: boolean }) =>
    enrollmentApi.decide(enrollmentId, body),
  );

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
        description="一键安装预编译 Agent，无需令牌直接进入接入审批；批准后节点自动常驻上线。"
        badge={<Badge tone="primary">接入</Badge>}
      />

      {!canManage ? (
        <Notice tone="warning" title="操作入口已锁定">
          当前账户没有节点接入或审批权限，仍可查看请求，请联系管理员授权。
        </Notice>
      ) : null}
      {session.writesLocked ? (
        <Notice tone="warning" title="会话安全验证未完成">
          请重新登录后再审批接入请求。
        </Notice>
      ) : null}
      {outcome ? <Notice tone="success" title="操作完成">{outcome}</Notice> : null}

      <InstallationPanel key={session.viewEpoch} />

      <Panel
        title="接入审批"
        description="核对待审批接入请求的节点名称后再批准；批准后节点会自动连接上线。"
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
            ? `请求 ${decision.enrollment.id} · ${decision.enrollment.name}。决定会提交到服务端并记录审计；批准后节点会自动连接上线。`
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
