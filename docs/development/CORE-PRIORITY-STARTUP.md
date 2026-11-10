# 五项核心功能：部署接线与验证边界

2026-11-10 R25补充：parent在启动child前冻结已有请求，readiness期间新请求正常处理；遗留mailbox/update-active.json与slot update-lock同样阻止重启，不自行删除。Controller停机交接沿用统一任务的独立审批/版本/计划和默认30分钟期限；普通命令只用同一次已校验profile快照。细则见 `../contracts/revisions/v09-r25-core-lifecycle.md`，启动字段和Root权限保持。

2026-11-09；V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01。本页说明集中编码分支的真实入口，验收记录另行补入。**未运行验证不能当可部署证明；未操作生产、VPS 或真实外部账号。** 固定 Go1.27.3、Node24.22.1、npm11；普通 Linux UID，私有目录0700/文件0600，无符号链接/硬链接，不从 Windows 搬运行材料。实际签名和部署由操作员预置，开发命令不生成生产签名。

## 受控普通命令 / Root

Agent serve/connect 显式 -command-profiles 指向节点私有规范 JSON；字段 version=1、nodeId、profiles，每项 id/executable/sha256/directory/prefixArguments/maximumArguments/maximumSeconds。使用 `agent command-policy -file /absolute/private/profiles.json -node node-id` 只读取公开策略 SHA 与 ID。程序按实际 FD 验证，不自动把参数作为 shell 执行。正式任务页登记独立 argv、超时、策略摘要与 ordinary/root 类别；普通执行也需人工批准、当前节点/命令授权和 live admission。

Root 另需 -root-helper 的受控注册和真正 root-owned 助手/程序及所有不可写祖先；配置格式/固定 sudo -n -- 调用见 R20 候选。系统管理员须为确切 helper 手动配置仅必要 sudoers 授权，不能给 Agent 任意免密 Shell；开发流程不修改 sudoers、不运行 sudo。Root helper 的独立 mTLS 客户端证书与 ledger 保存在 root 私有目录，Agent 不读取这些私钥。未批准、撤权、网络不明会拒绝/中止并保留未知副作用；程序输出不进入任务日志，终态只代表观察到的退出状态。

Root 策略、配置、证书/私钥、ledger、执行程序和工作目录逐层拒绝符号链接、非 root 所有或组/其他用户可写的祖先；仅把最内层目录 chmod 0700 不够，不能放在 /tmp 或普通用户可替换的父目录下。普通用户 profile 仍按原当前 UID 私有配置规则读取。

## 节点文件恢复

-file-root（Agent）和 -file-deployment（Controller）的原可信映射保持。正式文件页“节点传输恢复”读取已结束原任务的确认偏移，创建新的 files.resume 人工审批任务。Controller 私有 resume-cache 保存已核验完整源，最多128份/合计512MiB/每文件64MiB；不足时拒绝而不淘汰未知结果。Agent 只采用已 fsync checkpoint 的实际前缀，一次 adoption 后复制到新执行；最终 publish intent/完成标记存在则拒绝。进程、网络或用户上传到 Controller 的原续传会话不是同一层恢复，不混称已有完整执行证据。旧版本没有 checkpoint 的片段不能凭猜测恢复。

## 独立监督进程 / 签名更新

新命令 `controller supervise -config /absolute/private/supervisor.json` 在 Controller 或 Agent 主机作为普通用户启动**自己的**一个子进程。`-describe` 仅输出 component/nodeId/policySha256，不启动程序。监督程序可复用 Controller 二进制的 CLI 子命令；Agent 本身不导入 updates/DB。不能把已运行 systemd/PID 接管成自己拥有的进程。

监督配置的实际字段见 internal/updates/resident_supervisor.go：format=1，component=controller/agent，nodeId、directory、mailbox、inbox、stageDir、healthFile（各自独立的私有目录）、arguments/environment（固定 argv/env）、initial（完整规范 ReleaseManifest）、policy（Component/Architecture/MinimumSequence/MaxBytes/TrustedKeys）。active 实际文件为0700且必须匹配 initial 字节；TrustedKeys 是离线信任的 Ed25519 公钥映射，不是在线签名私钥。rootCertificate/rootCertificateSha256 冻结健康 HTTPS CA；Agent 还需 identityDir/controllerOrigin/controllerAddresses/rootPin，Controller 还需 database/schemaSha256（真实 sqlite_master 指纹）。不通过网页传可执行路径、健康回调或信任键。

可选 feed 是固定 origin/addresses 的 HTTPS 来源，另设 feedRootCertificate/feedRootSha256；路径为 /releases/{component}/linux/{architecture}/{version}/manifest.json、signature.bin 和 artifact。也可由操作员预置已签名 inbox。每次从同一冻结任务复核当前审批/权限，签名和实际字节必须相符，序列高水位在 DB 外持久保存；目录部分写入/更新锁不自动重放。构建脚本现在为两个组件都输出 `{component}.manifest.json` 待离线签名，仍明确 UNSIGNED/UNVERIFIED，不会灌入 inbox 或安装。

Agent 的 serve/connect 配置 -update-mailbox /absolute/private/mailbox.json：format=1/nodeId/directory/policySha256；directory 正是监督配置 mailbox，摘要来自上述 describe。保留节点真实 identityDir；普通 WSS typed task 将确切 intent 写私有 mailbox。正式更新页按同平台/同策略分批登记，人工管理员批准，先1台再5台，非成功不扩批。Agent 重启后的普通任务结束消息不证明安装；独立监督程序观察同节点重新连接/PID/nonce/版本/字节后向 Controller 提交。UNKNOWN 不自动换任务重放。

Controller 子进程 arguments 包含原管理 HTTPS/PKI/DB 选项和 `-resident-supervisor /absolute/private/supervisor.json`，配置必须与 parent 完全一致。替换期间业务和任务受 update-lock 维护门拒绝；parent 独有 nonce 的内部只读 readiness 与 private health file 用于核对实际程序/版本/schema。先真实 SQLite 一致快照，schema 漂移不允许自动回滚。旧 Controller 内自带本地 Agent slot 的 update-deployment 与此新模式互斥；选择新模式时让 Agent 使用各自独立 supervisor，避免两套父进程争同一进程。旧 local slot 模式仍可独立使用。Controller 重启中断的原任务仍 UNKNOWN；更新页读取独立真实安装回执，不改写原历史。

## 恢复 / 真实隔离 / 新权威

原离线 stage/reconcile 入口保持，不自动释放 hold。配置独立 parent fence listener 后，才具备本模型的实际进程隔离：supervisor 指定 fenceConfiguration 和其完整文件 SHA256；配置 version=1/listen（确切IP:port）/certificate/key/clientRoot/clientCertificateSha256/rootCertificate/rootPin/authorizationDirectory/minimumTrustGeneration。listener 与 Controller child 独立，使用专用 mTLS peer/服务端证书及 Root pin；不复用浏览器 cookie、Agent普通入网或旧 Controller 声明。

新主控私有 runtime JSON：version=1/directory（原隔离目录）/authorization（现行已离线签名公开授权文件）/newPkiDirectory/rootPin/fence。fence 字段 origin/addresses/rootCertificate/rootSha256/serverCertificateSha256/certificate/key，固定独立 parent 的 CA/leaf/IP 与专用客户端身份；密钥由操作员分别预置，不经任务或聊天复制。原、新机器需要时钟同步（新 nonce 观察窗口为2秒）。新 supervisor 的 recoveryRuntime 指向此文件，database 必须确切 directory/restored.db、nodeId/rootPin 为新权威，新 Controller 的 -agent-pki-dir 是 newPkiDirectory，-credential-key-dir 是 directory/credentials。

显式 `controller recovery activate -runtime /absolute/private/runtime.json -resident-supervisor /absolute/private/new-supervisor.json -offline-ack`：验证 Root 授权仍有效及既有 reconciliation；实际请求独立 parent stop/wait 自己拥有的旧 Controller，完成不可逆 fence 后才事务记录新权威/审计，最后写 release。此命令不启动新主控，也不触碰陌生进程。随后用新 supervisor 正常启动新 Controller，整个运行时持续核对当前外部 fence、Root/issuer/代次/撤销和原 hold，证据/网络失败即停止业务。`controller recovery status -runtime ... -resident-supervisor ... -offline-ack` 实时复核但不激活。正式备份页只读观察当前状态，不接受恢复/Root私钥，也不解除隔离。

新主控不复活旧 Agent 证书、会话、审批、在途任务或未知 cleanup；在新私有 Agent 状态目录重新注册并人工核对节点/权限，不覆盖/删除旧状态。已发生的远端未知副作用不自动重放。旧 Controller 不属于独立 parent、无法确认其停止、有未知更新锁/进程 reservation、外部 fence 不可用时维持 BLOCKED。此模型是实际拥有进程的隔离，不能冒充物理双机、电源/网络 fence、ARM运行、硬1C1G或真实供应商认证。B03—B06/G2/发布仍需各自正式验收。


2026-11-10 R26修复：激活凭据activation-attempt.json/activation.json按固定名独占持久化；授权缓存文件名必须绑定实际规范JSON字节SHA256，最大128KiB（普通凭据16KiB）。独立parent先验签/核对权威与代次，再写最多32份的缓存；已有合法相同字节文档在满额时仍可观察。失败/重复激活不覆盖旧凭据，不自动重放；实时隔离端不可达仍维持隔离。
