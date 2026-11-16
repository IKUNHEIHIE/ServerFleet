import { useState } from "react";
import { Terminal, Copy } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Checkbox, Field, TextArea, TextInput } from "@/components/ui/form";
import { Panel } from "@/components/ui/panel";
import { apiClient } from "@/lib/api/client";
import { describeApiError } from "@/lib/api/errors";
import { useResource } from "@/lib/data/use-resource";

interface Download {
  readonly url: string;
  readonly sha256: string;
}
interface Catalog {
  readonly source: {
    readonly origin: string;
    readonly addresses: readonly string[];
    readonly rootPem: string;
    readonly version: string;
    readonly keyId: string;
    readonly publicKey: string;
    readonly minimumSequence: number;
  };
  readonly identityRootPem: string;
  readonly httpsRootPem: string;
  readonly controllerOrigin: string;
  readonly controllerAddresses: readonly string[];
  readonly rootPin: string;
  readonly installerScript: Download;
  readonly bootstrap: Readonly<Record<"amd64" | "arm64", Download>>;
  readonly agentScript: Download;
}

const quote = (s: string) => `'${s.replaceAll("'", `'"'"'`)}'`;

export function installationShortCommand(script: Download, name: string, systemFiles = false): string {
  return `curl -fsSL --proto '=https' --tlsv1.3 ${quote(script.url)} -o /tmp/sf-agent.sh && printf '%s  %s\\n' ${quote(script.sha256)} /tmp/sf-agent.sh | sha256sum --check --status && sh /tmp/sf-agent.sh ${quote(name)}${systemFiles ? " --system-files" : ""}`;
}

interface InstallationPanelProps {
  readonly nodeName?: string;
}

function InstallationContent({ nodeName }: InstallationPanelProps) {
  const session = useSession();
  const page = useResource(`installation-catalog:${session.viewEpoch}`, signal =>
    apiClient.request<{ available: boolean; catalog?: Catalog }>("/api/v1/installations/catalog", { signal }).then(r => r.data),
  );
  const [fallbackName, setFallbackName] = useState("node");
  const name = nodeName ?? fallbackName;
  const [message, setMessage] = useState<string | null>(null);
  const [systemFiles, setSystemFiles] = useState(false);
  const data = page.state.status === "ready" ? page.state.data : null;
  const valid = name.trim().length > 0 && name.length <= 128 && !/[\x00-\x1f\x7f]/.test(name);
  const command = data?.catalog && valid ? installationShortCommand(data.catalog.agentScript, name.trim(), systemFiles) : "";

  return (
    <div className="space-y-4 px-5 py-4">
      {page.state.status === "loading" ? <p role="status" className="text-sm text-muted-foreground">正在读取可信安装信息...</p> : null}
      {page.state.status === "error" ? (
        <Notice tone="error" title="安装信息不可用">
          {describeApiError(page.state.error)}
          <Button size="sm" onClick={page.reload}>重试</Button>
        </Notice>
      ) : null}
      {data && !data.available ? (
        <Notice tone="warning" title="尚未配置可信安装分发">管理员需按部署说明配置 installation-catalog，提供公开信任材料。</Notice>
      ) : null}
      {data?.catalog ? (
        <>
          <Notice tone="info" title="在目标节点以 root 运行一次">
            命令会创建专用系统用户与常驻服务（含文件互传能力），无需输入接入令牌；审批通过后服务自动上线。
            <p className="mt-1">内部 Root CA 指纹：<code className="rounded bg-card px-1.5 py-0.5 font-mono text-xs">{data.catalog.rootPin}</code>，首次安装前请与管理员离线提供的指纹核对。</p>
          </Notice>
          {nodeName === undefined ? (
            <Field label="节点名称">
              {ids => <TextInput id={ids.inputId} value={fallbackName} maxLength={128} onChange={e => setFallbackName(e.target.value)} />}
            </Field>
          ) : null}
          {!valid ? <p role="alert" className="text-xs text-destructive">请填写有效节点名称。</p> : null}
          <Field label="一键安装命令">
            {ids => <TextArea id={ids.inputId} readOnly rows={2} value={command} className="font-mono text-xs" />}
          </Field>
          <p className="text-xs text-muted-foreground">命令从本页面下载安装脚本并核对摘要，不含令牌；请在目标节点以 root 运行。</p>
          <label className="flex items-start gap-2 text-xs text-muted-foreground">
            <Checkbox aria-label="整机文件浏览" className="mt-0.5" checked={systemFiles} onChange={event => setSystemFiles(event.target.checked)} />
            <span>整机文件浏览（以 root 运行）：可读写服务器上的所有文件，风险更高。不勾选时文件互传只作用于专用目录。</span>
          </label>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">运行后到下方「接入审批」批准该节点；批准后自动启动常驻服务并上线。</p>
            <Button
              variant="primary"
              disabled={!command}
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(command);
                  setMessage("安装命令已复制。在目标节点以 root 运行，然后回到本页批准接入。");
                } catch {
                  setMessage("剪贴板不可用，请手动复制安装命令。");
                }
              }}
            >
              <Copy aria-hidden="true" className="size-3.5" />
              复制一键安装命令
            </Button>
          </div>
        </>
      ) : null}
      {message ? <Notice title="安装提示" tone="info">{message}</Notice> : null}
    </div>
  );
}

export function InstallationPanel({ nodeName }: InstallationPanelProps = {}) {
  const session = useSession();
  return (
    <Panel
      title="一键安装节点"
      description="下载预编译 Agent，免令牌直接进入接入审批，批准后自动常驻上线。"
      actions={
        <Badge tone="primary" dot>
          <Terminal aria-hidden="true" className="size-3" />
          一键安装
        </Badge>
      }
    >
      <InstallationContent key={session.viewEpoch} nodeName={nodeName} />
    </Panel>
  );
}
