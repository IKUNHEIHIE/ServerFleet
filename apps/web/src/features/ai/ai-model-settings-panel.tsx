import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
  const aiKeys = credentialsLoaded.filter(credential => credential.kind === "ai_api_key" && credential.usageScope.includes("ai"));

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
      <details className="text-xs text-muted-foreground">
        <summary className="cursor-pointer">连接信息</summary>
        <div className="mt-2 grid gap-1 sm:grid-cols-2">
          <p>接入方式：OpenAI 兼容 Responses</p>
          <p>密钥引用：{data.credentialRef ? `已绑定（第 ${data.credentialRevision} 版）` : "未绑定"}</p>
          <p>配置修订：{data.revision}</p>
        </div>
      </details>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="模型 ID" hint="填写服务商提供的模型 ID。">
          {ids => <TextInput id={ids.inputId} value={model} onChange={event => setModel(event.target.value)} maxLength={128} disabled={!isAdmin || writesLocked} />}
        </Field>
        <Field label="模型密钥" hint="密钥只在「安全凭据」录入一次，此处仅选择。">
          {ids => (
            <Select id={ids.inputId} value={credentialRef} onChange={event => setCredentialRef(event.target.value)} disabled={!isAdmin || writesLocked}>
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
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={!isAdmin || writesLocked} />
        启用模型生成（只在你手动触发时调用）
      </label>
      {saveError && !saveErrorCode.includes("MODEL_REVISION_CONFLICT") ? (
        <Notice tone="error" title="保存失败">{describeApiError(saveError)}</Notice>
      ) : null}
      {!isAdmin ? <p className="text-xs text-muted-foreground">当前身份不是管理员：只能查看，不能修改。</p> : null}
      <Button type="submit" variant="primary" loading={save.pending} disabled={!isAdmin || writesLocked || (enabled && (!model.trim() || !credentialRef))}>
        保存模型设置
      </Button>
    </form>
  );
}