# R32 产品、Web 与 API 同步

任务 V09-CLOUD-MONITOR-ALERT-01，独立目录／分支；继承已接收 R31。集成负责人协调当前模块、Web、迁移与此独立候选，原 G2／R01—R31 不覆写，不宣称冻结。使用与限制见 [持续监控与通知](../../development/MONITORING-ALERTS.md)。

| 类型 | 产品与 Web | REST／传输 |
|---|---|---|
| 新增 | 监控告警事件、等待渠道／实际接受／UNKNOWN、统一任务入口 | GET /api/v1/availability-monitors/{monitorId}/alerts，最近20项，当前 monitor.read |
| 修改 | 失败／恢复次数与通知选择真正执行，保存者绑定后台授权；删除实际生效 | GET/POST/PATCH availability-monitors 新增 automationOwnerId / automationRevision，failureCount / recoveryCount 表示阈值；DELETE /{monitorId} |
| 修改 | 明确由 Controller 探测，不把节点选择伪装成已执行 | 非空 sourceNodeId 返回400 unsupported_probe_source；SSRF／白名单照常执行 |
| 修改 | 配额重置日、时区、方向、多个阈值、提醒天数实际持久化；展示覆盖 | GET/PUT /api/v1/nodes/{nodeId}/traffic-quota；新增 coverage / dataComplete / automationOwnerId / automationRevision，无当前 usage 时省略 usedBytes / usedGb / source |
| 新增 | Agent 主机网卡计数进入持久流量与覆盖 | 使用原有 MetricPoint：network.rx_bytes、network.tx_bytes（bytes）和 network.interfaces.{sha256}（set=1）；相同观察时间，boot 来自已认证 Hello；不改 Proto 或生成文件 |
| 修改 | 通知选择加密凭据、当前修订与后台就绪状态，空配置仍可创建 | channels 增 credentialId / credentialRevision / automationOwnerId / backgroundReady；POST credentials 仍用 secretRef，但真实装配强制已有、当前所有者、正确用途的加密凭据 ID |
| 修改 | 测试只创建统一任务，投递状态独立展示 | POST test 返回真实 queued / taskId；deliveries 增 taskId。HTTP200不表示发送成功。标准 Controller 任务 notifications.send，仅私有意图摘要进入任务参数 |
| 新增 | 已确认失败的显式重试；UNKNOWN 导向现有人工处置 | POST /api/v1/notifications/deliveries/{deliveryId}/retry，空对象，202，当前 notifications.write／管理员／所有者／退避／次数与来源检查；UNKNOWN等返回409 |
| 删除 | 通知页把密码当 secretRef 的输入；SMTP明文选项；未实现供应商账单来源切换 | 凭据秘密只由原加密凭据 API 创建／轮换；本轮不增加真实云厂商抓账单接口 |

新增独立 OpenAPI `api/openapi/revisions/v09-r32/observability-candidate.yaml`。既有响应形状保留；新字段只读，Web 更新仅回传写字段。通知策略原端点不变，重试次数约束人工请求，不启用后台自动重发。手动探测已落库后不因发送失败返回“未执行”；并发配置／观察变化返回409。新增告警读、删除及重试严格校验 query／body，统一会话、Origin、CSRF／请求标记、恢复运行闸门与原模块权限仍由 Controller 强制。

ds/0084 通知意图／绑定／claim；ds/0085 监控保存者、阈值、恢复计数、渠道与 outbox；ds/0086 配额周期配置、Agent基线与覆盖。正式版本数45→48；历史版本选择器只增加排除本轮新版本，不改旧迁移 SQL、checksum 或来源夹具。

兼容规则：旧监控／配额重新保存授权，旧渠道重新绑定；旧 queued 无证据转 UNKNOWN。旧 Agent 没有流量指标时不报零；主机统计不冒充完整账单。节点侧远程探测未在本轮实现，明确展示 Controller 源。G2、真实平台、真实节点与完整产品验收仍独立开放。

安全补丁：旧 Go1.27.3 扫描命中 GO-2026-6859 / CVE-2026-97054；项目 Go 指令和固定工具版本同步升级1.27.4，无新增第三方依赖。修复依据与旧FAIL保留见[工具链](../../development/TOOLCHAIN.md)。
