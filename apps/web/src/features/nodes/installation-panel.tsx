import { useState } from "react";
import { Terminal, Copy } from "lucide-react";
import { useSession } from "@/app/session";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Notice } from "@/components/ui/feedback";
import { Field, TextArea, TextInput } from "@/components/ui/form";
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
}

const quote = (s: string) => `'${s.replaceAll("'", `'"'"'`)}'`;
function base64UTF8(s: string) {
  return btoa(Array.from(new TextEncoder().encode(s), b => String.fromCharCode(b)).join(""));
}

export function installationCommand(c: Catalog, user: string, name: string): string {
  const config = {
    format: 1,
    directory: `/var/lib/${user}/fleet`,
    source: c.source,
    provision: {
      format: 1,
      component: "agent",
      directory: "",
      artifact: "",
      manifest: "",
      supervisorExecutable: "",
      rootCertificate: "",
      rootPin: c.rootPin,
      controllerOrigin: c.controllerOrigin,
      controllerAddresses: c.controllerAddresses,
      releaseKeys: {},
      agent: { nodeId: "", identityDir: "", name },
    },
    httpsRootPem: c.httpsRootPem,
    identityRootPem: c.identityRootPem,
    serviceMode: "external",
    waitSeconds: 300,
  };
  const script = c.installerScript;
  const [major = 0, minor = 0, patch = 0] = c.source.version.split(".").map(Number);
  const modern = major > 0 || minor > 9 || (minor === 9 && patch >= 2);
  const ca = quote(base64UTF8(c.source.rootPem));
  const localScriptCA = new URL(script.url).origin === new URL(c.source.origin).origin ? `--cacert "$_sf_install/source-ca.pem" ` : "";
  const flags = `--config-base64 ${quote(base64UTF8(JSON.stringify(config)))} --amd64-url ${quote(c.bootstrap.amd64.url)} --amd64-sha256 ${quote(c.bootstrap.amd64.sha256)} --arm64-url ${quote(c.bootstrap.arm64.url)} --arm64-sha256 ${quote(c.bootstrap.arm64.sha256)} --system-user ${quote(user)}${modern ? ` --bootstrap-ca-base64 ${ca}` : ""}`;
  return `(_sf_install=$(mktemp -d) && chmod 700 "$_sf_install" && printf '%s' ${ca} | base64 --decode > "$_sf_install/source-ca.pem" && chmod 600 "$_sf_install/source-ca.pem" && curl --fail --silent --show-error --proto '=https' --tlsv1.3 --noproxy '*' --max-time 60 --max-filesize 131072 ${localScriptCA}${quote(script.url)} -o "$_sf_install/install.sh" && printf '%s  %s\\n' ${quote(script.sha256)} "$_sf_install/install.sh" | sha256sum --check --status && sh "$_sf_install/install.sh" ${flags}; _sf_result=$?; [ -z "\${_sf_install:-}" ] || rm -rf -- "$_sf_install"; exit "$_sf_result")`;
}

interface InstallationPanelProps {
  readonly nodeName?: string;
}

function InstallationContent({ nodeName }: InstallationPanelProps) {
  const session = useSession();
  const page = useResource(`installation-catalog:${session.viewEpoch}`, signal =>
    apiClient.request<{ available: boolean; catalog?: Catalog }>("/api/v1/installations/catalog", { signal }).then(r => r.data),
  );
  const [user, setUser] = useState("serverfleet");
  const [fallbackName, setFallbackName] = useState("node");
  const name = nodeName ?? fallbackName;
  const [message, setMessage] = useState<string | null>(null);
  const data = page.state.status === "ready" ? page.state.data : null;
  const valid = /^[a-z][a-z0-9_-]{2,31}$/.test(user) && name.trim().length > 0 && name.length <= 128 && !/[\x00-\x1f\x7f]/.test(name);
  const command = data?.catalog && valid ? installationCommand(data.catalog, user, name.trim()) : "";

  return (
    <div className="space-y-4 border-t border-border/70 px-5 py-4">
      {page.state.status === "loading" ? <p role="status" className="text-sm text-muted-foreground">正在读取可信安装信息…</p> : null}
      {page.state.status === "error" ? (
        <Notice tone="error" title="安装信息不可用">
          {describeApiError(page.state.error)}
          <Button size="sm" onClick={page.reload}>重试读取安装信息</Button>
        </Notice>
      ) : null}
      {data && !data.available ? (
        <Notice tone="warning" title="尚未配置可信安装分发">
          管理员需按部署说明配置 installation-catalog，提供签名安装包、安装器摘要及公开信任材料。
        </Notice>
      ) : null}
      {data?.catalog ? (
        <>
          <Notice tone="info" title="在目标节点以 root 运行一次">
            安装器会创建专用普通用户、校验签名、注册服务并等待接入审批。短期令牌会在终端隐藏输入；命令中不包含令牌。发行版本 {data.catalog.source.version}。
            <p className="mt-1">
              内部 Root CA 指纹：<code className="rounded bg-card px-1.5 py-0.5 font-mono text-xs">{data.catalog.rootPin}</code>。首次安装前请与管理员离线提供的指纹核对。
            </p>
          </Notice>
          <div className="grid gap-3.5 sm:grid-cols-2">
            <Field label="专用服务账户">
              {ids => <TextInput id={ids.inputId} value={user} maxLength={32} onChange={e => setUser(e.target.value)} />}
            </Field>
            {nodeName === undefined ? (
              <Field label="安装节点名称">
                {ids => <TextInput id={ids.inputId} value={fallbackName} maxLength={128} onChange={e => setFallbackName(e.target.value)} />}
              </Field>
            ) : null}
          </div>
          {!valid ? (
            <p role="alert" className="text-xs text-destructive">
              账户须为3—32位小写字母、数字、横杠或下划线并以字母开头；请填写有效节点名称。
            </p>
          ) : null}
          <Field label="一键安装命令">
            {ids => <TextArea id={ids.inputId} readOnly rows={4} value={command} className="font-mono text-xs" />}
          </Field>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="text-xs text-muted-foreground">
              等待审批或中断时保留原目录，使用同一条命令继续。安装器不会覆盖已有身份、数据库、其他服务或防火墙配置。
            </p>
            <Button
              variant="primary"
              disabled={!command}
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(command);
                  setMessage("安装命令已复制。运行后粘贴上方生成的短期令牌，再回到主控批准该接入请求。");
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
  const [open, setOpen] = useState(false);
  return (
    <Panel
      title={nodeName === undefined ? "一键安装节点" : "2. 可信安装"}
      description="可信下载、专用用户、服务注册和接入审批。"
      actions={
        <>
          <Badge tone="primary" dot>
            <Terminal aria-hidden="true" className="size-3" />
            Bootstrap Command
          </Badge>
          <Button size="sm" variant={open ? "secondary" : "primary"} aria-expanded={open} onClick={() => setOpen(v => !v)}>
            {open ? "收起安装命令" : "生成一键安装命令"}
          </Button>
        </>
      }
    >
      {open ? (
        <InstallationContent key={session.viewEpoch} nodeName={nodeName} />
      ) : (
        <div className="px-5 py-3.5 text-xs text-muted-foreground">
          从可信安装分发清单生成包含 Root CA 指纹与 SHA-256 校验的单行安装脚本（不含短期令牌明文）。
        </div>
      )}
    </Panel>
  );
}
