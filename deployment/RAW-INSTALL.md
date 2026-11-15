# raw.githubusercontent.com 安装入口

主控与被控共用一个脚本：

```text
https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/v0.9.2/deployment/raw-install.sh
```

固定版本链接可复现；`main/deployment/raw-install.sh` 是同仓库的更新入口。先下载成功再执行，一条命令启动已有主控的分发配置：

```sh
curl --fail --silent --show-error --proto '=https' --tlsv1.3 https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/v0.9.2/deployment/raw-install.sh -o serverfleet-install.sh && sudo sh serverfleet-install.sh setup
```

脚本入口需要信任此 GitHub 仓库与系统 HTTPS 根。后续脚本固定到同版本 tag，并核对内置 SHA256；程序安装继续校验离线 Ed25519 签名、全量文件摘要和内部 Root pin，不接受 HTTP、跳过 TLS 或把注册令牌放进命令。

## 被控服务器安装 Agent

管理员完成分发配置后，在控制台“添加节点”复制一键安装命令到被控服务器。默认命令只做三件事：下载脚本、核对摘要、执行：

```sh
curl -fsSL --proto '=https' --tlsv1.3 'https://<面板>/install/agent.sh' -o /tmp/sf-agent.sh && printf '%s  %s\n' '<脚本摘要>' /tmp/sf-agent.sh | sha256sum --check --status && sh /tmp/sf-agent.sh '<节点名>'
```

- `GET /install/agent.sh` 是无需会话的安装边界入口，只返回由 installation catalog 派生的静态脚本：预编译 Agent 的发行 URL、Root/HTTPS 证书、Root pin 与接入审批循环。脚本不含短期令牌；本版接入为免令牌审批，管理员在控制台批准后节点转为常驻服务。
- 摘要来自 `GET /api/v1/installations/catalog` 的 `agentScript.sha256`，控制台会自动带入；脚本本身对所有请求返回同一份字节，节点名称只作为脚本参数传入并在 shell 中单引号转义。
- 脚本下载 Agent 到 `/usr/local/bin`，信任材料写入 `/var/lib/serverfleet-agent`，随后每 5 秒发起一次接入申请，批准后 `exec` 常驻服务。
- 面板必须使用公开可信 HTTPS（ADR-BOOT-001）。若面板使用私有 CA，控制台“完整安装命令”回退会把信任材料直接写进命令，不依赖面板证书。
- 注册失败时 agent 保留统一失败文案，并追加一行本地推断原因（本地身份/证书、网络或 TLS、控制端拒绝）。原因只用于本机排查，不包含令牌或响应正文。
- 需要 Python 3/OpenSSL 的 `raw-install.sh` 流程仍用于分发准备、主控安装与离线包校验，不是控制台 Agent 命令的默认路径。

## 首次准备签名安装源

发行包保持未签名核心包；部署者的发行私钥留在离线环境。以下准备只需首次或发行更新时做：

1. 在联网 Linux 普通用户下运行同一个入口的 `download` 模式：

   ```sh
   sh serverfleet-install.sh download
   ```

   自动下载 v0.9.2 双架构包、SHA256SUMS、构建溯源和签名工具，核对 GitHub 资产摘要、源码 tag 和 raw 脚本摘要。独立保存输出的 **SHA256SUMS SHA256**，将输出目录带到离线机器。无需自行编译。
2. **断网后，以普通用户**在该目录运行：

   ```sh
   python3 -I setup.py prepare --offline-ack
   ```

   填入先前保存的摘要指纹。脚本验证两种架构并自动签包；仅带回 `installation-public.tar.gz`，独立核对新输出的 **trust.json SHA256**。发行私钥/Root 私钥均不上传。不能只对同一份未知文件自行计算摘要后就把它当外部信任。
3. 在已有主控运行 `sudo sh serverfleet-install.sh setup`，填公开包/指纹和固定地址等。新 catalog 默认使用 raw 安装入口，签名程序与 Web 文件通过受信任 HTTPS 分发提供。

## 已有分发升级

先按 [UPGRADE.md](UPGRADE.md) 升级实际运行的 Controller 到 v0.9.2，再准备本版签名公开包，运行：

```sh
sudo sh serverfleet-install.sh update-distribution
```

只更新原向导目录的公开版本文件、分发索引与 catalog；版本和 sequence 必须增加，旧文件保留以支持已生成的命令，旧身份/DB/keyring 不变。失败尝试恢复原 catalog/索引并重启原服务，保留新文件供核查。`--check` 只做预检。此操作**不替换正在运行的 Controller 二进制**，也不绕过受监督更新策略。自建目录/容器/包装服务仍由原部署者按其策略集成。

## 新主控安装

同一入口的 `controller` 模式会提示私有安装 JSON、已核对的签名公开包、其独立指纹和专用服务账户，验证后调用原生 `controller install`，完成账户/服务与实际就绪核验：

```sh
sudo sh serverfleet-install.sh controller
```

安装 JSON 使用 [examples/install-controller.json](examples/install-controller.json)，其中签名来源须已可用；HTTPS、离线 Root 签发的在线 Intermediate、相应普通用户可读的 TLS/PKI 材料须按 [README.md](README.md) 预先准备。脚本不在线生成离线 Root，不自动复制 `/root` 下的私钥，也不覆盖已有 runtime。`--check` 可以先核对输入；这是带信任准备的安装流程。

可用模式：`download`、`setup`、`update-distribution`、`controller`、`agent`、`--help`。主控/分发向导另需 Python 3、OpenSSL 3 和 systemd；不需要安装 Python 第三方依赖。正式 systemd/root/VPS 与干净节点验收由部署者完成。
