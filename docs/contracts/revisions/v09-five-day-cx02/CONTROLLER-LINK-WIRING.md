# Controller Agent连接接线候选

`transport.ControllerLinks`实现既有Sender；NewControllerLinks要求PeerVerifier、协议/语义/原始帧限额、1–60秒心跳、1–10000连接上限，以及Handshake/Admit/Event/Disconnected四个可信回调，无默认通过。Serve仅消费由TLS监听器得到的ConnectionState和通过已审WSS适配器得到的FrameSocket。FrameSocket为Read(ctx)(binary,raw,error)/Write(ctx,raw)/Close，适配必须SetReadLimit后再Read、禁压缩、只写binary、取消/关闭中断IO；库依赖S04接收前根工程不import第三方库。

升级请求由独立Agent监听器处理，拒绝所有Origin/Cookie/Authorization头（包括空值），TLS1.3强制mTLS；监听器还须限速/限制待握手连接。NewConnectionID用crypto随机产生128位ID，经升级响应X-SF-Connection-ID交给Agent，不能接受帧的connectionId作为绑定。升级后使用服务生存期ctx（不用Hijack后的request ctx）。这些精确适配步骤已在隔离WSS测试使用固定coder/websocket v1.8.15实际运行；未写主入口未接收依赖。

5秒AgentHello期限、服务器能力目录与Agent声明求交、ControllerHello确认；之后每帧有界Proto/方向/阶段/序列/绑定检查。每次事件和发送都查询当前证书指纹/序号/启用/撤销/信任世代；不缓存登录时授权。Event必须由apps组合映射到DS节点/任务事务，成功返回前完成持久化；transport不导入nodes/tasks。Admit必须核验sole tasks的现时授权/审批/任务/AI范围/插件状态，成功写WSS也不等于任务执行成功。写失败标ErrDeliveryUnknown，调度只以原executionId对账，不能无条件重排。

同节点新连接替换时，旧事件回调/发送与替换串行，旧连接关闭；旧disconnect不把新连接置离线。回调不能递归Serve，须响应取消；Event可以Send，registry锁不跨业务/IO。构造复制Action schema目录；每次Send复制Envelope，覆盖连接元数据，拒绝绕过握手及未授予action。Read 3个心跳周期超时断开，无盲目重连重发队列。Agent主动重连/安装身份、执行事件、持久连接账本仍需后续接线，不由hub虚构。

实际PoC：本机WSS+mTLS+二进制Proto，生产hub握手、受控offer、ack回调、替换关闭旧连接、旧disconnect不污染新连接、长连接吊销后收发拒绝；回调/Admit为明确fixture，不是实际DS授权/执行验收。根依赖文件hash未变。该候选不冻结Proto/G2，不代表Linux/1C1G/正式PKI原文或真实Agent准入通过。
