# CX-02 共享接收差异

## 文件会话

`controller-files.patch` 针对379d5e0新增Config.AgentFileGate和Server.AgentFiles，在enrollment中显式选择minor1/AllowFileFrames；桥接Event将File回复交FileClient，TaskAck仍交原任务审计，禁止文件任务TaskOutput。默认nil继续minor0/无文件授权。Proto/gen本轮不改。

FileFrameGate必须读**当前**唯一任务/审批/执行绑定：task.PlanHash、完整Params、目标node及稳定executionID、无CancelRequested/终态、当前identity/source/scope/许可及已消费审批；不能只核TaskID存在或缓存第一次授权。每次request和reply都调用。客户端在持久ack之前不发open，Agent只有executor持久reservation之后登记会话。TaskOffer重投的previouslySeen拒绝打开，不自动重写有副作用的UNKNOWN。

DS对接：`NewSession(ctx,node,offer)` → `Open(ctx)` → `Chunk(ctx,generated FileChunkRequest)` → Close。Offer.ActionID=`files.transfer.v1`，schema1，normal，无plugin。调用只允许统一任务worker；当前通用dispatcher直接发offer与FileClient.Open不可重复发。总控/DS须让该动作走同一个file worker，不建新任务中心。

FileParameters含transferId/relativePath/direction/size/sha256/conflictPolicy/createParents，**任务JSON七个值均为string**（当前tasks存储端口要求），size严格十进制、createParents严格false。可直接json.Marshal(transport.FileParameters)；未知/重复/大小写别名/不支持策略拒绝。路径由任务冻结的显式映射产生，不能来自分块请求。read允许size=0/sha256空，只允许批准路径stat；Open观察并冻结私有快照size/SHA，再读字节。已知读摘要则必须一致。绝不自动把节点绝对路径改相对路径。

只实现skip（新文件写入、已有常规目的则skipped）；rename/overwrite/createParents/resume_offset非0全部拒绝。32KiB chunk/64MiB总量/1在途请求。写完成回执丢失：目标可能已存在，ledgerUNKNOWN，重新连接同执行拒绝，须显式对账；不是重新执行/覆盖。源空文件在Open终结；目标空文件需要finish空chunk。

transport语义改动处于CX允许目录、仅显式AllowFileFrames+minor>=1+capability才接受；应用共享patch并启用前须审查完整字段/兼容和逐帧门，默认拒绝回归保留。运行时/PRIV/G2未冻结，其他root/Shell保持关闭。
