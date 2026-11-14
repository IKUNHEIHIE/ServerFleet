# 当前实现与交付状态

核对日期：**2026-11-14**。这是当前状态汇总入口；历史进度与任务单不替代本文。后续代码合并须重新核对更新。

## 代码与发布基线

| 项目 | 已核对事实 |
|---|---|
| 仓库 | 公有 `IKUNHEIHIE/ServerFleet` |
| 代码基线 | main `c6204762c7fb285c475b619886474351c26dca31`，PR #31 已合并 |
| 最新发布 | [v0.9.2](https://github.com/IKUNHEIHIE/ServerFleet/releases/tag/v0.9.2)，Linux amd64/arm64 |
| 迁移 | 50 项；历史 SQL/checksum 不变；R39 无新迁移 |
| 工具 | Go 1.27.4、Node 24.22.1、npm 11.x；版本源见 [工具链](TOOLCHAIN.md) |
| 产品验收 | 核心代码已交付；完整 G2 冻结/原始需求全量验收未记录完成；实机验收开放 |

本轮 R40 只更新文档，不改变代码、契约、脚本、迁移、发行包或 tag，不产生新的运行验收。文档合并后 main SHA 会变化，上表记录对应代码/发布证据的基线，不是后续回退指令。

## 已实现与运行前提

| 范围 | 当前实现 | 运行/验收边界 |
|---|---|---|
| 身份、节点、权限 | 登录/RBAC/资源授权/审计、分类/服务组、mTLS 续期/过期重入网、诊断/readyz | 需 PKI/HTTPS、明确节点授权；管理员无节点通配 |
| 任务与文件 | 批次/审批/命令/传输/显式续传、UNKNOWN 人工闭环 | 文件根、命令策略、提权独立配置；UNKNOWN 不重放 |
| AI 运维 | 多轮模型→工具→任务→结果回传、范围与审批 | BYOK/真实模型及节点联调未据此完成；任意 root 仍需真人审批 |
| 网关、DNS、证书、发布 | Nginx 纳管、Cloudflare、ACME/内部证书、续签安装发布编排 | 可信工件/供应商/执行配置；实际证书和服务重载待验证 |
| 监控、通知、流量 | HTTP/TCP/TLS、节点来源、连续阈值、SMTP/QQ、配额 | 未观测数据保持未知；实际供应商和通知送达须验证 |
| 更新、备份、灾备 | 监督更新/回滚、加密备份、S3/WebDAV 保留清理、恢复/fence | 离线密钥、可信 supervisor、隔离证据；真实双机故障/回滚未全验收 |
| Web | React 正式多功能页面和 Controller 客户端 | UI 重构为后续任务；API 全字段核验须逐功能进行 |

详细行为见 [功能矩阵](../product/FEATURES.md)、[API](../api/README.md)、[部署](../../deployment/README.md)。代码存在不等于所有环境可直接启用。

## 明确未收尾事项

| 类别 | 开放项 | 后续边界 |
|---|---|---|
| 生产装配 | Xray PluginRuntime/XrayTaskHost 与准入 | 业务组件/声明式 UI 已有，当前不能当完整插件；不编进核心 Release 代替准入 |
| HTTP 适配 | 受限只读 Bearer 的正式节点/任务读取 | 管理和校验端口已有，正式 HTTP 未接；签发不等于可用 |
| 升级实证 | 实际旧 45 迁移快照的升级问题 | 未取得原 DB；合成 49→50/Version1 identity 与原 keyring 验证不能证明现场已修复 |
| 实机验收 | 供应商/提权/双机 fence/物理 ARM/硬 1核1GB、长期续期/故障/回滚/浏览器 | 需用户/Qoder 的精确环境证据，交叉构建不能替代 |
| 产品/协议收口 | 原始需求全量追踪、G2 冻结、API 全字段核验 | 路由索引、CI、Release 不能代替 |
| 体验 | 前端 UI 重构 | 保留现有安全/业务语义，同步功能/API |

MFA 尚未实现；工作流无通用 Cron 调度器；Eino/WASM 未获生产运行时准入。这些是能力边界，不自动构成下一位 Agent 的任务承诺。

## v0.9.2 安装与修复边界

R39 修复 provisioned 重复 Intermediate/缺 Root 的入网失败，链为 `[leaf, Intermediate, Root]`，保留唯一性、签名与 Root pin 校验。仅对严格匹配已知旧形状的有效回执修复公开链，不重签 leaf、不改身份/私钥/旧账本、不绕过过期或撤销。

主控与 Agent 共用 [raw 入口](../../deployment/RAW-INSTALL.md)：

`https://raw.githubusercontent.com/IKUNHEIHIE/ServerFleet/v0.9.2/deployment/raw-install.sh`

入口 SHA256：`7c776cf799b0442eb43f4321f57f726ac08bcd0a10889f4307126132bc73155a`。公开 Release 压缩包**未作离线发行签名**，首次仍须离线签名/Root 信任准备，不能宣传零准备安装。Agent 命令由控制台生成，令牌不进入 argv。

分发向导仅自动接管支持的普通用户/直接 ExecStart systemd 部署；容器、冻结监督策略、分离管理与 Agent 入口等不自动接管。`update-distribution` 更新分发材料/配置，不替换运行中的 Controller 二进制。

## 发布与检查回执

- [PR #31](https://github.com/IKUNHEIHIE/ServerFleet/pull/31) 已合并；最终 head `b164e98a4817450454264721d6a10459e9550cbe` 的 [Foundation CI 43286304878](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43286304878) 成功。
- 合并基线的 [Foundation CI 43366199102](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43366199102) 和 [Release 43366199340](https://github.com/IKUNHEIHIE/ServerFleet/actions/runs/43366199340) 成功；[发布回执](https://github.com/IKUNHEIHIE/ServerFleet/pull/31#issuecomment-6844271811)记录产物与摘要。
- Release 含双架构 tar.gz、SHA256SUMS、build-manifest.json；交叉构建不等于物理 ARM 验收。该 PR 的 COMMENT 审查不冒称独立 APPROVE。
- 旧 PR #25 测试计数、R34 审计与旧 FAIL/SKIP 保留原基线，不能挪作当前或未来 head 的检查结果。
