# CX-02 共享接收差异

## 文件会话

`controller-files.patch` 针对379d5e0新增Config.AgentFileGate和Server.AgentFiles，在enrollment中显式选择minor1/AllowFileFrames；桥接Event将File回复交FileClient，TaskAck仍交原任务审计，禁止文件任务TaskOutput。默认nil继续minor0/无文件授权。Proto/gen本轮不改。

FileFrameGate必须读**当前**唯一任务/审批/执行绑定：task.PlanHash、完整Params、目标node及稳定executionID、无CancelRequested/终态、当前identity/source/scope/许可及已消费审批；不能只核TaskID存在或缓存第一次授权。每次request和reply都调用。客户端在持久ack之前不发open，Agent只有executor持久reservation之后登记会话。TaskOffer重投的previouslySeen拒绝打开，不自动重写有副作用的UNKNOWN。

DS对接：`NewSession(ctx,node,offer)` → `Open(ctx)` → `Chunk(ctx,generated FileChunkRequest)` → Close。Offer.ActionID=`files.transfer.v1`，schema1，normal，无plugin。调用只允许统一任务worker；当前通用dispatcher直接发offer与FileClient.Open不可重复发。总控/DS须让该动作走同一个file worker，不建新任务中心。

FileParameters含transferId/relativePath/direction/size/sha256/conflictPolicy/createParents，**任务JSON七个值均为string**（当前tasks存储端口要求），size严格十进制、createParents严格false。可直接json.Marshal(transport.FileParameters)；未知/重复/大小写别名/不支持策略拒绝。路径由任务冻结的显式映射产生，不能来自分块请求。read允许size=0/sha256空，只允许批准路径stat；Open观察并冻结私有快照size/SHA，再读字节。已知读摘要则必须一致。绝不自动把节点绝对路径改相对路径。

只实现skip（新文件写入、已有常规目的则skipped）；rename/overwrite/createParents/resume_offset非0全部拒绝。32KiB chunk/64MiB总量/1在途请求。写完成回执丢失：目标可能已存在，ledgerUNKNOWN，重新连接同执行拒绝，须显式对账；不是重新执行/覆盖。源空文件在Open终结；目标空文件需要finish空chunk。

transport语义改动处于CX允许目录、仅显式AllowFileFrames+minor>=1+capability才接受；应用共享patch并启用前须审查完整字段/兼容和逐帧门，默认拒绝回归保留。运行时/PRIV/G2未冻结，其他root/Shell保持关闭。

## AI预算/审计/本地TLS验证

controller-ai.patch新增Config.LocalAIProvider（只允许显式loopback TLS协议夹具）与AITypedAudit；AppendModelSettingsAudit和通用Audit.AppendTx共用当前Session DS Tx。默认外部provider不变，默认typed sink未启用时仍保留outbox。迁移后总控才启用AITypedAudit；不能先启用后忽略缺表。

controller-ai-schema.patch新增迁移构造器，version由总控分配新不可变编号；SchemaStatements包含budget聚合、typed预算审计与model审计三表，作者未改共享DDL/历史迁移。预算绑定唯一OperationID与全请求摘要，只存元数据；单次输出预留8192，正文输入按字节计，明确不是token/费用估算。不得删除历史operation fence来“清理配额”。窗口变更由管理员显式CAS，旧窗口计数和fence保留，时钟回退失败关闭。

组合方式：用同一storage.DB的BEGIN IMMEDIATE实现GenerationBudgetSQLBackend.InTx，绑定同一tx.Read/Exec及AppendGenerationBudgetAudit；构造PersistentGenerationBudget（当前管理员gate，当前Conversation.CheckGenerationBudget），注入Config.AIGenerationQuota=budget.Admit。配置策略必须校验真人当前管理员/owner/profile以及有效窗口，不能由AI输出或普通请求自报；missing policy拒绝。该组合构造在tests/integration/v09/platform/generation_budget_sql_test.go与带标签主控测试中完整演示。预算事务不嵌套Session写事务；私有观察/当前scope/profile在预算前后与秘密借用独立检查，授权失败不能继续供应商I/O。

重现共享候选测试：先`node tests/poc/v09/platform/prepare-wave6-r1-overlays.mjs`，再`go test -tags cx_r1_controller -overlay reports/wave6-r1/combined-overlay.json ./tests/integration/v09/platform -run TestR1ControllerHTTPBorrowedBYOKLocalTLSBudgetAndTypedAudit`。准备器仅写忽略reports的.txt，不改共享源码；同时组合files和AI补丁。WSL入口`node tests/poc/v09/platform/run-wave6-r1-linux.mjs controller`。测试没有真实外部账号，仍不等于主线已接收/浏览器逐token/发布准入。

Configure的管理员gate在事务前、事务内及提交后均运行，必须只读且与同一coordinator DB串行写权威一致。已认证操作入口先验当前Cookie/Origin/CSRF/管理员/owner/profile；事务内可用IdentityService.AuthorizeAdministrator核当前用户仍有效且为管理员，不能在该门中调用会开启另一writer的Authenticate。此只读门不接受浏览器自报Subject，不验证会话，也不批准资源/root动作。当前仅程序注册预算，无公开管理预算路由。

## 更新/恢复组合接缝

Linux程序注册`updates.NewOwnedProcessSupervisor(OwnedSupervisorConfig)`：Directory需专用0700绝对目录，active需常规0700已批准ELF；ExecutionID来自唯一任务冻结输入；Arguments/Environment来自批准schema白名单，不能来自Shell文本。Gate每次重新查统一任务/执行/审批/取消/工件；Ready以实际PID、StartedAt、随机Nonce验证产品版本/服务健康/存储兼容，返回ErrHealthUnhealthy才允许确定性回滚，其他错误保持UNKNOWN。将Stop/Start/Health绑定既有InstallHooks；RollbackSafe和Commit仍由总控/DS提供当前存储兼容及同事务审计，不能用历史receipt充当实时健康。

Supervisor只持有自己Cmd.Start得到的进程句柄；不附着systemd/任意PID，不创建系统服务、不杀外部进程。reservation先于Start持久化，未确认退出不会删除；进程/主控崩溃后的未解标记须独立对账，禁止自动重启/重放。该实现没有cgroup/后代进程治理/完整沙箱，不能供任意二进制或root任务使用；真实产品UPDATE准入仍未完成。本轮未改共享入口，既有TaskInstallExecutor/InstallHooks即可注入，具体实际进程组合见supervisor_linux_test.go。

RestoreIsolated的Authority必须提供严格高于备份的当前trust generation及最新revocations；新检查不替代离线恢复权威、旧主控fence、新密钥生成与所有入口quarantine.Check。Commit在新隔离SQLite里原子清session/approval、对账UNKNOWN、合并revocations和审计；即便Commit成功只返回restored_quarantined。旧主控未被外部可靠fence时不释放、不启动调度；未有共享DDL/入口冻结，作者不自行实现生产release。
