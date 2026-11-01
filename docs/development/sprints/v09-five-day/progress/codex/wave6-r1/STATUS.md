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
