import { useState, type FormEvent } from "react";
import { useSession } from "@/app/session";
import { AsyncPanel } from "@/components/ui/async-panel";
import { Button } from "@/components/ui/button";
import { EmptyState, Notice } from "@/components/ui/feedback";
import { Field, Select, TextArea, TextInput } from "@/components/ui/form";
import { Modal } from "@/components/ui/modal";
import { PageHeader, Panel } from "@/components/ui/panel";
import { CodeBlock, DataTable, TabBar } from "@/components/ui/table";
import { describeApiError } from "@/lib/api/errors";
import { pluginsApi } from "@/lib/api/endpoints-ai";
import { xrayApi, type XrayDeployPreview, type XrayJobRef, type XrayManagedUser, type XrayPreview } from "@/lib/api/endpoints-xray";
import { useAction, useResource } from "@/lib/data/use-resource";
import { formatBytes, formatDateTime } from "@/lib/format";

type Tab = "nodes" | "install" | "config" | "users" | "traffic" | "history";

/** 声明式插件页面：标签、标题和说明由核心 manifest 提供，渲染器不执行任何插件脚本。 */
export const XRAY_MANIFEST: readonly { readonly id: Tab; readonly label: string; readonly description: string }[] = [
  { id: "nodes", label: "节点与实例", description: "查看节点探测、实例状态与启停/更新/卸载动作。" },
  { id: "install", label: "安装", description: "预检后创建安装任务；接管已有实例需明确确认。" },
  { id: "config", label: "配置", description: "模板/自定义 JSON、预检、差异和分批部署。" },
  { id: "users", label: "用户", description: "持久用户身份、启停、受控客户端导出。" },
  { id: "traffic", label: "流量", description: "代理统计与采样设置；不等同云账单。" },
  { id: "history", label: "历史", description: "配置版本与回滚。" },
];

function JobModal({ job, onClose }: { readonly job: XrayJobRef | null; readonly onClose: () => void }) {
  const [latest, setLatest] = useState<XrayJobRef | null>(null);
  const refresh = useAction((jobId: string) => xrayApi.job(jobId));
  const current = latest ?? job;
  return (
    <Modal
      open={job !== null}
      title={current ? `Xray 作业 ${current.jobId}` : "Xray 作业"}
      onClose={() => {
        setLatest(null);
        onClose();
      }}
      footer={
        <>
          <Button
            type="button"
            loading={refresh.pending}
            onClick={async () => {
              if (!current) return;
              const result = await refresh.run(current.jobId);
              if (result.ok && result.data) setLatest(result.data.data);
            }}
          >
            刷新作业
          </Button>
          <Button type="button" onClick={onClose}>关闭</Button>
        </>
      }
    >
      {current ? (
        <div className="space-y-2 text-sm">
          <p>状态：{current.status} · accepted {current.accepted ? "是" : "否"}</p>
          <p>目标节点：{current.targetNodeIdsSnapshot?.join("、") || "未返回"}</p>
          {current.taskId ? <p><a className="text-primary underline" href={`#/tasks/${encodeURIComponent(current.taskId)}`}>查看统一任务 {current.taskId}</a></p> : null}
          {current.correlationId ? <p className="font-mono text-xs">correlation {current.correlationId}</p> : null}
          <p className="text-xs text-muted-foreground">accepted/queued 不代表执行成功；逐节点结果以任务与实例状态为准。</p>
        </div>
      ) : null}
    </Modal>
  );
}

export function XrayPage() {
  const session = useSession();
  const plugin = useResource(`xray:plugin:${session.viewEpoch}`, signal => pluginsApi.installedOne("xray-manager", signal).then(result => result.data));
  const enabled = plugin.state.status === "ready" && plugin.state.data.status === "enabled" && plugin.state.data.enabled;

  if (plugin.state.status === "loading") {
    return <Panel title="Xray 节点管理器"><div role="status" className="px-5 py-8 text-sm text-muted-foreground">正在确认插件安装状态…</div></Panel>;
  }
  if (plugin.state.status === "error" || !enabled) {
    return (
      <div className="space-y-4">
        <PageHeader title="Xray 节点管理器" description="可选插件；未安装或停用时业务界面不可执行。" />
        <Notice tone="warning" title="插件未启用">
          {plugin.state.status === "error" ? describeApiError(plugin.state.error) : `当前状态：${plugin.state.status === "ready" ? plugin.state.data.status : "未知"}。`}
          <p className="mt-2"><a className="text-primary underline" href="#/plugins">前往插件中心安装并启用</a></p>
        </Notice>
      </div>
    );
  }
  return <XrayConsole />;
}

function XrayConsole() {
  const session = useSession();
  const [tab, setTab] = useState<Tab>("nodes");
  const [job, setJob] = useState<XrayJobRef | null>(null);
  const [outcome, setOutcome] = useState<string | null>(null);
  const nodes = useResource(`xray:nodes:${session.viewEpoch}`, signal => xrayApi.nodes(signal).then(result => result.data));
  const templates = useResource(`xray:templates:${session.viewEpoch}`, signal => xrayApi.templates(signal).then(result => result.data));
  const users = useResource(`xray:users:${session.viewEpoch}`, signal => xrayApi.users(signal).then(result => result.data));
  const traffic = useResource(`xray:traffic:${session.viewEpoch}`, signal => xrayApi.traffic(signal).then(result => result.data));
  const settings = useResource(`xray:traffic-settings:${session.viewEpoch}`, signal => xrayApi.trafficSettings(signal).then(result => result.data));

  const nodeItems = nodes.state.status === "ready" ? nodes.state.data.items : [];
  const templateItems = templates.state.status === "ready" ? templates.state.data.items : [];
  const userItems = users.state.status === "ready" ? users.state.data.items : [];
  const trafficItems = traffic.state.status === "ready" ? traffic.state.data.items : [];
  const canManage = session.can("plugin.manage") || session.can("xray.manage") || session.can("settings.manage");
  const writesLocked = session.writesLocked;

  // Install state
  const [installNodes, setInstallNodes] = useState<ReadonlySet<string>>(new Set());
  const [targetVersion, setTargetVersion] = useState("");
  const [installPort, setInstallPort] = useState("");
  const [manageExisting, setManageExisting] = useState(false);
  const [preview, setPreview] = useState<XrayPreview | null>(null);
  const installPreview = useAction((body: Parameters<typeof xrayApi.previewInstall>[0]) => xrayApi.previewInstall(body));
  const install = useAction((body: Parameters<typeof xrayApi.install>[0]) => xrayApi.install(body));

  // Instance state
  const [instanceNode, setInstanceNode] = useState<string | null>(null);
  const instance = useResource(`xray:instance:${session.viewEpoch}:${instanceNode ?? "none"}`, signal =>
    xrayApi.instance(instanceNode ?? "", signal).then(result => result.data),
    { enabled: Boolean(instanceNode) },
  );
  const instanceAction = useAction((nodeId: string, body: Parameters<typeof xrayApi.instanceAction>[1]) => xrayApi.instanceAction(nodeId, body));

  // Config state
  const [configNodes, setConfigNodes] = useState<ReadonlySet<string>>(new Set());
  const [templateId, setTemplateId] = useState<"vless-tls-vision" | "vless-reality-vision" | "custom-json">("vless-tls-vision");
  const [configPort, setConfigPort] = useState("443");
  const [configVersion, setConfigVersion] = useState("");
  const [configDomain, setConfigDomain] = useState("");
  const [customJson, setCustomJson] = useState("{}");
  const [validation, setValidation] = useState<Awaited<ReturnType<typeof xrayApi.validate>>["data"] | null>(null);
  const [deployPreview, setDeployPreview] = useState<XrayDeployPreview | null>(null);
  const validate = useAction((body: Parameters<typeof xrayApi.validate>[0]) => xrayApi.validate(body));
  const previewDeploy = useAction((body: Parameters<typeof xrayApi.deployPreview>[0]) => xrayApi.deployPreview(body));
  const deploy = useAction((body: Parameters<typeof xrayApi.deploy>[0]) => xrayApi.deploy(body));

  // Users state
  const [userNode, setUserNode] = useState("");
  const [inboundTag, setInboundTag] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [exportUser, setExportUser] = useState<XrayManagedUser | null>(null);
  const [exportFormat, setExportFormat] = useState<"vless-uri" | "qrcode" | "json">("vless-uri");
  const [exportValue, setExportValue] = useState<string | null>(null);
  const [deleteUser, setDeleteUser] = useState<XrayManagedUser | null>(null);
  const createUser = useAction((body: Parameters<typeof xrayApi.createUser>[0]) => xrayApi.createUser(body));
  const patchUser = useAction((userId: string, body: Parameters<typeof xrayApi.patchUser>[1]) => xrayApi.patchUser(userId, body));
  const removeUser = useAction((userId: string) => xrayApi.deleteUser(userId));
  const exportClient = useAction((userId: string, body: { format: "vless-uri" | "qrcode" | "json" }) => xrayApi.exportClient(userId, body));

  // History state
  const [historyNode, setHistoryNode] = useState("");
  const [rollbackRevision, setRollbackRevision] = useState("");
  const [currentRevision, setCurrentRevision] = useState("");
  const [rollbackOpen, setRollbackOpen] = useState(false);
  const revisions = useResource(`xray:revisions:${session.viewEpoch}:${historyNode}`, signal =>
    historyNode ? xrayApi.revisions(historyNode, signal).then(result => result.data) : Promise.resolve({ items: [] }),
    { enabled: Boolean(historyNode) },
  );
  const rollback = useAction((nodeId: string, body: { revision: number; expectedCurrentRevision: number }) => xrayApi.rollback(nodeId, body));

  // Traffic settings state
  const [sampling, setSampling] = useState("60");
  const [thresholds, setThresholds] = useState("");
  const saveSettings = useAction((body: Parameters<typeof xrayApi.putTrafficSettings>[0]) => xrayApi.putTrafficSettings(body));

  function toggle(set: ReadonlySet<string>, setter: (next: ReadonlySet<string>) => void, id: string, checked: boolean) {
    const next = new Set(set);
    if (checked) next.add(id);
    else next.delete(id);
    setter(next);
  }

  function batch() {
    return { mode: "batch" as const, concurrency: 3, maxFailures: 1, stopOnHealthFailure: true };
  }

  function documentBody() {
    if (templateId === "custom-json") {
      return { templateId, customJson: JSON.parse(customJson || "{}") };
    }
    return {
      templateId,
      params: {
        port: Number(configPort),
        ...(configDomain ? { domain: configDomain } : {}),
        listen: "0.0.0.0",
      },
    };
  }

  async function runInstallPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (installNodes.size === 0 || !targetVersion.trim()) return setOutcome("请选择节点并填写固定 Xray 版本。");
    setPreview(null);
    const result = await installPreview.run({
      targets: { nodeIds: [...installNodes] },
      targetVersion: targetVersion.trim(),
      ...(installPort ? { port: Number(installPort) } : {}),
      manageExisting,
      batchPolicy: batch(),
    });
    if (result.ok && result.data) setPreview(result.data.data);
  }

  async function runInstall() {
    if (!preview) return;
    const result = await install.run({ previewId: preview.previewId, planHash: preview.planHash, confirmAdoption: manageExisting });
    if (result.ok && result.data) {
      setJob(result.data.data);
      setPreview(null);
      setOutcome("安装作业已提交；不表示已安装成功。");
      nodes.reload();
    }
  }

  async function runValidate() {
    if (configNodes.size === 0 || !configVersion.trim()) return setOutcome("配置预检需要目标节点与目标 Xray 版本。");
    setValidation(null);
    try {
      const document = documentBody();
      const result = await validate.run({ targetNodeIds: [...configNodes], document, targetXrayVersion: configVersion.trim() });
      if (result.ok && result.data) setValidation(result.data.data);
    } catch {
      setOutcome("自定义 JSON 无法解析；请修正后再预检。");
    }
  }

  async function runDeployPreview() {
    if (configNodes.size === 0) return setOutcome("请选择部署目标节点。");
    setDeployPreview(null);
    try {
      const result = await previewDeploy.run({ targets: { nodeIds: [...configNodes] }, document: documentBody(), batchPolicy: batch() });
      if (result.ok && result.data) setDeployPreview(result.data.data);
    } catch {
      setOutcome("自定义 JSON 无法解析；请修正后再预览。");
    }
  }

  async function runDeploy() {
    if (!deployPreview) return;
    const result = await deploy.run({ previewId: deployPreview.previewId, planHash: deployPreview.planHash });
    if (result.ok && result.data) {
      setJob(result.data.data);
      setDeployPreview(null);
      setOutcome("配置部署作业已提交；逐节点结果以任务为准。");
    }
  }

  async function submitCreateUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!userNode || !inboundTag.trim() || !displayName.trim()) return setOutcome("创建用户需要节点、inbound 标签和显示名。");
    const result = await createUser.run({ nodeId: userNode, inboundTag: inboundTag.trim(), displayName: displayName.trim(), generateCredential: true });
    if (result.ok) {
      setOutcome("Xray 用户已创建；敏感身份仅保存为服务端凭据引用。");
      setDisplayName("");
      users.reload();
    }
  }

  async function runExport() {
    if (!exportUser) return;
    const result = await exportClient.run(exportUser.userId, { format: exportFormat });
    if (result.ok && result.data) setExportValue(result.data.data.oneTimeValue);
  }

  async function runRollback() {
    if (!historyNode) return;
    const revision = Number(rollbackRevision);
    const expected = Number(currentRevision);
    if (!Number.isInteger(revision) || !Number.isInteger(expected)) return setOutcome("回滚需要整数版本。");
    const result = await rollback.run(historyNode, { revision, expectedCurrentRevision: expected });
    if (result.ok && result.data) {
      setJob(result.data.data);
      setRollbackOpen(false);
      setOutcome("回滚作业已提交；不保证任意命令回滚。");
    }
  }

  async function saveTrafficSettings(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const result = await saveSettings.run({
      samplingIntervalSeconds: Number(sampling),
      thresholdsBytes: thresholds.split(",").map(value => Number(value.trim())).filter(value => Number.isFinite(value)),
    });
    if (result.ok) setOutcome("Xray 流量采样设置已保存（服务端结果）。");
  }

  return (
    <div className="space-y-5">
      <PageHeader
        title="Xray 节点管理器"
        description="第一版唯一官方可选插件。声明式界面由核心 manifest 渲染，不执行插件脚本；未启用时不可操作。"
        badge={<span className="rounded-md border px-2 py-1 text-xs">声明式插件 UI</span>}
        actions={<Button type="button" onClick={() => { nodes.reload(); users.reload(); traffic.reload(); }}>刷新</Button>}
      />
      {outcome ? <Notice tone="success" title="操作结果">{outcome}</Notice> : null}
      <TabBar label="Xray 插件页面" active={tab} onChange={setTab} tabs={XRAY_MANIFEST.map(item => ({ id: item.id, label: item.label }))} />
      <p className="text-xs text-muted-foreground">{XRAY_MANIFEST.find(item => item.id === tab)?.description}</p>

      {tab === "nodes" ? (
        <Panel title="Xray 节点" description="GET /api/v1/plugins/xray-manager/nodes；managed 与探测状态来自服务端。">
          <AsyncPanel state={nodes.state} onRetry={nodes.reload} errorTitle="Xray 节点加载失败" empty={<EmptyState title="没有可见节点" />} isEmpty={data => data.items.length === 0}>
            {() => (
              <DataTable
                caption="Xray 节点"
                rows={nodeItems}
                rowKey={node => node.nodeId}
                columns={[
                  { key: "name", header: "节点", render: node => <span>{node.name}<span className="block font-mono text-xs text-muted-foreground">{node.nodeId}</span></span> },
                  { key: "online", header: "在线", render: node => (node.online ? "是" : "否") },
                  { key: "managed", header: "已托管", render: node => (node.managed ? "是" : "否") },
                  { key: "detected", header: "检测到 Xray", render: node => (node.xrayDetected ? "是" : "否") },
                  { key: "arch", header: "架构", render: node => node.arch ?? "—" },
                  { key: "actions", header: "操作", render: node => <Button size="sm" onClick={() => setInstanceNode(node.nodeId)}>实例</Button> },
                ]}
              />
            )}
          </AsyncPanel>
        </Panel>
      ) : null}

      {tab === "install" ? (
        <Panel title="安装预检与任务" description="POST installations/preview → installations；校验版本、端口、磁盘与架构。">
          <form onSubmit={runInstallPreview} className="space-y-3 px-5 py-4">
            {installPreview.error ? <Notice tone="error" title="预检失败">{describeApiError(installPreview.error)}</Notice> : null}
            <fieldset className="max-h-40 overflow-y-auto rounded-lg border px-4 py-3">
              <legend className="px-1 text-sm font-medium">目标节点</legend>
              {nodeItems.map(node => (
                <label key={node.nodeId} className="mt-1 flex items-center gap-2 text-sm">
                  <input type="checkbox" className="size-4" checked={installNodes.has(node.nodeId)} onChange={event => toggle(installNodes, setInstallNodes, node.nodeId, event.target.checked)} />
                  {node.name} {node.online ? "" : "（离线）"} {node.xrayDetected ? "（已有 Xray）" : ""}
                </label>
              ))}
            </fieldset>
            <div className="grid gap-3 sm:grid-cols-3">
              <Field label="固定目标版本">{ids => <TextInput id={ids.inputId} value={targetVersion} onChange={event => setTargetVersion(event.target.value)} placeholder="例如 25.3.6" />}</Field>
              <Field label="端口（可选）">{ids => <TextInput id={ids.inputId} type="number" min={1} max={65535} value={installPort} onChange={event => setInstallPort(event.target.value)} />}</Field>
              <label className="mt-6 flex items-center gap-2 text-sm">
                <input type="checkbox" className="size-4" checked={manageExisting} onChange={event => setManageExisting(event.target.checked)} />
                接管已有实例（需确认）
              </label>
            </div>
            <Button type="submit" variant="primary" loading={installPreview.pending} disabled={!canManage || writesLocked}>预检安装</Button>
          </form>
          {preview ? (
            <div className="border-t px-5 py-4 text-sm">
              <p>可安装：{preview.eligible ? "是" : "否"} · planHash <span className="break-all font-mono text-xs">{preview.planHash}</span></p>
              <ul className="mt-2 space-y-1">
                {preview.checks.map(check => (
                  <li key={check.nodeId} className="text-xs">
                    {check.nodeId}：{check.eligible ? "通过" : "不可用"}{check.existingInstallation ? " · 已有安装" : ""}{check.hasPortConflict ? " · 端口冲突" : ""}
                    {check.issues?.length ? ` · ${check.issues.map(issue => issue.message ?? issue.code ?? "问题").join("；")}` : ""}
                  </li>
                ))}
              </ul>
              {preview.requiresAdoptionApproval && !manageExisting ? <Notice tone="warning" title="需要接管确认">请勾选接管已有实例后再安装。</Notice> : null}
              {install.error ? <Notice tone="error" title="安装失败">{describeApiError(install.error)}</Notice> : null}
              <Button type="button" className="mt-3" variant="primary" onClick={runInstall} loading={install.pending} disabled={!preview.eligible || !canManage || writesLocked}>创建安装任务</Button>
            </div>
          ) : null}
        </Panel>
      ) : null}

      {tab === "config" ? (
        <div className="space-y-5">
          <Panel title="配置文档" description="模板或自定义 JSON；密钥只允许引用，不在界面回显私钥。">
            <div className="space-y-3 px-5 py-4">
              {validate.error ? <Notice tone="error" title="预检失败">{describeApiError(validate.error)}</Notice> : null}
              {previewDeploy.error ? <Notice tone="error" title="部署预览失败">{describeApiError(previewDeploy.error)}</Notice> : null}
              {deploy.error ? <Notice tone="error" title="部署失败">{describeApiError(deploy.error)}</Notice> : null}
              <fieldset className="max-h-32 overflow-y-auto rounded-lg border px-4 py-3">
                <legend className="px-1 text-sm font-medium">目标节点</legend>
                {nodeItems.map(node => (
                  <label key={node.nodeId} className="mt-1 flex items-center gap-2 text-sm">
                    <input type="checkbox" className="size-4" checked={configNodes.has(node.nodeId)} onChange={event => toggle(configNodes, setConfigNodes, node.nodeId, event.target.checked)} />
                    {node.name}
                  </label>
                ))}
              </fieldset>
              <div className="grid gap-3 sm:grid-cols-3">
                <Field label="模板">
                  {ids => (
                    <Select id={ids.inputId} value={templateId} onChange={event => setTemplateId(event.target.value as typeof templateId)}>
                      {templateItems.map(template => <option key={template.templateId} value={template.templateId}>{template.name}</option>)}
                      {templateItems.length === 0 ? <option value="vless-tls-vision">VLESS + TLS + Vision</option> : null}
                    </Select>
                  )}
                </Field>
                <Field label="端口">{ids => <TextInput id={ids.inputId} type="number" value={configPort} onChange={event => setConfigPort(event.target.value)} />}</Field>
                <Field label="目标 Xray 版本">{ids => <TextInput id={ids.inputId} value={configVersion} onChange={event => setConfigVersion(event.target.value)} />}</Field>
              </div>
              <Field label="域名（TLS 模板可选）">{ids => <TextInput id={ids.inputId} value={configDomain} onChange={event => setConfigDomain(event.target.value)} />}</Field>
              {templateId === "custom-json" ? (
                <Field label="自定义 JSON" hint="服务端会脱敏存储与安全预检；不得暴露公网管理 API。">
                  {ids => <TextArea id={ids.inputId} value={customJson} onChange={event => setCustomJson(event.target.value)} className="font-mono" />}
                </Field>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button type="button" onClick={runValidate} loading={validate.pending} disabled={!canManage || writesLocked}>预检配置</Button>
                <Button type="button" onClick={runDeployPreview} loading={previewDeploy.pending} disabled={!canManage || writesLocked}>预览部署</Button>
                {deployPreview ? <Button type="button" variant="primary" onClick={runDeploy} loading={deploy.pending} disabled={!deployPreview.valid || !canManage || writesLocked}>创建部署任务</Button> : null}
              </div>
              {validation ? (
                <div className="rounded-lg border px-4 py-3 text-sm">
                  <p>配置有效：{validation.valid ? "是" : "否"} · 支持：{validation.supported === undefined ? "未返回" : validation.supported ? "是" : "否"}</p>
                  {validation.notes?.length ? <ul className="mt-1 list-disc pl-5 text-xs">{validation.notes.map(note => <li key={note}>{note}</li>)}</ul> : null}
                  {validation.sanitizedDiff ? <CodeBlock label="脱敏差异">{validation.sanitizedDiff}</CodeBlock> : null}
                </div>
              ) : null}
              {deployPreview ? (
                <div className="rounded-lg border px-4 py-3 text-sm">
                  <p>部署预览：{deployPreview.valid ? "有效" : "无效"} · planHash <span className="break-all font-mono text-xs">{deployPreview.planHash}</span></p>
                  {deployPreview.sanitizedDiff ? <CodeBlock label="预部署差异">{deployPreview.sanitizedDiff}</CodeBlock> : null}
                  <ul className="mt-2 space-y-1 text-xs">
                    {deployPreview.checks?.map(check => <li key={check.nodeId}>{check.nodeId}：{check.eligible ? "可部署" : "不可部署"}{check.issues?.length ? ` · ${check.issues.map(issue => issue.message ?? "问题").join("；")}` : ""}</li>)}
                  </ul>
                </div>
              ) : null}
            </div>
          </Panel>
        </div>
      ) : null}

      {tab === "users" ? (
        <div className="space-y-5">
          <Panel title="用户" description="GET /api/v1/plugins/xray-manager/users；UUID 只以凭据引用保存。">
            <AsyncPanel state={users.state} onRetry={users.reload} errorTitle="Xray 用户加载失败" empty={<EmptyState title="暂无 Xray 用户" />} isEmpty={data => data.items.length === 0}>
              {() => (
                <DataTable
                  caption="Xray 托管用户"
                  rows={userItems}
                  rowKey={user => user.userId}
                  columns={[
                    { key: "name", header: "用户", render: user => <span>{user.displayName}<span className="block font-mono text-xs text-muted-foreground">{user.userId}</span></span> },
                    { key: "node", header: "节点", render: user => `${user.nodeId} · ${user.inboundTag}` },
                    { key: "stats", header: "统计标识", render: user => <span className="font-mono text-xs">{user.statsIdentifier}</span> },
                    { key: "enabled", header: "启用", render: user => (user.enabled ? "是" : "否") },
                    { key: "traffic", header: "累计", render: user => `${formatBytes((user.totalUplinkBytes ?? 0) + (user.totalDownlinkBytes ?? 0)) ?? "0 B"}` },
                    {
                      key: "actions",
                      header: "操作",
                      render: user => (
                        <div className="flex flex-wrap gap-1">
                          <Button size="sm" onClick={() => { setExportUser(user); setExportValue(null); }}>导出</Button>
                          <Button size="sm" onClick={() => void patchUser.run(user.userId, { enabled: !user.enabled }).then(result => { if (result.ok) users.reload(); })} disabled={!canManage || writesLocked}>{user.enabled ? "禁用" : "启用"}</Button>
                          <Button size="sm" onClick={() => void patchUser.run(user.userId, { rotateCredential: true }).then(result => { if (result.ok) { setOutcome("凭据轮换已提交（服务端结果）。"); users.reload(); } })} disabled={!canManage || writesLocked}>轮换</Button>
                          <Button size="sm" variant="danger" onClick={() => setDeleteUser(user)} disabled={!canManage || writesLocked}>删除</Button>
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </AsyncPanel>
          </Panel>
          <Panel title="创建用户" description="POST /api/v1/plugins/xray-manager/users；生成凭据时只返回引用，不回显 UUID。">
            <form onSubmit={submitCreateUser} className="grid gap-3 px-5 py-4 sm:grid-cols-3">
              <Field label="节点">{ids => (
                <Select id={ids.inputId} value={userNode} onChange={event => setUserNode(event.target.value)}>
                  <option value="">请选择</option>
                  {nodeItems.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name}</option>)}
                </Select>
              )}</Field>
              <Field label="Inbound 标签">{ids => <TextInput id={ids.inputId} value={inboundTag} onChange={event => setInboundTag(event.target.value)} />}</Field>
              <Field label="显示名称">{ids => <TextInput id={ids.inputId} value={displayName} onChange={event => setDisplayName(event.target.value)} />}</Field>
              <div className="sm:col-span-3">
                {createUser.error ? <Notice tone="error" title="创建失败">{describeApiError(createUser.error)}</Notice> : null}
                <Button type="submit" variant="primary" loading={createUser.pending} disabled={!canManage || writesLocked}>创建用户</Button>
              </div>
            </form>
          </Panel>
        </div>
      ) : null}

      {tab === "traffic" ? (
        <div className="space-y-5">
          <Notice tone="info" title="统计口径">Xray 代理流量统计不等同服务器网卡或云账单；不完整的采样必须显示缺口。</Notice>
          <Panel title="流量样本" description="GET /api/v1/plugins/xray-manager/traffic；sourceType 应为 proxy_only。">
            <AsyncPanel state={traffic.state} onRetry={traffic.reload} errorTitle="流量加载失败" empty={<EmptyState title="暂无代理流量样本" />} isEmpty={data => data.items.length === 0}>
              {data => (
                <>
                  <p className="border-b px-5 py-2 text-xs text-muted-foreground">{data.notice ?? "服务端未提供口径说明。"} · 来源 {data.sourceType}</p>
                  <DataTable
                    caption="Xray 流量样本"
                    rows={trafficItems}
                    rowKey={(sample, index) => `${sample.nodeId}-${sample.scope}-${sample.userId ?? sample.inboundTag ?? ""}-${sample.sampledAt}-${index}`}
                    columns={[
                      { key: "time", header: "采样时间", render: sample => formatDateTime(sample.sampledAt) ?? "—" },
                      { key: "node", header: "节点", render: sample => sample.nodeId },
                      { key: "scope", header: "范围", render: sample => sample.scope },
                      { key: "id", header: "对象", render: sample => sample.userId ?? sample.inboundTag ?? "—" },
                      { key: "up", header: "上行", align: "right", render: sample => formatBytes(sample.uplinkBytes) ?? "0 B" },
                      { key: "down", header: "下行", align: "right", render: sample => formatBytes(sample.downlinkBytes) ?? "0 B" },
                      { key: "complete", header: "完整", render: sample => (sample.complete ? "是" : `否${sample.gapReason ? `（${sample.gapReason}）` : ""}`) },
                    ]}
                  />
                </>
              )}
            </AsyncPanel>
          </Panel>
          <Panel title="采样设置" description="PUT /api/v1/plugins/xray-manager/traffic/settings">
            <form onSubmit={saveTrafficSettings} className="space-y-3 px-5 py-4">
              {saveSettings.error ? <Notice tone="error" title="保存失败">{describeApiError(saveSettings.error)}</Notice> : null}
              <p className="text-xs text-muted-foreground">当前服务端设置：{settings.state.status === "ready" ? `采样间隔 ${settings.state.data.samplingIntervalSeconds}s` : "未加载"}</p>
              <div className="grid gap-3 sm:grid-cols-2">
                <Field label="采样间隔（秒，30–300）">{ids => <TextInput id={ids.inputId} type="number" min={30} max={300} value={sampling} onChange={event => setSampling(event.target.value)} />}</Field>
                <Field label="阈值字节（逗号分隔）">{ids => <TextInput id={ids.inputId} value={thresholds} onChange={event => setThresholds(event.target.value)} />}</Field>
              </div>
              <Button type="submit" variant="primary" loading={saveSettings.pending} disabled={!canManage || writesLocked}>保存采样设置</Button>
            </form>
          </Panel>
        </div>
      ) : null}

      {tab === "history" ? (
        <Panel title="配置历史与回滚" description="GET nodes/{node}/configurations；回滚是异步任务。">
          <div className="space-y-3 px-5 py-4">
            <Field label="节点">
              {ids => (
                <Select id={ids.inputId} value={historyNode} onChange={event => setHistoryNode(event.target.value)}>
                  <option value="">请选择</option>
                  {nodeItems.map(node => <option key={node.nodeId} value={node.nodeId}>{node.name}</option>)}
                </Select>
              )}
            </Field>
            {historyNode ? (
              <AsyncPanel state={revisions.state} onRetry={revisions.reload} errorTitle="历史加载失败" empty={<EmptyState title="暂无配置历史" />} isEmpty={data => data.items.length === 0}>
                {data => (
                  <DataTable
                    caption="Xray 配置版本"
                    rows={data.items}
                    rowKey={revision => String(revision.revision)}
                    columns={[
                      { key: "rev", header: "版本", render: revision => revision.revision },
                      { key: "status", header: "状态", render: revision => revision.status },
                      { key: "template", header: "模板", render: revision => revision.templateId ?? "—" },
                      { key: "hash", header: "配置哈希", render: revision => <span className="font-mono text-xs">{revision.configHash}</span> },
                      { key: "time", header: "创建", render: revision => formatDateTime(revision.createdAt) ?? "—" },
                      { key: "by", header: "创建者", render: revision => revision.createdBy ?? "—" },
                      { key: "actions", header: "操作", render: revision => (
                        <Button size="sm" onClick={() => { setRollbackRevision(String(revision.revision)); setCurrentRevision(String(revision.revision + 1)); setRollbackOpen(true); }} disabled={!canManage || writesLocked}>回滚</Button>
                      ) },
                    ]}
                  />
                )}
              </AsyncPanel>
            ) : <p className="text-sm text-muted-foreground">选择节点后读取版本历史。</p>}
          </div>
        </Panel>
      ) : null}

      <Modal
        open={instanceNode !== null}
        title={instanceNode ? `Xray 实例 · ${instanceNode}` : "Xray 实例"}
        onClose={() => setInstanceNode(null)}
        footer={<Button type="button" onClick={() => setInstanceNode(null)}>关闭</Button>}
      >
        <AsyncPanel state={instance.state} onRetry={instance.reload} errorTitle="实例加载失败">
          {data => (
            <div className="space-y-3 px-5 py-4 text-sm">
              <p>托管：{data.managed ? "是" : "否"} · 接管状态 {data.adoptionState ?? "—"}</p>
              <p>服务：{data.serviceStatus} · 版本 {data.version ?? "—"} · 架构 {data.architecture ?? "—"}</p>
              <p>配置 revision {data.configRevision ?? "—"} · inbound {data.inboundCount ?? "—"}</p>
              {data.lastError ? <Notice tone="error" title="实例错误">{data.lastError}</Notice> : null}
              <div className="flex flex-wrap gap-2">
                {(["start", "stop", "restart"] as const).map(action => (
                  <Button key={action} size="sm" disabled={!canManage || writesLocked} onClick={async () => { if (!instanceNode) return; const result = await instanceAction.run(instanceNode, { action }); if (result.ok && result.data) setJob(result.data.data); }}>
                    {action}
                  </Button>
                ))}
                <Button size="sm" variant="danger" disabled={!canManage || writesLocked} onClick={async () => { if (!instanceNode) return; const result = await instanceAction.run(instanceNode, { action: "uninstall", confirmRemoveNodeXray: true, keepConfig: true }); if (result.ok && result.data) setJob(result.data.data); }}>
                  卸载节点 Xray（保留配置）
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">动作需服务端审批/任务校验；卸载主控插件与卸载节点 Xray 是不同操作。</p>
            </div>
          )}
        </AsyncPanel>
      </Modal>

      <Modal
        open={exportUser !== null}
        title={exportUser ? `受控导出 · ${exportUser.displayName}` : "受控导出"}
        onClose={() => { setExportUser(null); setExportValue(null); }}
        footer={
          <>
            <Button type="button" onClick={() => { setExportUser(null); setExportValue(null); }}>关闭</Button>
            <Button type="button" variant="primary" onClick={runExport} loading={exportClient.pending}>生成一次性导出</Button>
          </>
        }
      >
        <div className="space-y-3">
          <Field label="格式">
            {ids => (
              <Select id={ids.inputId} value={exportFormat} onChange={event => setExportFormat(event.target.value as typeof exportFormat)}>
                <option value="vless-uri">VLESS URI</option>
                <option value="json">JSON</option>
                <option value="qrcode">二维码数据</option>
              </Select>
            )}
          </Field>
          {exportClient.error ? <Notice tone="error" title="导出失败">{describeApiError(exportClient.error)}</Notice> : null}
          {exportValue ? (
            <Notice tone="warning" title="一次性连接数据">
              此值含敏感连接信息，仅显示一次，不写入日志；关闭后从页面移除。
              <CodeBlock>{exportValue}</CodeBlock>
            </Notice>
          ) : <p className="text-xs text-muted-foreground">导出需要服务端权限与审批；不会返回私钥。</p>}
        </div>
      </Modal>

      <Modal
        open={deleteUser !== null}
        title="删除 Xray 用户"
        description={deleteUser ? `删除 ${deleteUser.displayName}；身份将从后续配置同步移除。` : undefined}
        onClose={() => setDeleteUser(null)}
        footer={
          <>
            <Button type="button" onClick={() => setDeleteUser(null)} disabled={removeUser.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={async () => { if (!deleteUser) return; const result = await removeUser.run(deleteUser.userId); if (result.ok && result.data) { setJob(result.data.data); setDeleteUser(null); users.reload(); } }} loading={removeUser.pending}>确认删除</Button>
          </>
        }
      >
        {removeUser.error ? <Notice tone="error" title="删除失败">{describeApiError(removeUser.error)}</Notice> : null}
      </Modal>

      <Modal
        open={rollbackOpen}
        title="回滚 Xray 配置"
        onClose={() => setRollbackOpen(false)}
        footer={
          <>
            <Button type="button" onClick={() => setRollbackOpen(false)} disabled={rollback.pending}>取消</Button>
            <Button type="button" variant="danger" onClick={runRollback} loading={rollback.pending}>确认回滚</Button>
          </>
        }
      >
        <div className="space-y-3">
          {rollback.error ? <Notice tone="error" title="回滚失败">{describeApiError(rollback.error)}</Notice> : null}
          <Field label="目标版本">{ids => <TextInput id={ids.inputId} value={rollbackRevision} onChange={event => setRollbackRevision(event.target.value)} />}</Field>
          <Field label="当前版本（并发保护）">{ids => <TextInput id={ids.inputId} value={currentRevision} onChange={event => setCurrentRevision(event.target.value)} />}</Field>
          <p className="text-xs text-muted-foreground">回滚只恢复配置版本；不保证所有外部副作用自动回滚。</p>
        </div>
      </Modal>

      <JobModal job={job} onClose={() => setJob(null)} />
    </div>
  );
}
