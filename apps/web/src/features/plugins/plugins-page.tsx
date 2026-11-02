import { useEffect, useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { DataTable } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { pluginsApi } from "@/lib/api/endpoints-ai";
import type { InstalledPlugin, PluginCatalogEntry, PluginOperation } from "@/lib/api/dto";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatDateTime } from "@/lib/format";

const PLUGIN_STATUS: Record<InstalledPlugin["status"], string> = {
  installed_disabled: "已安装·停用",
  disabled: "已安装·停用",
  starting: "启动中",
  enabled: "已启用",
  draining: "排空中",
  updating: "更新中",
  uninstalling: "卸载中",
  stopping: "停止中",
  runtime_unknown: "运行状态未知",
  stop_unknown: "停止状态未知",
  failed: "失败",
};

const OPERATION_STATUS: Record<PluginOperation["status"], string> = {
  queued: "排队",
  verifying: "校验中",
  draining: "排空中",
  applying: "应用中",
  succeeded: "成功",
  failed: "失败",
  rolled_back: "已回滚",
  deferred: "已延期",
};

export function PluginsPage() {
  const session = useSession();
  const [mode, setMode] = useState<"official" | "custom">("official");
  const [url, setUrl] = useState("");
  const [ref, setRef] = useState("main");
  const [installing, setInstalling] = useState<PluginCatalogEntry | null>(null);
  const [accepted, setAccepted] = useState<ReadonlySet<string>>(new Set());
  const [enableAfter, setEnableAfter] = useState(false);
  const [installedAction, setInstalledAction] = useState<InstalledPlugin | null>(null);
  const [updateVersion, setUpdateVersion] = useState("");
  const [uninstalling, setUninstalling] = useState<InstalledPlugin | null>(null);
  const [operation, setOperation] = useState<PluginOperation | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);

  const repository = useResource(`plugins:repository:${session.viewEpoch}`, signal => pluginsApi.repository(signal).then(result => result.data));
  const catalog = useResource(`plugins:catalog:${session.viewEpoch}`, signal => pluginsApi.catalog(signal).then(result => result.data));
  const installed = useResource(`plugins:installed:${session.viewEpoch}`, signal => pluginsApi.installed(signal).then(result => result.data));

  const repo = repository.state.status === "ready" ? repository.state.data : null;
  const catalogItems = catalog.state.status === "ready" ? catalog.state.data.items : [];
  const installedItems = installed.state.status === "ready" ? installed.state.data.items : [];
  const canManage = session.can("plugin.manage") || session.can("plugin.install") || session.can("settings.manage");
  const canRepo = session.can("plugin.repository.manage") || session.can("plugin.manage");

  const putRepository = useAction((body: { mode: "official" | "custom"; url?: string; ref: string; path: "Plugin" }) => pluginsApi.putRepository(body));
  const refreshCatalog = useAction(() => pluginsApi.refreshCatalog());
  const install = useAction((pluginId: string, body: Parameters<typeof pluginsApi.install>[1]) => pluginsApi.install(pluginId, body));
  const activation = useAction((pluginId: string, enabled: boolean) => pluginsApi.activation(pluginId, enabled));
  const update = useAction((pluginId: string, body: Parameters<typeof pluginsApi.update>[1]) => pluginsApi.update(pluginId, body));
  const uninstall = useAction((pluginId: string) => pluginsApi.uninstall(pluginId));
  const refreshOperation = useAction((operationId: string) => pluginsApi.operation(operationId));

  useEffect(() => {
    if (!repo) return;
    setMode(repo.mode);
    setUrl(repo.url ?? "");
    setRef(repo.ref);
  }, [repo]);

  async function saveRepository(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (mode === "custom" && !url.trim()) return setOutcome("自定义来源必须填写 GitHub 仓库 URL。");
    setOutcome(null);
    const result = await putRepository.run({
      mode,
      ...(mode === "custom" ? { url: url.trim() } : {}),
      ref: ref.trim() || "main",
      path: "Plugin",
    });
    if (result.ok) {
      setOutcome("插件来源已保存；不会自动替换已安装插件。");
      repository.reload();
      catalog.reload();
    }
  }

  function openInstall(entry: PluginCatalogEntry) {
    setInstalling(entry);
    setAccepted(new Set());
    setEnableAfter(false);
  }

  async function submitInstall(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!installing) return;
    const required = installing.permissions;
    if (required.some(permission => !accepted.has(permission))) return;
    if (!installing.sourceCommit || !installing.artifactSha256) return;
    const result = await install.run(installing.id, {
      version: installing.version,
      sourceCommit: installing.sourceCommit,
      expectedSha256: installing.artifactSha256,
      acceptedPermissions: [...accepted],
      enableAfterInstall: enableAfter,
    });
    if (result.ok && result.data) {
      setInstalling(null);
      setOperation(result.data.data);
      setOutcome("安装操作已提交；完成状态以操作详情为准。");
      installed.reload();
      session.refreshPlugins();
    }
  }

  async function toggle(plugin: InstalledPlugin) {
    const result = await activation.run(plugin.id, !plugin.enabled);
    if (result.ok && result.data) {
      setOperation(result.data.data);
      setOutcome(plugin.enabled ? "停用操作已提交；入口将在服务端确认后消失。" : "启用操作已提交；入口出现仍需服务端确认。");
      installed.reload();
      session.refreshPlugins();
    }
  }

  async function submitUpdate(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!installedAction) return;
    const target = catalogItems.find(entry => entry.id === installedAction.id && entry.version === updateVersion);
    if (!target?.sourceCommit || !target.artifactSha256) return;
    const result = await update.run(installedAction.id, {
      version: target.version,
      sourceCommit: target.sourceCommit,
      expectedSha256: target.artifactSha256,
      acceptedPermissions: target.permissions,
      deferIfBusy: true,
    });
    if (result.ok && result.data) {
      setInstalledAction(null);
      setOperation(result.data.data);
      setOutcome("更新操作已提交；失败时服务端应保持旧版可用。");
      installed.reload();
      session.refreshPlugins();
    }
  }

  async function remove() {
    if (!uninstalling) return;
    const result = await uninstall.run(uninstalling.id);
    if (result.ok && result.data) {
      setUninstalling(null);
      setOperation(result.data.data);
      setOutcome("卸载操作已提交；插件业务菜单应随停用/卸载消失。");
      installed.reload();
      session.refreshPlugins();
    }
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="插件中心"
        description="可选插件不进入核心发布包；未安装/禁用时不显示业务菜单。唯一官方插件为 Xray 节点管理器。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">可选插件</span>}
        actions={<Button type="button" onClick={() => { repository.reload(); catalog.reload(); installed.reload(); }}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}

      <Panel title="插件来源" description="GET/PUT /api/v1/plugins/repository；自定义来源必须显示为非官方并锁定不可变版本。">
        <form onSubmit={saveRepository} className="space-y-3 px-5 py-4">
          {putRepository.error ? <Notice tone="error" title="保存来源失败">{describeApiError(putRepository.error)}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-3">
            <Field label="来源模式">
              {ids => (
                <Select id={ids.inputId} value={mode} onChange={event => setMode(event.target.value as "official" | "custom")} disabled={!canRepo || session.writesLocked}>
                  <option value="official">官方</option>
                  <option value="custom">第三方 Fork</option>
                </Select>
              )}
            </Field>
            <Field label="Ref">
              {ids => <TextInput id={ids.inputId} value={ref} onChange={event => setRef(event.target.value)} disabled={!canRepo || session.writesLocked} />}
            </Field>
            <Field label="路径" hint="固定 Plugin/">
              {ids => <TextInput id={ids.inputId} value="Plugin" disabled />}
            </Field>
          </div>
          {mode === "custom" ? (
            <Field label="GitHub 仓库 URL" hint="只接受受信任的 github.com/owner/repo 形式；页面会显示非官方来源。">
              {ids => <TextInput id={ids.inputId} value={url} onChange={event => setUrl(event.target.value)} disabled={!canRepo || session.writesLocked} />}
            </Field>
          ) : null}
          <p className="text-xs text-muted-foreground">
            {repo ? `当前来源：${repo.mode}${repo.resolvedCommit ? ` · commit ${repo.resolvedCommit}` : " · commit 未解析"}${repo.officialVerified ? " · 官方已核验" : ""}` : "来源尚未加载"}
          </p>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" variant="primary" loading={putRepository.pending} disabled={!canRepo || session.writesLocked}>保存来源</Button>
            <Button
              type="button"
              loading={refreshCatalog.pending}
              disabled={!canRepo || session.writesLocked}
              onClick={async () => {
                const result = await refreshCatalog.run();
                if (result.ok) {
                  setOutcome("目录元数据已刷新；不会自动安装。");
                  catalog.reload();
                }
              }}
            >
              刷新目录
            </Button>
          </div>
          {refreshCatalog.error ? (
            <Notice tone="error" title="目录刷新失败">{describeApiError(refreshCatalog.error)}</Notice>
          ) : null}
        </form>
      </Panel>

      <Panel title="插件目录" description="GET /api/v1/plugins/catalog；只读取元数据，主动安装才获取插件包。">
        {catalog.state.status === "error" && catalog.state.error.code === "PLUGIN_CATALOG_SOURCE_UNAVAILABLE" ? (
          <div className="px-5 pb-4">
            <Notice tone="warning" title="目录源不可用（B03 硬依赖）">
              服务端没有可用目录源或工件准入；本页不会安装/运行未审查产物，也不会回退到演示插件。
            </Notice>
          </div>
        ) : null}
        <AsyncPanel state={catalog.state} onRetry={catalog.reload} errorTitle="目录加载失败" empty={<EmptyState title="目录为空" description="官方仓库可能尚未绑定或没有可安装条目。" />} isEmpty={data => data.items.length === 0}>
          {() => (
            <DataTable
              caption="可选插件目录"
              rows={catalogItems}
              rowKey={entry => `${entry.id}@${entry.version}`}
              columns={[
                { key: "name", header: "插件", render: entry => <span>{entry.name}<span className="block font-mono text-xs text-muted-foreground">{entry.id}</span></span> },
                { key: "version", header: "版本", render: entry => entry.version },
                { key: "type", header: "类型", render: entry => entry.entryType ?? "未声明" },
                { key: "permissions", header: "权限", render: entry => entry.permissions.join("、") || "无" },
                { key: "hash", header: "校验", render: entry => entry.artifactSha256 ? "有 SHA-256" : <span className="text-destructive">缺少 SHA-256</span> },
                {
                  key: "actions",
                  header: "操作",
                  render: entry => (
                    <Button size="sm" variant="primary" onClick={() => openInstall(entry)} disabled={!canManage || session.writesLocked}>
                      安装
                    </Button>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Panel title="已安装插件" description="GET /api/v1/plugins；禁用/卸载后业务菜单与 API 不应可执行。">
        <AsyncPanel state={installed.state} onRetry={installed.reload} errorTitle="已安装插件加载失败" empty={<EmptyState title="未安装可选插件" description="核心功能不依赖任何可选插件。" />} isEmpty={data => data.items.length === 0}>
          {() => (
            <DataTable
              caption="已安装插件"
              rows={installedItems}
              rowKey={plugin => plugin.id}
              columns={[
                { key: "id", header: "插件", render: plugin => <span>{plugin.id}<span className="block text-xs text-muted-foreground">{plugin.version}</span></span> },
                { key: "status", header: "状态", render: plugin => PLUGIN_STATUS[plugin.status] },
                { key: "enabled", header: "启用", render: plugin => (plugin.enabled ? "是" : "否") },
                { key: "source", header: "来源", render: plugin => plugin.sourceURL ?? plugin.source ?? plugin.sourceCommit ?? "—" },
                { key: "installed", header: "安装时间", render: plugin => formatDateTime(plugin.installedAt) ?? "—" },
                { key: "error", header: "错误", render: plugin => plugin.lastError ?? "—" },
                {
                  key: "actions",
                  header: "操作",
                  render: plugin => (
                    <div className="flex flex-wrap gap-1">
                      <Button size="sm" onClick={() => { setInstalledAction(plugin); setUpdateVersion(catalogItems.find(entry => entry.id === plugin.id && entry.version !== plugin.version)?.version ?? ""); }} disabled={!canManage || session.writesLocked}>更新</Button>
                      <Button size="sm" onClick={() => void toggle(plugin)} loading={activation.pending} disabled={!canManage || session.writesLocked}>
                        {plugin.enabled ? "停用" : "启用"}
                      </Button>
                      <Button size="sm" variant="danger" onClick={() => setUninstalling(plugin)} disabled={!canManage || session.writesLocked}>卸载</Button>
                    </div>
                  ),
                },
              ]}
            />
          )}
        </AsyncPanel>
      </Panel>

      <Modal
        open={installing !== null}
        title={installing ? `安装 ${installing.name} ${installing.version}` : "安装插件"}
        description="安装前必须核对来源、版本、权限与完整性；未安装/禁用不会加载业务菜单。"
        onClose={() => setInstalling(null)}
        wide
        footer={
          <>
            <Button type="button" onClick={() => setInstalling(null)} disabled={install.pending}>取消</Button>
            <Button type="submit" form="plugin-install" variant="primary" loading={install.pending} disabled={!installing?.sourceCommit || !installing?.artifactSha256 || !canManage || session.writesLocked}>确认安装</Button>
          </>
        }
      >
        {installing ? (
          <form id="plugin-install" onSubmit={submitInstall} className="space-y-3 text-sm">
            {install.error ? <Notice tone="error" title="安装失败">{describeApiError(install.error)}</Notice> : null}
            <p>来源 commit：<span className="font-mono text-xs">{installing.sourceCommit ?? "缺少"}</span></p>
            <p>产物 SHA-256：<span className="break-all font-mono text-xs">{installing.artifactSha256 ?? "缺少（禁止安装）"}</span></p>
            <p>类型：{installing.entryType ?? "未声明"} · 最低主控版本 {installing.minControllerVersion ?? "未声明"}</p>
            {!installing.sourceCommit || !installing.artifactSha256 ? (
              <Notice tone="error" title="不可安装">目录条目缺少不可变 commit 或 SHA-256；不能声称验证通过。</Notice>
            ) : null}
            <fieldset className="rounded-lg border px-4 py-3">
              <legend className="px-1 font-medium">权限确认</legend>
              {installing.permissions.length === 0 ? <p className="text-xs text-muted-foreground">该条目未声明权限；请核对来源。</p> : null}
              {installing.permissions.map(permission => (
                <label key={permission} className="mt-1 flex items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    className="size-4 accent-[var(--primary)]"
                    checked={accepted.has(permission)}
                    onChange={event => {
                      setAccepted(current => {
                        const next = new Set(current);
                        if (event.target.checked) next.add(permission);
                        else next.delete(permission);
                        return next;
                      });
                    }}
                  />
                  {permission}
                </label>
              ))}
            </fieldset>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="size-4 accent-[var(--primary)]" checked={enableAfter} onChange={event => setEnableAfter(event.target.checked)} />
              安装后启用
            </label>
          </form>
        ) : null}
      </Modal>

      <Modal
        open={installedAction !== null}
        title={installedAction ? `更新 ${installedAction.id}` : "更新插件"}
        description="热更新会校验新版、排空任务并在失败时保持旧版；不能承诺零停顿。"
        onClose={() => setInstalledAction(null)}
        footer={
          <>
            <Button type="button" onClick={() => setInstalledAction(null)}>取消</Button>
            <Button type="submit" form="plugin-update" variant="primary" loading={update.pending}>确认更新</Button>
          </>
        }
      >
        <form id="plugin-update" onSubmit={submitUpdate} className="space-y-3 text-sm">
          {update.error ? <Notice tone="error" title="更新失败">{describeApiError(update.error)}</Notice> : null}
          <p>当前版本：{installedAction?.version}</p>
          <Field label="目标版本">
            {ids => (
              <Select id={ids.inputId} value={updateVersion} onChange={event => setUpdateVersion(event.target.value)} required>
                <option value="" disabled>请选择目录版本</option>
                {catalogItems.filter(entry => entry.id === installedAction?.id && entry.version !== installedAction?.version).map(entry => (
                  <option key={entry.version} value={entry.version}>{entry.version}</option>
                ))}
              </Select>
            )}
          </Field>
          <p className="text-xs text-muted-foreground">提交时使用目录中的不可变 sourceCommit 与 SHA-256；缺少时服务端应拒绝。</p>
        </form>
      </Modal>

      <Modal
        open={uninstalling !== null}
        title="卸载插件"
        description={uninstalling ? `将卸载 ${uninstalling.id}；不会自动卸载节点上的 Xray-core。` : undefined}
        onClose={() => setUninstalling(null)}
        footer={
          <>
            <Button type="button" onClick={() => setUninstalling(null)} disabled={uninstall.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={remove} loading={uninstall.pending}>确认卸载</Button>
          </>
        }
      >
        {uninstall.error ? <Notice tone="error" title="卸载失败">{describeApiError(uninstall.error)}</Notice> : null}
        <p className="text-sm text-muted-foreground">契约没有“保留/清除插件数据”选择字段；本页不承诺数据保留策略。卸载完成后业务菜单应消失。</p>
      </Modal>

      <Modal
        open={operation !== null}
        title={operation ? `插件操作 ${operation.id}` : "插件操作"}
        onClose={() => setOperation(null)}
        footer={
          <>
            <Button
              type="button"
              loading={refreshOperation.pending}
              onClick={async () => {
                if (!operation) return;
                const result = await refreshOperation.run(operation.id);
                if (result.ok && result.data) setOperation(result.data.data);
              }}
            >
              刷新操作
            </Button>
            <Button type="button" onClick={() => setOperation(null)}>关闭</Button>
          </>
        }
      >
        {operation ? (
          <div className="space-y-2 text-sm">
            <p>动作：{operation.action} · 状态 {OPERATION_STATUS[operation.status]}</p>
            <p>版本：{operation.previousVersion ?? "—"} → {operation.targetVersion ?? "—"}</p>
            {operation.error ? <Notice tone="error" title="操作错误">{operation.error.code} · {operation.error.message}</Notice> : null}
            <p className="text-xs text-muted-foreground">操作异步执行；只有服务端状态为 succeeded 才能视为完成，失败时旧版应保持可用。</p>
          </div>
        ) : null}
      </Modal>
    </div>
  );
}
