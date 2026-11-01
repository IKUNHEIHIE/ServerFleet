import { useEffect, useRef, useState, type FormEvent } from "react";
import { navigate } from "@/app/router";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable, TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { credentialsApi, nodesApi, serviceGroupsApi } from "@/lib/api/endpoints";
import { aiApi, modelSettingsApi } from "@/lib/api/endpoints-ai";
import type {
  AIApprovalEvent,
  AIApprovalMode,
  AIApprovalPolicy,
  AIApprovalRequest,
  AIExecuteResult,
  AIMessage,
  AIModelSettings,
  AIResourceRecommendation,
  AISession,
  CredentialMetadata,
  NodeSummary,
  ServiceGroup,
} from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { useEventStream } from "@/lib/sse/use-event-stream";
import type { SSEEvent } from "@/lib/sse/event-stream";

const MODE_LABEL: Record<AIApprovalMode, string> = {
  per_action: "每次审批",
  automatic: "自动审批",
  full_access: "完全访问（限时）",
};

const MODE_DESCRIPTION: Record<AIApprovalMode, string> = {
  per_action: "每次工具调用都需要人工批准；纯语言聊天不需要。",
  automatic: "服务端白名单内的只读/低风险操作自动执行，其余仍需人工审批。",
  full_access: "需风险确认并限时；仍不扩大资源范围，AI 任意 root Shell 仍需真人管理员确认。",
};

interface AIStreamEvent {
  readonly id: string | null;
  readonly event: string;
  readonly toolName: string | null;
  readonly approvalId: string | null;
  readonly state: string | null;
  readonly message: string | null;
  readonly receivedAt: number;
}

function parseAIEvent(event: SSEEvent): AIStreamEvent {
  let data: Record<string, unknown> | null = null;
  try {
    const parsed: unknown = JSON.parse(event.data);
    if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) data = parsed as Record<string, unknown>;
  } catch {
    data = null;
  }
  const bounded = (value: unknown) => (typeof value === "string" && value.length > 0 && value.length <= 300 ? value : null);
  return {
    id: event.id,
    event: event.event,
    toolName: bounded(data?.toolName),
    approvalId: bounded(data?.approvalId),
    state: bounded(data?.state),
    message: bounded(data?.message),
    receivedAt: Date.now(),
  };
}

export function AIPage() {
  const session = useSession();
  const policy = useResource(`ai:security:${session.viewEpoch}`, signal => aiApi.securityPolicy(signal).then(result => result.data));
  const create = useAction(() => aiApi.createSession());
  const canUse = session.can("ai.use") || session.can("ai.manage");

  return (
    <div className="space-y-5">
      <PageHeader
        title="AI 运维"
        description="每个会话默认零资源授权、每次审批。审批模式只改变人工批准策略，绝不扩大节点/RBAC 权限。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">默认拒绝</span>}
        actions={
          <Button
            type="button"
            variant="primary"
            disabled={!canUse || session.writesLocked}
            loading={create.pending}
            onClick={async () => {
              const result = await create.run();
              if (result.ok && result.data) navigate(`/ai/${encodeURIComponent(result.data.data.id)}`);
            }}
          >
            新建 AI 会话
          </Button>
        }
      />
      {create.error ? <Notice tone="error" title="创建会话失败">{describeApiError(create.error)}</Notice> : null}
      <Notice tone="info" title="会话列表契约缺口">
        当前 OpenAPI 只有创建会话与按 ID 读取，没有“列出我的 AI 会话”端点。本页不伪造历史列表；请使用创建后返回的会话或已知会话链接。
      </Notice>
      <Panel title="服务端安全策略声明" description="GET /api/v1/ai/security-policy；声明不等于实际校验，实际仍由后端逐目标执行。">
        <AsyncPanel state={policy.state} onRetry={policy.reload} errorTitle="安全策略加载失败">
          {data => (
            <div className="grid gap-3 px-5 py-4 text-sm sm:grid-cols-2 lg:grid-cols-3">
              <p>默认拒绝：{data.defaultDeny ? "是" : "否"}</p>
              <p>服务端强制：{data.serverSideEnforced ? "是" : "否"}</p>
              <p>未授权发现：{data.allowUnscopedDiscovery ? "允许" : "禁止"}</p>
              <p>scopeRevision 必需：{data.scopeRevisionRequired ? "是" : "未声明"}</p>
              <p>横向移动限制：{data.lateralMovementRestricted ? "是" : "未声明"}</p>
              <p>授权与会话交集：{data.groupGrantMemberPolicy ?? "未声明"}</p>
            </div>
          )}
        </AsyncPanel>
      </Panel>
      <ModelSettingsPanel key={session.viewEpoch} />
    </div>
  );
}

/**
 * 所有者作用域的模型配置（GET/PUT /api/v1/ai/model-settings）。GET 返回已保存配置，
 * 不代表供应商健康；PUT 是当前真人管理员的 owner CAS（expectedRevision）。
 * 页面只显示凭据引用与修订号，绝不显示/保存 BYOK 明文；秘密值只在凭据页签提交一次。
 * 表单状态以服务端快照为初始值（key=revision）：revision 变化即以服务端为准重建表单，
 * 不用同步 effect 回写，避免迟到 effect 覆盖用户输入。
 */
function ModelSettingsPanel() {
  const session = useSession();
  const [conflict, setConflict] = useState(false);
  const settings = useResource(`ai:model-settings:${session.viewEpoch}`, signal =>
    modelSettingsApi.get(signal).then(result => result.data),
  );
  const credentials = useResource(`ai:model-credentials:${session.viewEpoch}`, signal =>
    credentialsApi.list(signal).then(result => result.data),
  );
  const isAdmin = session.identity?.roles.includes("administrator") ?? false;
  const credentialsLoaded = credentials.state.status === "ready" ? credentials.state.data : [];
  const aiKeys = credentialsLoaded.filter(credential => credential.kind === "ai_api_key" && credential.usageScope.includes("ai"));

  return (
    <Panel
      title="AI 模型设置（当前用户）"
      description="GET/PUT /api/v1/ai/model-settings；PUT 是 owner CAS，仅当前真人管理员可写。保存的配置不代表供应商健康。"
    >
      {conflict ? (
        <Notice tone="error" title="配置已被其他操作修改（409 MODEL_REVISION_CONFLICT）">
          已重新读取服务端最新修订；请核对后再次提交，不要覆盖他人变更。
        </Notice>
      ) : null}
      <AsyncPanel state={settings.state} onRetry={settings.reload} errorTitle="模型设置加载失败">
        {data => (
          <ModelSettingsForm
            key={data.revision}
            data={data}
            aiKeys={aiKeys}
            isAdmin={isAdmin}
            writesLocked={session.writesLocked}
            onSaved={() => { setConflict(false); settings.reload(); }}
            onConflict={() => { setConflict(true); settings.refreshSilent(); }}
          />
        )}
      </AsyncPanel>
    </Panel>
  );
}

function ModelSettingsForm({
  data,
  aiKeys,
  isAdmin,
  writesLocked,
  onSaved,
  onConflict,
}: {
  readonly data: AIModelSettings;
  readonly aiKeys: readonly CredentialMetadata[];
  readonly isAdmin: boolean;
  readonly writesLocked: boolean;
  readonly onSaved: () => void;
  readonly onConflict: () => void;
}) {
  const [enabled, setEnabled] = useState(data.enabled);
  const [model, setModel] = useState(data.model);
  const [credentialRef, setCredentialRef] = useState(data.credentialRef);
  const selected = aiKeys.find(credential => credential.id === credentialRef) ?? null;

  const save = useAction((body: {
    expectedRevision: number;
    enabled: boolean;
    provider: string;
    model: string;
    credentialRef: string;
    credentialRevision: number;
  }) => modelSettingsApi.set(body));

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enabled && (!model.trim() || !credentialRef)) return;
    const result = await save.run({
      expectedRevision: data.revision,
      enabled,
      provider: "openai-responses",
      model: model.trim(),
      credentialRef,
      credentialRevision: selected?.revision ?? 0,
    });
    // 成功后的新 revision 由 key 重建表单；409 用静默刷新保住提示并取回服务端最新
    // 修订（旧 revision 已被服务端拒绝，本地绝不覆盖他人变更）。
    if (result.ok) {
      onSaved();
    } else if (result.error?.status === 409 && result.error.code === "MODEL_REVISION_CONFLICT") {
      onConflict();
    }
  }

  const saveError = save.error;
  const saveErrorCode = (saveError as { code?: string } | null)?.code ?? "";

  return (
    <form onSubmit={submit} className="space-y-4 px-5 py-4">
      <div className="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
        <p>配置状态：{data.enabled ? "已启用" : "未启用"}</p>
        <p>配置修订：{data.revision}</p>
        <p>供应商：{data.provider || "未设置"}</p>
        <p>密钥引用：{data.credentialRef ? <span className="font-mono text-xs">{data.credentialRef}（r{data.credentialRevision}）</span> : "未绑定"}</p>
      </div>
      {data.updatedAt ? <p className="text-xs text-muted-foreground">更新时间：{formatDateTime(data.updatedAt) ?? "—"}</p> : null}
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="模型标识" hint="仅 openai-responses 供应商；不带 sk- 前缀。">
          {ids => <TextInput id={ids.inputId} value={model} onChange={event => setModel(event.target.value)} maxLength={128} disabled={!isAdmin || writesLocked} />}
        </Field>
        <Field label="AI 凭据（ai_api_key）" hint="只显示引用与修订号；秘密值在「设置 → 凭据」页签提交一次，绝不回显。">
          {ids => (
            <Select id={ids.inputId} value={credentialRef} onChange={event => setCredentialRef(event.target.value)} disabled={!isAdmin || writesLocked}>
              <option value="">未绑定</option>
              {aiKeys.map(credential => (
                <option key={credential.id} value={credential.id}>
                  {credential.name}（{credential.id.slice(0, 12)}… · {credential.status} · r{credential.revision}）
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          checked={enabled}
          onChange={event => setEnabled(event.target.checked)}
          disabled={!isAdmin || writesLocked}
        />
        启用本用户的模型生成（显式触发；不产生自动调用）
      </label>
      {saveError && !saveErrorCode.includes("MODEL_REVISION_CONFLICT") ? (
        <Notice tone="error" title="保存失败">{describeApiError(saveError)}</Notice>
      ) : null}
      {saveErrorCode.includes("AI_SOURCE_UNAVAILABLE") ? (
        <p className="text-xs text-muted-foreground">服务端无法验证该凭据引用（类型/范围/状态/有效期），已保持原配置。</p>
      ) : null}
      {!isAdmin ? (
        <p className="text-xs text-muted-foreground">当前身份不是管理员：只能查看本页配置，不能修改（服务端强制）。</p>
      ) : null}
      <Button
        type="submit"
        variant="primary"
        loading={save.pending}
        disabled={!isAdmin || writesLocked || (enabled && (!model.trim() || !credentialRef))}
      >
        保存模型设置（CAS）
      </Button>
    </form>
  );
}

function ModeCards({
  policy,
  canManage,
  writesLocked,
  onChange,
  error,
}: {
  readonly policy: AIApprovalPolicy | null;
  readonly canManage: boolean;
  readonly writesLocked: boolean;
  readonly onChange: (mode: AIApprovalMode, duration?: 900 | 1800 | 3600) => void;
  readonly error: unknown;
}) {
  const [fullOpen, setFullOpen] = useState(false);
  const [duration, setDuration] = useState<900 | 1800 | 3600>(900);
  const [confirmed, setConfirmed] = useState(false);
  const modes: AIApprovalMode[] = ["per_action", "automatic", "full_access"];
  return (
    <div className="space-y-3 px-5 py-4">
      {error ? <Notice tone="error" title="切换失败">{describeApiError(error)}</Notice> : null}
      <div className="grid gap-3 md:grid-cols-3">
        {modes.map(mode => {
          const active = policy?.mode === mode;
          return (
            <button
              key={mode}
              type="button"
              aria-pressed={active}
              disabled={!canManage || writesLocked}
              onClick={() => (mode === "full_access" ? setFullOpen(true) : onChange(mode))}
              className={`rounded-lg border px-4 py-3 text-left text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "border-primary bg-primary/10" : "bg-card hover:bg-secondary"}`}
            >
              <span className="font-medium">{MODE_LABEL[mode]}</span>
              <span className="mt-1 block text-xs text-muted-foreground">{MODE_DESCRIPTION[mode]}</span>
              {active ? <span className="mt-2 block text-xs text-primary">当前模式</span> : null}
            </button>
          );
        })}
      </div>
      {policy ? (
        <p className="text-xs text-muted-foreground">
          生效模式：{MODE_LABEL[policy.effectiveMode]} · scopeRevision {policy.scopeRevision} · 到期 {formatDateTime(policy.expiresAt) ?? "未设置"}
          {policy.enforcedRestrictions?.length ? ` · 强制限制：${policy.enforcedRestrictions.join("；")}` : ""}
        </p>
      ) : null}
      <Modal
        open={fullOpen}
        title="启用完全访问（限时）"
        description="完全访问不等于全部服务器权限，也不免除 AI 任意 root Shell 的真人管理员批准。"
        onClose={() => setFullOpen(false)}
        footer={
          <>
            <Button type="button" onClick={() => setFullOpen(false)}>取消</Button>
            <Button
              type="button"
              variant="danger"
              disabled={!confirmed}
              onClick={() => {
                setFullOpen(false);
                onChange("full_access", duration);
              }}
            >
              确认启用
            </Button>
          </>
        }
      >
        <div className="space-y-3">
          <Field label="有效期">
            {ids => (
              <Select id={ids.inputId} value={duration} onChange={event => setDuration(Number(event.target.value) as 900 | 1800 | 3600)}>
                <option value={900}>15 分钟</option>
                <option value={1800}>30 分钟</option>
                <option value={3600}>60 分钟</option>
              </Select>
            )}
          </Field>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1 size-4 accent-[var(--destructive)]" checked={confirmed} onChange={event => setConfirmed(event.target.checked)} />
            我理解：完全访问只在当前资源授权和 RBAC 范围内生效，到期恢复每次审批；root Shell 仍需真人确认。
          </label>
        </div>
      </Modal>
    </div>
  );
}

function ApprovalsPanel({
  sessionId,
  session,
  canApprove,
  writesLocked,
}: {
  readonly sessionId: string;
  readonly session: AISession;
  readonly canApprove: boolean;
  readonly writesLocked: boolean;
}) {
  const [toolName, setToolName] = useState("");
  const [targets, setTargets] = useState("");
  const [args, setArgs] = useState("{}");
  const [approval, setApproval] = useState<AIApprovalRequest | null>(null);
  const [executeResult, setExecuteResult] = useState<AIExecuteResult | null>(null);
  const [approvalId, setApprovalId] = useState("");
  const [note, setNote] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const createApproval = useAction((body: { toolName: string; arguments: Record<string, unknown>; targetNodeIds: readonly string[]; scopeRevision: number }) =>
    aiApi.createApproval(sessionId, body),
  );
  const decide = useAction((body: { decision: "approve" | "deny"; note?: string }) =>
    aiApi.decideApproval(sessionId, approval?.id ?? "", body),
  );
  const execute = useAction((body: { scopeRevision: number; toolName: string; targetNodeIds: readonly string[]; arguments: Record<string, unknown>; approvalId?: string }) =>
    aiApi.execute(sessionId, body),
  );

  function parseArgs(): Record<string, unknown> | null {
    try {
      const parsed: unknown = JSON.parse(args);
      if (typeof parsed === "object" && parsed !== null && !Array.isArray(parsed)) return parsed as Record<string, unknown>;
      setLocalError("工具参数必须是 JSON 对象。");
      return null;
    } catch {
      setLocalError("工具参数不是合法 JSON。");
      return null;
    }
  }

  function targetList(): readonly string[] {
    const list = targets.split(",").map(value => value.trim()).filter(Boolean);
    if (list.length === 0) {
      setLocalError("至少填写一个目标节点 ID。");
    }
    return list;
  }

  async function requestApproval(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parsed = parseArgs();
    const targetNodeIds = targetList();
    if (!parsed || targetNodeIds.length === 0 || !toolName.trim()) return;
    setLocalError(null);
    const result = await createApproval.run({
      toolName: toolName.trim(),
      arguments: parsed,
      targetNodeIds,
      scopeRevision: session.scopeRevision,
    });
    if (result.ok && result.data) {
      setApproval(result.data.data);
      setApprovalId(result.data.data.id);
    }
  }

  async function decideApproval(decision: "approve" | "deny") {
    if (!approval) return;
    const result = await decide.run({ decision, ...(note.trim() ? { note: note.trim() } : {}) });
    if (result.ok && result.data) setApproval(result.data.data);
  }

  async function executeTool(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const parsed = parseArgs();
    const targetNodeIds = targetList();
    if (!parsed || targetNodeIds.length === 0 || !toolName.trim()) return;
    setLocalError(null);
    const result = await execute.run({
      scopeRevision: session.scopeRevision,
      toolName: toolName.trim(),
      targetNodeIds,
      arguments: parsed,
      ...(approvalId.trim() ? { approvalId: approvalId.trim() } : {}),
    });
    if (result.ok && result.data) setExecuteResult(result.data.data);
  }

  return (
    <div className="grid gap-5 xl:grid-cols-2">
      <Panel title="创建一次性工具审批" description="POST /api/v1/ai/sessions/{id}/approvals；绑定工具、参数摘要、目标快照与 scopeRevision。">
        <form onSubmit={requestApproval} className="space-y-3 px-5 py-4">
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          {createApproval.error ? <Notice tone="error" title="创建审批失败">{describeApiError(createApproval.error)}</Notice> : null}
          <Field label="工具名称">{ids => <TextInput id={ids.inputId} value={toolName} onChange={event => setToolName(event.target.value)} placeholder="例如 system.metrics.read" required />}</Field>
          <Field label="目标节点 ID（逗号分隔）" hint="只能使用当前会话 allowedNodeIds 内的目标；客户端选择不构成授权。">
            {ids => <TextInput id={ids.inputId} value={targets} onChange={event => setTargets(event.target.value)} required />}
          </Field>
          <Field label="工具参数（JSON 对象）" hint="服务端会规范化并摘要；不要粘贴秘密。">
            {ids => <TextArea id={ids.inputId} value={args} onChange={event => setArgs(event.target.value)} className="font-mono" />}
          </Field>
          <p className="text-xs text-muted-foreground">可用于授权的节点：{session.allowedNodeIds?.join("、") || "无（先添加资源授权）"}</p>
          <Button type="submit" variant="primary" loading={createApproval.pending} disabled={writesLocked || !canApprove}>请求审批</Button>
        </form>
        {approval ? (
          <div className="border-t px-5 py-4 text-sm">
            <p>审批 {approval.id} · {approval.state} · 风险 {approval.riskLevel ?? "未返回"}</p>
            <p className="mt-1 break-all font-mono text-xs">参数摘要 {approval.canonicalArgumentsHash}</p>
            <p className="mt-1">到期：{formatDateTime(approval.expiresAt) ?? "—"}</p>
            {approval.state === "pending" ? (
              <div className="mt-3 space-y-2">
                <Field label="备注（可选）">{ids => <TextInput id={ids.inputId} value={note} onChange={event => setNote(event.target.value)} />}</Field>
                {decide.error ? <Notice tone="error" title="决策失败">{describeApiError(decide.error)}</Notice> : null}
                <div className="flex gap-2">
                  <Button type="button" onClick={() => void decideApproval("approve")} loading={decide.pending} disabled={!canApprove || writesLocked}>批准一次</Button>
                  <Button type="button" variant="danger" onClick={() => void decideApproval("deny")} loading={decide.pending} disabled={!canApprove || writesLocked}>拒绝</Button>
                </div>
              </div>
            ) : <p className="mt-2 text-xs text-muted-foreground">审批已不处于待批准状态；批准凭证不能重放。</p>}
          </div>
        ) : null}
      </Panel>

      <Panel title="受控工具执行" description="POST /api/v1/ai/tools/execute；服务端逐目标校验资源、scopeRevision、审批与风险。">
        <form onSubmit={executeTool} className="space-y-3 px-5 py-4">
          {execute.error ? <Notice tone="error" title="执行未接受">{describeApiError(execute.error)}</Notice> : null}
          <Field label="审批 ID（可选）" hint="仅服务端策略要求人工批准时填写；不能由客户端切换模式绕过。">
            {ids => <TextInput id={ids.inputId} value={approvalId} onChange={event => setApprovalId(event.target.value)} />}
          </Field>
          <Button type="submit" variant="primary" loading={execute.pending} disabled={writesLocked}>提交受控工具调用</Button>
        </form>
        {executeResult ? (
          <div className="border-t px-5 py-4 text-sm">
            <p>已接受：{executeResult.accepted ? "是" : "否"} · 状态 {executeResult.state}</p>
            <p>验证节点：{executeResult.validatedNodeIds.join("、") || "无"}</p>
            <p>scopeRevision：{executeResult.scopeRevision}</p>
            {executeResult.deniedReason ? <p className="text-destructive">拒绝原因：{executeResult.deniedReason}</p> : null}
            {executeResult.taskId ? <p className="mt-1"><a className="text-primary underline" href={`#/tasks/${encodeURIComponent(executeResult.taskId)}`}>查看任务 {executeResult.taskId}</a></p> : null}
            <p className="mt-1 text-xs text-muted-foreground">accepted/queued 不等于实际操作成功；逐节点结果以任务详情为准。</p>
          </div>
        ) : null}
      </Panel>
    </div>
  );
}

export function AISessionPage({ sessionId }: { readonly sessionId: string }) {
  const appSession = useSession();
  const [tab, setTab] = useState<"grants" | "policy" | "approval" | "chat">("grants");
  const [events, setEvents] = useState<readonly AIStreamEvent[]>([]);
  const [streamError, setStreamError] = useState<unknown>(null);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const session = useResource(`ai:session:${appSession.viewEpoch}:${sessionId}`, signal => aiApi.getSession(sessionId, signal).then(result => result.data));
  const policy = useResource(`ai:policy:${appSession.viewEpoch}:${sessionId}`, signal => aiApi.approvalPolicy(sessionId, signal).then(result => result.data));
  const messages = useResource(`ai:messages:${appSession.viewEpoch}:${sessionId}`, signal =>
    aiApi.messages(sessionId, { limit: 100 }, signal).then(result => result.data),
  );
  const approvalEvents = useResource(`ai:approval-events:${appSession.viewEpoch}:${sessionId}`, signal =>
    aiApi.approvalEvents(sessionId, { limit: 100 }, signal).then(result => result.data),
  );
  const nodes = useResource(`ai:nodes:${appSession.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const serviceGroups = useResource(`ai:servicegroups:${appSession.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data));

  const current = session.state.status === "ready" ? session.state.data : null;
  const nodeItems: readonly NodeSummary[] = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const groupItems: readonly ServiceGroup[] = serviceGroups.state.status === "ready" ? serviceGroups.state.data.items : [];

  const [resourceType, setResourceType] = useState<"node" | "service_group">("node");
  const [resourceId, setResourceId] = useState("");
  const [actions, setActions] = useState("nodes.read");
  const [recommendReason, setRecommendReason] = useState("");
  const [recommendations, setRecommendations] = useState<readonly AIResourceRecommendation[]>([]);
  const [decisionResource, setDecisionResource] = useState<Record<string, string>>({});
  const [messageText, setMessageText] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);

  const grant = useAction((body: { resourceType: "node" | "service_group"; resourceId: string; allowedActions: readonly string[] }) =>
    aiApi.grant(sessionId, body),
  );
  const revokeGrant = useAction((grantId: string) => aiApi.revokeGrant(sessionId, grantId));
  const updatePolicy = useAction((body: { mode: AIApprovalMode; durationSeconds?: 900 | 1800 | 3600; userConfirmedFullAccess?: boolean }) =>
    aiApi.updateApprovalPolicy(sessionId, body),
  );
  const recommend = useAction((body: { reason: string; neededCapabilities: readonly string[] }) => aiApi.recommend(sessionId, body));
  const decideRecommendation = useAction((recommendationId: string, approved: boolean, grantBody: { resourceType: "node" | "service_group"; resourceId: string; allowedActions: readonly string[] } | null) =>
    aiApi.decideRecommendation(sessionId, recommendationId, {
      requestId: recommendationId,
      approved,
      grants: grantBody ? [grantBody] : [],
    }),
  );
  const send = useAction((body: { content: string; clientMessageId: string; scopeRevision: number }) => aiApi.sendMessage(sessionId, body));
  const [generatingMessageId, setGeneratingMessageId] = useState<string | null>(null);
  const [generateError, setGenerateError] = useState<string | null>(null);

  /** 显式生成：成功后服务端返回完整助手消息并持久化，刷新消息列表即可见；
   * 失败时按服务端错误码诚实显示，不在本地编造回复。 */
  async function runGenerate(messageId: string) {
    setGeneratingMessageId(messageId);
    setGenerateError(null);
    try {
      await aiApi.generate(sessionId, messageId);
      messages.refreshSilent();
    } catch (error) {
      const code = (error as { code?: string })?.code ?? "";
      const known: Record<string, string> = {
        AI_SOURCE_UNAVAILABLE: "模型或配额不可用（503 AI_SOURCE_UNAVAILABLE）：未配置模型设置或供应商未接线。",
        NOT_AUTHORIZED: "服务端拒绝本次生成（403 NOT_AUTHORIZED）：当前身份/会话/审批策略不满足。",
        GENERATION_OUTCOME_UNKNOWN: "生成结果未知（503 GENERATION_OUTCOME_UNKNOWN）：服务端不会自动重试，请核对消息状态。",
        MODEL_REVISION_CONFLICT: "模型配置在生成期间被修改（409）：请重新检查模型设置。",
      };
      setGenerateError(known[code] ?? describeApiError(error));
    } finally {
      setGeneratingMessageId(null);
    }
  }

  const stream = useEventStream({
    url: current ? aiApi.eventsUrl(sessionId) : null,
    enabled: Boolean(current) && current?.status === "active",
    onEvent: event => {
      setEvents(list => [parseAIEvent(event), ...list].slice(0, 200));
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
      refreshTimer.current = setTimeout(() => {
        messages.refreshSilent();
        approvalEvents.refreshSilent();
        session.refreshSilent();
      }, 500);
    },
    onTerminalError: error => setStreamError(error),
  });

  useEffect(() => {
    return () => {
      if (refreshTimer.current) clearTimeout(refreshTimer.current);
    };
  }, [sessionId]);

  const canUse = appSession.can("ai.use") || appSession.can("ai.manage");
  const canApprove = appSession.can("ai.approve") || appSession.can("ai.manage");
  const writesLocked = appSession.writesLocked;
  const messageItems: readonly AIMessage[] = messages.state.status === "ready" ? messages.state.data.items : [];
  const approvalEventItems: readonly AIApprovalEvent[] = approvalEvents.state.status === "ready" ? approvalEvents.state.data.items : [];

  async function addGrant(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!resourceId) return setLocalError("必须选择资源。");
    const allowedActions = actions.split(",").map(value => value.trim()).filter(Boolean);
    if (allowedActions.length === 0) return setLocalError("至少填写一个允许动作。");
    setLocalError(null);
    const result = await grant.run({ resourceType, resourceId, allowedActions });
    if (result.ok) {
      setResourceId("");
      session.refreshSilent();
      policy.refreshSilent();
    }
  }

  async function changeMode(mode: AIApprovalMode, duration?: 900 | 1800 | 3600) {
    const result = await updatePolicy.run({
      mode,
      ...(mode === "full_access" ? { durationSeconds: duration ?? 900, userConfirmedFullAccess: true } : {}),
    });
    if (result.ok) {
      policy.refreshSilent();
      session.refreshSilent();
    }
  }

  async function runRecommend() {
    if (!recommendReason.trim()) return setLocalError("请填写建议原因。");
    setLocalError(null);
    const result = await recommend.run({ reason: recommendReason.trim(), neededCapabilities: ["nodes.read"] });
    if (result.ok && result.data) setRecommendations(result.data.data.suggestions);
  }

  async function decideSuggestion(suggestion: AIResourceRecommendation, approved: boolean) {
    const type = suggestion.resourceType === "service_group" ? "service_group" : "node";
    const id = decisionResource[suggestion.id];
    if (approved && !id) {
      setLocalError("批准建议前必须由管理员选择具体资源 ID。");
      return;
    }
    const result = await decideRecommendation.run(suggestion.id, approved, approved && id ? { resourceType: type, resourceId: id, allowedActions: ["nodes.read"] } : null);
    if (result.ok) {
      setRecommendations(list => list.map(item => item.id === suggestion.id ? { ...item, status: approved ? "approved" : "rejected" } : item));
      session.refreshSilent();
    }
  }

  async function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!messageText.trim() || !current) return;
    const content = messageText.trim();
    setMessageText("");
    const clientMessageId = globalThis.crypto?.randomUUID?.() ?? `msg-${Date.now()}`;
    const result = await send.run({ content, clientMessageId, scopeRevision: current.scopeRevision });
    if (result.ok) messages.refreshSilent();
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title={current ? `AI 会话 ${current.id}` : "AI 会话"}
        description="默认零资源授权、每次审批；授权变化会递增 scopeRevision，旧计划与旧审批不能继续执行。"
        actions={
          <>
            <a href="#/ai" className="inline-flex h-9 items-center rounded-md border bg-card px-3 text-sm hover:bg-secondary">返回 AI 入口</a>
            <Button type="button" onClick={() => { session.reload(); policy.reload(); messages.reload(); }}>刷新</Button>
          </>
        }
      />
      {!canUse ? <Notice tone="warning" title="AI 使用入口已锁定">当前身份未返回 AI 使用权限（`ai.use`/`ai.manage`）。</Notice> : null}
      {writesLocked ? <Notice tone="warning" title="写入被 CSRF 契约锁定">重新登录后才能授权/审批/发送消息。</Notice> : null}
      {streamError ? <Notice tone="error" title="AI 事件流终止">{describeApiError(streamError)} 不会自动循环重连。</Notice> : null}

      <AsyncPanel state={session.state} onRetry={session.reload} errorTitle="AI 会话加载失败" empty={<EmptyState title="会话不存在" description="服务端未返回该会话，或当前身份无权查看。" />} isEmpty={() => false}>
        {data => (
          <div className="space-y-5">
            <Panel title="会话快照" description="GET /api/v1/ai/sessions/{sessionId}；allowedNodeIds 由服务端计算。">
              <div className="grid gap-3 px-5 py-4 text-sm sm:grid-cols-2 lg:grid-cols-4">
                <p>状态：{data.status}</p>
                <p>scopeRevision：{data.scopeRevision}</p>
                <p>授权数：{data.resourceGrants.length}</p>
                <p>创建者：{data.createdBy ?? "—"}</p>
                <p className="lg:col-span-4">有效节点：{data.allowedNodeIds?.join("、") || "空（默认无资源）"}</p>
              </div>
              <div className="border-t px-5 py-2 text-xs text-muted-foreground">
                事件流：{stream.state} · 游标 {stream.lastEventId ?? "尚未收到"} · 事件 {events.length} 条（只显示投影字段）
                <Button size="sm" className="ml-2" onClick={stream.reconnect}>重连</Button>
              </div>
              {events.length ? (
                <ul className="max-h-40 divide-y overflow-y-auto border-t">
                  {events.map((event, index) => (
                    <li key={`${event.id ?? "e"}-${event.receivedAt}-${index}`} className="px-5 py-1.5 text-xs">
                      {event.event} {event.toolName ? `· ${event.toolName}` : ""} {event.state ? `· ${event.state}` : ""} {event.message ? `· ${event.message}` : ""}
                    </li>
                  ))}
                </ul>
              ) : null}
            </Panel>

            <TabBar
              label="AI 会话分区"
              active={tab}
              onChange={setTab}
              tabs={[
                { id: "grants", label: "资源授权" },
                { id: "policy", label: "审批策略" },
                { id: "approval", label: "工具审批" },
                { id: "chat", label: "消息与审批审计" },
              ]}
            />

            {tab === "grants" ? (
              <div className="space-y-5">
                <Panel title="当前资源授权" description="服务组授权保存成员快照；移除成员实时失权，新成员不继承旧授权。">
                  {data.resourceGrants.length === 0 ? <EmptyState title="默认零授权" description="AI 不能读取任何节点；请显式添加。" /> : (
                    <ul className="divide-y">
                      {data.resourceGrants.map(grantItem => (
                        <li key={grantItem.grantId} className="flex flex-wrap items-start justify-between gap-3 px-5 py-3 text-sm">
                          <div>
                            <p>{grantItem.resourceType} · {grantItem.resourceId}</p>
                            <p className="text-xs text-muted-foreground">动作：{grantItem.allowedActions.join("、")}</p>
                            {grantItem.memberNodeIdsSnapshot?.length ? <p className="text-xs text-muted-foreground">组快照：{grantItem.memberNodeIdsSnapshot.join("、")}</p> : null}
                          </div>
                          <Button size="sm" variant="danger" disabled={writesLocked || !canUse} onClick={async () => { const result = await revokeGrant.run(grantItem.grantId); if (result.ok) { session.refreshSilent(); policy.refreshSilent(); } }}>
                            撤回
                          </Button>
                        </li>
                      ))}
                    </ul>
                  )}
                </Panel>
                <Panel title="添加资源授权" description="显式由管理员加入；AI 模型不能自行调用此入口扩大授权。">
                  <form onSubmit={addGrant} className="space-y-3 px-5 py-4">
                    {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
                    {grant.error ? <Notice tone="error" title="授权失败">{describeApiError(grant.error)}</Notice> : null}
                    <div className="grid gap-3 sm:grid-cols-2">
                      <Field label="资源类型">
                        {ids => (
                          <Select id={ids.inputId} value={resourceType} onChange={event => { setResourceType(event.target.value as "node" | "service_group"); setResourceId(""); }}>
                            <option value="node">节点</option>
                            <option value="service_group">服务组</option>
                          </Select>
                        )}
                      </Field>
                      <Field label="资源">
                        {ids => (
                          <Select id={ids.inputId} value={resourceId} onChange={event => setResourceId(event.target.value)}>
                            <option value="">请选择</option>
                            {resourceType === "node"
                              ? nodeItems.map(node => <option key={node.id} value={node.id}>{node.name.trim() || node.id}</option>)
                              : groupItems.map(group => <option key={group.id} value={group.id}>{group.name}（{group.memberNodeIds.length} 台）</option>)}
                          </Select>
                        )}
                      </Field>
                    </div>
                    <Field label="允许动作（逗号分隔）" hint="例如 nodes.read,metrics.read,tasks.execute；最终取用户权限交集。">
                      {ids => <TextInput id={ids.inputId} value={actions} onChange={event => setActions(event.target.value)} />}
                    </Field>
                    <Button type="submit" variant="primary" loading={grant.pending} disabled={writesLocked || !canUse}>添加授权</Button>
                  </form>
                </Panel>
                <Panel title="资源建议" description="AI 只能建议资源类型/能力，不能自行读取未授权节点或批准建议。">
                  <div className="space-y-3 px-5 py-4">
                    <Field label="建议请求原因">{ids => <TextInput id={ids.inputId} value={recommendReason} onChange={event => setRecommendReason(event.target.value)} />}</Field>
                    <Button type="button" onClick={runRecommend} loading={recommend.pending} disabled={writesLocked || !canUse}>请求资源建议</Button>
                    {recommend.error ? <Notice tone="error" title="建议请求失败">{describeApiError(recommend.error)}</Notice> : null}
                    {recommendations.length ? (
                      <ul className="divide-y rounded-lg border">
                        {recommendations.map(suggestion => (
                          <li key={suggestion.id} className="px-4 py-3 text-sm">
                            <p>{suggestion.reason}</p>
                            <p className="text-xs text-muted-foreground">{suggestion.resourceType} · {suggestion.recommendedCapability} · {suggestion.status}{suggestion.metadataOnly ? " · 仅元数据" : ""}</p>
                            {suggestion.status === "pending" ? (
                              <div className="mt-2 flex flex-wrap items-end gap-2">
                                {suggestion.resourceType === "node" || suggestion.resourceType === "service_group" ? (
                                  <label className="text-xs">
                                    由管理员选择资源
                                    <Select
                                      className="mt-1"
                                      value={decisionResource[suggestion.id] ?? ""}
                                      onChange={event => setDecisionResource(current => ({ ...current, [suggestion.id]: event.target.value }))}
                                    >
                                      <option value="">请选择</option>
                                      {(suggestion.resourceType === "node" ? nodeItems : groupItems).map(item => <option key={item.id} value={item.id}>{item.name?.trim?.() || item.id}</option>)}
                                    </Select>
                                  </label>
                                ) : <span className="text-xs text-muted-foreground">该类型不能直接授权；只能拒绝。</span>}
                                <Button size="sm" onClick={() => void decideSuggestion(suggestion, true)} disabled={writesLocked || !canUse}>批准并授权</Button>
                                <Button size="sm" variant="danger" onClick={() => void decideSuggestion(suggestion, false)} disabled={writesLocked || !canUse}>拒绝</Button>
                              </div>
                            ) : null}
                          </li>
                        ))}
                      </ul>
                    ) : null}
                  </div>
                </Panel>
              </div>
            ) : null}

            {tab === "policy" ? (
              <Panel title="审批策略" description="GET/PATCH /api/v1/ai/sessions/{id}/approval-policy；服务端可强制更严格策略。">
                <AsyncPanel state={policy.state} onRetry={policy.reload} errorTitle="审批策略加载失败">
                  {policyData => (
                    <ModeCards
                      policy={policyData}
                      canManage={canUse}
                      writesLocked={writesLocked}
                      onChange={(mode, duration) => void changeMode(mode, duration)}
                      error={updatePolicy.error}
                    />
                  )}
                </AsyncPanel>
              </Panel>
            ) : null}

            {tab === "approval" ? (
              <ApprovalsPanel sessionId={sessionId} session={data} canApprove={canApprove} writesLocked={writesLocked} />
            ) : null}

            {tab === "chat" ? (
              <div className="grid gap-5 xl:grid-cols-2">
                <Panel
                  title="消息"
                  description="GET/POST /api/v1/ai/sessions/{id}/messages；内容为服务端脱敏文本。生成必须显式触发，返回缓冲至终态的完整消息。"
                >
                  <AsyncPanel state={messages.state} onRetry={messages.reload} errorTitle="消息加载失败" empty={<EmptyState title="暂无消息" />} isEmpty={page => page.items.length === 0}>
                    {() => (
                      <ul className="max-h-80 divide-y overflow-y-auto">
                        {messageItems.map(message => (
                          <li key={message.messageId} className="px-5 py-3 text-sm">
                            <p className="text-xs text-muted-foreground">{message.role} · {formatDateTime(message.createdAt) ?? "—"}</p>
                            <p className="mt-1 whitespace-pre-wrap">{message.redactedContent ?? "服务端未返回内容"}</p>
                            {message.taskIds?.length ? <p className="mt-1 text-xs">任务：{message.taskIds.join("、")}</p> : null}
                            {message.role === "user" ? (
                              <div className="mt-2">
                                <Button
                                  type="button"
                                  size="sm"
                                  loading={generatingMessageId === message.messageId}
                                  disabled={writesLocked || !canUse || generatingMessageId !== null}
                                  onClick={() => void runGenerate(message.messageId)}
                                >
                                  生成回复
                                </Button>
                              </div>
                            ) : null}
                          </li>
                        ))}
                      </ul>
                    )}
                  </AsyncPanel>
                  {generateError ? <Notice tone="error" title="生成失败（未产生助手回复）">{generateError}</Notice> : null}
                  <p className="px-5 pt-2 text-xs text-muted-foreground">
                    生成说明：点击「生成回复」向 POST …/messages/{"{messageId}"}/generate 显式发起；响应为服务端缓冲上游 SSE 至终态并复核撤权/配额后的完整脱敏消息，不是浏览器逐 token 流。重复对 generating/unknown 消息发起会被服务端拒绝。
                  </p>
                  <form onSubmit={sendMessage} className="space-y-3 border-t px-5 py-4">
                    {send.error ? <Notice tone="error" title="发送失败">{describeApiError(send.error)}</Notice> : null}
                    <Field label="消息内容" hint="每次提交带 clientMessageId 与当前 scopeRevision；过期会被拒绝。">
                      {ids => <TextArea id={ids.inputId} value={messageText} onChange={event => setMessageText(event.target.value)} maxLength={32000} />}
                    </Field>
                    <Button type="submit" variant="primary" loading={send.pending} disabled={writesLocked || !canUse || !messageText.trim()}>发送消息</Button>
                  </form>
                </Panel>
                <Panel title="审批审计" description="GET /api/v1/ai/sessions/{id}/approval-events；只读。">
                  <AsyncPanel state={approvalEvents.state} onRetry={approvalEvents.reload} errorTitle="审批审计加载失败" empty={<EmptyState title="暂无审批事件" />} isEmpty={data2 => data2.items.length === 0}>
                    {() => (
                      <DataTable
                        caption="AI 审批审计"
                        rows={approvalEventItems}
                        rowKey={event => event.id}
                        columns={[
                          { key: "time", header: "时间", render: event => formatDateTime(event.timestamp) ?? "—" },
                          { key: "actor", header: "主体", render: event => event.actor },
                          { key: "action", header: "动作", render: event => event.action },
                          { key: "result", header: "结果", render: event => event.result },
                          { key: "tool", header: "工具", render: event => event.toolName ?? "—" },
                          { key: "targets", header: "目标", render: event => event.targetNodeIds?.join("、") ?? "—" },
                        ]}
                      />
                    )}
                  </AsyncPanel>
                </Panel>
              </div>
            ) : null}
          </div>
        )}
      </AsyncPanel>
    </div>
  );
}
