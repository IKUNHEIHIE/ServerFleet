import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Modal } from "@/components/ui/modal";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, Select, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { credentialsApi } from "@/lib/api/endpoints";
import { modelSettingsApi } from "@/lib/api/endpoints-ai";
import type { AIModelSettings, CredentialMetadata } from "@/lib/api/dto";
import { describeApiError } from "@/lib/api/errors";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

/**
 * 所有者作用域的模型配置（GET/PUT /api/v1/ai/model-settings）。GET 返回已保存配置，
 * 不代表供应商健康；PUT 是当前真人管理员的 owner CAS（expectedRevision）。
 * 页面只显示凭据引用与修订号，绝不显示/保存 BYOK 明文；秘密值只在凭据页签提交一次。
 */
export function AiModelSettingsPanel() {
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
  const [removedKeys, setRemovedKeys] = useState<readonly string[]>([]);
  const aiKeys = credentialsLoaded.filter(credential =>
    credential.kind === "ai_api_key" && credential.status !== "revoked" &&
    credential.usageScope.includes("ai") && !removedKeys.includes(credential.id),
  );
  const [keyName, setKeyName] = useState("");
  const [keySecret, setKeySecret] = useState("");
  const [keyRequest, setKeyRequest] = useState<string | null>(null);
  const [keyNotice, setKeyNotice] = useState<string | null>(null);
  const createKey = useAction((body: { name: string; kind: string; secret: string; usageScope: readonly string[] }, requestKey: string) =>
    credentialsApi.create(body, requestKey),
  );
  const canCreateKey = session.can("credential.manage") || session.can("settings.manage") || session.can("user.manage");
  const [removing, setRemoving] = useState<CredentialMetadata | null>(null);
  const remove = useAction((credential: CredentialMetadata) => credentialsApi.removeAIKey(credential.id, credential.revision));
  const savedSettings = settings.state.status === "ready" ? settings.state.data : null;

  async function removeKey() {
    if (!removing) return;
    const result = await remove.run(removing);
    if (result.ok) {
      setRemovedKeys(ids => [...ids, removing.id]);
      setRemoving(null);
      setKeyNotice("AI API Key 已删除。");
      credentials.refreshSilent();
    }
  }

  async function addKey(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!keyName.trim() || !keySecret.trim()) return;
    setKeyNotice(null);
    const requestKey = keyRequest ?? globalThis.crypto.randomUUID();
    setKeyRequest(requestKey);
    const result = await createKey.run({ name: keyName.trim(), kind: "ai_api_key", secret: keySecret.trim(), usageScope: ["ai"] }, requestKey);
    if (result.ok) {
      setKeyRequest(null);
      setKeyName("");
      setKeySecret("");
      setKeyNotice("AI 密钥已保存；请在下方模型设置中选中它并保存。");
      credentials.refreshSilent();
    }
  }

  return (
    <Panel title="AI 模型" description="选择模型与密钥；保存成功不代表服务商当前可用。">
      {conflict ? (
        <Notice tone="error" title="配置已被其他操作修改">
          已重新读取服务端最新配置；请核对后再次保存，不要覆盖他人变更。
        </Notice>
      ) : null}
      <AsyncPanel state={settings.state} onRetry={settings.reload} errorTitle="模型设置加载失败">
        {data => (
          <ModelSettingsForm
            key={data.revision}
            data={data}
            aiKeys={aiKeys}
            credentialsReady={credentials.state.status === "ready"}
            isAdmin={isAdmin}
            writesLocked={session.writesLocked}
            onSaved={() => { setConflict(false); settings.reload(); }}
            onConflict={() => { setConflict(true); settings.refreshSilent(); }}
          />
        )}
      </AsyncPanel>
      <div className="border-t border-border/80 px-5 py-4">
        <AsyncPanel state={credentials.state} onRetry={credentials.reload} errorTitle="AI 密钥列表加载失败">
          {() => aiKeys.length > 0 ? (
            <ul aria-label="已保存的 AI API Key" className="mb-3 space-y-2">
              {aiKeys.map(key => (
                <li key={key.id} className="flex items-center justify-between gap-2 rounded-lg border border-border px-3 py-2">
                  <span className="min-w-0 break-all text-sm">{key.name}{savedSettings?.credentialRef === key.id ? " · 当前绑定" : ""}</span>
                  <Button type="button" size="sm" variant="danger" aria-label={"删除 " + key.name}
                    disabled={!canCreateKey || session.writesLocked || remove.pending} onClick={() => setRemoving(key)}>删除</Button>
                </li>
              ))}
            </ul>
          ) : null}
        </AsyncPanel>
        {keyNotice ? <Notice tone="success" title="操作完成">{keyNotice}</Notice> : null}
        <details open={aiKeys.length === 0}>
          <summary className="cursor-pointer text-sm font-semibold">新增 AI API Key</summary>
          <form onSubmit={addKey} className="mt-3 space-y-3">
            <p className="text-xs text-muted-foreground">密钥只在提交时传输一次，保存后不再显示；不会写入浏览器存储。</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="密钥名称">
                {ids => <TextInput id={ids.inputId} value={keyName} onChange={event => setKeyName(event.target.value)} placeholder="例如 StepFun" maxLength={64} disabled={!canCreateKey || session.writesLocked || createKey.pending} autoComplete="off" />}
              </Field>
              <Field label="API Key">
                {ids => <TextInput id={ids.inputId} type="password" value={keySecret} onChange={event => setKeySecret(event.target.value)} placeholder="粘贴服务商密钥" maxLength={4096} disabled={!canCreateKey || session.writesLocked || createKey.pending} autoComplete="new-password" />}
              </Field>
            </div>
            {createKey.error ? <Notice tone="error" title="保存失败">{describeApiError(createKey.error)}</Notice> : null}
            {keyRequest ? <Notice tone="warning" title="提交结果待核对">再次提交会复用同一请求编号，避免重复创建密钥。</Notice> : null}
            {!canCreateKey ? <p className="text-xs text-muted-foreground">当前身份不能新增安全凭据。</p> : null}
            <Button type="submit" size="sm" variant="primary" loading={createKey.pending} disabled={!canCreateKey || session.writesLocked || !keyName.trim() || !keySecret.trim()}>
              保存 AI 密钥
            </Button>
          </form>
        </details>
      </div>
      <Modal open={removing !== null} title="删除 AI API Key"
        description={removing ? "将从 Server Fleet 删除“" + removing.name + "”。绑定该密钥的模型需要换绑才能继续生成。" : undefined}
        onClose={() => { if (!remove.pending) setRemoving(null); }}
        footer={<>
          <Button type="button" onClick={() => setRemoving(null)} disabled={remove.pending}>取消</Button>
          <Button type="button" variant="danger" onClick={() => void removeKey()} loading={remove.pending}>确认删除</Button>
        </>}>
        {remove.error ? <Notice tone="error" title="删除失败">{describeApiError(remove.error)}</Notice> : null}
      </Modal>
    </Panel>
  );
}

function ModelSettingsForm({
  data,
  aiKeys,
  credentialsReady,
  isAdmin,
  writesLocked,
  onSaved,
  onConflict,
}: {
  readonly data: AIModelSettings;
  readonly aiKeys: readonly CredentialMetadata[];
  readonly credentialsReady: boolean;
  readonly isAdmin: boolean;
  readonly writesLocked: boolean;
  readonly onSaved: () => void;
  readonly onConflict: () => void;
}) {
  const [enabled, setEnabled] = useState(data.enabled);
  const [provider, setProvider] = useState(data.provider || "openai-responses");
  const [endpoint, setEndpoint] = useState(data.endpoint ?? "");
  const [model, setModel] = useState(data.model);
  const [credentialRef, setCredentialRef] = useState(data.credentialRef);
  const [catalog, setCatalog] = useState<readonly string[] | null>(null);
  const selected = aiKeys.find(credential => credential.id === credentialRef) ?? null;

  const save = useAction((body: {
    expectedRevision: number;
    enabled: boolean;
    provider: string;
    endpoint?: string;
    model: string;
    credentialRef: string;
    credentialRevision: number;
  }) => modelSettingsApi.set(body));
  const listModels = useAction(() => modelSettingsApi.catalog());

  async function fetchModels() {
    setCatalog(null);
    const result = await listModels.run();
    if (result.ok && result.data) setCatalog(result.data.data.items);
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enabled && (!model.trim() || !selected)) return;
    const result = await save.run({
      expectedRevision: data.revision,
      enabled,
      provider,
      ...(endpoint.trim() || data.endpoint ? { endpoint: endpoint.trim() } : {}),
      model: model.trim(),
      credentialRef: selected?.id ?? "",
      credentialRevision: selected?.revision ?? 0,
    });
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
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge tone={data.enabled ? "success" : "neutral"}>{data.enabled ? "生成已启用" : "生成未启用"}</Badge>
        <span className="font-medium">{data.model || "未选择模型"}</span>
        <span className="text-xs text-muted-foreground">
          密钥 {selected?.name ?? (data.credentialRef ? "已绑定" : "未绑定")}
          {data.updatedAt ? ` · 更新于 ${formatDateTime(data.updatedAt) ?? "—"}` : ""}
        </span>
      </div>
      {credentialsReady && data.enabled && data.credentialRef && !selected && (
        <Notice tone="warning" title="需要选择可用的 AI 密钥">换绑并保存后，模型才能继续生成。</Notice>
      )}
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">连接信息</summary>
        <div className="mt-2 grid gap-1 sm:grid-cols-2">
          <p>接入方式：{data.provider === "openai-chat-completions" ? "Chat Completions" : "Responses"}</p>
          <p>密钥引用：{data.credentialRef ? `已绑定（第 ${data.credentialRevision} 版）` : "未绑定"}</p>
          <p>配置修订：{data.revision}</p>
        </div>
      </details>
      <Field label="接口协议" hint="按服务商文档选择；保存后用于聊天、Agent 工具调用与流式输出。">
        {ids => (
          <Select id={ids.inputId} value={provider} onChange={event => setProvider(event.target.value)} disabled={!isAdmin || writesLocked}>
            <option value="openai-responses">OpenAI Responses</option>
            <option value="openai-chat-completions">OpenAI Chat Completions</option>
          </Select>
        )}
      </Field>
      <Field label="模型端点" hint="OpenAI 兼容基础地址，例如 https://api.stepfun.com/step_plan/v1；留空使用部署默认。">
        {ids => <TextInput id={ids.inputId} type="url" value={endpoint} onChange={event => setEndpoint(event.target.value)} placeholder="https://api.example.com/v1" maxLength={512} disabled={!isAdmin || writesLocked} autoComplete="off" />}
      </Field>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="模型 ID" hint="可手动填写，或保存后点击“获取模型”选择。">
          {ids => <TextInput id={ids.inputId} value={model} onChange={event => setModel(event.target.value)} maxLength={128} disabled={!isAdmin || writesLocked} />}
        </Field>
        <Field label="模型密钥" hint="密钥只在「安全凭据」录入一次，此处仅选择。">
          {ids => (
            <Select id={ids.inputId} value={selected?.id ?? ""} onChange={event => setCredentialRef(event.target.value)} disabled={!isAdmin || writesLocked}>
              <option value="">未绑定</option>
              {aiKeys.map(credential => (
                <option key={credential.id} value={credential.id}>
                  {credential.name}（{credential.status}）
                </option>
              ))}
            </Select>
          )}
        </Field>
      </div>
      <div className="rounded-xl border border-border/70 bg-[#FAFBFD] p-3">
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" size="sm" variant="secondary" loading={listModels.pending} disabled={!isAdmin || writesLocked || !credentialRef || !(endpoint.trim() || data.endpoint)} onClick={() => void fetchModels()}>
            获取模型
          </Button>
          <span className="text-xs text-muted-foreground">先保存端点与密钥，再获取模型列表；选择后会填入上方模型 ID。</span>
        </div>
        {listModels.error ? <Notice tone="error" title="获取模型失败">{describeApiError(listModels.error)}</Notice> : null}
        {catalog ? (
          catalog.length === 0 ? (
            <p className="mt-2 text-xs text-muted-foreground">服务商没有返回可用模型。</p>
          ) : (
            <div className="mt-2 flex max-h-32 flex-wrap gap-1.5 overflow-y-auto">
              {catalog.map(item => (
                <button
                  key={item}
                  type="button"
                  aria-pressed={model === item}
                  onClick={() => setModel(item)}
                  className={"rounded-full border px-2.5 py-1 text-xs transition-colors " + (model === item ? "border-primary bg-primary/10 font-semibold text-primary" : "border-border bg-card text-foreground hover:border-primary/40")}
                >
                  {item}
                </button>
              ))}
            </div>
          )
        ) : null}
      </div>
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={!isAdmin || writesLocked} />
        启用模型生成（只在你手动触发时调用）
      </label>
      {saveError && !saveErrorCode.includes("MODEL_REVISION_CONFLICT") ? (
        <Notice tone="error" title="保存失败">{describeApiError(saveError)}</Notice>
      ) : null}
      {!isAdmin ? <p className="text-xs text-muted-foreground">当前身份不是管理员：只能查看，不能修改。</p> : null}
      <Button type="submit" variant="primary" loading={save.pending} disabled={!isAdmin || writesLocked || (enabled && (!model.trim() || !selected))}>
        保存模型设置
      </Button>
    </form>
  );
}