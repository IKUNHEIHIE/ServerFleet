# 安装向导：启用节点一键安装

适用于已经部署、能登录的 v0.9.1 主控，解决“尚未配置可信安装分发”。不需要手写 catalog JSON、逐架构签名命令、分发服务器或 systemd 启动覆盖。脚本仅用 Python 3 标准库、系统 OpenSSL 3 和 systemd；不需要编译或安装 Python 依赖。

这不是从空机器初始化主控的脚本。全新主控的 HTTPS、离线 Root/在线 Intermediate 仍按 [部署说明](README.md) 初始化，已有的 `controller install` 见 [INSTALL.md](INSTALL.md)。不能把未签名的 GitHub 压缩包直接作为可信安装源，也不能把离线签名密钥搬到主控来省略离线步骤。

## 运行入口

在本仓库或包含新向导的发行包目录运行：

```sh
sudo python3 deployment/setup.py
```

选择“为已有主控启用节点一键安装”，按提示提供服务名、固定 IP、签名公开文件包、离线记录的公开包指纹、HTTPS 根 CA 文件。其余文件路径、内部 Root 与指纹、两种架构摘要及 systemd 配置自动处理。默认新增分发端口 **9444/TCP**；安全组/防火墙须由部署者允许目标节点访问。脚本不改变现有防火墙和反代。

`v0.9.1` 的已有 Release 不含本向导；从当前源码取得 `deployment/setup.py` 与同一提交的 `deployment/install.sh`。先检查脚本来源再使用；不要将未知脚本直接 `curl | sh`。当前在线环境不具备离线签名密钥，仓库不会冒充发布已签名的官方安装目录。

## 首次准备公开文件包（一次离线操作）

1. 从 [v0.9.1 Release](https://github.com/IKUNHEIHIE/ServerFleet/releases/tag/v0.9.1) 下载两个架构的 `serverfleet_0.9.1_linux_amd64.tar.gz`、`serverfleet_0.9.1_linux_arm64.tar.gz`，以及同一源码提交的 `setup.py`、`install.sh`。保存到离线 Linux amd64/arm64 环境。无需自行解压或编译。
2. **断网后，以普通用户运行**（不要 sudo）：

   ```sh
   python3 setup.py prepare --offline-ack
   ```

   向导校验脚本内固定的官方压缩包 SHA256，安全解压，创建/复用独立的离线发行密钥，调用现有 Controller 的离线签名命令签署两种架构，生成 `installation-public.tar.gz`。默认离线密钥保存在 `~/serverfleet-offline/keys`，不会进入公开文件包。
3. 只上传生成的 `installation-public.tar.gz` 到主控。**另行保存并核对终端显示的 `trust.json SHA256`**，在线向导会询问它。不要仅从上传包里重新计算并盲信指纹；这个指纹同时固定签名公钥、版本、安装脚本和所有公开文件的摘要。`--offline-ack` 是操作者对断网环境的确认，脚本不声称能够证明主机离线。

不再需要手动拼接 `release package-installation` 命令、逐个复制 Web 文件或维护 bootstrap 摘要。

## 在线预检或非交互运行

可以先用 `configure --check` 填完提示，只校验，不修改 systemd 或主控。非交互示例：

```sh
sudo python3 deployment/setup.py configure \
  --service serverfleet-controller.service \
  --bundle /private/installation-public.tar.gz \
  --trust-sha256 离线记录的64位摘要 \
  --addresses 你的主控固定IP \
  --https-root /private/https-root-ca.pem \
  --check
```

预检通过，去掉 `--check` 执行。自动安装会短暂重启原主控来加载 catalog。成功后刷新“添加节点”，生成原有的一键安装命令，在目标节点输入注册令牌并完成原有接入/审批流程。令牌不会被本向导获取或加入命令历史。

## 实际修改与回退

- 默认新目录 `/var/lib/serverfleet-installation`：仅含公开发行文件、分发程序、catalog 和回执；发行私钥与内部 Root 私钥不进入此目录。catalog 位于 `controller-config/installation-catalog.json`，其父目录为主控用户所有、0700，文件同属主控用户、0600，满足产品实际私有文件读取规则。
- 新增 `serverfleet-distribution.service`：普通主控用户运行，TLS 1.3，只读提供经过核对的公开文件。没有目录浏览，不提供私钥/配置文件，不跟随 URL 跳转、代理、任意路径或查询参数。
- 新增主控服务的 `90-serverfleet-installation.conf` drop-in：保留实际直接启动参数，仅追加 `-installation-catalog`。不改原 unit，不重建数据库、凭据 keyring 或节点身份。
- 在修改主控前验证真实分发 HTTPS/摘要；重启后检查 `/readyz`。失败会删除本次新增的启动覆盖和分发 unit，尝试恢复原服务；保留文件和回执用于排障，不删除产品数据。进程被 SIGKILL/断电时来不及执行自动回退，按下方步骤恢复。

主动回退时，删除本次新增的主控 drop-in（具体路径由向导输出），再执行：

```sh
sudo systemctl disable --now serverfleet-distribution.service
sudo systemctl daemon-reload
sudo systemctl restart 你的原主控服务名
```

原主控 unit、DB 和身份均保持原位置。已有配置、同名服务或安装目录不覆盖；重复运行遇到冲突会停止，应先核对原目录/回执。不要删除正在使用的发行目录或凭据。

## 适用条件与验收边界

- **支持固定普通用户、直接 ExecStart 的 systemd 系统服务**，主控 db/tls-cert/tls-key 使用绝对路径，已配置 provisioned PKI。root 主控、容器、systemd user manager、监督程序/包装启动及冻结的更新策略不会被自动改写；由原部署者按其策略集成 catalog，不能重建已有 runtime 来强行套用。
- v0.9.1 将 catalog 的 `controllerOrigin` 绑定管理 `-origin`。本向导要求 `-agent-authority` 与该 Origin 的域名/端口一致，Agent 入口为直连 mTLS，并验证固定 IP 的主控就绪状态。**管理和 Agent 分离入口暂不支持本向导**，不能靠把 Agent 地址填成管理地址来绕过。
- 分发复用当前主控 TLS 证书/密钥的**原文件路径**，不复制私钥。普通服务用户须可读这些原文件；原目录的额外 systemd bind-mount/凭据挂载不自动继承。证书应为目标节点系统信任的 HTTPS 链，或事先由部署者在节点安装其受信任根；现有外层 curl 不自动导入私有根。HTTPS 根参数须为单个有效自签 CA，不能用网站 leaf/fullchain 替代。
- TLS 证书续签后重启 `serverfleet-distribution.service` 以加载新链；如果更换文件路径、域名或 HTTPS 根，还需重新审核原 catalog 和服务配置。
- 分发服务当前监听 IPv4 `0.0.0.0`，至少需要一个可直达的固定 IPv4；不支持仅 IPv6。9444/TCP 必须从目标节点可达，不经会丢失 Agent 客户端证书的 HTTP 代理。
- 自动检查包含固定摘要、真实 Ed25519、真实本地 TLS、归档路径/链接拒绝，以及模拟 systemd 事务的成功/预检/回退和数据保留。它们不等于真实 VPS/root/systemd 安装验收。最终还须管理员确认 `/api/v1/installations/catalog` 返回 `available:true`，并在一台干净节点完成真实安装、审批与在线连接。

命令行为依据 [OpenSSL pkeyutl](https://docs.openssl.org/3.5/man1/openssl-pkeyutl/) 和 [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)。不新增业务 HTTP API 或放宽既有授权。
