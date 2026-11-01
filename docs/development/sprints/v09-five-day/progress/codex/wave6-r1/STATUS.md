# V09-R6-CX-02 / codex-06

审查 codex-review-02；基线379d5e0e4818844eb44d8ac13010f251d215d8c0；沿用独立分支/目录和CX允许范围。不覆盖主控已接收的密钥/Windows/撤销修复，无其他Agent、实机、云、main合并或消息操作。

## 检查点1：真实 WSS 文件会话

使用已生成 oneof33—36。transport 新增显式 opt-in 文件语义校验、FileClient/FileSession；默认仍拒绝。连接身份/协商 minor>=1/capability、每帧当前 FileFrameGate、持久 TaskAck、完整 offer 和 task/execution/transfer 关联、窗口1、32KiB分块/64MiB文件上限。旧连接或 previouslySeen 执行不能重开文件。

Agent `-file-root` 显式 Linux 非root私有根；与 `-serve`/`-connect-once`组合，接入真实 session/执行ledger/TaskCancel/TaskFinished。源快照stat/SHA先冻结，实际字节通过 File*，不进入TaskOutput；write完整SHA后原子拒绝覆盖发布。skip实现；rename/overwrite/createParents明确拒绝，不假装支持。task参数采用当前唯一任务中心的map[string]string，size/boolean严格十进制/true|false，重复键/别名/未知键拒绝。绝对路径必须由总控/DS做显式映射，Agent不剥斜杠。

Linux逐组件NOFOLLOW+目录fd锚定；1000次父目录/符号链接并发换位未读到兄弟路径；写后校验发布对象身份与SHA。完成回执丢失时确有目标字节、执行ledger记录UNKNOWN；断开并重新建立真实WSS后，同执行重开拒绝，未重复写。

验证：Windows agent/runtime/transport PASS（Linux项SKIP）；本机非root WSL agent 41顶层 PASS/0 SKIP，包括两个实际mTLS+WSS Agent会话传递84KB左右字节、源路径内容更换后仍传冻结快照、取消终态、当前授权撤销、重复执行、完成回执丢失/重连/UNKNOWN和符号链接竞态。授权回调是明确合成fixture，**不等于主控HTTP/DS真实任务中心组合完成**。

首次WSS负向夹具FAIL：收到取消分块回执后立即发下一任务，尚未收到TaskFinished而被正确AGENT_BUSY拒绝；修复测试等待取消终态，并回收已finished worker计数，不弱化任务门控。

controller-files.patch 是针对新基线的最小组合差异（server/enrollment/agent_links），不修改共享目录。显式Config.AgentFileGate存在才允许minor1/文件协商；总控必须实现当前任务/执行/计划/源读目标写分别授权/审批/取消/AI scope门，不能返回nil当默认放行。新增Server.AgentFiles由唯一任务worker使用，不挂Web直调；仍需DS路径映射、每项稳定ID、调度/终态适配。适用性与Go overlay编译另记录于HANDOFF。

剩余文件项：真实ENOSPC/容量受限目录、两端主控+DS+任务中心组合及实机、其他冲突策略；partial写/unknown不自动恢复。CLI opt-in配置不能代替运行时准入。继续AI持久配额/typed审计、平台独立项。

检查点1 SHA：d58020c67d9ac798ec502853015e2f392c62cd49；controller-files.patch适用检查与Go overlay编译PASS。

## 检查点2：持久生成预算、typed审计及主控HTTP供应商闭环

新增独立SQLGenerationBudgetStore和PersistentGenerationBudget：显式owner/profile/window/CAS配置，不自动滚动窗口；唯一OperationID、不可变请求摘要、请求数/输入字节/输出token上界、clock floor、防重放与typed审计同一DS事务。最大输出token与实际HTTP请求8192一致，是保守预留，不是实测token/货币费用；UNKNOWN不退还、已见operation不重复消费。只接私有generating观察及当前scope/profile gate，不接受模型自报权限；所有秘密和正文不入预算/审计。

AppendModelSettingsAudit保留SubjectID/ProfileID/revision/action/time；接SessionSQLStatements.AppendModelAudit，同一事务更新aggregate与通用audit，失败不推进配置revision。SchemaStatements不自动执行、不编号，controller-ai-schema.patch交总控分配新不可变迁移号。独立表防止旧session writer丢弃新预算字段/记录。

新增LocalTLSProviderConfig及显式构造器：只允许loopback HTTPS +预注册根、二次解析只准loopback，拒绝URL凭据/路径/查询/外域/重定向、无环境密钥/重试；主线固定外部供应商默认不变。controller-ai.patch只增加候选注入和AITypedAudit（迁移完成后显式启用，否则保留outbox，不丢事件）。

验证：真实SQLite 12并发只放行1、审计插入后失败整笔回滚、重启不重置、实际提交确认丢失后重启拒绝重试PASS；本地TLS正常/取消/撤权/重定向PASS。带cx_r1_controller标签且通过候选Go overlay的真实Controller HTTPS链路：登录→写合成BYOK→模型owner CAS→queued消息→当前秘密借用→供应商真实HTTP/SSE→完整assistant；第二次调用被持久额度拒绝，供应商调用数仍1；typed ProfileID/revision精确入库、触发器强制审计失败回滚配置revision，PASS。WSL初验AI34、预算1、主控闭环1 PASS/0 SKIP；最终固定提交复验另列。

首次负向fixture取消测试未消费request body，供应商等待Context取消导致测试清理hang；限时诊断FAIL保留，修夹具读body并限定等待后PASS，仅停止本人精确匹配的旧测试进程。另两次主控夹具失败分别是误把audit action当role permission、错把已有message POST的200当失败；改为真实当前管理员会话验证和真实现有HTTP状态，未降低任何产品安全门。

后续继续平台独立适配/资源负向。真实外部模型、浏览器逐token和主线DDL/接线仍待接收。
