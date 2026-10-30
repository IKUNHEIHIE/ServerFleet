# Agent连接循环与本机WSS执行实验（候选）

`apps/agent/session.go`实现单条已验证主动连接的应用循环；不在main启动、不添加根WSS依赖，不接受URL/任意命令或跳过TLS选项。经审dialer须用AgentTLSConfig完成TLS1.3+mTLS/ServerName检查，从真实升级响应取得唯一connectionId及ConnectionState；循环再核对完整Root SPKI pin/证书有效期，Current/Admit/Probe均强制注入且无默认成功。Current必须读当前本地信任/撤销/profile权威，不能将缓存TLS状态当永久准入。

AgentHello如实使用进程实际GOOS/GOARCH、配置版本/bootId及注册动作schema；ControllerHello必须被既有有界Proto/ConnectionGuard校验，版本、帧上限、能力子集及1—60秒心跳有界。不伪造lastAckedControllerSequence，当前为0。每次收发都重新Current核验；单reader+序列化writer；真实WSS Ping/Pong与心跳并行读取，任何来源/消息/Probe失败关闭连接、取消在途工作。

TaskOffer同时要求协商capability、固定本地schema/参数Validate、normal权限、无plugin、当前Admit及executor全字段/TTL校验。1—5个本地并发上限，忙时拒绝新占位，不另建任务队列。running中的同executionId只确认已持久的原占位；未完成占位时返回固定ControlError，不把尚未持久的请求标已接受；换绑定拒绝。完成记录重复回Ack(previouslySeen)及真实终态，不再start或执行。

新执行只在claim持久后发Ack、started marker持久后发TaskStarted，再调用固定注册能力。TaskCancel只按当前taskId+executionId取消对应工作；不能声明取消等于回滚，副作用不明保留UNKNOWN。StateSync返回持久快照，只有本循环确知仍活跃且未完成的worker可覆盖成UNSPECIFIED+实际开始意图；重启后不得把磁盘旧记录当live。未知/损坏状态失败关闭。

当前本增量只回Ack/Started/Finished/Heartbeat/StateSync，尚未转发日志chunks：本地保存的有界输出未转发时Finished.OutputTruncated=true，不假装完整日志。持久日志游标及部分日志接线下一增量推进，lastOutputSequence仍0。没有修改Proto或将TaskOutput偷作文件传输。

断开后等待合作worker最多5秒；未退出返回独立errAgentWorkersUnknown，不自动重连或重新派发。程序注册动作/本地Current/Admit/Probe须遵守ctx，Go函数回调不能强杀不合作执行器；正式PRIV进程隔离/profile/安装器仍缺，不得据此开放root/Shell/插件或高权限真实节点。所有资源执行仍须Controller唯一授权/tasks/approval/audit，局部传输证明不能替代DS接缝。

## 实际证据及复现

`node tests/poc/v09/platform/run.mjs`现在把当前apps/agent全部Go源原样复制到自己忽略的隔离子模块，并记录agent-source-hashes.json，编译同一session/executor实现；不是另写一个测试客户端替代Agent。依赖仍固定coder/websocket1.8.15/wazero1.12.0/protobuf1.36.12及原sum，根三份锁前后哈希保持不变。Context7 resolve/query coder后核对本地固定1.8.15的Ping/Read/SetReadLimit/CloseNow，生产依赖接收仍属S04。

真实本机WSS+mTLS+二进制Protobuf：ControllerLinks→Agent应用循环→持久executor→一次真实临时文件副作用→Finished；真实Ping/Pong心跳；断线重新主动连接、新connectionId序列、StateSync保持终态、重发同offer无start/副作用；当前Agent证书被权威映射吊销后断线并取消在途能力，磁盘结果UNKNOWN。不使用真实密钥/外部节点。

根模块另测真实本机TLS握手+明确的FrameSocket通道double，覆盖本地撤权、取消/live快照、重复完成、错Root pin/缺gate不发Hello。Controller业务准入/持久Node/Task审计仍是明确fixture；注册动作只是测试文件操作，不是被准入的生产capability。Windows证据不等于Linux、安装器、正式接入审批或全链v0.9。
