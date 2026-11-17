import { AIMarkdown } from "./ai-markdown";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { Bot, Brain, Check, CheckCircle2, ChevronDown, Gauge, Layers, Loader2, RefreshCw, Send, Server, ShieldAlert, Sparkles, User, Zap } from "lucide-react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button, LinkButton } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { PageHeader, Panel } from "@/components/ui/panel";
import { describeApiError } from "@/lib/api/errors";
import { nodesApi, serviceGroupsApi } from "@/lib/api/endpoints";
import { aiAgentApi, aiApi, modelSettingsApi, type AgentRunInput } from "@/lib/api/endpoints-ai";
import type {
  AIApprovalMode,
  AIApprovalPolicy,
  AIContextStats,
  AIApprovalRequest,
  AIExecuteResult,
  AIMessage,
  AISession,
  NodeSummary,
  ServiceGroup,
} from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";
import { useEventStream } from "@/lib/sse/use-event-stream";
import type { SSEEvent } from "@/lib/sse/event-stream";
import { AgentRunPanel, InlineAgentRun } from "./agent-run-panel";

const MODE_LABEL: Record<AIApprovalMode, string> = {
  per_action: "每次询问",
  automatic: "自动审批",
  full_access: "完全访问",
};

const EFFORT_LABEL: Record<string, string> = { "": "默认", low: "低", medium: "中", high: "高" };

const MODE_DESCRIPTION: Record<AIApprovalMode, string> = {
  per_action: "执行命令、修改文件或访问网络前，始终询问",
  automatic: "仅在检测到潜在风险时询问",
  full_access: "不再询问，可自由操作已授权节点的文件和网络",
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
  const canUse = session.can("ai.use") || session.can("ai.manage");
  const [activeId, setActiveId] = useState<string | null>(null);
  const [query, setQuery] = useState("");
  const bootRef = useRef(false);
  const sessions = useResource("ai:sessions:" + session.viewEpoch, signal => aiApi.sessions(signal).then(result => result.data));
  const create = useAction(() => aiApi.createSession());
  const items = sessions.state.status === "ready" ? sessions.state.data.items : [];
  const visible = query.trim() ? items.filter(item => item.title.toLowerCase().includes(query.trim().toLowerCase())) : items;

  async function startConversation() {
    const result = await create.run();
    if (result.ok && result.data) {
      setActiveId(result.data.data.id);
      sessions.reload();
    }
  }

  useEffect(() => {
    if (activeId || sessions.state.status !== "ready") return;
    if (items.length > 0) { setActiveId(items[0].id); return; }
    if (bootRef.current || !canUse || session.writesLocked) return;
    bootRef.current = true;
    void startConversation();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeId, items, canUse, session.writesLocked, sessions.state.status]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2.5">
          <h1 className="text-xl font-bold tracking-tight">AI 运维</h1>
          <Badge tone="primary">对话</Badge>
          <span className="text-sm text-muted-foreground">与 AI 对话处理服务器任务</span>
        </div>
        <Button type="button" variant="primary" loading={create.pending} disabled={!canUse || session.writesLocked} onClick={() => void startConversation()}>
          <Sparkles aria-hidden="true" className="size-3.5" />
          新建对话
        </Button>
      </div>
      {create.error ? <Notice tone="error" title="无法开始对话">{describeApiError(create.error)}</Notice> : null}
      {!canUse ? <Notice tone="warning" title="AI 使用入口已锁定">当前身份未返回 AI 使用权限。</Notice> : null}
      <div className="flex h-[calc(100vh-13rem)] min-h-[560px] overflow-hidden rounded-2xl border border-border/90 bg-card shadow-[var(--shadow-card)]">
        <aside className="flex w-60 shrink-0 flex-col border-r border-border/80 bg-[#FAFBFD]">
          <div className="flex h-11 items-center justify-between border-b border-border/80 px-3">
            <span className="text-sm font-semibold">对话列表</span>
            <Button size="sm" variant="ghost" className="size-7 p-0" onClick={() => sessions.reload()} aria-label="刷新对话列表">
              <RefreshCw aria-hidden="true" className="size-3.5" />
            </Button>
          </div>
          <div className="border-b border-border/80 p-2">
            <TextInput value={query} onChange={event => setQuery(event.target.value)} placeholder="搜索对话" aria-label="搜索对话" className="h-8" />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
            {visible.length === 0 ? (
              <p className="px-3 py-8 text-center text-xs text-muted-foreground">{query.trim() ? "没有匹配的对话" : "还没有对话"}</p>
            ) : (
              <ul className="space-y-0.5">
                {visible.map(item => (
                  <li key={item.id}>
                    <button
                      type="button"
                      onClick={() => setActiveId(item.id)}
                      className={"w-full rounded-lg px-3 py-2 text-left transition-colors " + (item.id === activeId ? "bg-primary/10" : "hover:bg-secondary")}
                    >
                      <span className="flex items-center gap-1.5">
                        <span className={"size-1.5 shrink-0 rounded-full " + (item.status === "active" ? "bg-emerald-500" : "bg-slate-300")} />
                        <span className="truncate text-sm">{item.title}</span>
                      </span>
                      <span className="mt-0.5 block pl-3 text-[11px] text-muted-foreground">
                        {item.lastActivityAt ? formatDateTime(item.lastActivityAt) ?? "" : item.id.slice(0, 8)}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="border-t border-border/80 px-3 py-2 text-[11px] text-muted-foreground">AI 仅能看到已授权的节点</div>
        </aside>
        <div className="min-w-0 flex-1">
          {activeId ? (
            <AISessionPage key={activeId} sessionId={activeId} embedded />
          ) : (
            <EmptyState title="正在准备对话" description="服务端创建会话后即可开始；如果长时间没有响应，请检查 AI 使用权限。" />
          )}
        </div>
      </div>
    </div>
  );
}

function AccessModeMenu({
  policy,
  disabled,
  busy,
  onChange,
}: {
  readonly policy: AIApprovalPolicy | null;
  readonly disabled: boolean;
  readonly busy: boolean;
  readonly onChange: (mode: AIApprovalMode) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const current = policy?.effectiveMode ?? policy?.mode ?? "per_action";
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  const modes: AIApprovalMode[] = ["per_action", "automatic", "full_access"];
  const CurrentIcon = current === "per_action" ? ShieldAlert : current === "automatic" ? CheckCircle2 : Zap;
  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="权限模式"
        title="权限模式"
        disabled={disabled}
        onClick={() => setOpen(value => !value)}
        className="inline-flex items-center gap-1 rounded-lg border border-border/80 bg-card px-2 py-1 text-xs font-medium text-foreground hover:bg-secondary disabled:opacity-50"
      >
        <CurrentIcon aria-hidden="true" className="size-3.5 text-primary" />
        {MODE_LABEL[current]}
        <ChevronDown aria-hidden="true" className="size-3 text-muted-foreground" />
      </button>
      {open ? (
        <div
          role="menu"
          aria-label="权限模式"
          className="absolute bottom-full left-0 z-30 mb-1.5 w-72 rounded-xl border border-border bg-card p-1.5 shadow-lg"
        >
          {modes.map(mode => {
            const ModeIcon = mode === "per_action" ? ShieldAlert : mode === "automatic" ? CheckCircle2 : Zap;
            return (
              <button
                key={mode}
                type="button"
                role="menuitemradio"
                aria-checked={current === mode}
                disabled={busy}
                onClick={() => {
                  setOpen(false);
                  if (mode !== current) onChange(mode);
                }}
                className={`flex w-full items-start gap-2 rounded-lg px-2 py-2 text-left text-sm hover:bg-secondary disabled:opacity-50 ${
                  mode === "full_access" ? "text-destructive" : "text-foreground"
                }`}
              >
                <ModeIcon aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
                <span className="min-w-0 flex-1">
                  <span className="block font-medium">{MODE_LABEL[mode]}</span>
                  <span className="mt-0.5 block text-[11px] font-normal leading-4 text-muted-foreground">{MODE_DESCRIPTION[mode]}</span>
                </span>
                {current === mode ? <Check aria-hidden="true" className="mt-1 size-3.5 shrink-0" /> : null}
              </button>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function CompactMenu({
  label,
  icon,
  value,
  options,
  disabled,
  loading,
  onOpen,
  onSelect,
}: {
  readonly label: string;
  readonly icon: ReactNode;
  readonly value: string;
  readonly options: readonly { readonly value: string; readonly label: string }[];
  readonly disabled: boolean;
  readonly loading: boolean;
  readonly onOpen: () => void;
  readonly onSelect: (value: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const current = options.find(option => option.value === value)?.label ?? label;
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => {
      if (!ref.current?.contains(event.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", close);
    return () => document.removeEventListener("mousedown", close);
  }, [open]);
  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={label}
        title={label}
        disabled={disabled}
        onClick={() => {
          if (!open) onOpen();
          setOpen(value => !value);
        }}
        className="inline-flex max-w-56 items-center gap-1 rounded-lg border border-border/80 bg-card px-2 py-1 text-xs font-medium text-foreground hover:bg-secondary disabled:opacity-50"
      >
        {icon}
        <span className="truncate">{current}</span>
        <ChevronDown aria-hidden="true" className="size-3 shrink-0 text-muted-foreground" />
      </button>
      {open ? (
        <div role="menu" aria-label={label} className="absolute bottom-full left-0 z-30 mb-1.5 max-h-64 w-60 overflow-y-auto rounded-xl border border-border bg-card p-1.5 shadow-lg">
          {loading ? <p className="px-2 py-2 text-xs text-muted-foreground">正在读取…</p> : null}
          {options.map(option => (
            <button
              key={option.value}
              type="button"
              role="menuitemradio"
              aria-checked={option.value === value}
              disabled={loading}
              onClick={() => {
                setOpen(false);
                if (option.value !== value) onSelect(option.value);
              }}
              className="flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-xs text-foreground hover:bg-secondary disabled:opacity-50"
            >
              <span className="min-w-0 flex-1 truncate">{option.label}</span>
              {option.value === value ? <Check aria-hidden="true" className="size-3.5 shrink-0 text-primary" /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function formatTokens(value: number): string {
  return value >= 1000 ? `${(value / 1000).toFixed(1)}k` : String(value);
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
      <Panel title="创建一次性工具审批" description="为指定工具与目标创建一次性审批。">
        <form onSubmit={requestApproval} className="space-y-3 px-5 py-4">
          {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
          {createApproval.error ? <Notice tone="error" title="创建审批失败">{describeApiError(createApproval.error)}</Notice> : null}
          <Field label="工具名称">{ids => <TextInput id={ids.inputId} value={toolName} onChange={event => setToolName(event.target.value)} placeholder="例如 system.metrics.read" required />}</Field>
          <Field label="目标节点 ID（逗号分隔）" hint="只能使用当前会话已授权的目标节点。">
            {ids => <TextInput id={ids.inputId} value={targets} onChange={event => setTargets(event.target.value)} required />}
          </Field>
          <Field label="工具参数（JSON 对象）" hint="请勿粘贴秘密或凭据。">
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

      <Panel title="受控工具执行" description="执行受授权与审批约束。">
        <form onSubmit={executeTool} className="space-y-3 px-5 py-4">
          {execute.error ? <Notice tone="error" title="执行未接受">{describeApiError(execute.error)}</Notice> : null}
          <Field label="审批 ID（可选）" hint="仅在需要人工批准时填写。">
            {ids => <TextInput id={ids.inputId} value={approvalId} onChange={event => setApprovalId(event.target.value)} />}
          </Field>
          <Button type="submit" variant="primary" loading={execute.pending} disabled={writesLocked}>提交受控工具调用</Button>
        </form>
        {executeResult ? (
          <div className="border-t px-5 py-4 text-sm">
            <p>已接受：{executeResult.accepted ? "是" : "否"} · 状态 {executeResult.state}</p>
            <p>验证节点：{executeResult.validatedNodeIds.join("、") || "无"}</p>
            <p>范围版本：{executeResult.scopeRevision}</p>
            {executeResult.deniedReason ? <p className="text-destructive">拒绝原因：{executeResult.deniedReason}</p> : null}
            {executeResult.taskId ? <p className="mt-1"><a className="text-primary underline" href={`#/tasks/${encodeURIComponent(executeResult.taskId)}`}>查看任务 {executeResult.taskId}</a></p> : null}
            <p className="mt-1 text-xs text-muted-foreground">accepted/queued 不等于实际操作成功；逐节点结果以任务详情为准。</p>
          </div>
        ) : null}
      </Panel>
    </div>
  );
}

export function AISessionPage({ sessionId, embedded = false }: { readonly sessionId: string; readonly embedded?: boolean }) {
  const appSession = useSession();
  const [tab, setTab] = useState<"chat" | "agent" | "approval">("chat");
  const [events, setEvents] = useState<readonly AIStreamEvent[]>([]);
  const [livePreview, setLivePreview] = useState("");
  const [liveSummary, setLiveSummary] = useState("");
  const [liveReasoning, setLiveReasoning] = useState("");
  const [runSteps, setRunSteps] = useState(16);
  const [runSeconds, setRunSeconds] = useState(1800);
  const [streamError, setStreamError] = useState<unknown>(null);
  const refreshTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const session = useResource(`ai:session:${appSession.viewEpoch}:${sessionId}`, signal => aiApi.getSession(sessionId, signal).then(result => result.data));
  const messages = useResource(`ai:messages:${appSession.viewEpoch}:${sessionId}`, signal =>
    aiApi.messages(sessionId, { limit: 100 }, signal).then(result => result.data),
  );
  const nodes = useResource(`ai:nodes:${appSession.viewEpoch}`, signal => nodesApi.list({ signal }).then(result => result.data));
  const serviceGroups = useResource(`ai:servicegroups:${appSession.viewEpoch}`, signal => serviceGroupsApi.list(signal).then(result => result.data));
  const runs = useResource(`ai:runs:chat:${appSession.viewEpoch}:${sessionId}`, signal => aiAgentApi.list(sessionId, signal).then(result => result.data.items), { refreshIntervalMs: 4000 });
  const context = useResource(`ai:context:${appSession.viewEpoch}:${sessionId}:${session?.state.status === "ready" ? session.state.data.scopeRevision : 0}`, signal => aiApi.context(sessionId, signal).then(result => result.data), { refreshIntervalMs: 5000 });

  const current = session.state.status === "ready" ? session.state.data : null;
  const nodeItems: readonly NodeSummary[] = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const groupItems: readonly ServiceGroup[] = serviceGroups.state.status === "ready" ? serviceGroups.state.data.items : [];

  const [resourceType, setResourceType] = useState<"node" | "service_group">("node");
  const [resourceId, setResourceId] = useState("");
  const [actions, setActions] = useState("nodes.read,tasks.command,tasks.exec.v1,host.root");
  const [messageText, setMessageText] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const [policyOverride, setPolicyOverride] = useState<AIApprovalPolicy | null>(null);
  const [models, setModels] = useState<readonly string[] | null>(null);
  const policyData = policyOverride ?? current?.approvalPolicy ?? null;

  const grant = useAction((body: { resourceType: "node" | "service_group"; resourceId: string; allowedActions: readonly string[] }) =>
    aiApi.grant(sessionId, body),
  );
  const revokeGrant = useAction((grantId: string) => aiApi.revokeGrant(sessionId, grantId));
  const updatePolicy = useAction((body: { mode: AIApprovalMode }) => aiApi.updateApprovalPolicy(sessionId, body));
  const updatePreferences = useAction((body: { model: string; reasoningEffort: "low" | "medium" | "high" | "" }) => aiApi.updatePreferences(sessionId, body));
  const catalog = useAction(() => modelSettingsApi.catalog());
  const probeModel = useAction((model: string) => aiApi.probeModel(model));
  const [capability, setCapability] = useState<{ forModel: string; toolCalls: boolean; reasoning: boolean } | null>(null);
  const compactContext = useAction((body: { keep: number }) => aiApi.compactContext(sessionId, body));
  const send = useAction((body: { content: string; clientMessageId: string; scopeRevision: number }) => aiApi.sendMessage(sessionId, body));
  const runAgent = useAction((body: AgentRunInput) => aiAgentApi.create(sessionId, body));
  const [composerMode, setComposerMode] = useState<"chat" | "agent">("chat");
  const [activeRunId, setActiveRunId] = useState<string | null>(null);
  const [generatingMessageId, setGeneratingMessageId] = useState<string | null>(null);
  const [generateError, setGenerateError] = useState<string | null>(null);
  const listRef = useRef<HTMLDivElement | null>(null);

  /** 显式生成：成功后服务端返回完整助手消息并持久化，刷新消息列表即可见；
   * 失败时按服务端错误码诚实显示，不在本地编造回复。 */
  async function runGenerate(messageId: string) {
    setLivePreview("");
    setLiveSummary("");
    setLiveReasoning("");
    setGeneratingMessageId(messageId);
    setGenerateError(null);
    try {
      const result = await aiApi.generate(sessionId, messageId, (kind, text) => {
        if (kind === "text") setLivePreview(text);
        if (kind === "summary") setLiveSummary(text);
        if (kind === "reasoning") setLiveReasoning(text);
      });
      if (!messageItems.some(message => message.messageId === result.data.messageId)) {
        setLivePreview(result.data.redactedContent ?? "");
      }
      messages.refreshSilent();
    } catch (error) {
      const code = (error as { code?: string })?.code ?? "";
      const known: Record<string, string> = {
        AI_SOURCE_UNAVAILABLE: "模型不可用（503 AI_SOURCE_UNAVAILABLE）：检查模型设置、端点以及工具调用兼容性。",
        NOT_AUTHORIZED: "服务端拒绝本次生成（403 NOT_AUTHORIZED）：当前身份/会话/审批策略不满足。",
        GENERATION_OUTCOME_UNKNOWN: "生成结果未知（503 GENERATION_OUTCOME_UNKNOWN）：服务端不会自动重试，请核对消息状态。",
        MODEL_REVISION_CONFLICT: "模型配置在生成期间被修改（409）：请重新检查模型设置。",
      };
      setGenerateError(known[code] ?? describeApiError(error));
    } finally {
      setGeneratingMessageId(null);
      setLivePreview("");
      setLiveSummary("");
    setLiveReasoning("");
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
  const visibleMessages = messageItems;
  const runItems = runs.state.status === "ready" ? runs.state.data : [];
  const runById = new Map(runItems.map(run => [run.id, run]));
  const renderedRunIds = new Set<string>();
  const contextData: AIContextStats | null = context.state.status === "ready" ? context.state.data : null;
  const currentEffort = current?.reasoningEffort ?? "";
  const currentModel = current?.model ?? "";
  const modelOptions = [
    { value: "", label: "默认模型" },
    ...(models ?? []).map(model => ({ value: model, label: model })),
  ];
  if (currentModel && !modelOptions.some(option => option.value === currentModel)) {
    modelOptions.push({ value: currentModel, label: currentModel });
  }
  const effortOptions = (["", "low", "medium", "high"] as const).map(value => ({ value, label: EFFORT_LABEL[value] }));

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [visibleMessages.length, livePreview, generatingMessageId]);

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
    }
  }

  async function changeMode(mode: AIApprovalMode) {
    const result = await updatePolicy.run({ mode });
    if (result.ok && result.data) {
      setPolicyOverride(result.data.data);
      session.refreshSilent();
    }
  }

  async function loadModels() {
    if (models !== null || catalog.pending) return;
    const result = await catalog.run();
    if (result.ok && result.data) setModels(result.data.data.items);
  }

  async function changePreferences(model: string, effort: "low" | "medium" | "high" | "") {
    const result = await updatePreferences.run({ model, reasoningEffort: effort });
    if (result.ok) {
      setActiveRunId(null);
      session.refreshSilent();
      messages.refreshSilent();
      runs.refreshSilent();
      context.refreshSilent();
    }
  }

  async function compressContext() {
    const result = await compactContext.run({ keep: 6 });
    if (result.ok) {
      context.refreshSilent();
      messages.refreshSilent();
    }
  }

  async function sendMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!messageText.trim() || !current) return;
    const content = messageText.trim();
    if (composerMode === "agent") {
      const goalKey = globalThis.crypto?.randomUUID?.() ?? `run-${Date.now()}`;
      const result = await runAgent.run({ goal: content, clientRunId: goalKey, scopeRevision: current.scopeRevision, maxSteps: runSteps, timeoutSeconds: runSeconds });
      if (result.ok && result.data) {
        setMessageText("");
        setActiveRunId(result.data.data.id);
        messages.refreshSilent();
      }
      return;
    }
    setMessageText("");
    const clientMessageId = globalThis.crypto?.randomUUID?.() ?? `msg-${Date.now()}`;
    const result = await send.run({ content, clientMessageId, scopeRevision: current.scopeRevision });
    if (result.ok && result.data) {
      messages.refreshSilent();
      await runGenerate(result.data.data.messageId);
    }
  }

  function resourceDisplayName(kind: "node" | "service_group", id: string): string {
    if (kind === "node") return nodeItems.find(node => node.id === id)?.name ?? id;
    return groupItems.find(group => group.id === id)?.name ?? id;
  }

  async function attachResource(kind: "node" | "service_group", id: string) {
    if (writesLocked || !canUse) return;
    const result = await grant.run({ resourceType: kind, resourceId: id, allowedActions: ["nodes.read", "tasks.command", "tasks.exec.v1", "host.root"] });
    if (!result.ok) return;
    session.refreshSilent();
    const token = `@${resourceDisplayName(kind, id)}`;
    setMessageText(prev => (prev ? `${prev} ${token} ` : `${token} `));
  }

  return (
    <div className={embedded ? "flex h-full min-h-0 flex-col" : "space-y-5"}>
      {embedded ? null : <PageHeader
        title={current ? `AI 会话 ${current.id}` : "AI 会话"}
        description="默认无资源授权、每次操作需审批；授权变化后，旧计划与旧审批失效。"
        actions={
          <>
            <LinkButton href="#/ai">返回 AI 入口</LinkButton>
            <Button type="button" onClick={() => { session.reload(); messages.reload(); }}>刷新</Button>
          </>
        }
      />}
      {!canUse ? <Notice tone="warning" title="AI 使用入口已锁定">当前身份未返回 AI 使用权限（`ai.use`/`ai.manage`）。</Notice> : null}
      {writesLocked ? <Notice tone="warning" title="写入已锁定">重新登录后才能授权/审批/发送消息。</Notice> : null}
      <div className="flex flex-wrap items-center gap-2 px-4 text-xs text-muted-foreground">
        <Button type="button" size="sm" loading={probeModel.pending} disabled={writesLocked || !appSession.can("ai.manage")} onClick={async () => {
          const forModel = current?.model ?? "";
          const result = await probeModel.run(forModel);
          if (result.ok && result.data) setCapability({ forModel, ...result.data.data });
        }}>检测模型能力</Button>
        <span>发送小型检测请求，不执行节点操作</span>
        {capability && capability.forModel === (current?.model ?? "") ? <span>工具调用：{capability.toolCalls ? "已验证" : "未通过"} · 推理输出：{capability.reasoning ? "已验证" : "未验证"}</span> : null}
        {probeModel.error ? <span role="alert">{describeApiError(probeModel.error)}</span> : null}
      </div>
      {composerMode === "agent" ? (
        <div className="flex flex-wrap items-center gap-3 px-4 text-xs text-muted-foreground">
          <label>每段步数 <select aria-label="每段步数" value={runSteps} onChange={event => setRunSteps(Number(event.target.value))}>{[8, 16, 24].map(n => <option key={n} value={n}>{n} 步</option>)}</select></label>
          <label>运行期限 <select aria-label="运行期限" value={runSeconds} onChange={event => setRunSeconds(Number(event.target.value))}>{[900, 1800, 3600].map(n => <option key={n} value={n}>{n / 60} 分钟</option>)}</select></label>
        </div>
      ) : null}
      {streamError ? <Notice tone="error" title="AI 事件流终止">{describeApiError(streamError)} 不会自动循环重连。</Notice> : null}

      <AsyncPanel state={session.state} onRetry={session.reload} errorTitle="AI 会话加载失败" empty={<EmptyState title="会话不存在" description="服务端未返回该会话，或当前身份无权查看。" />} isEmpty={() => false}>
        {data => (
          <div className={embedded ? "flex min-h-0 flex-1 flex-col gap-3 p-3" : "space-y-5"}>
            <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b border-border/80 px-4 py-2">
              <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm">
                <span className="font-semibold">对话</span>
                <Badge tone={data.status === "active" ? "success" : "neutral"}>{data.status === "active" ? "进行中" : "已结束"}</Badge>
                <span className="text-xs text-muted-foreground">授权 {data.resourceGrants.length} · 范围版本 {data.scopeRevision}</span>
                <span className="truncate text-xs text-muted-foreground">{`有效节点：${data.allowedNodeIds?.join("、") || "空（默认无资源）"}`}</span>
              </div>
              <div className="flex flex-wrap items-center gap-1 text-xs">
                {([
                  ...(tab === "chat" ? [] : [{ id: "chat" as const, label: "返回对话" }]),
                  { id: "agent" as const, label: "自动化执行" },
                  { id: "approval" as const, label: "工具审批" },
                ]).map(item => (
                  <Button key={item.id} size="sm" variant="ghost" className={item.id === tab ? "text-foreground" : "text-muted-foreground"} onClick={() => setTab(item.id)}>{item.label}</Button>
                ))}
                <details className="relative">
                  <summary className="cursor-pointer list-none rounded-md border border-border px-2 py-1 text-muted-foreground hover:bg-secondary">连接事件（{events.length}）</summary>
                  <div className="absolute right-0 z-20 mt-1 w-72 rounded-md border border-border bg-card p-2 shadow-lg">
                    <div className="mb-2 flex items-center justify-between text-[11px] text-muted-foreground"><span>事件流：{stream.state}</span><Button size="sm" onClick={stream.reconnect}>重连</Button></div>
                    {events.length ? <ul className="max-h-40 overflow-y-auto rounded bg-[#141724] text-slate-200">{events.map((event, index) => <li key={`${event.id ?? "e"}-${event.receivedAt}-${index}`} className="px-2 py-1 font-mono text-[11px]"><span className="text-violet-300">{event.event}</span> {event.state ? `· ${event.state}` : ""}</li>)}</ul> : <p className="text-[11px] text-muted-foreground">暂无事件</p>}
                  </div>
                </details>
                <Button size="sm" variant="ghost" onClick={() => { session.reload(); messages.reload(); }}>刷新</Button>
              </div>
            </div>



            {tab !== "chat" ? (
              <div className="min-h-0 flex-1 overflow-y-auto pr-1">
            {tab === "agent" ? <AgentRunPanel key={`${sessionId}:${data.scopeRevision}:${appSession.viewEpoch}`} session={data} focusRunId={activeRunId} /> : null}

            {tab === "approval" ? (
              <ApprovalsPanel sessionId={sessionId} session={data} canApprove={canApprove} writesLocked={writesLocked} />
            ) : null}
              </div>
            ) : null}

            {tab === "chat" ? (
              <div className="grid min-h-0 flex-1 gap-3 md:grid-cols-[minmax(0,1fr)_17rem]">
                <div className="flex min-h-0 flex-col overflow-hidden rounded-2xl border border-border/90 bg-card shadow-[var(--shadow-card)]">
                  <div className="flex min-h-0 flex-1 flex-col">
                  <AsyncPanel state={messages.state} onRetry={messages.reload} errorTitle="消息加载失败" empty={<EmptyState title="开始对话" description="输入消息，或把右侧节点拖进输入框授权 AI 使用。" />} isEmpty={page => page.items.length === 0}>
                    {() => (
                      <div ref={listRef} className="min-h-0 flex-1 space-y-3 overflow-y-auto bg-[#FAFBFD] px-4 py-4">
                        {visibleMessages.map(message => {
                          if (message.agentRunId) {
                            if (renderedRunIds.has(message.agentRunId)) return null;
                            renderedRunIds.add(message.agentRunId);
                            const run = runById.get(message.agentRunId);
                            return (
                              <div key={message.messageId} className="space-y-2">
                                {message.role === "user" ? (
                                  <div className="ml-6 rounded-2xl border border-primary/25 bg-primary-light/75 p-4 text-sm shadow-2xs">
                                    <div className="flex items-center gap-2">
                                      <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-muted-foreground">
                                        <User aria-hidden="true" className="size-3.5 text-primary" />你 · {formatDateTime(message.createdAt) ?? "—"}
                                      </span>
                                    </div>
                                    <p className="mt-2 whitespace-pre-wrap leading-6 text-foreground">{run?.goal ?? "Agent 任务"}</p>
                                  </div>
                                ) : null}
                                <InlineAgentRun
                                  sessionId={sessionId}
                                  runId={message.agentRunId}
                                  canApprove={canApprove}
                                  showGoal={false}
                                  compact
                                  onOpen={() => setTab("agent")}
                                />
                              </div>
                            );
                          }
                          const isUser = message.role === "user";
                          return (
                            <div
                              key={message.messageId}
                              className={`rounded-2xl border p-4 text-sm shadow-2xs ${
                                isUser
                                  ? "ml-6 border-primary/25 bg-primary-light/75"
                                  : "mr-6 border-border/80 bg-card"
                              }`}
                            >
                              <div className="flex items-center justify-between gap-2">
                                <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-muted-foreground">
                                  {isUser ? <User aria-hidden="true" className="size-3.5 text-primary" /> : <Bot aria-hidden="true" className="size-3.5 text-emerald-600" />}
                                  {isUser ? "你" : "AI"} · {formatDateTime(message.createdAt) ?? "—"}
                                </span>
                              </div>
                              {message.reasoningContent ? (
                                <details className="mt-2">
                                  <summary className="cursor-pointer text-sm text-muted-foreground">思考过程 · 模型输出</summary>
                                  <AIMarkdown className="mt-2">{message.reasoningContent}</AIMarkdown>
                                </details>
                              ) : null}
                              {message.reasoningSummary ? (
                                <details className="mt-2">
                                  <summary className="cursor-pointer text-sm text-muted-foreground">思考摘要</summary>
                                  <AIMarkdown className="mt-2">{message.reasoningSummary}</AIMarkdown>
                                </details>
                              ) : null}
 {isUser ? <p className="mt-2 whitespace-pre-wrap leading-6 text-foreground">{message.redactedContent ?? "服务端未返回内容"}</p> : <AIMarkdown className="mt-2">{message.redactedContent ?? "服务端未返回内容"}</AIMarkdown>}
                              {message.taskIds?.length ? <p className="mt-1.5 text-xs text-muted-foreground">任务：{message.taskIds.join("、")}</p> : null}
                              {message.role === "user" && !message.agentRunId && generateError && generatingMessageId === null ? (
                                <div className="mt-3">
                                  <Button type="button" size="sm" variant="secondary" onClick={() => void runGenerate(message.messageId)}>重试回复</Button>
                                </div>
                              ) : null}
                            </div>
                          );
                        })}
                        {generatingMessageId !== null && (livePreview || liveSummary || liveReasoning) ? (
                          <div className="px-4 py-3" aria-live="polite">
                            {liveReasoning ? (
                              <details open>
                                <summary className="cursor-pointer text-sm text-muted-foreground">思考过程 · 模型输出</summary>
                                <AIMarkdown>{liveReasoning}</AIMarkdown>
                              </details>
                            ) : null}
                            {liveSummary ? (
                              <details>
                                <summary className="cursor-pointer text-sm text-muted-foreground">思考摘要</summary>
                                <AIMarkdown>{liveSummary}</AIMarkdown>
                              </details>
                            ) : null}
                            <AIMarkdown>{livePreview}</AIMarkdown>
                            <span className="text-xs text-muted-foreground">正在生成，尚未确认完成</span>
                          </div>
                        ) : null}
                        {generatingMessageId !== null && !livePreview ? (
                          <div className="mr-6 inline-flex items-center gap-2 rounded-2xl border border-border/80 bg-card px-4 py-3 text-sm text-muted-foreground">
                            <Loader2 aria-hidden="true" className="size-4 animate-spin text-primary" />
                            AI 正在思考并生成回复…
                          </div>
                        ) : null}
                        {activeRunId && !renderedRunIds.has(activeRunId) ? (
                          <InlineAgentRun
                            sessionId={sessionId}
                            runId={activeRunId}
                            canApprove={canApprove}
                            showGoal={false}
                            compact
                            onOpen={() => setTab("agent")}
                          />
                        ) : null}
                      </div>
                    )}
                  </AsyncPanel>
                  </div>
                  {generateError ? (
                    <div className="shrink-0 border-t border-border/70 px-3 py-2">
                      <Notice tone="error" title="生成失败（未产生助手回复）">{generateError}</Notice>
                    </div>
                  ) : null}
                  <form
                    onSubmit={sendMessage}
                    onDragOver={event => {
                      event.preventDefault();
                      event.dataTransfer.dropEffect = "copy";
                    }}
                    onDrop={event => {
                      event.preventDefault();
                      const raw = event.dataTransfer.getData("text/plain").trim();
                      const [kind, id] = raw.split(":");
                      if ((kind === "node" || kind === "service_group") && id) void attachResource(kind, id);
                    }}
                    className="shrink-0 border-t border-border/70 bg-card p-3"
                  >
                    {send.error ? <div className="mb-2"><Notice tone="error" title="发送失败">{describeApiError(send.error)}</Notice></div> : null}
                    {runAgent.error ? <div className="mb-2"><Notice tone="error" title="执行请求未确认">{describeApiError(runAgent.error)} 请先在“自动化执行”核对运行记录，不要重复创建。</Notice></div> : null}
                    <div className="mb-2 flex flex-wrap items-center gap-1.5 text-xs">
                      <AccessModeMenu
                        policy={policyData}
                        disabled={writesLocked || !canUse || updatePolicy.pending}
                        busy={updatePolicy.pending}
                        onChange={mode => void changeMode(mode)}
                      />
                      <span aria-hidden="true" className="mx-0.5 h-4 w-px bg-border" />
                      <Button type="button" size="sm" variant={composerMode === "chat" ? "secondary" : "ghost"} aria-pressed={composerMode === "chat"} onClick={() => setComposerMode("chat")}>
                        对话
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant={composerMode === "agent" ? "secondary" : "ghost"}
                        aria-pressed={composerMode === "agent"}
                        disabled={!data.allowedNodeIds?.length}
                        onClick={() => setComposerMode("agent")}
                      >
                        执行任务
                      </Button>
                      <span className="ml-1 text-muted-foreground">
                        {composerMode === "agent"
                          ? "AI 将按左侧权限模式规划并操作已授权节点。"
                          : "只对话，不改动服务器；需要操作时切到“执行任务”。"}
                      </span>
                    </div>
                    {updatePolicy.error ? (
                      <div className="mb-2">
                        <Notice tone="error" title="权限模式切换失败">{describeApiError(updatePolicy.error)}</Notice>
                      </div>
                    ) : null}
                    <div className="mb-2 flex flex-wrap items-center gap-1.5 text-xs">
                      <CompactMenu
                        label="模型"
                        icon={<Brain aria-hidden="true" className="size-3.5 text-primary" />}
                        value={currentModel}
                        options={modelOptions}
                        disabled={writesLocked || !canUse || updatePreferences.pending}
                        loading={catalog.pending}
                        onOpen={() => void loadModels()}
                        onSelect={value => void changePreferences(value, currentEffort)}
                      />
                      <CompactMenu
                        label="思考深度"
                        icon={<Gauge aria-hidden="true" className="size-3.5 text-primary" />}
                        value={currentEffort}
                        options={effortOptions}
                        disabled={writesLocked || !canUse || updatePreferences.pending}
                        loading={false}
                        onOpen={() => undefined}
                        onSelect={value => void changePreferences(currentModel, value as "low" | "medium" | "high" | "")}
                      />
                      <span className="inline-flex items-center gap-1 rounded-lg border border-border/80 bg-card px-2 py-1 text-muted-foreground">
                        <Gauge aria-hidden="true" className="size-3.5 text-muted-foreground" />
                        约 {formatTokens(contextData?.estimatedTokens ?? 0)} tokens · {contextData?.messageCount ?? 0} 条
                        {contextData?.summaryPresent ? " · 已压缩" : ""}
                      </span>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        className="h-6 px-2 text-xs"
                        loading={compactContext.pending}
                        disabled={writesLocked || !canUse || compactContext.pending || (contextData?.messageCount ?? 0) < 8}
                        onClick={() => void compressContext()}
                        title="把较早对话压缩为摘要，保留最近 6 条消息"
                      >
                        压缩上下文
                      </Button>
                    </div>
                    {updatePreferences.error || compactContext.error || catalog.error ? (
                      <div className="mb-2">
                        <Notice tone="error" title="AI 设置未完成">
                          {describeApiError(updatePreferences.error ?? compactContext.error ?? catalog.error)}
                        </Notice>
                      </div>
                    ) : null}
                    <div className="relative">
                      <TextArea
                        aria-label="消息内容"
                        value={messageText}
                        onChange={event => setMessageText(event.target.value)}
                        maxLength={32000}
                        placeholder={composerMode === "agent" ? "描述要 AI 完成的运维目标，例如：检查已授权节点的磁盘占用并处理异常…" : "输入消息，或把右侧节点拖进这里授权 AI 使用…"}
                        className="min-h-[88px] max-h-44 resize-none pr-14"
                        onKeyDown={event => {
                          if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
                            event.preventDefault();
                            event.currentTarget.form?.requestSubmit();
                          }
                        }}
                      />
                      <Button
                        type="submit"
                        variant="primary"
                        aria-label={composerMode === "agent" ? "执行" : "发送"}
                        title={composerMode === "agent" ? "执行任务" : "发送"}
                        className="absolute right-2.5 bottom-2.5 size-9 rounded-full p-0 shadow-md"
                        disabled={writesLocked || !canUse || !messageText.trim() || generatingMessageId !== null || runAgent.pending}
                      >
                        {generatingMessageId !== null || runAgent.pending ? (
                          <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                        ) : (
                          <Send aria-hidden="true" className="size-4" />
                        )}
                      </Button>
                    </div>
                    <p className="mt-1.5 px-1 text-[11px] text-muted-foreground">
                      {composerMode === "agent" ? "Enter 开始执行，Shift+Enter 换行；执行过程与审批在“自动化执行”中查看。" : "发送后自动生成回复；Enter 发送，Shift+Enter 换行。"}
                    </p>
                  </form>
                </div>
                <div className="min-h-0 space-y-3 overflow-y-auto">
                  <Panel title="可用资源" description="把节点拖进左侧输入框即授权 AI 读取；也可点击添加。">
                    <div className="space-y-3 px-5 py-4">
                      <div className="flex flex-wrap gap-1.5">
                        {nodeItems.map(node => (
                          <span
                            key={`pick-node-${node.id}`}
                            draggable
                            onDragStart={event => {
                              event.dataTransfer.setData("text/plain", `node:${node.id}`);
                              event.dataTransfer.effectAllowed = "copy";
                            }}
                            onClick={() => void attachResource("node", node.id)}
                            className="inline-flex cursor-grab items-center gap-1 rounded-full border border-border/80 bg-card px-2.5 py-1 text-xs font-medium shadow-2xs hover:border-primary/40 hover:bg-primary-light/50"
                          >
                            <Server aria-hidden="true" className="size-3 text-primary" />
                            {node.name || node.id}
                          </span>
                        ))}
                        {groupItems.map(group => (
                          <span
                            key={`pick-sg-${group.id}`}
                            draggable
                            onDragStart={event => {
                              event.dataTransfer.setData("text/plain", `service_group:${group.id}`);
                              event.dataTransfer.effectAllowed = "copy";
                            }}
                            onClick={() => void attachResource("service_group", group.id)}
                            className="inline-flex cursor-grab items-center gap-1 rounded-full border border-border/80 bg-card px-2.5 py-1 text-xs font-medium shadow-2xs hover:border-primary/40 hover:bg-primary-light/50"
                          >
                            <Layers aria-hidden="true" className="size-3 text-blue-600" />
                            {group.name || group.id}
                          </span>
                        ))}
                      </div>
                      <div>
                        <p className="text-xs font-semibold text-muted-foreground">当前授权（{data.resourceGrants.length}）</p>
                        {data.resourceGrants.length === 0 ? (
                          <p className="mt-1 text-xs text-muted-foreground">默认零授权，AI 不能读取任何节点。</p>
                        ) : (
                          <ul className="mt-1 space-y-1.5">
                            {data.resourceGrants.map(grantItem => (
                              <li key={grantItem.grantId} className="flex items-center justify-between gap-2 rounded-lg border border-border/70 px-2.5 py-1.5 text-xs">
                                <span className="min-w-0 truncate">
                                  {grantItem.resourceType === "service_group" ? "服务组" : "节点"} · {grantItem.resourceType === "service_group" ? groupItems.find(group => group.id === grantItem.resourceId)?.name ?? grantItem.resourceId : nodeItems.find(node => node.id === grantItem.resourceId)?.name ?? grantItem.resourceId}
                                </span>
                                <Button size="sm" variant="ghost" disabled={writesLocked || !canUse} onClick={async () => { const result = await revokeGrant.run(grantItem.grantId); if (result.ok) { session.refreshSilent(); } }}>撤回</Button>
                              </li>
                            ))}
                          </ul>
                        )}
                      </div>
                      <form onSubmit={addGrant} className="space-y-2 border-t border-border/70 pt-3">
                        <div className="grid grid-cols-2 gap-2">
                          <Field label="资源类型">
                            {ids => (
                              <Select id={ids.inputId} value={resourceType} onChange={event => setResourceType(event.target.value as "node" | "service_group")} disabled={writesLocked || !canUse}>
                                <option value="node">节点</option>
                                <option value="service_group">服务组</option>
                              </Select>
                            )}
                          </Field>
                          <Field label="资源">
                            {ids => (
                              <Select id={ids.inputId} value={resourceId} onChange={event => setResourceId(event.target.value)} disabled={writesLocked || !canUse}>
                                <option value="">请选择</option>
                                {resourceType === "node"
                                  ? nodeItems.map(node => <option key={node.id} value={node.id}>{node.name || node.id}</option>)
                                  : groupItems.map(group => <option key={group.id} value={group.id}>{group.name || group.id}</option>)}
                              </Select>
                            )}
                          </Field>
                        </div>
                        <Field label="允许动作（逗号分隔）" hint="多个动作以逗号分隔。">
                          {ids => <TextInput id={ids.inputId} value={actions} onChange={event => setActions(event.target.value)} disabled={writesLocked || !canUse} />}
                        </Field>
                        {localError ? <Notice tone="error" title="无法授权">{localError}</Notice> : null}
                        {grant.error ? <Notice tone="error" title="授权失败">{describeApiError(grant.error)}</Notice> : null}
                        <Button type="submit" size="sm" variant="primary" loading={grant.pending} disabled={writesLocked || !canUse}>添加授权</Button>
                      </form>
                    </div>
                  </Panel>
                </div>
              </div>
            ) : null}
          </div>
        )}
      </AsyncPanel>
    </div>
  );
}
