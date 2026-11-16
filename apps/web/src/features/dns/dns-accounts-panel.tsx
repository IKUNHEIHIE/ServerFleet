import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { Panel, SubCard } from "@/components/ui/panel";
import { describeApiError } from "@/lib/api/errors";
import { credentialsApi } from "@/lib/api/endpoints";
import { dnsApi } from "@/lib/api/endpoints-infra";
import type { DNSAccount, DNSConnectionTest } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";

async function createDnsCredential(displayName: string, token: string): Promise<{ ref: string; revision: number }> {
  const created = await credentialsApi.create({
    name: `DNS ${displayName}`,
    kind: "cloudflare_token",
    secret: token,
    usageScope: ["dns"],
  });
  return { ref: created.data.id, revision: created.data.revision };
}

function AccountEditor({
  open,
  account,
  onClose,
  onSaved,
}: {
  readonly open: boolean;
  readonly account: DNSAccount | null;
  readonly onClose: () => void;
  readonly onSaved: () => void;
}) {
  const session = useSession();
  const [displayName, setDisplayName] = useState("");
  const [note, setNote] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [apiToken, setApiToken] = useState("");
  const [localError, setLocalError] = useState<string | null>(null);
  const action = useAction(async (body: { displayName?: string; note?: string; enabled?: boolean; apiToken?: string }) => {
    if (account) {
      const response = await dnsApi.updateAccount(account.id, { displayName: body.displayName, note: body.note, enabled: body.enabled });
      return response.data;
    }
    const credential = await createDnsCredential(body.displayName ?? "", body.apiToken ?? "");
    const response = await dnsApi.createAccount({
      providerCode: "cloudflare",
      displayName: body.displayName ?? "",
      note: body.note,
      enabled: body.enabled,
      apiToken: credential.ref,
      credentialRevision: credential.revision,
    });
    return response.data;
  });

  useEffect(() => {
    if (!open) return;
    setDisplayName(account?.displayName ?? "");
    setNote(account?.note ?? "");
    setEnabled(account?.enabled ?? true);
    setApiToken("");
    setLocalError(null);
  }, [open, account]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!displayName.trim()) return setLocalError("显示名称不能为空。");
    if (!account && !apiToken) return setLocalError("创建账号必须填写 Cloudflare API Token。");
    setLocalError(null);
    const result = await action.run({
      displayName: displayName.trim(),
      ...(note.trim() ? { note: note.trim() } : {}),
      enabled,
      ...(!account ? { apiToken } : {}),
    });
    setApiToken("");
    if (result.ok) onSaved();
  }

  const disabled = !session.can("dns.account.manage") || session.writesLocked;

  return (
    <Modal
      open={open}
      title={account ? "编辑 DNS 账号" : "添加 Cloudflare 账号"}
      description="Token 只写入服务端凭据存储，账号仅保存引用；创建后不再回显。"
      onClose={() => {
        if (!action.pending) onClose();
      }}
      footer={
        <>
          <Button type="button" onClick={onClose} disabled={action.pending}>取消</Button>
          <Button type="submit" form="dns-account-editor" variant="primary" loading={action.pending} disabled={disabled}>保存账号</Button>
        </>
      }
    >
      <form id="dns-account-editor" onSubmit={submit} className="space-y-4">
        {localError ? <Notice tone="error" title="无法提交">{localError}</Notice> : null}
        {action.error ? <Notice tone="error" title="保存失败">{describeApiError(action.error)}</Notice> : null}
        <Field label="显示名称">{ids => <TextInput id={ids.inputId} value={displayName} onChange={event => setDisplayName(event.target.value)} required disabled={disabled} />}</Field>
        <Field label="备注（可选）">{ids => <TextInput id={ids.inputId} value={note} onChange={event => setNote(event.target.value)} disabled={disabled} />}</Field>
        <label className="flex items-center gap-2 text-sm">
          <Checkbox checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={disabled} />
          启用账号
        </label>
        {!account ? (
          <Field label="Cloudflare API Token" hint="只写入服务端凭据存储；保存后自动创建 DNS 专用凭据并绑定引用。">
            {ids => (
              <TextInput
                id={ids.inputId}
                type="password"
                autoComplete="new-password"
                value={apiToken}
                onChange={event => setApiToken(event.target.value)}
                disabled={disabled}
              />
            )}
          </Field>
        ) : null}
      </form>
    </Modal>
  );
}

export function DnsAccountsPanel() {
  const session = useSession();
  const canManage = session.can("dns.account.manage");
  const [editor, setEditor] = useState<{ open: boolean; account: DNSAccount | null }>({ open: false, account: null });
  const [deleting, setDeleting] = useState<DNSAccount | null>(null);
  const [rotating, setRotating] = useState<DNSAccount | null>(null);
  const [rotateToken, setRotateToken] = useState("");
  const [testResult, setTestResult] = useState<{ accountId: string; result: DNSConnectionTest } | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const accounts = useResource(`dns:accounts:${session.viewEpoch}`, signal => dnsApi.accounts(signal).then(result => result.data));
  const accountItems = accounts.state.status === "ready" ? accounts.state.data.items : [];
  const deleteAction = useAction((id: string) => dnsApi.removeAccount(id));
  const rotateAction = useAction(async (id: string, token: string, displayName: string) => {
    const credential = await createDnsCredential(`DNS 轮换 ${displayName}`, token);
    return dnsApi.rotateCredential(id, credential.ref, credential.revision);
  });
  const defaultAction = useAction((id: string) => dnsApi.setDefault(id));
  const testAction = useAction((id: string) => dnsApi.test(id));

  async function removeAccount() {
    if (!deleting) return;
    const result = await deleteAction.run(deleting.id);
    if (result.ok) {
      setDeleting(null);
      setOutcome("账号绑定已解除；不会删除 Cloudflare 上的真实 Zone/记录。");
      accounts.reload();
    }
  }

  async function rotateCredential() {
    if (!rotating || !rotateToken) return;
    const result = await rotateAction.run(rotating.id, rotateToken, rotating.displayName);
    setRotateToken("");
    if (result.ok) {
      setRotating(null);
      setOutcome("凭据已更换；旧凭据不会自动删除，验证新凭据通过后可到安全凭据页撤销旧凭据。");
      accounts.reload();
    }
  }

  async function setDefault(account: DNSAccount) {
    const result = await defaultAction.run(account.id);
    if (result.ok) {
      setOutcome(`默认账号已切换为 ${account.displayName}。`);
      accounts.reload();
    }
  }

  async function test(account: DNSAccount) {
    setTestResult(null);
    const result = await testAction.run(account.id);
    if (result.ok && result.data) {
      setTestResult({ accountId: account.id, result: result.data.data });
      accounts.reload();
    }
  }

  return (
    <div className="space-y-4">
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      <Panel
        title="DNS 账号"
        description="Cloudflare 账号与凭据只存引用，不显示 Token 明文；DNS 管理页默认使用这里的默认账号。"
        actions={<Button size="sm" variant="primary" disabled={!canManage || session.writesLocked} onClick={() => setEditor({ open: true, account: null })}>添加账号</Button>}
      >
        <AsyncPanel
          state={accounts.state}
          onRetry={accounts.reload}
          errorTitle="账号加载失败"
          empty={<EmptyState title="暂无 Cloudflare 账号" description="添加账号后即可在 DNS 管理页导入 Zone 与查看记录。" />}
          isEmpty={data => data.items.length === 0}
        >
          {() => (
            <ul className="divide-y">
              {accountItems.map(account => (
                <li key={account.id} className="px-5 py-4">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <p className="font-medium">
                        {account.displayName}
                        {account.isDefault ? <span className="ml-2 text-xs text-primary">默认</span> : null}
                      </p>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        {account.enabled ? "启用" : "停用"} · 凭据 {account.credentialStatus}
                        {account.zoneCount !== undefined ? ` · ${account.zoneCount} 个已导入 Zone` : ""}
                      </p>
                    </div>
                    <div className="flex items-center gap-1">
                      <Button size="sm" onClick={() => void test(account)} loading={testAction.pending}>检查</Button>
                      <details className="relative">
                        <summary className="cursor-pointer list-none rounded-md border border-border px-2 py-1 text-xs text-muted-foreground hover:bg-secondary">更多</summary>
                        <div className="absolute right-0 z-20 mt-1 min-w-28 rounded-md border border-border bg-card p-1 shadow-lg">
                          <button type="button" className="block w-full rounded px-3 py-1.5 text-left text-sm hover:bg-secondary disabled:opacity-50" disabled={!canManage || session.writesLocked} onClick={event => { event.currentTarget.closest("details")?.removeAttribute("open"); setEditor({ open: true, account }); }}>编辑</button>
                          <button type="button" className="block w-full rounded px-3 py-1.5 text-left text-sm hover:bg-secondary disabled:opacity-50" disabled={!canManage || session.writesLocked || account.isDefault} onClick={event => { event.currentTarget.closest("details")?.removeAttribute("open"); void setDefault(account); }}>设为默认</button>
                          <button type="button" className="block w-full rounded px-3 py-1.5 text-left text-sm hover:bg-secondary disabled:opacity-50" disabled={!canManage || session.writesLocked} onClick={event => { event.currentTarget.closest("details")?.removeAttribute("open"); setRotating(account); }}>更换凭据</button>
                          <button type="button" className="block w-full rounded px-3 py-1.5 text-left text-sm text-destructive hover:bg-destructive/5 disabled:opacity-50" disabled={!canManage || session.writesLocked} onClick={event => { event.currentTarget.closest("details")?.removeAttribute("open"); setDeleting(account); }}>解除绑定</button>
                        </div>
                      </details>
                    </div>
                  </div>
                  {testResult?.accountId === account.id ? (
                    <SubCard className="mt-3 p-2.5 text-xs">
                      凭据检查：{testResult.result.status === "success" ? "通过" : testResult.result.status === "partial" ? "部分通过" : "失败"}
                      {" · "}Zone 读取 {testResult.result.zoneRead ? "通过" : "未通过"}
                      {" · "}记录写入 {testResult.result.recordWrite ? "通过" : "未通过"}
                    </SubCard>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </AsyncPanel>
      </Panel>

      <AccountEditor
        open={editor.open}
        account={editor.account}
        onClose={() => setEditor({ open: false, account: null })}
        onSaved={() => {
          setEditor({ open: false, account: null });
          setOutcome("DNS 账号已保存（服务端结果）。");
          accounts.reload();
        }}
      />
      <Modal
        open={deleting !== null}
        title="解除账号绑定"
        description={deleting ? `解除“${deleting.displayName}”。不会删除 Cloudflare 上的真实 Zone 或记录。` : undefined}
        onClose={() => setDeleting(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeleting(null)} disabled={deleteAction.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={removeAccount} loading={deleteAction.pending}>确认解除</Button>
          </>
        }
      >
        {deleteAction.error ? <Notice tone="error" title="解除失败">{describeApiError(deleteAction.error)}</Notice> : null}
      </Modal>
      <Modal
        open={rotating !== null}
        title="更换 DNS 凭据"
        description={rotating ? `账号 ${rotating.displayName}；粘贴新的 Cloudflare API Token，保存时自动创建凭据并切换引用。` : undefined}
        onClose={() => { if (!rotateAction.pending) setRotating(null); }}
        footer={
          <>
            <Button type="button" onClick={() => setRotating(null)} disabled={rotateAction.pending}>取消</Button>
            <Button type="button" variant="primary" onClick={rotateCredential} loading={rotateAction.pending} disabled={!rotateToken}>提交</Button>
          </>
        }
      >
        {rotateAction.error ? <Notice tone="error" title="更换失败">{describeApiError(rotateAction.error)}</Notice> : null}
        <Field label="新的 Cloudflare API Token">
          {ids => <TextInput id={ids.inputId} type="password" autoComplete="new-password" value={rotateToken} onChange={event => setRotateToken(event.target.value)} />}
        </Field>
      </Modal>
    </div>
  );
}