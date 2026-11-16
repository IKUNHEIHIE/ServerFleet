# 当前实现与交付状态

核对日期：**2026-11-16**。这是当前状态汇总入口；历史进度与任务单不替代本文。后续代码合并须重新核对更新。

## 2026-11-16 节点接入、整机文件能力与互传修复（已合并、已部署测试站）

本轮以 `71dbca3` 为核对基线，合并 PR #41、#43、#44 与 #46–#52；下方 11-15 补丁段为历史事实，最新状态以本节为准。

- 安装：控制台输出短命令（下载并校验 `/install/agent.sh` 摘要），脚本创建专用 `serverfleet` 用户 + systemd；追加 `--system-files` 为显式整机模式（root 运行、`-file-system-root`）。安装器修复了 runner 路径覆盖与重跑 `ETXTBSY`（临时文件 + 原子 install）。
- 节点：`aliyun-hk-1b`、`azure-20-215` 在线，当前以整机模式运行发布版预编译 agent（`a18f1228`）；`wentian-host-01` 在线但为旧 agent（沙箱、无硬件字段，重装后补齐）。首个 Bootstrap 管理员覆盖当前节点授权不变。
- 文件互传：修复帧字段上限（64→2048，目录列表不再 `FRAME_FIELD_LIMIT`）与暂存目录跨挂载点发布（`EXDEV`，改为目标同目录 `.sf-<transfer>`）；实测 `/etc` 列表、跨节点复制（sha256 一致）、上传、删除均通过。
- 硬件与流量：AgentHello 9–12 上报 CPU 型号/核数/内存/磁盘；`GET /traffic-usage` 提供本月上下行；详情页移除账户授权与协议/能力清单。
- 监控：节点资源与节点列表同为 5 秒刷新，指标显示“采样 时间”而非固定“无采样时间”。
- 前端结构：文件互传拆为 `file-pane.tsx` / `transfer-history.tsx` / `files-page.tsx`，传输历史收进页头弹窗。
- 验证：`check:web` 54 文件 / 482 用例、`check:contracts`、`go test -race ./apps/controller/server/` 通过；PR #46/#47/#48/#49/#50/#51/#52 精确 head 的 CI 全绿。
- 边界：整机模式以 root 运行文件服务（显式选择）；浏览器点击未自动化，接口层已实测；G2 全量验收仍未记录完成。

## 2026-11-16 DNS 独立可用（未提交、已部署测试站）

- 账号：控制台直接粘贴 Cloudflare API Token，自动创建 DNS 专用凭据（kind `cloudflare_token`、scope `dns`），账号只保存引用与凭据修订号；轮换同样走 Token。账号管理位于“系统设置 → DNS 账号”，DNS 页固定使用默认账号。
- Zone：`GET /dns/accounts/{id}/provider-zones` 实时读取账号下可导入 Zone；`POST /dns/accounts/{id}/zones/sync` 按服务商 Zone ID 导入；`POST /dns/zones/{id}/records/sync` 拉取记录替换本地缓存（保留 A/AAAA/CNAME/TXT/MX，其余跳过，不修改远端）。
- 凭据借用：独立 DNS 请求不再依赖 ACME 配置；服务端为每次请求建立一次性借用窗口（模块 `dns`、账号绑定的修订号、窗口外拒绝），远端写操作仍走统一审批任务。
- 迁移：追加 `ds/0091_dns_credential_revision`（`dns_accounts.credential_revision`，默认 0）；历史账号需“轮换”重新绑定后才能通过供应商校验。
- 验证：`go test ./internal/dns/...`、`go vet`、`check:web` 54 文件 / 484 用例通过；测试站真实账号（3 个 Zone）检查通过，wentian.org 导入并刷新出 11 条真实记录。

## 2026-11-16 AI 运维端点接入（未提交、已部署测试站，已联调通过）

- 端点：Controller 新增 `-ai-text-endpoint` 私有启动参数；测试站当前指向 StepFun `https://api.stepfun.com/step_plan/v1/responses`，模型 `step-5-preview`（模型 ID 由模型档案决定，未配置时保持默认 OpenAI）。
- 修复：流式解析忽略 `reasoning_text.*` / `reasoning_summary_*` 推理事件；补齐 `ai.use/ai.manage/ai.approve` 权限（此前管理员也被锁）；消息提交补 `Idempotency-Key`（此前真实 Controller 必然 400）；审批事件 404 显示为“当前 Controller 未提供”，不再当作加载失败。
- 配置：测试站已创建 `ai_api_key` 凭据（StepFun）与模型档案；模型配置已迁入“系统设置 → AI 模型”。
- 联调：浏览器新建 AI 会话 → 发送消息 → 显式生成，服务端流式调用 StepFun 并落库助手回复，事件 `ai.generation.state=succeeded`。
- 2026-11-17 交互调整：移除“用量与预算”界面并在生成前停用预算拦截；模型配置迁入系统设置；AI 运维入口即聊天窗口（左侧历史会话、中间对话、右侧资源拖选），发送消息后自动生成回复；授权节点默认包含读取/命令/受控执行/root 任务动作。
- 兼容修复：OpenAI 兼容网关要求历史 assistant 消息以 `output_text` 分段提交，已修正（此前多轮对话会 503）。新增 `GET /api/v1/ai/sessions` 列出当前用户会话。
- 布局参考宝塔面板：左侧扁平对话列表（搜索/刷新）、中间满高聊天（发送即自动回复）、右侧节点资源拖选；移除大幅页头与独立状态卡，状态并入会话顶栏。
- 2026-11-17 对话区排版：消息列表撑满可用高度、输入框贴底；发送改为输入框内右侧纸飞机按钮（Enter 发送 / Shift+Enter 换行）；已校验的助手回复逐字流式呈现，生成等待期间显示思考占位。
- 2026-11-17 交互补丁（未提交、已部署测试站）：对话输入区增加“对话 / 执行任务”，执行模式直接创建可自动推进的 Agent 运行并切到执行过程；DNS 记录增删改改走 DNS 账号模块的授权/审计接口，不再依赖统一任务部署；测试站已生成 ACME 账户密钥与部署配置并挂载 `-acme-deployment`，签发与续签配置不再 503；系统设置“AI 模型”内可直接新增 AI API Key；统计卡片改为随鼠标移动的动态渐变，并增加蓝/绿/青/琥珀等色调。
- 2026-11-17 权限兼容：补充 UI 权限键到后端权限键的别名（证书、监控、配额、备份、更新、插件、文件、节点接入等），避免管理员界面误显示“无权限”。
- 2026-11-17 实机验收补充：真实管理员会话下登录、节点、任务、凭据、DNS、监控、证书读取与 AI 会话接口均返回 200（此前页面 403 未复现）；DNS 与 ACME 改为各自独立 Provider，网页 DNS 写入不再落入 ACME 任务作用域；DNS 记录名统一为 FQDN、TXT 内容去除供应商引号，真实 Cloudflare TXT 新增→读回→删除通过；`-acme-deployment` 因 drop-in 字母序被覆盖的问题已改为最后加载，证书公共配置返回 configured=true；AI 执行模式在人工批准后完成 `nodes.read` 并读取真实节点状态，运行 completed。临时测试记录已清理。

## 2026-11-15 节点管理补丁（未提交、已部署）

用户追加批准在 awskr 安装 C 编译器、验证后部署线上 VPS。本轮已安装 build-essential/GCC 14.2；nodes、transport、Controller server、v09 集成相关 race 通过，显式 NGINX_BIN=/usr/sbin/nginx 的 platform race 370.362 秒、services race 6.022 秒通过。全仓 race 未获得完整成功结果；internal/updates 既有超时仍保留 FAIL，不宣称全仓全绿。

线上 Controller 与 Web 已更新为手工部署版本 0.9.3-node-management-20261115（基线 8f46c938ee4c7a9edb8ec9a2d34f3fc5c1fb022a 加未提交工作树，非正式 Release）。停止 Controller 后保存一致数据库、旧程序/Web、credential-keys、PKI、安装状态及服务配置；备份数据库与停服原库 SHA256 相同、两者 integrity_check=ok，备份身份只读 upgrade-check 通过。备份目录 /var/lib/serverfleet/backups/pre-node-management-20261115。原服务参数、Agent 二进制、Nginx 与身份材料保持原配置；新迁移 ds/0089_node_archives 已应用，50→51 项，原 5 条节点记录保留。新库 integrity_check=ok，公网 readyz 五项 ready；Controller 无自动重启，首页与 JavaScript SHA256 和构建产物一致。

本机 Agent 在停服期间重连预算耗尽，由原 systemd 自动重启一次后恢复 online 和持续心跳。部署前另外两条 Agent 连接尚未恢复，最后心跳停在停服时刻；原因未确定，不能归为历史无效节点或宣称所有节点恢复。浏览器连接器不可用，本次线上浏览器复验未完成；先前隔离 Controller 的 11 项浏览器验收结果仍仅适用于隔离环境。未提交、未推送 GitHub、未触发 GitHub CI。回滚必须恢复配套旧数据库和身份状态，不能让旧二进制直接打开新增迁移后的库。

### 部署前开发验证记录

awskr 的 fix/node-management-20261115 从 8f46c938ee4c7a9edb8ec9a2d34f3fc5c1fb022a 开发节点管理与接入体验。新增详情/元数据 CAS 编辑/归档删除、可见节点分类结果、一键分类、命名与安装一致及 5 秒状态刷新；工作区追加 ds/0089_node_archives 后共 51 项迁移。下方发布基线与旧 50 项迁移为历史交付事实，当前补丁不改变线上服务。本次 Web 完整检查 54 个文件、481 项测试及生产构建通过；完整接口契约、策略、依赖安全检查通过，节点/接入/Controller 集成和 vet/staticcheck 通过；独立最终代码复核未发现阻断问题。CGO race 因服务器缺 C 编译器无法启动，完整 Go 回归中本次追加迁移涉及的旧快照/计数已修正，v09、平台恢复、备份恢复及显式 NGINX_BIN=/usr/sbin/nginx 的真实网关专项通过。全仓 vet/staticcheck、模块 verify/tidy 通过。更新监督器启动与回滚超时在未改动基线 e2fa5e9 复现，保留 FAIL；权威丢失用例在当前树初始子进程就绪超时，旧基线单跑通过；该用例和监督器源码与旧基线字节一致，本次未改 internal/updates，仍保留未解决 FAIL。隔离真实 Controller 浏览器 11 项通过，涵盖授权、编辑/CAS、5 秒状态刷新、一键分类、自定义名称/令牌、文案/移动布局、归档删除；节点/状态使用独立 DB 合成数据，不代替真实 Agent 心跳或安装验收。证据：`reports/browser-fixture-20261114T185611933Z-f88e66/results.json`。未提交、推送或部署。

上述段落为部署前记录；缺编译器、未部署等状态已由本节部署事实取代，既有失败和验收边界仍保留。

## 代码与发布基线

| 项目 | 已核对事实 |
|---|---|
| 仓库 | 公有 `IKUNHEIHIE/ServerFleet` |
| 代码基线 | main `71dbca3c9d4a69890468d2f574362eaf6ea15c48`（2026-11-16，PR #52 合并后） |
| 最新发布 | [v0.9.2](https://github.com/IKUNHEIHIE/ServerFleet/releases/tag/v0.9.2)，Linux amd64/arm64 |
| 迁移 | 53 项（含 ds/0089_node_archives、ds/0090_node_live_metrics、工作区待提交的 ds/0091_dns_credential_revision）；历史 SQL/checksum 不变 |
| 工具 | Go 1.27.4、Node 24.22.1、npm 11.x；版本源见 [工具链](TOOLCHAIN.md) |
| 产品验收 | 核心代码已交付；完整 G2 冻结/原始需求全量验收未记录完成；实机验收开放 |

本轮 R40 只更新文档，不改变代码、契约、脚本、迁移、发行包或 tag，不产生新的运行验收。文档合并后 main SHA 会变化，上表记录对应代码/发布证据的基线，不是后续回退指令。

## 已实现与运行前提

| 范围 | 当前实现 | 运行/验收边界 |
|---|---|---|
| 身份、节点、权限 | 登录/RBAC/资源授权/审计、分类/服务组、mTLS 续期/过期重入网、诊断/readyz | 需 PKI/HTTPS；首个 Bootstrap 管理员拥有当前节点读取/编辑/暂停/删除权，其他账户须明确节点授权 |
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
