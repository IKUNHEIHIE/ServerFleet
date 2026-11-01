# Wave6 · CX/DS连续实施包与GM待交增量

总控codex-review-02；2026-11-01。先读[共享接收说明](WAVE6-SHARED.md)及[Wave6](WAVE6.md)。精确基线为本轮最终main交接SHA；原三作者目录不重建、不强制覆盖。CX/DS只在确认干净且原HEAD分别4a3e31e/fd2e015时快进到交接SHA；GM继续当前未提交任务，总控不移动它的分支。

## V09-R6-CX-02 / codex-06

允许目录沿用CX整包，进度另建`progress/codex/wave6-r1`；审查人codex-review-02+安全/通信。原工作目录`.worktrees/feature--v09-r6-platform-cx06`、分支`feature/v09-r6-platform-cx06`。

1. **真实文件会话/常驻联动**：直接使用已生成的FileOpen/Chunk类型（33—36），实现主动WSS文件会话的严格有界路由、逐帧身份/任务/执行/方向/摘要校验；只在当前授权、minor>=1、files.transfer.v1实际协商、已持久接受TaskOffer后开放。明确给总控可应用的`agent_links.go`/组合及语义Validator差异，不能由跨模块import绕过。
2. **Agent真实文件读写**：接CLI/会话到现有私有文件组件；与DS统一ID格式、stat/SHA冻结、绝对到相对路径的显式映射、skip/rename/overwrite、父目录与cancel。未实现策略明确拒绝；符号链接竞态、低磁盘、完成回执丢失、重连/重启/UNKNOWN不重跑须有真实Linux目录测试。保留正常常驻/取消/去重；任意Shell/root继续关闭，不能把仅固定自检称通用执行。
3. **AI可独立项继续**：实现持久且绑定唯一OperationID的生成配额/预算端口、模型typed审计写入端口与同事务需求（原事件不得丢ProfileID/revision）。给总控最小DDL/迁移/组合补丁；实现本地TLS供应商可注入测试配置，证明主控HTTP→当前秘密借用→实际HTTP/SSE→完整消息，并保留撤权/配额/超时/取消/未知结果不重试。外部模型与浏览器逐token分开计证据。
4. **其余平台独立项**：更新真实supervisor/健康/备份和恢复fence/新epoch/撤销不复活继续给可审查适配，root统一任务worker/共享DDL只交patch。Xray继续独立PoC、安全工件筛选和可选包边界；不得把已拒绝工件或未批准Runtime打开换取成功。

本轮主控已经替换固定密钥、补Windows权限与撤销后重启、接通配置/模型候选路由，不再提交旧版整文件覆盖这些修复。密钥/凭据真实内容不得进入Git/报告。

## V09-R6-DS-02 / deepseek-06

允许目录沿用DS整包，进度另建`progress/deepseek/wave6-r1`；审查人codex-review-02+安全/数据。原工作目录`.worktrees/feature--v09-r6-services-ds06`、分支`feature/v09-r6-services-ds06`。

1. **真正接通CX文件接口**：使用生成候选，不另建oneof40—47；每项transferID去掉与CX不兼容的冒号，executionID统一128字节/ASCII白名单。实现显式路径映射端口（未配置拒绝）、逐帧版本/TaskID/ExecutionID/TransferID/方向校验、task-admission/执行ID稳定绑定，提供总控调度/组合补丁。源/目标读写分别授权，不能直接从Web调用协调器。
2. **中断/资源/并发**：补源下载流的即时spool上限，安全私有目录检查，执行原子占用/重复调用、逐项进度持久化、取消和restart的保守状态恢复。新增首项撤权测试和主控修复保留；分块期间及读完写前撤权也必须停止/结果不报成功。未知目标写不自动重做；把任务RecordResult接入真实tasks状态，不只fake reporter。
3. **CA/供应商安全完整性**：在本轮公钥/域名/有效期修复基础上，验证完整证书链与明确信任根，拒绝不符订单的authorization域名/type/wildcard、非法跨origin端点和异常证书链。清理失败不能吞掉，DNS-01记录ID与失败清理给持久接缝；QQ token端点仅成功状态可用、SMTP不同提交阶段的unknown/rejected边界补测。真实账号仍未授权，使用可验证本地TLS协议端收证。
4. **发布真实目标/补偿**：给plan持久保存gateway/zone/certificate目标及revision的最小模型/DDL patch，复核每个阶段/逆序补偿前的当前授权、撤权、并发与幂等。Nginx语法/实际隔离代理已由总控补验，但Agent apply/reload/restore仍未实现；与CX给受控动作schema及artifact摘要/审批方案，主控接入前不得报生产发布成功。
5. **秘密适配接缝**：Cloudflare/SMTP/QQ的Resolver不能成为通用返回明文接口；与CX按当前owner/引用revision/操作/范围绑定秘密借用，实际供应商I/O应包在借用回调内做前后检查。给总控具体注入差异，其他无依赖项继续。

## GM与总控

- `V09-R6-GM / gemini-06`继续原任务；用户确认旧639eff2是中断后误交的Wave5提交。新SHA未交前不接收、不变更其目录。后续审查需注意默认文件传输已停止本机fallback，原70步里的本机上传成功属于显式fixture，不再算远端成功。
- 总控负责apps/controller、公共API/Proto/gen、DDL编号/根锁/CI及awskr；作者不自行合并main、不操作awskr、不碰其他作者未提交内容。具体共享patch随可运行增量交SHA，总控接收后发布统一SHA。
- 以上是连续实施包，首个优先检查点为真实FileOpen→chunk→commit和任务终态。每2—4小时或每个可运行增量交SHA后继续其他独立项；共享依赖只暂停对应子项，完成全部独立项/确无可推进工作时才停止。需要外部账号/高权限/生产准入时说明具体缺口，不能默认为获得授权。
- 停止时交命令/输出/精确SHA、正常与负向测试、候选差异及FAIL/BLOCKED/UNTESTED。目标仍是完整v0.9；本轮模块/隔离实机通过不关闭G2、24项全量验收或11月3日交付风险。
