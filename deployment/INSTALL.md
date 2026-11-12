# 一键安装与可信发行

R31 提供 Linux amd64 / arm64 的 Controller、Agent 安装器与「添加节点 → 生成一键安装命令」正式入口。安装先验证外部提供的安装器 SHA256，再验证离线 Ed25519 签名覆盖的二进制、组件清单和整个 Web 目录。Release 构建不会自动签名或发布。

**已有主控显示“尚未配置可信安装分发”**：优先使用 [setup.py 安装向导](QUICK-SETUP.md)，自动制作离线签名公开包，并在线创建 HTTPS 分发、catalog 与 systemd drop-in。支持条件与不接管的运行方式见该文档；以下保留手动配置说明。

## 发行方准备

1. 在干净提交上运行原有 scripts/build-release.mjs 构建两种架构，保留 source commit、sequence 和摘要；可选插件仍不进入核心包。
2. 在离线环境执行 controller release package-installation，传入 -source-directory、-output、-key、-key-id 和 -offline-ack。完整参数以 controller release package-installation -h 为准。输出目录必须为 VERSION/ARCH，例如 /private/signed/1.2.3/amd64。两个架构分别签名。签名私钥不进入 Web、Controller 或分发目录。
3. 将签名后的每个架构目录以 HTTPS 发布为 ORIGIN/VERSION/ARCH/installation.manifest.json、installation.signature 及所列文件。发行公钥、最低 sequence、可信 HTTPS CA、固定目标 IP、bootstrap Controller 二进制摘要及 install.sh 摘要通过独立可信渠道核对。安装器不接受跳转、代理、降级 HTTP 或跳过 TLS 验证。
4. 复制 examples/installation-catalog.json 到主控私有目录，填入实际公开材料，权限 0600。配置 -installation-catalog /absolute/private/installation-catalog.json。内部 Root CA 指纹必须与当前主控身份 CA 一致；公开 HTTPS CA 与内部身份 CA 分别填写。此文件仅含公开材料，可随配置备份；新灾备权威仍需按恢复规则重新配置。

## 添加节点

在添加节点页生成短期注册令牌，再展开一键安装。核对页面内部 Root CA 指纹，选择专用服务账户与节点名称，复制命令到目标机器执行。命令不含令牌；安装器在终端关闭回显后请求输入。无终端时使用权限 0600 的 enrollmentTokenFile，禁止把令牌放命令参数、环境日志或工单。

root 仅执行显式 -system-user 包装流程：创建专用普通用户和 systemd 系统服务，下载与产品操作全部降权运行。服务设置 User、UMask=0077、NoNewPrivileges=yes、Restart=no，不改防火墙或 sudoers。账户必须以小写字母开头，3—32 位。已有账户须有正确 /var/lib/ACCOUNT home、所有权与 0700 权限。安装目录必须是该 home 的直接子目录。

Agent 先创建并持久化自己的身份 CSR 与 request ID，再申请接入。人工审批时，主控批准同一请求，安装器继续轮询。超时返回 WAITING_FOR_NODE_APPROVAL；保留目录并运行原命令继续，不生成新身份。证书拒绝、令牌过期或网络结果不明时先检查原请求。

安装成功必须有新鲜的产品 readiness、实际运行 PID 与已签名程序摘要；Agent 还须有实际节点/连接标识，Controller 还核验私有 HTTPS readiness。systemctl 启动成功本身不算产品健康。

## 主控安装

examples/install-controller.json 是配置模板。主控首次启用仍须预先完成离线 Root 签发与在线中间 CA 配置，提供普通服务用户可读的公网 TLS 与身份材料。安装器不会把 /root 下的秘密复制给服务账户，也不会在线生成离线 Root 私钥。

用已外部核对 SHA256 的 Controller 引导程序运行：

    controller install -config /absolute/private/install-controller.json -system-user serverfleet -check
    controller install -config /absolute/private/install-controller.json -system-user serverfleet

普通用户可选择 serviceMode=user，要求已有可用的 systemd user manager；root 包装采用 external，创建持久系统服务并以普通用户完成健康核验。bootstrap 管理员令牌只报告私有文件路径及十分钟有效期，不回显令牌值。

## 中断恢复与冲突

install-state.json 绑定原配置与签名清单。重复运行复用已验证文件、原身份和确定性接入请求。配置、清单、权限、文件摘要不一致立即停止，不覆盖未知目录；并发安装由文件锁阻止。

如果 provision 已创建部分 runtime 但未产生 prepared.json，保留现场并检查 provision-attempt.json，先修复或人工决定恢复方式。既有不同 systemd 单元不覆盖；已有身份、数据库不重新初始化。root 输入锚点按账户和组件分开，Controller、Agent 可以使用同一专用账户下不同安装目录。

本轮自动验证覆盖签名、HTTPS、摘要、组件绑定、权限、并发和界面交互。真实 Linux systemd/root 安装、真实 CA/DNS 续签与双机连通仍由部署者联调，不以合成测试替代。

相关行为以 systemd 官方 systemctl 文档与 curl 官方选项文档为准：
https://www.freedesktop.org/software/systemd/man/latest/systemctl.html
https://curl.se/docs/manpage.html

## v0.9.1 身份生命周期

常驻 Agent 自动续期并安全切换连接。过期证书不会被启动重试自动替换；保留账本，按 [UPGRADE.md](UPGRADE.md) 的显式恢复流程操作。批准界面勾选单节点读取授权才能使相应账户看到节点，批准和执行权限互不替代。令牌 ID 与入网请求 ID 不同，安装器会等待同一请求的审批。
