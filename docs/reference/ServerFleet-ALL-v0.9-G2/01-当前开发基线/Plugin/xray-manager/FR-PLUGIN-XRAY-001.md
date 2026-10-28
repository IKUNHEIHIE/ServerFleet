# FR-PLUGIN-XRAY-001 — Xray 节点管理器（已批准）

**产品归属：** Server Fleet v0.9 规划，第一版唯一已批准的官方可选插件  
**状态：** 需求获批；原型与 OpenAPI 为设计，不代表 Xray-core 已真实安装  
**架构原则：** 插件按需安装，不预装于主控/Agent 的 Release。官方仓库 `Plugin/xray-manager/` 是计划目录，尚未给出正式仓库地址。实施前需核对既有插件、权限、任务、PKI ADR。

## 1. 产品目标与边界

- 在现有 Server Fleet 节点列表选择单个或多个在线 Linux 节点，安装、检测、更新、启停和按明确许可卸载 Xray-core。
- 支持 **VLESS + TLS + XTLS Vision**、**VLESS + REALITY + XTLS Vision** 两个版本化预设模板，以及用户自定义 Xray 配置（在服务器端由目标版本 Xray 实际验证）。
- 支持入站及用户管理、客户端连接配置导出、节点/入站/用户流量历史与阈值告警。
- 复用既有任务、证书、凭据、流量配额、通知、RBAC、AI 资源授权与审计；不为插件复制第二套队列与安全系统。
- 插件不是计费面板或订阅门户；首版不提供收费、流量套餐销售、自动购买或多实例编排。
- 一个节点默认最多管理一个受管 Xray 实例，实例可拥有多个入站。发现已有实例时只读检测，显式接管前不得覆盖其配置或二进制文件。

## 2. 页面与用户操作

### 2.1 插件中心

插件中心仅有一个已获批准的**目录条目**：Xray 节点管理器。首次进入**未安装**；需管理员确认来源、版本、权限、设备影响再选择“安装并启用”。启用后出现 `扩展能力 → Xray 节点管理器`；禁用后导航隐藏且不可启动新插件任务。仓库可切换官方/Fork；第三方源必须醒目标注来源和可信度。卸载插件不自动移除目标服务器的 Xray。

### 2.2 插件内部导航

`概览 | Xray 节点 | 配置管理 | 用户管理 | 流量统计 | 部署记录`

- 概览：已受管/待接管节点数，运行中/异常数，今日、本期代理流量，最近失败任务。
- Xray 节点：多选节点，批量安装、查看进程/服务状态、版本、入站数量、升级、重启、卸载节点 Xray（与卸载插件完全分开）。
- 配置管理：模板选择、节点参数覆盖、敏感凭据引用、自定义 JSON、配置预检查、差异预览、发布与版本回滚。
- 用户管理：按受管实例/入站增删改停用用户；生成独立身份；查看脱敏凭据，单次受控导出可复制链接和二维码。
- 流量统计：节点、入站、用户的上传、下载、时间范围、采样完整性、进程重启计数段与数据缺口。与主系统网卡统计分离。
- 部署记录：复用 Server Fleet 任务ID；展示目标快照、分批策略、逐节点阶段/结果、错误日志和回滚结果。

### 2.3 安装向导

1. 选在线节点/服务组，固定具体节点快照，逐目标 RBAC 校验。
2. **只读预检**：操作系统、CPU 架构、可用空间、端口占用、权限策略、现有 Xray/Nginx/其他服务、网络访问、官方版本/包签名或哈希。
3. 若发现已有实例：默认“跳过，不接管”；可选“请求接管”，明确展示现有路径/服务/影响并创建备份，禁止未经确认覆盖。
4. 选择明确的 Xray 目标版本（正式版推荐经过兼容测试的稳定版本，不能只用浮动 latest）；确认来源，选金丝雀/分批/失败熔断策略。
5. 展示风险与检查结果，管理员确认后创建幂等后台安装任务；不可用/冲突节点不可隐式安装。
6. 安装后执行版本确认、服务启动、配置测试与适当的本机/远程健康检查。失败显示诊断，并按适用程度恢复旧实例。
7. 不允许任意脚本由插件直接以无限 root 权限运行；只允许控制器权限策略授权的特定软件安装/服务/文件操作能力。

### 2.4 配置发布向导

1. 模板：`vless-tls-vision` / `vless-reality-vision` / `custom-json`。
2. 指定目标节点或服务组，快照保存；可以按节点覆盖域名、监听地址、端口、证书引用、REALITY 密钥引用等。
3. 标准模板校验：协议和目标 Xray 版本兼容性、VLESS 用户身份和 flow、端口、防火墙提示、TLS 证书 SAN/有效期、REALITY target/serverNames/shortIds/密钥有效性及可达性。
4. 自定义 JSON 保留原配置结构，应用最小安全审查、禁公网 API、关键文件访问边界和特定危险指令警告；不擅自重写用户配置。
5. 先在目标机器上使用**目标 Xray 版本**执行 `xray run -test -c <staged-config>`；通过不意味着进程/端口/连通性必然正常。
6. 显示配置差异（秘密脱敏），管理员确认，再按节点部署策略写入临时文件、备份旧配置、原子替换、启动/重载或受控重启，并做服务健康检查。
7. 如果安装/发布失败，保留每节点结果，按照旧配置及版本恢复；恢复失败须显示人工修复入口，不能虚构全量自动回滚。
8. 允许历史记录、配置比对、指定修订回滚，使用 revision / `If-Match` 防并发覆盖；各节点单独结果，不将部分成功显示为全部成功。

### 2.5 两个官方模板的关键规则

- **VLESS + TLS + Vision**：TCP/RAW + TLS、合法 `xtls-rprx-vision` flow、VLESS 用户ID、域名与证书关联、证书私钥受控访问。避免与现有 Nginx 等服务监听同一个地址/端口冲突。
- **VLESS + REALITY + Vision**：TCP/RAW + REALITY、目标服务的 `target`、允许 SNI 的 `serverNames`、`shortIds`、服务端密钥引用，以及向客户端导出相应的验证/公钥参数；密钥逐实例独立，不复制固定私人密钥到所有节点。
- 两种模板都必须根据实际 Xray 目标版本生成配置，不能假定所有历史版本字段相同。官方新 VLESS 入站示例使用 `settings.users`；部分旧示例使用 `clients`，模板引擎不得盲目通用化。
- REALITY 非认证连接可能转发至 `target`，须显示潜在滥用风险并检查目标/限流；不得自动猜测伪装网站或承诺规避网络封锁。
- 无论协议，默认管理 API 仅本机回环监听，禁止直接暴露到公网上；不得默认关闭系统防火墙。

### 2.6 用户管理与连接资料

- 业务用户具有 Server Fleet 内部不变 `userId`、Xray 统计标识 `email`（可使用非真实邮件的唯一内部标识）、服务端 VLESS `id` 凭据引用、入站、流控、启用状态。显示名与统计标识分开，修改显示名不导致统计链中断。
- `HandlerService` 可对支持的入站增加/移除用户，但实时修改不等于磁盘配置持久化；必须持久更新受管配置并做状态对账。停用用户可能需要关闭现有连接，不能仅靠前端开关表示即时断连。
- 客户端导出时应具备查看/导出机密权限，防止自动显示 UUID、REALITY 密钥等敏感字段；链接/二维码不写审计日志，也不分享给无权用户。
- 用户级统计依赖 Xray `email` 和 policy 统计开关；未配置时准确标记“不可用/未启用”，不能将缺失显示为 0。

## 3. 流量监控规范

- **来源**：Xray `StatsService` / 内部计数器；按用户 `user>>>[email]>>>traffic>>>uplink|downlink`、入站 `inbound>>>[tag]>>>traffic>>>uplink|downlink`、出站对应 tag，可用性按实际支持的配置决定。
- 开启计数需有 `stats: {}` 并在 policy 启用对应 `statsUserUplink/Downlink` 或 `statsInboundUplink/Downlink` 等；管理 API 在本机回环调用。
- **保存**：采样数据入主系统允许的时序存储，不依赖 Xray 内存计数器长期保存；每次采样记录采样时刻、实例/进程计数段标识、原始字节值、重置事件、来源健康、数据完整性。
- **重置语义**：不可直接把 QueryStats(reset=true) 的数值当作永久累计值。采集器要统一选择一种读取/复位策略，确保多个读者不会互相清空计数；重启、计数器溢出、重置或采集丢失需处理为新计数段/缺口。
- **方向语义**：标注 Xray 上下行的视角；节点代理流量、用户流量、网卡计费流量不得简单相加。以节点累计、入站累计、用户累计分别呈现，不能直接当作云厂商账单。
- **周期统计**：默认 60 秒间隔，可调；支持日/月区间和按用户/节点/入站过滤，阈值触发复用 QQ Bot / SMTP 告警；未采集到数据应显示“未知/缺失”，而非 0。
- **配额联动**：Xray 代理消耗可作为主控节点流量配额面板的“相关参考”，不能替代主机总流量或账单额度；默认只通知不自动断网/停服。

## 4. 任务与权限边界

- 全部变更继承主控用户身份、节点 RBAC、AI 会话明确资源范围、三级审批、操作类型许可、审计和异步作业模型。
- 插件不能使用任意 shell、SSH 密钥或主控私有 CA 密钥绕过授权；敏感路径必须限制到白名单及当前受管实例。
- 批量安装/下发采用已批准的金丝雀、分批、失败熔断、任务超时、幂等键和目标节点固定快照机制。
- 安装/配置/更新/用户操作须审计 actor、nodeId、targetSnapshot、revision、action、result、jobId，不记录明文 UUID、私钥、配置中的敏感字段。
- 运行时 HandlerService 权限独立，不允许通过插件自动开放所有入站管理 API；所有调用由本地受控执行器发起。
- 对存量未托管 Xray 的处理是检测后“请求接管”，且需得到特定用户许可；删除插件默认**保留**节点上的实例/配置/数据。卸载节点 Xray 是独立高风险操作，需确认可恢复的备份范围。
- 插件版本/来源/签名校验、热生命周期与可用性由原 `FR-PLUGIN-004` 治理，不因引入 Xray 插件取消默认禁用的安全策略。

## 5. 接口规范与数据模型

使用命名空间 `/api/v1/plugins/xray-manager`，明确这属于 **Server Fleet 管理 REST API**，而非 Xray-core 原生 gRPC 接口。对应正式草案：`Xray-Manager-OpenAPI.yaml`，同步纳入主契约 `ServerFleet-API-Contract-v0.9.yaml`。

核心类型：`XrayInstance`、`XrayTemplate`、`XrayInstallationPreview`、`XrayManagedConfiguration`、`XrayDeploymentPreview`、`XrayManagedUser`、`XrayTrafficSample`、`XrayJobRef`、`XrayError`。敏感材料采用 `credentialRef` 而非 API GET 明文。

必须明确的拒绝错误：`NOT_AUTHORIZED`、`NODE_OFFLINE`、`ALREADY_MANAGED`、`EXISTING_UNMANAGED_INSTANCE`、`PORT_CONFLICT`、`UNSUPPORTED_PLATFORM`、`UNSUPPORTED_XRAY_VERSION`、`CONFIG_TEST_FAILED`、`CONFIG_REVISION_CONFLICT`、`SECRET_UNAVAILABLE`、`TASK_EXPIRED`、`FAILED_SAFETY_CHECK`、`STATS_UNAVAILABLE`。

所有后台任务返回 jobId 并复用 Server Fleet 统一状态查询/审计；不在插件内造第二套作业引擎。请求带幂等键、预检查与目标快照；任务确认凭证一次性与具体操作、节点和配置摘要绑定。

## 6. MVP 验收清单

1. 官方插件目录初始只有本插件，未安装时插件业务入口不存在；按需安装/启用后可见，禁用后隐藏；卸载不影响核心功能。
2. 两台不同架构在线节点可独立完成安装预检查与受控安装；有现有 Xray 时未经接管确认绝不覆盖。
3. 两个模板能在锁定目标 Xray 版本下通过配置测试并实际启动；配置影响已有 Nginx/端口则阻止。
4. 自定义 JSON 失败时不会破坏旧配置；配置发布和回滚均显示逐节点结果与错误说明。
5. 按目标节点/入站/用户显示上/下行流量；Xray 重启后历史不清空；缺失采样明确显示缺口。
6. 用户变更持久化，客户端链接导出需机密查看权限；不将秘密写入普通读接口、日志或 AI 提示词。
7. AI 即便完全访问也不得操作未拖入当前会话的节点；插件没有未经授权的 root、SSH、私钥和任意管理 API 权限。
8. 安装/发布分批失败时阻止后续任务，并对已成功节点给出清晰状态；恢复失败不误报成功。
9. 卸载主控插件时节点 Xray 保持运行；移除节点 Xray 需要独立的强确认与审计。
10. 与现有 DNS、TLS、凭据、网关、流量配额及通知模块互不覆盖、单一数据源。

## 7. 官方参考文档（2026-10-29 查阅）

- Xray VLESS inbound: https://xtls.github.io/en/config/inbounds/vless.html
- REALITY: https://xtls.github.io/en/config/transports/reality.html
- Xray TLS: https://xtls.github.io/en/config/transports/tls.html
- Xray Stats: https://xtls.github.io/en/config/stats.html
- Xray Policy: https://xtls.github.io/en/config/policy.html
- Xray API: https://xtls.github.io/en/config/api.html
- Xray CLI: https://xtls.github.io/en/document/command
- Official releases: https://github.com/XTLS/Xray-core/releases

实施时应根据锁定的 Xray-core 版本再次核对上游文档、实际 CLI/help 和该版本配置校验结果。
