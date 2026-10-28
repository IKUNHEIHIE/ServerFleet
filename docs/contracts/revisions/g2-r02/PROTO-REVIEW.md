# M0-P1-r1 Protobuf 修订审查

状态：**工程修订的无wire变化有证据；协议安全语义仍候选**。本次不修改proto、gen/go、Buf配置、基线或锁文件。

## 实际差异与兼容

原始 `g2/proto/serverfleet/agent/v1/agent.proto` 对比工程 `proto/serverfleet/agent/v1/agent.proto`：仅新增 `option go_package = "serverfleet.local/serverfleet/gen/go/serverfleet/agent/v1;agentv1";` 和展开TaskOutput.Stream格式。测试剥离该option与空白后token完全一致。字段号1～7、oneof20～32、枚举、包、消息、注释约束均保留；无gRPC service。

原候选 `buf build` 可成功，原lint的问题是module路径，不是Proto语法不能编译；Go生成缺go_package/M映射的问题见原诊断。P1的根module路径是工具修订，go_package影响Go导入，不影响wire；未来module名变化需显式迁移生成物，不能编辑原始候选或为了消错更新兼容基线。

| 检查 | 证据范围 |
|---|---|
| 原始descriptor构建 + P1对原始WIRE breaking | 字段/编号/线格式兼容，不证明安全策略 |
| 现有STANDARD lint/format/build、FILE breaking against proto-baseline | 固定P1结构/格式基线，无基线更新 |
| 固定protoc-gen-go再生成一致性 | 当前Go生成物与源一致；gen/go最终字节不变 |
| Envelope13种oneof序列化往返 | Go支持每种已定义payload；无网络、无真正执行 |
| 截断wire拒绝 + 既有字段类型/复用编号变异拒绝 | 真实解码器/Buf拒绝，不是只检查文档文字 |
| 空Envelope、空ID、Privilege=0/999可解析 | **通过的局限证明**；必须有独立语义门禁，不能视为安全拒绝已实现 |

Go生成代码仅Controller/Agent/transport使用，Web依架构禁止直接消费Agent协议。因此不擅自增加TS生成目标/依赖。G2-C07原要求的TS生成、跨语言往返仍 **UNTESTED**，需通信owner澄清消费端与授权范围后由集成人修改共享生成配置。

## 字段到安全边界的审查

| 消息/字段 | 正确解释 | 当前缺口/待测试 |
|---|---|---|
| Envelope.node_id/connection_id/sequence | 对账与重放控制，不是身份凭证；身份来自验证过的mTLS证书+节点记录 | major=0、空payload/ID、未知oneof/enum、重放sequence、错nodeId、错方向必须拒绝；Protobuf会接受部分此类数据 |
| AgentHello/ControllerHello | 首帧与方向受限；major不兼容关闭，未知capability不可用 | AgentHello只有major范围，没有完整minor范围；能力子集/小版本演进和安全拒绝策略未冻结 |
| TaskOffer | task_id/execution_id稳定，action/schema/规范化参数/planDigest一致；Controller实时授权+审批后才能发，Agent落盘去重后才能启动 | 缺/空摘要、非法JSON、过期、超时0、Privilege=UNSPECIFIED/未知值、未授权动作/路径/身份需拒绝；不能添加签名票据/另造特权通道替代Controller |
| scope_revision/plugin_id | 提示任务授权上下文；不是可离线相信的授权票据 | 撤权、成员移除、插件禁用、审批过期后禁止新派发；竞态/重连无真实证据 |
| TaskAck/TaskStarted | ACK仅接收结果，Started才表示开始；也不能从Started推断最终成功 | durable记录与真正启动之间崩溃的unknown处理；重复ID换参数拒绝；ACK丢失不重复执行 |
| TaskFinished.result/exit_code | result枚举是终态依据；UNSPECIFIED不是成功；exit_code默认0不能当作已观察到成功 | proto3标量无存在性，REST exitCode未观察到时应null；缺完成时间/结果组合须门禁；迟到结果审计核对 |
| ExecutionSnapshot.result+started | 不是完整执行状态机，不能凭空还原acknowledged/cancel_requested等状态 | 恢复与Controller持久记录核对；无充分证据unknown，不用默认enum0猜running/成功 |
| StateSyncReply.truncated | 核对结果不全，不能宣称“没有其他执行” | 无续页游标；需通信owner设计有界继续核对/窗口和重复顺序，未获批准不加字段 |
| TaskOutput/MetricPoint/ControlError | chunk/自由字符串/浮点均是不可信输入；输出截断不代表结果失败或成功 | 帧/单片/速率/内存、NaN/Inf/负指标、脱敏、背压与日志quota；禁止文件流混入输出 |
| expires_at_unix_ms/TaskCancel | TTL只限未开始；取消仅尽力，不承诺回滚 | 时钟偏差/断连后期限、进程组退出、900秒后重连、恢复隔离默认不派发待真实测试 |

同一WebSocket二进制帧约定只含一个Envelope，但普通Protobuf解码器不自动证明“只有一个有效逻辑消息”，重复oneof/拼接编码的接收规则需协议owner明确并做负向测试。不能由往返成功推导帧长度/方向/认证/审批/去重/恢复已经实现。

## 不在本轮实施的具体后续需求

优先设计独立语义校验器和受控状态门禁，不修改wire即可拒绝空/非法值；持久去重、mTLS/证书吊销、身份绑定、主版本/能力白名单、限额和恢复核对必须真实PoC。若需要StateSync分页/显式执行状态、minor协商或标量存在性字段，先由通信+安全/任务owner给出兼容字段号和迁移方案，再获总控明确扩围；本轮没有任何proto字段提案被当作已批准变更。
