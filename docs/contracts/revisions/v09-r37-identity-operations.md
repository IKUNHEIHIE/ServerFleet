# R37 身份与运维接口差异

主控 chatgpt-repair-20261112 协调；新增独立候选，不覆写已批准 ADR 或历史 G2/HTTP/Proto 文件。

- 新增 GET /readyz、POST /api/v1/agent/identity/renew，分别为固定类别就绪和直接 mTLS 身份生命周期端口；没有新增 WSS 消息或 Proto 字段。
- 入网 token 新增可选 replaceNodeId；仅管理员且对目标有 nodes.revoke，拒绝暂停/吊销节点自动复活。
- 批准新增可选 grantRead，省略保持旧行为；Web明确确认后只授当前账户单节点 nodes.read。批准与grant是可恢复的两阶段，重复批准只补未完成步骤，不重复签发或复活已撤销grant。
- 节点 capabilities/diagnostics 保留已有字段，新增 recorded 能力快照、agentVersion 和 agent 有界历史；缺失观察不伪造事实，读取继续按 nodes.read。
- 固定 Host/Origin 诊断码与 X-SF-Diagnostic-Code 不返回期望配置或秘密；会话仍30分钟idle/8小时绝对期限。
- 原49迁移SQL/checksum保持；追加ds/0050_node_diagnostics。身份续期JSON新增可选内部字段可读取旧Version1，不削弱未知版本拒绝。

升级先Controller后Agent；旧Agent保持协议兼容但不会自动续期，需在原身份到期前替换。已过期实例走显式替换审批并保留执行账本。签名、一键安装、UNKNOWN、运行权威隔离和任务审批边界均不放宽。实际检查与接收证据留在精确head PR评论。
