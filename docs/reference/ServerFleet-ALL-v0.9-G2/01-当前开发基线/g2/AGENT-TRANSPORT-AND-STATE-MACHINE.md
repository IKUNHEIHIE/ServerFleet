# G2 Agent 通信与任务状态机候选规范

> 状态：**G2 候选，未冻结、未通过传输 PoC**。优先级：已批准 ADR-BOOT-001、PRIV-001、TASK-001、PKI-OPS-001 高于本稿的任何冲突描述。`agent.proto` 是待评审契约，不宣称已生成 Go/TypeScript SDK。

## 1. 控制通道与身份

- 初次注册：由 Agent 经可信 HTTPS，核对管理员明确提供的 Root CA SPKI SHA-256 指纹并提交 CSR。默认 10 分钟、一次性 256 位注册令牌；令牌不能用作后续任务授权。注册成功返回**证书链而非私钥**。重复请求只允许相同 requestId + CSR 公钥、在可证明密钥持有前提下幂等重取；不得为新的 CSR 复用已消耗令牌。
- 已注册节点：Agent 主动建立到固定主控域名的 **WSS + mTLS** 会话；主控将 TLS 对端证书映射到存储的 nodeId，并校验证书有效期、Node ID、吊销/停用状态及策略版本。消息中的 `node_id` 仅供对账，不能替代证书验证。
- 初始帧必须是 `AgentHello`。服务端完成 major/minor 版本和能力协商后发送 `ControllerHello`。不兼容 major 立即关闭；未知 capability 默认不可用，不降级安全检查。
- 二进制 WebSocket 帧每帧承载一个 protobuf `Envelope`；最大发送帧、输出单片、每秒消息数、连接心跳与队列水位应通过 PoC 冻结。建议初始上限（**待测**）：1 MiB/帧、64 KiB 输出片、30 秒心跳。
- 不通过主控转发客户端网站流量。文件字节传输另建受控流通道，**不得**把二进制文件混入任务日志 `TaskOutput`。

## 2. 控制消息与方向

| 消息 | 方向 | 效果 |
|---|---|---|
| AgentHello / ControllerHello | Agent→主控 / 主控→Agent | 版本、能力和心跳约束协商 |
| Heartbeat / MetricsBatch | Agent→主控 | 上报采集时间；断线后旧指标不得显示为实时值 |
| TaskOffer | 主控→Agent | **唯一任务引擎**创建、授权且持久化执行 ID 后下发 |
| TaskAck / TaskStarted / TaskOutput / TaskFinished | Agent→主控 | 持久去重 ACK、开始、受限日志、终态 |
| TaskCancel | 主控→Agent | 最佳努力终止；不能撤销已发生副作用 |
| StateSyncRequest / StateSyncReply | 主控→Agent / Agent→主控 | 断线/主控恢复后核对执行状态，不擅自重跑 |
| ControlError | 双向 | 明确错误码，不含密钥/原始环境变量 |

## 3. 任务状态机与禁止行为

**Controller 任务**：`awaiting_approval → queued → waiting_node/running → completed | partially_failed | failed | canceled | unknown | expired`；`paused` 只能影响尚未启动的批次。主控保存提交时 `targetNodeIdsSnapshot`、`planDigest`、授权与审批版本。操作前和每台执行前重新校验当前权限；服务组新成员不自动进入旧任务。

**每节点执行**：`queued → offered → acknowledged → running → succeeded/failed/canceled/timed_out/unknown`。Agent **在启动副作用进程前**持久记录 `execution_id` 与参数摘要。相同 ID 即使 WebSocket 重新发送也不得重复启动。`TaskAck` 仅代表接受/拒绝，不能代表命令成功。

- 已经发出但没有可靠完成回执，或主控/Agent 崩溃导致状态模糊：标记 `unknown`，人工核对现场后另建任务；不可自动重试 root/修改操作。
- 离线、未启动任务默认最多等 15 分钟；到期不得上线后补执行。已执行中任务由实际超时、取消和未知结果处理。
- 用户取消只阻止新批次和请求中断现有进程，不能承诺原子回滚。分批默认 root/修改先 1 台、再最多 5 台；首批失败不启动后批，已启动的本批任务收尾。
- `plan_digest_sha256`、`action_id` 和 `canonical_parameters_json` 必须由 Controller 的已注册操作及规范化编码得到；Agent 仍需校验动作类型、受管路径、执行身份、参数上限。不要在参数或日志中塞入私钥、恢复密钥和高敏凭证。
- 不能仅凭主控的 `scope_revision` 在 Agent 上离线校验权限；最终授权来源为 Controller 的强制校验，主控不在线时 Agent 不可自行扩大任务能力。

## 4. 恢复与安全边界

- Agent 身份私钥本地持有。重新安装按 ADR-BOOT-001 吊销/替换身份，不按同名直接信任。
- 恢复主控后进入**隔离只核对模式**，先核对信任根/签发状态、授权和任务，再开放调度。备份落后导致的吊销信息缺口不得自动恢复旧身份。
- 插件禁用、用户撤权、会话作用域变化、完全访问到期：主控不能再派发新的插件任务或后续批次；运行中任务必须执行能力允许的安全取消/收尾。旧 ID、旧审批不可重放。
- 本文件未定义专用文件流、更新二进制分块、签名根管理协议；这些项目进入 G2 扩展合同清单，不能由开发者从任务日志通道自行实现。

## 5. 必须通过的负向测试

1. 伪造 `node_id` 与证书身份不符、被吊销证书、过期证书、未知 major、未授权 capability → 拒绝并审计。
2. 同一 `execution_id` 重复 Offer、ACK 丢失后重发、Agent 在开始前/后崩溃 → 不二次执行修改任务。
3. 离线超过 15 分钟再重连 → 过期任务不执行。
4. `TaskFinished` 未送达 → 状态 `unknown` 而非成功；人工检查后使用新任务 ID。
5. 大帧/日志泛洪、恶意 metrics 值、无效 protobuf 帧 → 限流、限内存、拒绝且不拖垮 Controller。
6. 主控恢复/插件禁用/AI 撤权后旧任务试图执行 → 不允许新派发。

## 6. 尚需冻结的参数

| 参数 | 候选值 | 冻结条件 |
|---|---|---|
| WebSocket 最大 protobuf 帧 | 1 MiB | POC-07 内存与恶意帧测试 |
| 单片 stdout/stderr 最大长度 | 64 KiB | 流控、日志截断测试 |
| 心跳间隔 | 30 秒 | 10/30/100 节点真实丢包/空闲测试 |
| 事件保留/游标失效 | 任务日志 7 天建议 | SQLite/磁盘测试，410 或明确 gap 语义 |
| 采集与流量写入频率 | 1 分钟建议 | 磁盘 I/O 和精度 PoC |
| Go/TypeScript 生成版本 | 待确定 | buf lint/breaking、代码生成与跨语言往返测试 |
