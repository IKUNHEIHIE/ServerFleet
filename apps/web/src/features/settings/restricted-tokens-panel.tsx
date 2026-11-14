import { useState, type FormEvent } from "react";
import { KeyRound, RefreshCw, ShieldAlert } from "lucide-react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { authApi, type IssuedRestrictedToken, type RestrictedTokenMetadata } from "@/lib/api/endpoints";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

function parseNodeIds(raw: string): string[] {
  return [...new Set(raw.split(/[\s,，]+/).map(item => item.trim()).filter(Boolean))];
}

function RestrictedTokensContent() {
  const session = useSession();
  const tokens = useResource(
    `auth:restricted-tokens:${session.viewEpoch}`,
    signal => authApi.tokens(signal).then(result => result.data.items),
  );
  const issueAction = useAction((body: Parameters<typeof authApi.issueToken>[0]) => authApi.issueToken(body));
  const revokeAction = useAction((tokenId: string) => authApi.revokeToken(tokenId));

  const [name, setName] = useState("");
  const [ttlSeconds, setTtlSeconds] = useState("600");
  const [readNodes, setReadNodes] = useState(true);
  const [readTasks, setReadTasks] = useState(true);
  const [nodeIdsText, setNodeIdsText] = useState("");
  const [validation, setValidation] = useState<string | null>(null);
  const [issued, setIssued] = useState<IssuedRestrictedToken | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);

  const items: readonly RestrictedTokenMetadata[] = tokens.state.status === "ready" ? tokens.state.data : [];

  async function handleIssue(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    const parsedTtl = Number(ttlSeconds);
    const actions: ("nodes.read" | "tasks.read")[] = [
      ...(readNodes ? (["nodes.read"] as const) : []),
      ...(readTasks ? (["tasks.read"] as const) : []),
    ];
    const nodeIds = parseNodeIds(nodeIdsText);

    if (!trimmedName || trimmedName.length > 128) {
      setValidation("令牌名称不能为空，且不超过 128 个字符。");
      return;
    }
    if (!Number.isInteger(parsedTtl) || parsedTtl < 60 || parsedTtl > 3600) {
      setValidation("TTL 必须为 60—3600 秒之间的整数。");
      return;
    }
    if (actions.length === 0) {
      setValidation("至少选择一种只读权限（nodes.read 或 tasks.read）。");
      return;
    }
    if (nodeIds.length === 0 || nodeIds.length > 200 || nodeIds.some(id => id.length > 128 || /[\s*?[\]]/.test(id))) {
      setValidation("请提供 1—200 个显式合法节点 ID（不含空白或通配符）。");
      return;
    }

    setValidation(null);
    const result = await issueAction.run({
      name: trimmedName,
      purpose: "script-read",
      actions,
      nodeIds,
      ttlSeconds: parsedTtl,
    });
    if (result.ok && result.data) {
      setIssued(result.data.data);
      setName("");
      tokens.reload();
    }
  }

  async function handleRevoke(tokenId: string) {
    setRevokingId(tokenId);
    const result = await revokeAction.run(tokenId);
    setRevokingId(null);
    if (result.ok) {
      tokens.reload();
    }
  }

  return (
    <div className="space-y-4 px-5 py-4">
      <Notice tone="warning" title="只读受限边界与 Bearer 适配状态">
        令牌仅设计为 <code>script-read</code>、<code>nodes.read</code>/<code>tasks.read</code>、1—200 显式节点、60—3600 秒，不允许写入/AI/插件/注册/执行。目前正式节点/任务 HTTP 仍使用 Cookie 认证，尚无生产 Bearer 读取适配；不能将签发成功等同于脚本 Bearer 可用。
      </Notice>

      {issued ? (
        <div className="neurox-subcard space-y-2 border-amber-300/80 bg-amber-50/70">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-amber-900">
              <KeyRound aria-hidden="true" className="size-3.5" />
              新令牌一次性明文（列表接口不再返回；丢失须吊销并重签）
            </span>
            <Button size="sm" variant="secondary" onClick={() => setIssued(null)}>
              清除显示
            </Button>
          </div>
          <p className="break-all rounded-lg border border-amber-200 bg-white px-3 py-2 font-mono text-xs text-foreground">
            {issued.token}
          </p>
          <p className="text-xs text-muted-foreground">
            令牌 ID：<span className="font-mono">{issued.metadata.tokenId}</span> · 到期：{formatDateTime(issued.metadata.expiresAt) ?? issued.metadata.expiresAt}
          </p>
        </div>
      ) : null}

      <form onSubmit={handleIssue} className="neurox-subcard space-y-3.5">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="text-sm font-semibold text-foreground">签发新受限脚本令牌</p>
          <Badge tone="neutral">purpose: script-read</Badge>
        </div>
        {validation ? <Notice tone="error" title="参数无效">{validation}</Notice> : null}
        {issueAction.error ? <Notice tone="error" title="签发失败">{describeApiError(issueAction.error)}</Notice> : null}
        <div className="grid gap-3.5 sm:grid-cols-2">
          <Field label="令牌名称">
            {ids => (
              <TextInput
                id={ids.inputId}
                value={name}
                onChange={event => setName(event.target.value)}
                maxLength={128}
                placeholder="如 ci-readonly-check"
                disabled={session.writesLocked || issueAction.pending}
                required
              />
            )}
          </Field>
          <Field label="有效期（秒，60—3600）">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="number"
                min={60}
                max={3600}
                value={ttlSeconds}
                onChange={event => setTtlSeconds(event.target.value)}
                disabled={session.writesLocked || issueAction.pending}
                required
              />
            )}
          </Field>
        </div>
        <div className="grid gap-3.5 sm:grid-cols-2">
          <fieldset className="space-y-2 rounded-xl border border-border/70 bg-white px-3.5 py-2.5">
            <legend className="px-1 text-xs font-medium text-muted-foreground">允许只读权限</legend>
            <div className="flex flex-wrap gap-4">
              <label className="flex items-center gap-2 text-xs font-medium text-foreground">
                <input
                  type="checkbox"
                  checked={readNodes}
                  onChange={event => setReadNodes(event.target.checked)}
                  disabled={session.writesLocked || issueAction.pending}
                />
                <span>nodes.read</span>
              </label>
              <label className="flex items-center gap-2 text-xs font-medium text-foreground">
                <input
                  type="checkbox"
                  checked={readTasks}
                  onChange={event => setReadTasks(event.target.checked)}
                  disabled={session.writesLocked || issueAction.pending}
                />
                <span>tasks.read</span>
              </label>
            </div>
          </fieldset>
          <Field label="显式节点 ID（1—200 个，空格或逗号分隔）">
            {ids => (
              <TextArea
                id={ids.inputId}
                value={nodeIdsText}
                onChange={event => setNodeIdsText(event.target.value)}
                rows={2}
                placeholder="例如 node-1, node-2"
                className="font-mono text-xs"
                disabled={session.writesLocked || issueAction.pending}
                required
              />
            )}
          </Field>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" size="sm" variant="primary" loading={issueAction.pending} disabled={session.writesLocked}>
            签发受限令牌
          </Button>
          <Button type="button" size="sm" variant="secondary" onClick={tokens.reload} loading={tokens.state.status === "loading"}>
            <RefreshCw aria-hidden="true" className="size-3.5" />
            刷新列表
          </Button>
        </div>
      </form>

      {revokeAction.error ? <Notice tone="error" title="吊销失败">{describeApiError(revokeAction.error)}</Notice> : null}

      <AsyncPanel
        state={tokens.state}
        onRetry={tokens.reload}
        errorTitle="受限令牌列表加载失败"
        empty={<EmptyState title="暂无受限脚本令牌" description="签发后的受限令牌元数据将在此展示（不返回明文）。" />}
        isEmpty={data => data.length === 0}
      >
        {() => (
          <DataTable
            caption="我的受限脚本令牌"
            rows={items}
            rowKey={item => item.tokenId}
            columns={[
              { key: "name", header: "名称", render: item => <span className="font-medium">{item.name}</span> },
              { key: "id", header: "令牌 ID", render: item => <span className="font-mono text-xs">{item.tokenId}</span> },
              { key: "actionsList", header: "权限", render: item => <span className="font-mono text-xs">{item.actions.join("、")}</span> },
              { key: "nodes", header: "节点范围", render: item => `${item.nodeIds.length} 台 (${item.nodeIds.slice(0, 3).join(", ")}${item.nodeIds.length > 3 ? "…" : ""})` },
              { key: "expires", header: "到期", render: item => formatDateTime(item.expiresAt) ?? item.expiresAt },
              {
                key: "status",
                header: "状态",
                render: item => (
                  <Badge tone={item.revoked ? "danger" : "success"} dot>
                    {item.revoked ? "已吊销" : "有效"}
                  </Badge>
                ),
              },
              {
                key: "ops",
                header: "操作",
                render: item => (
                  <Button
                    size="sm"
                    variant="danger"
                    loading={revokeAction.pending && revokingId === item.tokenId}
                    disabled={item.revoked || session.writesLocked || revokeAction.pending}
                    onClick={() => void handleRevoke(item.tokenId)}
                  >
                    吊销令牌
                  </Button>
                ),
              },
            ]}
          />
        )}
      </AsyncPanel>
    </div>
  );
}

export function RestrictedTokensPanel() {
  const session = useSession();
  const [open, setOpen] = useState(false);

  return (
    <Panel
      title="受限脚本令牌（script-read）"
      description="GET/POST /api/v1/auth/tokens、DELETE /api/v1/auth/tokens/{tokenId}；用于最小权限只读脚本边界治理。"
      actions={
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone="warning" dot>
            <ShieldAlert aria-hidden="true" className="size-3" />
            Bearer Preview
          </Badge>
          <Button
            size="sm"
            variant={open ? "secondary" : "primary"}
            aria-expanded={open}
            onClick={() => setOpen(current => !current)}
          >
            {open ? "收起受限令牌管理" : "管理受限脚本令牌"}
          </Button>
        </div>
      }
    >
      {open ? (
        <RestrictedTokensContent key={session.viewEpoch} />
      ) : (
        <div className="px-5 py-4 text-xs leading-relaxed text-muted-foreground">
          支持创建、查看和吊销限定节点范围与短 TTL（60—3600 秒）的 <code>script-read</code> 只读令牌。当前正式节点/任务 HTTP 仍以浏览器 Cookie 会话为准。点击“管理受限脚本令牌”展开列表与签发表单。
        </div>
      )}
    </Panel>
  );
}
