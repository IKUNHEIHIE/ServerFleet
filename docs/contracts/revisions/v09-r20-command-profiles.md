# R20 受控普通命令与提权执行

V09-CLOUD-CODE-COMPLETE-01 / chatgpt-takeover-01；五项核心优先包进行中，**UNTESTED**。原 Proto/gen、迁移 SQL/hash、根锁/CI 不改，没有新迁移（仍预计 38 项）。旧 tasks.command 空对象自检保持；新增 tasks.exec.v1 与显式 host.root 程序注册，不把结构 allowlist 当授权。

普通 Agent 用 -command-profiles 私有规范 JSON：version=1/nodeId/profiles；每个 profile 只有 id/executable/sha256/directory/prefixArguments/maximumArguments/maximumSeconds。实际程序 FD 哈希、普通 UID、私有 cwd、固定最小环境、argv 分隔、参数和 1—300 秒边界、独立进程组、已持久 reservation 与终态；普通 profile 不允许 setuid/setgid 程序。agent command-policy -file/-node 只读取并输出公开策略 SHA/ID，不执行。当前 profile 不收集任意程序 stdout/stderr，避免秘密进入日志；任务结果是已观察退出状态，不是服务健康。

Root 仅在显式 -root-helper 启动注册时启用。Agent 私有 registration 指定 nodeId、sudoExecutable/sudoSha256、helperExecutable/helperSha256、rootConfiguration、policySha256。sudo/helper 文件及所有祖先须 root 拥有且不可 group/world 写；普通 Agent 不获得 root 私有配置或密码。固定调用只有 sudo -n -- <helper> root-executor -config <rootConfiguration>，stdin 为冻结原 TaskOffer；不写 sudoers、不自动授权，不接受浏览器提供任意 helper 路径。

Root helper 只有实际 euid0 可运行，配置/程序策略/独立 ledger 为 root 私有：version/nodeId/profiles/ledger/controllerOrigin/rootCertificate/rootPin/clientCertificate/clientKey。root-owned 已注册节点 mTLS 身份和固定 HTTPS，无重定向/代理/浏览器头；Controller 每次从当前证书注册映射复核精确 running task、节点、参数、executionID、有效期及真人管理员当前审批。执行前、执行中每秒及完成前复核；权限或网络不明即取消进程组，结果保持 UNKNOWN，不声称撤销已经发生的副作用。root ledger 的同 execution 不重跑。其他 capability 默认 NORMAL，不能因开启 root 而一并提权。

普通命令亦独立向 Controller 做当前 mTLS 准入与持续复核；任务采用修改类人工审批，所有执行仍来自统一授权/任务/审批/审计。新增 Web POST /api/v1/commands/tasks，7 个固定字符串字段：nodeId/privilege/profileId/policySha256/arguments/timeoutSeconds/idempotencyKey；任务页有正式表单和审批回执。Agent current/start 准入两个专用路径无 browser cookie/Origin/Authorization，根权限路径必须管理员。此增量只开放 Web 的显式 profile；AI/plugin 不通过此 schema 偷渡资源或任意 root，既有 AI root 真人审批要求保持，相关 scope 扩展不能默认放行。

Context7 专用工具当前未提供；sudo 上游文档站抓取被 robots 拒绝，已核对 Debian 官方 trixie sudo(8) https://manpages.debian.org/trixie/sudo/sudo.8.en.html 的 -n/-- 语义。sudo 是操作员显式预置并以真实 SHA 固定的系统工具，本次未运行 sudo、修改系统权限或读取真实凭据。部署必须由管理员配置仅此 helper 的精确授权，不得给 Agent 普遍 NOPASSWD Shell。缺少此实际环境保留 BLOCKED。

尚待统一验收：普通/Root 实际进程、字节与退出码，旧自检兼容、未批准/非管理员/错节点/错 schema/hash/超时/取消/撤权/断网、Root ledger 重启与输出隔离、root 文件祖先可写/符号链接/程序替换拒绝。其他四项核心代码继续实现，五项不可因本检查点标 DONE。
