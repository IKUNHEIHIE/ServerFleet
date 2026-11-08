# R12 加密备份、Controller本地任务与恢复候选

V09-CLOUD-CODE-COMPLETE-01，主控单写。UNTESTED：按用户要求集中编码后统一验收。现行原三候选/Proto字段号/gen/兼容基线/原始资料不变，新候选独立登记，不宣称G2冻结。

- 新增backups列表、统一任务创建、密钥确认文件/公开证明确认、密文下载。既有verify在明确配置加密备份时改为密文SHA核验，reason明确restore_not_proven；没有可信配置时不伪生成记录/成功。恢复preview仍缺实际fence时失败关闭，不能依据checkbox释放。
- 新Controller目标只由程序注册SubmitController创建，不经一般task-create HTTP选取，不建假node。物理旧task_targets.node_id列保存绑定Controller权威ID；公开投影targetType=controller、targetControllerId、空targetNodeIdsSnapshot，执行项controllerId、不冒充nodeId。原节点任务形状保持；旧严格节点消费者必须明确适配R12。Web备份/任务页面已做相应代码适配，尚待统一验证。
- 新任务动作backups.create/key.challenge只允许当前管理员和显式system生产器；AI/plugin不可使用，非任意root Shell。执行前及IO期间经统一任务/当前权限/运行隔离门，不新增调度器/SSH通道。任务审批策略不被本地目标绕过，审批者当前权限重查。
- 归档固定controller.db及七项当前私有材料，排除Root私钥/age恢复identity/网站私钥/令牌明文输出。age固定CLI和程序SHA，普通用户/0700/0600/single-link检查，流式加密，无线上明文tar。备份中凭据原有受保护状态不作为HTTP数据暴露。
- 新cx/0011_encrypted_backups、transport-owned cx/0012_recovery_revocations、cx/0013_recovery_reconciliation，共预计32项；已接收29项SQL/hash不变。不支持旧程序直接打开新库，回退需原快照和兼容程序。离线校验旧已知ledger，隔离目录内才可追加已登记新迁移。
- 离线stage验证独立cipher SHA、age完整认证、白名单/长度/文件SHA/PKI/凭据keyring/DB ledger及同快照身份/任务元数据，在任何DB操作前建立hold。reconcile必须另有新Intermediate与24小时内有效的离线Root签名授权，绑定密文/manifest/新旧权威/issuer/代次/撤销高水位；新主密钥、会话/旧审批/旧证书/在途任务/cleanup失效采用owner端口并进入同DB事务。仍不自动释放hold、不发起外部fence、不据此称B06通过。
- 发布回执新增受限强类型结构，支持至多8网关和逆序补偿；ResultSummary只允许已知有界结构保留完整摘要，其他任意文本/JSON仍脱敏。路由探针只消费已批准完成任务产生的私有上下文，并验证实际节点地址/当前WSS连接/Host/TLS/唯一响应token/HTTP状态/字节摘要；新ProbeCAFile是可信启动公开根，非HTTP字段。

集中验收需检查旧节点DTO/新Controller DTO、一般HTTP/AI拒绝本地目标、当前撤权与取消/重启UNKNOWN、真实age成功/错误身份/MAC损坏/重复或越界归档/旧权威及撤销倒退、事务失败/部分文件不重放、新迁移旧checksum、真实CLI与Web。不能用旧CI替代这些证据。

后续编码补入固定白名单configuration/controller.json及实际启用的各启动配置/发行源公开CA文件，单包最多15项（包含DB），旧8项归档仍仅作隔离验证。配置为路径/公开信任/非秘密启动选项及已验证启动文档，不打包离线Root、Agent或网站私钥。未配置每日任务身份和无已确认异地副本在Web明确告警；自动备份不会自行选用管理员身份。尚未统一验证。
