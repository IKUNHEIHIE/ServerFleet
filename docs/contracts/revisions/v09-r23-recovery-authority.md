# R23：真实拥有的旧 Controller 隔离与恢复激活（候选）

任务 V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01。目前代码已写入，集中验收前仍 UNTESTED。追加 cx/0019_controller_supervision，预计正式迁移 39 项；原 SQL/hash、Proto/gen、锁和 CI 不改。

## 隔离与新信任

独立普通 UID 的 controller supervise 可显式配置 fenceConfiguration 及其完整文件 SHA。独立 TLS13 listener 只接受操作员预置的 mTLS 客户端证书链和确切 leaf SHA。POST /v1/fence 接收 nonce 与公开离线 Root 签名授权；不接收路径、PID、命令或私钥。独立 Root pin、有效期、新旧 authority/代次和当前 supervisor 节点绑定必须匹配。先 fsync fenced-intent.json，使旧 Controller 的每个运行门拒绝，再 SIGTERM/必要时 Kill 自己实际启动的唯一子进程并 Wait；无更新未知锁、无进程 reservation，才持久 fenced.json。不能接管陌生 PID、systemd、远端主机或凭文本签名声称已隔离。

POST /v1/fence/current 使用原绑定 request 与新的随机 nonce，重新读取持久状态和当前进程所有权；TLS peer/服务端 leaf 固定，响应回显 nonce/新观察时间。成功 fence 的 parent 重启只服务观察，绝不重启旧 child；未完成记录/进程未知保持 BLOCKED。父进程或网络不可用时，新 Controller 的持续运行门拒绝。主机 root/同 UID 私有文件写入不在此隔离模型内，物理双机、电源/网络隔离及不同设备信任仍需实际外部验收，不能从此代码关闭 B06。

恢复流程为 stage → reconcile → 显式 activate。Root 授权绑定密文、manifest、新 issuer、新 authority、新代次和撤销高水位；旧任务/会话/审批/证书/cleanup 不复活。activate 私有 runtime 配置及新 supervisor 必须指向确切 restored.db、新 PKI、原 hold。先实际 fence，再将新 authority 的 supervisor 绑定、activation receipt 与审计写在同一事务，最后写不可变 release。任何中断均不自动重放。多次恢复将旧 reconciliation 保存在追加历史中，不用旧激活回执获得新权威。

已激活后的每个运行门重新校验原 Root 授权在已审计激活时的签名/有效性、当前新 PKI、数据库 reconciliation/activation、外部新 nonce fence 观察、私有 hold/release。不是将 24 小时应急签名变成永久可重复激活的授权；初次 activate 必须当时有效。旧证书全部撤销，Agent 需要在新私有状态目录中用新审批重新注册，旧执行及旧权限不自动搬入。

## 控制台与更新生命周期

GET /api/v1/system/recovery-authority 仅当前真人管理员，可读常规/受监督/恢复后模式、权威、代次/撤销及重新注册要求；不接收恢复私钥或解除隔离。外层 RuntimeHTTP 在读取和响应时复核当前 fence。正式备份页展示本次观察。

Controller 自更新时，update-lock 使所有业务 HTTP/worker/dispatch 保持维护拒绝。只有 /internal/resident-ready（实际 parent 随机 nonce、TLS、基础 authority/fence 复核）和私有健康文件允许 parent 读取版本/schema/PID/启动 nonce；无执行权限。签名字节/实际进程/健康与审计提交并清锁后恢复业务。旧 Controller 内部拥有本地 Agent slot 的历史 update-deployment 是单独部署模式；Controller 自监督选择独立远程 Agent supervisor，不同时启动两套进程所有权。

Agent 普通 TaskFinished 不得抢先结束更新 mailbox task；只有独立 supervisor 的 mTLS commit 和新 WSS 连接/实际健康证据可记录逐节点终态。真实子进程已退出是明确 unhealthy；缺失/超时/未知不是失败回滚授权。核心构建为 Controller 与 Agent 都生成待离线签名的规范 manifest，不生成签名或安装。

验收需覆盖真实普通 UID 拥有进程、TLS/Root/nonce/错误 peer/未知锁、旧进程停止与重启阻断、新主控启动与撤权/断网持续拒绝、新节点信任、实际 update-lock 维护门和健康回滚。没有操作生产/VPS、真实 Root 或物理主机；缺环境保留 BLOCKED。
