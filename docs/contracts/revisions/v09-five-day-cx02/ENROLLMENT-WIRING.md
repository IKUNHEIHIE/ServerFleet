# 短期接入、CSR与证书映射接线候选

transport.EnrollmentService自有状态仓储，SQLEnrollmentStore通过DS单一事务Read/Exec/RowsAffected接入；EnrollmentSchemaStatements返回独立业务表语句，不分配编号/自动建库。没有内存生产回退，缺表/行失败关闭。AppendAudit非nil须同Tx写入DS；nil仅真实保留持久出箱，不声称完成统一audit域消费。

精确程序端口：IssueToken(ctx, EnrollmentOperation, EnrollmentTokenInput)、Submit(ctx, AgentEnrollmentInput)、List(ctx,op)、Decide(ctx,op,approve)、Lookup(ctx,fingerprint)、Revoke(ctx,op,fingerprint)。operation的actor/action/enrollmentId/nodeId/fingerprint/revision来自可信Controller，强制gate逐字段核验当前真实管理员/统一任务或审批证明；字符串和客户端boolean不构成认证。gate不可默认通过，不在持有写事务时嵌套auth写事务；最终跨域提交世代闸门需总控与DS明确接收。

令牌256位只交付一次，持久摘要，60–600秒。首次Submit验pin和CSR签名，令牌一次绑定请求ID+CSR+版本+平台；相同输入只轮询当前状态，换CSR/换请求拒绝。未批准仅pending，无证书/节点权威注册成功。管理员按revision决定，签发证书与指纹/serial/nodeId/generation映射同一事务，失败无响应证书。token过期/吊销/换代后不能再次取得旧cert。重装必须显式指定已有nodeId和gate授权，批准新CSR时同事务吊销该节点旧证书；没有TOFU。

CertificateIssuer仅接受注入crypto.Signer、已验证CA链、精确root SPKI pin、generation和30分钟–24小时寿命。支持P256/Ed25519 CSR并验证签名，忽略CSR的全部subject/SAN/扩展；签发只含server nodeId的CN、digitalSignature和clientAuth，不能签CA或serverAuth，限于issuer有效期内。CSR私钥始终Agent本地生成，不上传，测试与API无私钥导出。此组件不批准在线Root CA/具体PKI profile；PKI-OPS缺失仍需新决策接收，不能自动生产启用。

Lookup直接适配现有PeerVerifier，长连接收发每次重查当前映射。持久Revoke并递增撤销序列，重开数据库不复活。备份回退必须先recovery隔离并与可信当前撤销证据对账，不能仅凭此聚合里的旧序列解除隔离。自动续签/信任包换代/安装器/SAN部署策略、全局限速、Node注册/审计同Tx与页面仍待接线；未修改原始OpenAPI或返回虚构已完成状态。

AgentEnrollResult新增certificatePem/trustGeneration/精确期限作为独立候选；既有路径与request字段输入保留。负责人接收后再将/api/v1/agent/enroll及管理员现有接入路径挂到相应TLS监听器，外部Agent入口不得接受Cookie/Authorization/Origin或共享管理员登录。当前没有默认开放的接入路由。
