# R26：修通离线接管凭据与真实mTLS隔离

2026-11-10；V09-CLOUD-CODE-COMPLETE-01 / chatgpt-resume-20261110，从70932d1继续。R20—R25公开路由、字段、Proto、40项迁移、UI权限和历史SQL保持；本轮没有新增公开API或页面。

## 修复

原activate/fence代码调用WriteOfflineRecord写activation-attempt.json、activation.json及authorization-{sha256}.json，但该函数只允许reconcile-attempt.json/reconciled.json，导致真实接管必定拒绝。现在显式准入两个固定激活凭据名，以及严格绑定实际JSON字节SHA256的授权文件名；仍只能在现有0700私有目录内以0600/O_EXCL新建，不覆盖hold、DB、旧凭据，不接受路径或任意文件名。普通凭据保持16KiB，授权文件最多128KiB。

独立parent先在内存校验离线Root签名、有效期、旧权威和最低信任代次，才落授权缓存。无效/篡改/错权威文档不占缓存。并发写入受互斥保护，新增文档总量最多32份；已存在且字节一致的已验签文档在满额时仍可重试观察。缓存不授予接管权，实际stop/wait与持久fence仍由独立parent完成。

## 实证范围

新增Linux普通用户真实子进程用例，将生产mTLS listener/client、离线Root签名、owned supervisor fence、恢复activate、SQLite权威/审计事务、磁盘凭据、实时quarantine gate和旧parent重启串联。错误专用客户端/服务端leaf pin、篡改签名、缓存满额新文档、不同新权威、重复激活、隔离端失联均应拒绝；已fenced的parent重启只能返回新nonce观察，不能复活旧child。

测试从显式构造的“已reconciled”文件/SQL开始，子进程使用私有就绪协议。它不证明age备份提取/reconcile的完整组合、新Controller正式二进制业务启动、物理双机/电源/网络隔离。Root helper、文件WSS恢复、完整多节点更新和正式Controller自更新等产品组合继续按独立证据验收；五项不据此整体结项。
