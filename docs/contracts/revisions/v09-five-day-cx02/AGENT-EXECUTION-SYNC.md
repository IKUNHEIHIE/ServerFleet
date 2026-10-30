# Agent持久执行阶段与重连对账（候选）

`apps/agent`新增`executeObserved`和`snapshots`，仍是私有应用组件，主入口没有启用WSS/执行profile。现有execute调用保持兼容。注册动作仍仅程序固定的Validate/Run，当前拒绝root/Shell/插件，状态对账不新增执行通道。

新执行先创建不可覆盖executionId目录及完整Offer绑定claim、同步文件和Linux目录，再通知accepted；通知失败保留占位且不产生新副作用。接着先写started.json并同步，再通知started，最后再次查ctx/过期后才Run。started表示已持久准备开始，不伪称外部副作用已完成。重放只回accepted(previouslySeen=true)及保存结果，不重新发started、不重新执行。缺完成记录始终UNKNOWN。

程序注册Run异常时持久UNKNOWN，不暴露panic文本。重复读取的结果增加enum与输出大小校验，不因私有文件可解JSON就相信任意枚举。TaskStarted/ack的观察回调只是事件适配，不能代替Controller当前授权/审批和Agent当前证书/能力准入；相关真实循环接线下一增量推进。

`snapshots`读取私有ledger并构造既有StateSyncReply：稳定executionId排序、最多扫描10000目录/返回1000条、逐条受控读和取消校验；超额返回truncated，Controller必须把它当作不完整对账，不得据此补跑未出现的执行。since边界包含同毫秒事件以免漏掉相同时间的记录。只返task/execution/result/started/updatedAt，不返参数或输出；尚未产生流式日志，lastOutputSequence保持0，不伪造日志确认。

无result为UNKNOWN；有started marker说明开始意图已持久；旧版本只有合法终态result的记录证明原注册调用已返回，可作为started依据，不修改历史。损坏claim/result/枚举、异常目录/符号链接、越界请求失败关闭，不返回部分“全量成功”。旧记录格式没有自动删除/压缩防重放，超额/并发写入期间不完整记录可能阻塞一次对账，需要实际运行时错误处理与后续归档协议；没有1核1GB承诺。

本机测试覆盖：ack/start发生时真实文件已存在且副作用计数仍0；两阶段通知失败后重开executor仍UNKNOWN且不跑；注册动作panic不重放；损坏终态拒绝；有界截断/取消。Windows仅逻辑/文件行为，Linux目录fsync、实际WSS接收→执行→对账链及完整DS组合仍需实际证据。
