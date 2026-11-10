# R25 核心运行时收口：单次策略快照、启动窗口与持续审批

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，2026-11-10，从c7520ba继续。R20—R24请求/响应、Proto、数据库迁移和权限范围保持；没有新公开接口/字段或生产UI功能。此文同步登记已承诺规则的实际修复，不改历史候选。

## 命令执行

既有 /api/v1/commands/tasks 的程序仍由节点私有profile指定。Agent解析参数、核对启动policy SHA和参数/超时边界后，只执行同一次读取获得的profile；移除“校验后再次忽略错误/摘要读取配置”的窗口。程序本身仍按同一打开FD核验字节后执行，进程组取消和独立mTLS live admission保持；网页不能选择执行路径、环境或秘密。

## 更新和恢复

独立parent在启动owned child之前冻结已有mailbox请求集合。readiness期间新产生的批准请求进入正常处理，不能被误记为重启旧请求而永远跳过。发现mailbox/update-active.json，即使尚无slot update-lock，也保留UNKNOWN并拒绝再次启动child。请求只接受一个完整严格JSON文档，尾随内容在installer副作用之前拒绝。已有请求仍不重放，更新/健康回滚后的高水位不会降低。

Controller parent在child停机期间沿用统一任务审批的task/revision/plan、独立审批人和默认30分钟期限；零/未来时间、自批、旧版本、过期均拒绝。admit还必须实际running/dispatched；只有当前parent内存中已接收的同一intent可在Controller重启后观察原UNKNOWN历史。每次仍复核当前管理员、运行门、schema和权威绑定。共享Approval.CurrentFor同时供scheduler/parent使用，不增加完全访问例外。

## 验证边界

新增跨OS进程的Agent确认前缀接续、旧执行失效/并发一次adoption/损坏/未知发布拒绝；普通pinned命令的实际副作用、配置/程序变化拒绝和mTLS撤权停止；实际签名子程序的启动窗口更新、Agent/Controller supervisor模式替换/重启、不健康回滚/高水位、停机后撤权UNKNOWN、未决重启拒绝、真实stop/wait及fenced parent重启。

Supervisor子程序使用私有测试就绪协议，不等于真实Agent/Controller产品完整更新链；跨进程文件端口也不等于完整Controller/WSS新恢复组合。直接持有trusted fence control的测试只证明实际进程隔离及不复活，不冒充对外mTLS/离线Root授权或物理双机。真实提权、完整产品/浏览器组合、物理双机/外部/ARM实跑/硬1C1G继续按各自证据。

当前用户命名空间的 / 归属UID1000，Root路径负向回归按真实文件所有权判断，生产仍严格要求Root所有权；不添加skip掩盖失败。集中检查原失败与定向复验保留在本轮验收记录。
