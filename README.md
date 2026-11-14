# Server Fleet

**v0.9.2 · 正式 PKI 入网与 raw 安装修复版**

接手开发请先读 **[开发交接入口](docs/development/HANDOFF.md)** 与 **[当前状态及未收尾事项](docs/development/CURRENT-STATE.md)**。完整文档分类见 [文档导航](docs/README.md)；旧任务和原型不代表当前实现状态。

轻量、可自托管的服务器群控系统。通过统一 Web 控制台管理节点、文件、服务、证书、任务与 AI 运维，让多台服务器的日常管理、变更执行和异常处置集中在同一套权限与审计体系中。

## 核心能力

| 领域 | 功能 |
|---|---|
| 节点管理 | 可信一键安装、接入审批、状态与资源监控、分类分组、服务组 |
| 任务与权限 | 批量目标预览、分批执行、独立审批、取消、资源授权、操作审计 |
| 文件互传 | 上传、下载、跨节点及多目标传输、冲突策略、摘要校验、显式续传 |
| 网关与发布 | Nginx 网关管理、路由与调度、服务发布向导、健康检查、失败补偿 |
| DNS 与证书 | Cloudflare DNS、公网与内部证书、节点独立身份、续签后的安装与发布编排 |
| AI 运维 | 资源范围约束、三级审批、受控工具、多轮 Agent 执行、任务结果回传 |
| 监控与通知 | Controller／指定节点 HTTP、TCP、TLS 探测，连续阈值告警，SMTP／QQ Bot |
| 流量与配额 | 主机流量增量、账期覆盖、配额阈值、到期提醒 |
| 更新与恢复 | Controller／Agent 受监督更新、健康回滚、加密备份、异地上传、保留与清理、灾备接管 |
| 异常处置 | UNKNOWN 收件箱、证据与快照、后续核验任务、独立复核、结案依据变化后重新核查 |

各功能的配置前提、实际行为和实现边界见[功能说明](docs/product/FEATURES.md)。

v0.9.1 已加入 Agent 身份自动续期、接入读取授权、升级预检、就绪检查与节点诊断，v0.9.2 继续修复下述正式 PKI 链问题。升级和已过期身份恢复请先阅读[升级说明](deployment/UPGRADE.md)，反代部署见[HTTPS 与 mTLS 参考](deployment/REVERSE-PROXY.md)。

v0.9.2 修复 provisioned 模式重复 Intermediate、缺少 Root 导致 Agent 拒绝入网/续期的问题；旧批准回执保留原 leaf 并修复公开响应链。主控/Agent 共用 [raw 安装入口](deployment/RAW-INSTALL.md)：

```sh
curl --fail --silent --show-error --proto '=https' --tlsv1.3 https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/v0.9.2/deployment/raw-install.sh -o serverfleet-install.sh && sudo sh serverfleet-install.sh setup
```

这是已有主控启用分发的入口；`controller` 安装新主控，Agent 完整命令由控制台生成并使用同一 raw URL。首次离线 Root/发行签名准备保持，不在主控生成离线私钥。

## 快速开始

### 1. 准备环境

核心程序面向 **Linux amd64／arm64**。源码构建使用 Go **1.27.4**、Node **24.22.1** 和 npm **11**，具体版本见[工具链](docs/development/TOOLCHAIN.md)。

准备直接 HTTPS 访问的 Controller 地址、私有运行目录及内部身份信任。Controller 和 Agent 通常以普通用户运行；需要提权的操作另行配置固定命令与独立审批。

### 2. 下载或构建核心发行包

预编译包在 [GitHub Releases](https://github.com/IKUNHEIHIE/ServerFleet/releases) 提供 Linux amd64／arm64 版本，包含 Controller、Agent、Web 和部署示例。下载后先核对随附 `SHA256SUMS` 并阅读[发行说明](docs/releases/v0.9.2.md)。当前下载包未做离线发行签名，不能直接作为一键安装／自动更新的受信任目录。

从源码自行构建：

在干净且已提交的源码目录执行：

```sh
npm ci
node scripts/build-release.mjs --version=0.9.2 --sequence=3 --output=/home/serverfleet/releases/0.9.2
```

示例输出路径须位于仓库外，父目录已存在，目标目录必须是新目录。构建包含两个架构的 Controller、Agent、正式 Web、配置示例和摘要清单。构建产物尚需按[部署说明](deployment/README.md)核对来源并进行离线发行签名，之后再分发和安装。

### 3. 初始化 Controller 并添加节点

按[部署与初始化](deployment/README.md)配置 HTTPS、内部 PKI、运行目录和监督进程，完成首次管理员初始化；再通过控制台“添加节点”生成短期注册令牌并使用[一键安装流程](deployment/INSTALL.md)。首次安装需独立核对发行签名公钥和 Root 指纹。

已部署主控如果显示“尚未配置可信安装分发”，使用[安装向导](deployment/QUICK-SETUP.md)：`sudo python3 deployment/setup.py`，或上述 raw 入口。首次签名公开文件包在离线机器用 `prepare --offline-ack` 自动制作；已有向导分发用 `update-distribution` 升级。本版发行包包含工具，旧 v0.9.1 不再作为新主控安装源；未签名压缩包仍不能直接充当可信安装目录。

节点接入后，为操作者明确分配节点资源权限，并根据需要配置文件根、命令策略、DNS／ACME、通知渠道及备份存储。

## 架构

| 组件 | 技术与职责 |
|---|---|
| Controller | Go 模块化单体、SQLite WAL；集中处理授权、统一任务、审批、审计和业务编排 |
| Agent | Go 独立进程，主动通过 WSS + mTLS 连接 Controller；执行受控能力并返回类型化回执 |
| Web | React、TypeScript、Vite；通过 Controller REST／SSE 交互 |
| 可选插件 | 与核心发行包分离，按需安装；执行环境须单独完成安全准入 |

所有执行经过统一任务体系；AI 的资源范围由代码强制校验。“完全访问”模式仍保留任意 root 操作的真人管理员审批。UNKNOWN 表示结果未确定，不自动重放有副作用操作。

## 文档导航

| 目的 | 文档 |
|---|---|
| 功能与操作规则 | [现行功能说明](docs/product/FEATURES.md) |
| 安装与首次使用 | [部署说明](deployment/README.md)、[一键安装](deployment/INSTALL.md) |
| 前端重构与接口对接 | [API 说明](docs/api/README.md)、[路由索引](docs/api/ROUTES.md)、[现有 API 客户端与 DTO](apps/web/src/lib/api) |
| AI 运维 | [执行循环与使用边界](docs/development/AI-AGENT-LOOP.md) |
| 监控与通知 | [持续监控／告警／流量](docs/development/MONITORING-ALERTS.md)、[节点探测](docs/development/NODE-PROBE-MONITORING.md) |
| 证书续签 | [安装与发布编排](docs/development/RENEWAL-DELIVERY.md) |
| 备份与异常 | [异地备份与保留清理](docs/development/BACKUP-OPERATIONS.md)、[人工处置闭环](docs/development/INCIDENT-CLOSURE.md) |
| 源码与架构 | [项目结构](docs/development/PROJECT-STRUCTURE.md)、[架构与 ADR 来源](docs/architecture/README.md) |
| 审计与已知问题 | [代码审计报告](docs/audits/2026-11-12.md) |

## 版本范围与已知限制

本版以核心服务器管理能力作为首发范围。以下事项继续跟踪：

- **Xray 可选插件**已有业务组件与声明式页面，生产执行环境及 Host 装配尚未完成；当前不能按完整可用插件使用。
- **受限只读令牌**已有签发、撤销和校验能力，正式节点／任务 HTTP 的 Bearer 读取适配尚未接入。
- 真实供应商、提权、双机灾备、物理 ARM 与硬限制 1 核 1 GB 环境仍需实机联调。首个正式版的命名不表示这些场景或完整 G2／发布验收已经完成。
- 前端计划进行独立 UI 重构；后续界面变化应保持现有会话、权限、审批和 UNKNOWN 等业务语义，并同步功能及接口文档。

当前代码/发布证据基线为 [PR #31](https://github.com/IKUNHEIHIE/ServerFleet/pull/31)，main `c6204762c7fb285c475b619886474351c26dca31`。[最终 PR head CI](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43286304878)、[合并后 CI](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43366199102)与[发行工作流](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43366199340)成功。历史测试计数不挪作本次结果，自动检查不替代实机验收；后续 main 变化以各自证据为准，详见[当前状态](docs/development/CURRENT-STATE.md)。

## 开发与贡献

先读 [HANDOFF](docs/development/HANDOFF.md)。以下是代码检查命令；纯文档变更按用户要求仅做文档核对，不运行完整 CI。

```sh
npm ci
npm run tools:install
npm run check
npm run check:security
```

新增或移动 HTTP 路由后，更新接口声明索引：

```sh
node scripts/api-docs.mjs --write
```

完整检查包含索引漂移检查；真实 Nginx 集成用例需要环境安装 Nginx。先阅读 [AGENTS.md](AGENTS.md)、[模块归属](docs/development/MODULE-OWNERSHIP.md)与[依赖规则](docs/development/DEPENDENCY-RULES.md)，为独立任务建立分支和工作目录，通过 PR 审查与检查后集成。

数据库迁移只追加，原始需求、已批准 ADR、历史候选与验收证据保留追溯。可选插件不编入核心 Release，运行时准入和生产部署按各自边界执行。[历史开发记录](docs/development/sprints/v09-five-day/CURRENT-PROGRESS.md)用于追溯，不作为当前功能清单。
